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
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cylog"
)

// aiState CLI 会话内的 AI 状态（懒加载，切换数据库时整体重置）。
// 对话历史仅存内存，不落盘；生成结果经缓冲区（lastSQL）与执行历史留痕。
// 与 Web 一致采用 agent 工具调用模式：system prompt 只含库/表名录，
// 表结构由模型经 list_databases/list_tables/get_schema 按需查询，不再静态注入字段。
type aiState struct {
	svc    aiService
	client chatClient
	msgs   []*schema.Message
	usage  llm.Usage
	sys    string // system prompt（库/表名录 + 工具约束）
}

// chatClient 抽象大模型对话能力，便于单测注入 mock（*llm.Client / agentChat 天然实现该接口）。
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
	getTableTree = engine.GetTableTree
	getTableMeta = engine.GetTableMeta
)

// aiGet 懒加载：初始化 AI 配置并构建 React Agent（含只读探索工具，与 Web agent 一致）。
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
	dialect := dialectLabel(s.dbType, s.connInfo.SubType)
	target, tables, err := s.aiTargetTables()
	if err != nil {
		return nil, err
	}
	sys := cliAgentSystemPrompt(cfg, dialect, target, tables)
	tools, err := s.buildAgentTools(cfg.MaxSchemaChars)
	if err != nil {
		return nil, err
	}
	agent, err := llm.NewReactAgent(context.Background(), llm.Config{
		BaseURL:     cfg.BaseURL,
		APIKey:      cfg.APIKey,
		Model:       cfg.Model,
		Temperature: cfg.Temperature,
		MaxTokens:   cfg.MaxTokens,
		Timeout:     time.Duration(cfg.TimeoutSec) * time.Second,
	}, tools, sys)
	if err != nil {
		return nil, err
	}
	ast := &aiState{svc: svc, client: &agentChat{a: agent}, sys: sys}
	s.ai = ast
	return ast, nil
}

// aiTargetTables 解析当前目标库及表名录（模型经 get_schema 工具按需查字段，不再预注入字段结构）。
// 目标库：当前库精确匹配，未匹配则取连接库，再退化第一个。
func (s *session) aiTargetTables() (string, []string, error) {
	conn := s.connInfo
	if s.currentDB != "" {
		conn.DBName = s.currentDB
	}
	tree, err := getTableTree(conn)
	if err != nil {
		return "", nil, fmt.Errorf("获取表结构失败: %w", err)
	}
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
		return "", nil, fmt.Errorf("未找到可用数据库")
	}
	for _, db := range tree {
		if db.Name == target {
			names := append([]string(nil), db.Tables...)
			sort.Strings(names)
			return target, names, nil
		}
	}
	return "", nil, fmt.Errorf("目标库 %s 未在表树中找到", target)
}

// aiEnsure 确保会话已初始化（system prompt 含库/表名录）。
func (s *session) aiEnsure() error {
	ast, err := s.aiGet()
	if err != nil {
		return err
	}
	if len(ast.msgs) == 0 {
		ast.msgs = []*schema.Message{schema.SystemMessage(ast.sys)}
	}
	return nil
}

