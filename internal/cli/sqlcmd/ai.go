package sqlcmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"dbimpex/internal/engine"
	"dbimpex/internal/llm"
	"dbimpex/internal/service"

	"github.com/atotto/clipboard"
	"github.com/cloudwego/eino/schema"
	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cylog"
)

// aiState CLI 会话内的 AI 状态（懒加载，切换数据库时整体重置）。
// 对话历史仅存内存，不落盘；生成结果经缓冲区（lastSQL）与执行历史留痕。
type aiState struct {
	svc    aiService
	client chatClient
	msgs   []*schema.Message
	usage  llm.Usage
	schema string
}

// chatClient 抽象大模型对话能力，便于单测注入 mock（*llm.Client 天然实现该接口）。
type chatClient interface {
	Chat(ctx context.Context, msgs []*schema.Message) (string, llm.Usage, error)
}

// aiService 抽象 aiState 用到的 service 能力（*service.Service 天然实现），便于单测注入 mock。
type aiService interface {
	Config() *service.AppConfig
	AIEnabled() bool
	AIStatus() service.AIStatus
}

// 以下包级函数变量用于依赖注入：默认指向真实实现，测试中可替换为 mock。
// 采用函数变量而非接口字段，避免改动 aiState 结构与所有调用点签名。
var (
	newAIService = service.NewServiceWith
	newLLMClient = llm.NewClient
	getTableTree = engine.GetTableTree
	getTableMeta = engine.GetTableMeta
)

// aiState 懒加载：获取配置并初始化客户端。
func (s *session) aiGet() (*aiState, error) {
	if s.ai != nil {
		return s.ai, nil
	}
	svc, err := newAIService("", "")
	if err != nil {
		return nil, fmt.Errorf("初始化服务失败: %w", err)
	}
	if !svc.AIEnabled() {
		return nil, fmt.Errorf("AI 功能未配置：请先在 config.yaml 或 Web 设置中填写 ai.base_url / ai.api_key / ai.model")
	}
	// 全局 debug：config 顶层 debug=true（或外层 --debug）时切到 debug 级别
	if svc.Config().Debug {
		cylog.InitDefault(cylog.WithLevelStr("debug"))
	}
	cfg := svc.Config().AI
	client, err := newLLMClient(context.Background(), llm.Config{
		BaseURL:     cfg.BaseURL,
		APIKey:      cfg.APIKey,
		Model:       cfg.Model,
		Temperature: cfg.Temperature,
		MaxTokens:   cfg.MaxTokens,
		Timeout:     time.Duration(cfg.TimeoutSec) * time.Second,
	})
	if err != nil {
		return nil, err
	}
	ast := &aiState{svc: svc, client: client}
	s.ai = ast
	return ast, nil
}

// aiSchema 构建当前库的表结构上下文（复用池化连接，仅取前 MaxSchemaTables 张表）。
func (s *session) aiSchema() (string, error) {
	ast, err := s.aiGet()
	if err != nil {
		return "", err
	}
	if ast.schema != "" {
		return ast.schema, nil
	}
	conn := s.connInfo
	if s.currentDB != "" {
		conn.DBName = s.currentDB
	}
	tree, err := getTableTree(conn)
	if err != nil {
		return "", fmt.Errorf("获取表结构失败: %w", err)
	}
	// 目标库：当前库精确匹配，未匹配则取连接库，再退化第一个
	target := s.currentDB
	found := false
	for _, db := range tree {
		if db.Name == target {
			found = true
			break
		}
	}
	if !found {
		target = ""
		if s.currentDB == "" && conn.DBName != "" {
			target = conn.DBName
		}
		if target == "" && len(tree) > 0 {
			target = tree[0].Name
		}
	}
	if target == "" {
		return "", fmt.Errorf("未找到可用数据库")
	}
	var tableNames []string
	for _, db := range tree {
		if db.Name == target {
			tableNames = db.Tables
			break
		}
	}
	sort.Strings(tableNames)
	aiCfg := ast.svc.Config().AI
	if len(tableNames) > aiCfg.MaxSchemaTables {
		tableNames = tableNames[:aiCfg.MaxSchemaTables]
	}
	sub := conn
	sub.DBName = target
	tables := make([]llm.TableInfo, 0, len(tableNames))
	for _, tn := range tableNames {
		meta, err := getTableMeta(sub, tn)
		if err != nil {
			tables = append(tables, llm.TableInfo{Schema: target, Table: tn})
			continue
		}
		ti := llm.TableInfo{Schema: target, Table: tn, Comment: meta.Comment}
		for _, col := range meta.Columns {
			ti.Columns = append(ti.Columns, llm.ColumnInfo{Name: col.Name, Type: col.DataType, Nullable: col.Nullable, Comment: col.Comment})
		}
		tables = append(tables, ti)
	}
	ast.schema = llm.BuildSchemaText(tables, aiCfg.MaxSchemaTables, aiCfg.MaxSchemaChars)
	return ast.schema, nil
}

// aiEnsure 确保会话已初始化（system prompt 含 schema）。
func (s *session) aiEnsure() error {
	ast, err := s.aiGet()
	if err != nil {
		return err
	}
	if len(ast.msgs) == 0 {
		schemaText, err := s.aiSchema()
		if err != nil {
			return err
		}
		dialect := dialectLabel(s.dbType, s.connInfo.SubType)
		sys := llm.RenderSystemPrompt(ast.svc.Config().AI.SystemPrompt, dialect, schemaText)
		ast.msgs = []*schema.Message{schema.SystemMessage(sys)}
	}
	return nil
}

// aiCall 单轮非流式对话：追加用户消息 → 生成 → 追加助手消息 → 返回文本 + 本轮 usage。
func (s *session) aiCall(userText string) (string, llm.Usage, error) {
	ast, err := s.aiGet()
	if err != nil {
		return "", llm.Usage{}, err
	}
	if err := s.aiEnsure(); err != nil {
		return "", llm.Usage{}, err
	}
	ast.msgs = append(ast.msgs, schema.UserMessage(userText))
	content, usage, err := ast.client.Chat(context.Background(), ast.msgs)
	if err != nil {
		return "", llm.Usage{}, err
	}
	ast.msgs = append(ast.msgs, schema.AssistantMessage(content, nil))
	ast.usage.Add(usage)
	return content, usage, nil
}

// aiReset 重置会话（清空消息与 token 累计，保留 schema 缓存）。
func (s *session) aiReset() {
	if s.ai == nil {
		return
	}
	s.ai.msgs = nil
	s.ai.usage = llm.Usage{}
	fmt.Println(dim("AI 会话已重置（对话上下文与 token 统计清零）"))
}

// aiCommand \ai 元命令分发。
func (s *session) aiCommand(args []string) {
	if len(args) == 0 {
		s.aiHelp()
		return
	}
	cmd := strings.ToLower(args[0])
	rest := strings.Join(args[1:], " ")
	switch cmd {
	case "help", "h", "?":
		s.aiHelp()
	case "status":
		s.aiStatus()
	case "config":
		s.aiConfig()
	case "copy":
		s.aiCopy()
	case "clear":
		s.aiReset()
	case "continue", "c":
		s.aiContinue(rest)
	case "explain":
		s.aiExplain(rest)
	case "fix":
		s.aiFix(rest)
	default:
		// 直接当作需求描述
		s.aiGenerate(strings.Join(args, " "))
	}
}

// aiStatus 展示配置状态与会话 token 统计。
func (s *session) aiStatus() {
	ast, err := s.aiGet()
	if err != nil {
		fmt.Fprintln(os.Stderr, red(err.Error()))
		return
	}
	st := ast.svc.AIStatus()
	fmt.Printf("AI 状态: %s\n", map[bool]string{true: green("已启用"), false: yellow("未配置")}[st.Enabled])
	fmt.Printf("  端点:   %s\n", st.BaseURL)
	fmt.Printf("  模型:   %s\n", st.Model)
	fmt.Printf("  温度:   %.2f   上限: %d token   超时: %ds\n", st.Temperature, st.MaxTokens, st.TimeoutSec)
	fmt.Printf("  表结构: 最多 %d 张表 / %d 字符\n", st.MaxSchemaTables, st.MaxSchemaChars)
	fmt.Printf("  debug 日志: %s（全局开关，--debug 或 config 顶层 debug）\n", map[bool]string{true: green("开启"), false: dim("关闭")}[ast.svc.Config().Debug])
	if s.ai != nil && len(s.ai.msgs) > 0 {
		fmt.Printf("  上下文: %d 条消息（含 system）\n", len(s.ai.msgs))
	}
	if ast.usage.TotalTokens > 0 {
		fmt.Printf("%s 输入 %d / 输出 %d / 合计 %d\n",
			dim("进程累计 token:"), ast.usage.PromptTokens, ast.usage.CompletionTokens, ast.usage.TotalTokens)
	} else {
		fmt.Println(dim("进程累计 token: 尚无消耗"))
	}
}