// cliAgentSystemPrompt 构建 CLI agent 的 system prompt：库/表名录 + 工具使用约束。
// 与 Web agent（service.agentSystemPrompt）保持一致：不预注入字段结构，
// 模型必须经 get_schema 按需查询真实字段，禁止凭想象生成 SQL。
func cliAgentSystemPrompt(cfg service.AIConfig, dialect, target string, tableNames []string) string {
	const maxListTables = 30
	var b strings.Builder
	custom := strings.TrimSpace(cfg.SystemPrompt)
	if custom != "" {
		b.WriteString(strings.ReplaceAll(custom, "{dialect}", dialect))
		b.WriteString("\n\n")
	} else {
		fmt.Fprintf(&b, "你是一个资深的 %s DBA 与 SQL 专家。根据用户的业务需求生成可直接执行的 %s SQL，或回答关于表结构/字段的元数据问题。", dialect, dialect)
		b.WriteString("\n\n")
	}
	b.WriteString("【最高优先级：必须先查表结构，禁止凭空生成 SQL】")
	b.WriteString("\n生成 SQL 前，必须先完整梳理需求涉及的所有表与关联关系，并对每一张涉及的表调用 get_schema(库名, 表名) 获取真实字段后再编写。只要还没有拿到某张表的真实字段信息，就必须先调用 get_schema 去查询确认，禁止在缺少表结构信息的情况下直接臆造表名、字段名或关联关系生成 SQL。")
	b.WriteString("\n如果上下文里还没有任何表结构信息，你的第一轮回复必须只调用 get_schema（或先 list_tables 定位表名）获取结构，禁止第一轮就直接输出 SQL。")
	b.WriteString("\n涉及多表关联时，先分析清楚各表之间靠哪个字段关联（外键/关联列），再用 get_schema 确认关联字段确实存在。")
	b.WriteString("\n【回答元数据问题（如“某表有哪些字段”）时必须聚焦】只调用 get_schema 查询用户明确指定的那一张表，只回答那一张表的字段，禁止把其它表或整个库的表结构一起列出。")
	fmt.Fprintf(&b, "\n你的工作范围默认限定在数据库 %q 内，优先只使用该库的表。", target)
	b.WriteString("\n仅当当前库的表确实无法满足需求（表名或字段对不上、关联表明显不在当前库）时，才调用 list_databases 查看其他库；禁止无依据地随意探索其他库。")
	b.WriteString("\n需要工具时直接发起工具调用，不要输出解释文本；可一次并行调用多个 get_schema 批量获取多张表结构。")
	b.WriteString("\n禁止输出思考过程、推理过程或任何 <think>/<thinking>/<reasoning> 标签包裹的内容，直接给出结果。")
	b.WriteString("\n生成的 SQL 放在 ```sql 代码块中。危险语句（DROP/TRUNCATE/无 WHERE 的 DELETE 等）一律拒绝。")
	b.WriteString("\n\n已知元数据（表结构需用 get_schema 查询）：\n")
	fmt.Fprintf(&b, "- %s: ", target)
	names := tableNames
	if len(names) > maxListTables {
		names = names[:maxListTables]
	}
	b.WriteString(strings.Join(names, ", "))
	if len(tableNames) > maxListTables {
		fmt.Fprintf(&b, ", …共 %d 张表（其余请用 list_tables/get_schema 查询）", len(tableNames))
	}
	return b.String()
}

// agentChat 适配器：把 ReactAgent.Stream（工具调用循环）包装为 chatClient 接口，
// 使 CLI 与 Web 共用同一套 agent 工具调用逻辑（list_databases/list_tables/get_schema）。
type agentChat struct{ a *llm.ReactAgent }

func (g *agentChat) Chat(ctx context.Context, msgs []*schema.Message) (string, llm.Usage, error) {
	content, usage, err := g.a.Stream(ctx, msgs, llm.AgentCallbacks{})
	return content, usage, err
}

// ---- Agent 只读工具（与 Web agent 一致：list_databases / list_tables / get_schema）----

// cliToolArgsListTables list_tables 工具参数。
type cliToolArgsListTables struct {
	DB string `json:"db" jsonschema:"description=数据库名（Oracle 为 schema 名）,required"`
}

// cliToolArgsSchema get_schema 工具参数。
type cliToolArgsSchema struct {
	DB    string `json:"db" jsonschema:"description=数据库名（Oracle 为 schema 名）,required"`
	Table string `json:"table" jsonschema:"description=表名,required"`
}