// aiCopy 复制缓冲区 SQL 到系统剪贴板（跨平台：macOS/Linux/Windows）。
func (s *session) aiCopy() {
	if strings.TrimSpace(s.lastSQL) == "" {
		fmt.Fprintln(os.Stderr, red("缓冲区为空：请先用 \\ai <需求> 生成 SQL"))
		return
	}
	if err := clipboard.WriteAll(s.lastSQL); err != nil {
		fmt.Fprintln(os.Stderr, red("复制到剪贴板失败: "+err.Error()))
		return
	}
	fmt.Println(green("已复制到系统剪贴板"))
}

// aiConfig 引导式修改 AI 配置（写回 config.yaml，Web 端下次启动读取）。
// 逐项提示，直接回车保持原值，输入 . 退出。
func (s *session) aiConfig() {
	svc, err := newAIService("", "")
	if err != nil {
		fmt.Fprintln(os.Stderr, red("初始化服务失败: "+err.Error()))
		return
	}
	cfg := svc.Config()
	cur := cfg.AI
	reader := bufio.NewReader(os.Stdin)
	ask := func(label, def string) string {
		fmt.Printf("  %s [%s]: ", label, def)
		line, _ := reader.ReadString('\n')
		return strings.TrimSpace(line)
	}
	fmt.Println("AI 配置引导（直接回车保持原值，输入 . 退出）：")
	next := &cfg.AI
	if v := ask("base_url", cur.BaseURL); v == "." {
		fmt.Println(dim("已取消"))
		return
	} else if v != "" {
		next.BaseURL = v
	}
	if v := ask("api_key（输入新值覆盖，回车保持）", maskAIKey(cur.APIKey)); v == "." {
		fmt.Println(dim("已取消"))
		return
	} else if v != "" && !strings.Contains(v, "****") {
		next.APIKey = v
	}
	if v := ask("model", cur.Model); v == "." {
		fmt.Println(dim("已取消"))
		return
	} else if v != "" {
		next.Model = v
	}
	if v := ask("temperature", fmt.Sprintf("%.2f", cur.Temperature)); v == "." {
		fmt.Println(dim("已取消"))
		return
	} else if v != "" {
		if f, err := strconv.ParseFloat(v, 32); err == nil {
			next.Temperature = float32(f)
		}
	}
	if v := ask("max_tokens", strconv.Itoa(cur.MaxTokens)); v == "." {
		fmt.Println(dim("已取消"))
		return
	} else if v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			next.MaxTokens = n
		}
	}
	if v := ask("timeout_sec", strconv.Itoa(cur.TimeoutSec)); v == "." {
		fmt.Println(dim("已取消"))
		return
	} else if v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			next.TimeoutSec = n
		}
	}
	if v := ask("max_schema_tables（注入上下文的最大表数）", strconv.Itoa(cur.MaxSchemaTables)); v == "." {
		fmt.Println(dim("已取消"))
		return
	} else if v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			next.MaxSchemaTables = n
		}
	}
	if v := ask("max_schema_chars（表结构文本字符上限）", strconv.Itoa(cur.MaxSchemaChars)); v == "." {
		fmt.Println(dim("已取消"))
		return
	} else if v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			next.MaxSchemaChars = n
		}
	}
	if v := ask("system_prompt（输入 clear 清空，回车保持）", "内置默认模板"); v == "." {
		fmt.Println(dim("已取消"))
		return
	} else if strings.EqualFold(v, "clear") {
		next.SystemPrompt = ""
	} else if v != "" {
		next.SystemPrompt = v
	}
	if err := svc.SaveConfig(*cfg); err != nil {
		fmt.Fprintln(os.Stderr, red("保存失败: "+err.Error()))
		return
	}
	fmt.Println(green("AI 配置已保存到 config.yaml"))
}

// maskAIKey 掩码显示 APIKey（保留首尾各 4 位）。
func maskAIKey(key string) string {
	if key == "" {
		return ""
	}
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "****" + key[len(key)-4:]
}

// aiHelp 帮助。
func (s *session) aiHelp() {
	fmt.Println(`AI 辅助 SQL（OpenAI 兼容协议，配置见 config.yaml ai 段或 Web 设置）:
  \ai <需求>                 生成 SQL 到缓冲区（可 \e 编辑后 \g 执行）
  \ai explain [SQL]          解释 SQL（缺省用缓冲区）
  \ai fix [报错信息]          修复缓冲区 SQL
  \ai continue <补充>        基于上文继续补充生成
  \ai copy                   复制缓冲区 SQL 到系统剪贴板
  \ai status                 查看配置状态与 token 统计
  \ai config                 引导式修改 AI 配置（写回 config.yaml）
  \ai clear                  重置当前会话（清空上下文与 token 统计）
  \ai help                   显示此帮助`)
}

// aiGenerate 生成 SQL 到缓冲区（可 \e 编辑后 \g 执行）。
func (s *session) aiGenerate(text string) {
	if strings.TrimSpace(text) == "" {
		fmt.Fprintln(os.Stderr, red("请输入需求描述，如: \\ai 查询最近 30 天订单量按天分组"))
		return
	}
	_, err := s.aiGet()
	if err != nil {
		fmt.Fprintln(os.Stderr, red(err.Error()))
		return
	}
	fmt.Println(dim("正在生成 SQL..."))
	content, usage, err := s.aiCall(llm.ActionPrompt("generate", text))
	if err != nil {
		fmt.Fprintln(os.Stderr, red("生成失败: "+err.Error()))
		return
	}
	s.aiOutput(content, usage)
}

// aiContinue 基于上文继续补充（追加普通用户消息）。
func (s *session) aiContinue(text string) {
	if strings.TrimSpace(text) == "" {
		fmt.Fprintln(os.Stderr, red("用法: \\ai continue <补充描述>"))
		return
	}
	fmt.Println(dim("正在继续生成..."))
	content, usage, err := s.aiCall(text)
	if err != nil {
		fmt.Fprintln(os.Stderr, red("生成失败: "+err.Error()))
		return
	}
	s.aiOutput(content, usage)
}

// aiExplain 解释 SQL。
func (s *session) aiExplain(sql string) {
	if strings.TrimSpace(sql) == "" {
		sql = s.lastSQL
	}
	if strings.TrimSpace(sql) == "" {
		fmt.Fprintln(os.Stderr, red("请提供 SQL 或先用 \\ai <需求> 生成"))
		return
	}
	fmt.Println(dim("正在解释 SQL..."))
	content, _, err := s.aiCall(llm.ActionPrompt("explain", sql))
	if err != nil {
		fmt.Fprintln(os.Stderr, red("解释失败: "+err.Error()))
		return
	}
	fmt.Println(content)
}

// aiFix 修复缓冲区 SQL（携带报错信息）。
func (s *session) aiFix(errMsg string) {
	if s.lastSQL == "" {
		fmt.Fprintln(os.Stderr, red("缓冲区为空：请先用 \\ai <需求> 生成 SQL"))
		return
	}
	detail := "原始 SQL：\n" + s.lastSQL
	if strings.TrimSpace(errMsg) != "" {
		detail += "\n报错信息：\n" + errMsg
	}
	fmt.Println(dim("正在修复 SQL..."))
	content, usage, err := s.aiCall(llm.ActionPrompt("fix", detail))
	if err != nil {
		fmt.Fprintln(os.Stderr, red("修复失败: "+err.Error()))
		return
	}
	s.aiOutput(content, usage)
}

// aiOutput 输出生成结果：提取 SQL、危险检查、写入缓冲区。
func (s *session) aiOutput(content string, usage llm.Usage) {
	sql := llm.ExtractSQL(content)
	if sql == "" {
		fmt.Fprintln(os.Stderr, yellow("模型未返回可执行的 SQL，原文如下："))
		fmt.Println(content)
		if usage.TotalTokens > 0 {
			fmt.Println(dim(fmt.Sprintf("本轮 token: 输入 %d / 输出 %d / 合计 %d", usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens)))
		}
		return
	}

	// 安全链路：危险函数检测（注释已在上游剥离）
	warnings, forbidden := checkDangerous(sql)
	if len(forbidden) > 0 {
		fmt.Fprintln(os.Stderr, red("已拒绝：生成结果包含禁止操作"))
		for _, f := range forbidden {
			fmt.Fprintln(os.Stderr, red("  - "+f))
		}
		return
	}
	for _, w := range warnings {
		fmt.Fprintln(os.Stderr, yellow("警告: "+w))
	}

	fmt.Println(bold("生成的 SQL："))
	fmt.Println(sql)
	if usage.TotalTokens > 0 {
		fmt.Println(dim(fmt.Sprintf("本轮 token: 输入 %d / 输出 %d / 合计 %d", usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens)))
	}
	// 写入缓冲区，可 \e 编辑 / \g 执行 / \ai continue 继续补充
	s.lastSQL = sql
	fmt.Println(dim("已写入缓冲区。可用 \\e 编辑、\\g 执行、\\ai continue 继续补充"))
}

// dialectLabel CLI 方言标签（与 service 层一致）。
func dialectLabel(dbType, subType string) string {
	t := strings.ToLower(strings.TrimSpace(dbType))
	if t == "" {
		return "sql"
	}
	if sub := strings.ToLower(strings.TrimSpace(subType)); sub != "" {
		return t + "(" + sub + ")"
	}
	return t
}