// buildAgentTools 构建三个只读探索工具（闭包捕获会话；每次调用在控制台打印进度）。
func (s *session) buildAgentTools(maxSchemaChars int) ([]tool.InvokableTool, error) {
	conn := s.connInfo
	if s.currentDB != "" {
		conn.DBName = s.currentDB
	}
	progress := func(name, args string) {
		fmt.Fprintln(os.Stderr, dim(fmt.Sprintf("  ⟳ %s (%s)...", name, args)))
	}

	listDBs, err := utils.InferTool("list_databases",
		"列出当前连接可访问的所有数据库（Oracle 为 schema 列表）。仅当确认需要跨库查询时才调用，默认应优先使用当前库。",
		func(ctx context.Context, _ struct{}) (string, error) {
			progress("正在列出可用的数据库", "")
			tree, err := getTableTree(conn)
			if err != nil {
				return "", fmt.Errorf("列出数据库失败: %w", err)
			}
			names := make([]string, 0, len(tree))
			for _, db := range tree {
				names = append(names, db.Name)
			}
			return strings.Join(names, "\n"), nil
		})
	if err != nil {
		return nil, fmt.Errorf("构建工具 list_databases 失败: %w", err)
	}

	listTables, err := utils.InferTool("list_tables",
		"列出指定数据库中的全部表名。",
		func(ctx context.Context, args cliToolArgsListTables) (string, error) {
			progress("正在查询表列表", args.DB)
			sub := conn
			sub.DBName = args.DB
			tree, err := getTableTree(sub)
			if err != nil {
				return "", fmt.Errorf("列出表失败: %w", err)
			}
			for _, db := range tree {
				if strings.EqualFold(db.Name, args.DB) {
					return strings.Join(db.Tables, "\n"), nil
				}
			}
			// 库名拼错：返回可用库列表（不返回 error，让模型纠正后重试）
			var dbNames []string
			for _, db := range tree {
				dbNames = append(dbNames, db.Name)
			}
			return fmt.Sprintf("数据库 %q 不存在。可用数据库：%s", args.DB, strings.Join(dbNames, ", ")), nil
		})
	if err != nil {
		return nil, fmt.Errorf("构建工具 list_tables 失败: %w", err)
	}

	getSchema, err := utils.InferTool("get_schema",
		"获取指定表的结构摘要（表注释 + 字段名/类型/可空/注释）。",
		func(ctx context.Context, args cliToolArgsSchema) (string, error) {
			progress("正在查询表结构", args.DB+"."+args.Table)
			// 先校验库名（大小写不敏感），避免模型拼错库名时拿到含糊的 not found
			tree, err := getTableTree(conn)
			if err != nil {
				return "", fmt.Errorf("获取库列表失败: %w", err)
			}
			realDB := ""
			var dbNames []string
			for _, db := range tree {
				dbNames = append(dbNames, db.Name)
				if strings.EqualFold(db.Name, args.DB) {
					realDB = db.Name
				}
			}
			if realDB == "" {
				// 库名拼错：返回可用库列表，让模型纠正后重试（不返回 error，避免 agent 直接终止）
				return fmt.Sprintf("数据库 %q 不存在。可用数据库：%s。请用正确的库名重试 get_schema。",
					args.DB, strings.Join(dbNames, ", ")), nil
			}
			sub := conn
			sub.DBName = realDB
			meta, err := getTableMeta(sub, args.Table)
			if err != nil {
				// 表不存在：返回该库可用表列表，让模型纠正表名（不返回 error）
				var tbls []string
				for _, db := range tree {
					if strings.EqualFold(db.Name, realDB) {
						tbls = db.Tables
						break
					}
				}
				return fmt.Sprintf("表 %q 在库 %q 中不存在。该库可用表（前 50 个）：%s。请用正确的表名重试。",
					args.Table, realDB, strings.Join(tbls, ", ")), nil
			}
			ti := llm.TableInfo{Schema: realDB, Table: args.Table, Comment: meta.Comment}
			for _, col := range meta.Columns {
				ti.Columns = append(ti.Columns, llm.ColumnInfo{
					Name:     col.Name,
					Type:     col.DataType,
					Nullable: col.Nullable,
					Comment:  col.Comment,
				})
			}
			return llm.BuildSchemaText([]llm.TableInfo{ti}, 1, maxSchemaChars), nil
		})
	if err != nil {
		return nil, fmt.Errorf("构建工具 get_schema 失败: %w", err)
	}
	return []tool.InvokableTool{listDBs, listTables, getSchema}, nil
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

// aiReset 重置会话（清空消息与 token 累计，保留 system 提示词）。
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
  \ai fix [报错信息]      修复缓冲区 SQL（缺省自动附带上次执行报错）
  \ai continue <补充>        基于上文继续补充生成
  \ai copy                   复制缓冲区 SQL 到系统剪贴板
  \ai status                 查看配置状态与 token 统计
  \ai config                 引导式修改 AI 配置（写回 config.yaml）
  \ai clear                  重置当前会话（清空上下文与 token 统计）
  \ai help                   显示此帮助
生成时自动调用工具（list_databases / list_tables / get_schema）查询真实表结构，无需手动刷新`)
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

// aiFix 修复缓冲区 SQL（携带报错信息；缺省自动附带最近一次执行报错）。
func (s *session) aiFix(errMsg string) {
	if s.lastSQL == "" {
		fmt.Fprintln(os.Stderr, red("缓冲区为空：请先用 \\ai <需求> 生成 SQL"))
		return
	}
	detail := "原始 SQL：\n" + s.lastSQL
	if strings.TrimSpace(errMsg) != "" {
		detail += "\n报错信息：\n" + errMsg
	} else if strings.TrimSpace(s.lastErr) != "" {
		detail += "\n报错信息：\n" + s.lastErr
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
