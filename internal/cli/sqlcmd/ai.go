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
	svc, err := newAIService(langCtx(), "", "")
	if err != nil {
		return nil, textErr(err, cliTextsFor(cliLang).errInitSvc)
	}
	if !svc.AIEnabled() {
		return nil, textErr(nil, cliTextsFor(cliLang).errAINotConfigured)
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
		return "", nil, textErr(err, cliTextsFor(cliLang).errSchemaFail)
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
		return "", nil, textErr(nil, cliTextsFor(cliLang).errNoTargetDB)
	}
	for _, db := range tree {
		if db.Name == target {
			names := append([]string(nil), db.Tables...)
			sort.Strings(names)
			return target, names, nil
		}
	}
	return "", nil, textErr(nil, cliTextsFor(cliLang).errNoTableInTree, target)
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
// 与 Web agent（service.agentSystemPrompt）共用 llm 语言注册表：按 CLI 语言选择
// 模板（缺失回退 zh），自定义 prompt 原样使用，不预注入字段结构。
func cliAgentSystemPrompt(cfg service.AIConfig, dialect, target string, tableNames []string) string {
	return llm.RenderSystemPrompt(cliLang, cfg.SystemPrompt, dialect,
		llm.AgentRules(cliLang, target)+llm.KnownTables(cliLang, target, tableNames))
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
// 工具描述与输出文本按 CLI 语言选择（与 Web agent 共用 llm 语言注册表）。
func (s *session) buildAgentTools(maxSchemaChars int) ([]tool.InvokableTool, error) {
	conn := s.connInfo
	if s.currentDB != "" {
		conn.DBName = s.currentDB
	}
	tt := llm.ToolTextsFor(cliLang)
	ut := cliTextsFor(cliLang)
	progress := func(name, args string) {
		fmt.Fprintln(os.Stderr, dim(sprintf(ut.toolRunning, name, args)))
	}

	listDBs, err := utils.InferTool("list_databases",
		tt.ListDBsDesc,
		func(ctx context.Context, _ struct{}) (string, error) {
			progress(ut.toolProgressListDBs, "")
			tree, err := getTableTree(conn)
			if err != nil {
				return "", fmt.Errorf(tt.ErrListDBs, err)
			}
			names := make([]string, 0, len(tree))
			for _, db := range tree {
				names = append(names, db.Name)
			}
			return strings.Join(names, "\n"), nil
		})
	if err != nil {
		return nil, textErr(err, cliTextsFor(cliLang).errToolListDBs)
	}

	listTables, err := utils.InferTool("list_tables",
		tt.ListTablesDesc,
		func(ctx context.Context, args cliToolArgsListTables) (string, error) {
			progress(ut.toolProgressListTables, args.DB)
			sub := conn
			sub.DBName = args.DB
			tree, err := getTableTree(sub)
			if err != nil {
				return "", fmt.Errorf(tt.ErrListTables, err)
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
			return sprintf(tt.DBNotFound, args.DB, strings.Join(dbNames, ", ")), nil
		})
	if err != nil {
		return nil, textErr(err, cliTextsFor(cliLang).errToolListTables)
	}

	getSchema, err := utils.InferTool("get_schema",
		tt.GetSchemaDesc,
		func(ctx context.Context, args cliToolArgsSchema) (string, error) {
			progress(ut.toolProgressGetSchema, args.DB+"."+args.Table)
			// 先校验库名（大小写不敏感），避免模型拼错库名时拿到含糊的 not found
			tree, err := getTableTree(conn)
			if err != nil {
				return "", fmt.Errorf(tt.ErrListDBsForSchema, err)
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
				return sprintf(tt.DBNotFoundSchema,
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
				return sprintf(tt.TableNotFound,
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
			// 用完整版渲染（不过滤敏感列）：工具语义是返回真实表结构，
			// 过滤会让模型误判字段不存在（如 email/mobile/password_*），导致臆造或报错
			return llm.BuildSchemaTextFull(cliLang, []llm.TableInfo{ti}, 1, maxSchemaChars), nil
		})
	if err != nil {
		return nil, textErr(err, cliTextsFor(cliLang).errToolGetSchema)
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
	fmt.Println(dim(cliTextsFor(cliLang).aiResetDone))
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
	txt := cliTextsFor(cliLang)
	ast, err := s.aiGet()
	if err != nil {
		fmt.Fprintln(os.Stderr, red(err.Error()))
		return
	}
	st := ast.svc.AIStatus()
	printf(txt.aiStatusTitle+"\n", map[bool]string{true: green(txt.aiEnabled), false: yellow(txt.aiNotConfigured)}[st.Enabled])
	printf(txt.aiEndpoint+"\n", st.BaseURL)
	printf(txt.aiModel+"\n", st.Model)
	printf(txt.aiTuning+"\n", st.Temperature, st.MaxTokens, st.TimeoutSec)
	printf(txt.aiSchemaLimit+"\n", st.MaxSchemaChars)
	printf(txt.aiDebugLog+"\n", map[bool]string{true: green(txt.aiDebugOn), false: dim(txt.aiDebugOff)}[ast.svc.Config().Debug])
	if s.ai != nil && len(s.ai.msgs) > 0 {
		printf(txt.aiContext+"\n", len(s.ai.msgs))
	}
	if ast.usage.TotalTokens > 0 {
		printf("%s %s\n", dim(txt.aiProcTokens),
			sprintf(txt.tokenInOut, ast.usage.PromptTokens, ast.usage.CompletionTokens, ast.usage.TotalTokens))
	} else {
		fmt.Println(dim(txt.aiProcTokensNone))
	}
}

// aiCopy 复制缓冲区 SQL 到系统剪贴板（跨平台：macOS/Linux/Windows）。
func (s *session) aiCopy() {
	txt := cliTextsFor(cliLang)
	if strings.TrimSpace(s.lastSQL) == "" {
		fmt.Fprintln(os.Stderr, red(txt.aiCopyEmpty))
		return
	}
	if err := clipboard.WriteAll(s.lastSQL); err != nil {
		fmt.Fprintln(os.Stderr, red(sprintf(txt.aiCopyFail, err.Error())))
		return
	}
	fmt.Println(green(txt.aiCopyOK))
}

// aiConfig 引导式修改 AI 配置（写回 config.yaml，Web 端下次启动读取）。
// 逐项提示，直接回车保持原值，输入 . 退出。
func (s *session) aiConfig() {
	txt := cliTextsFor(cliLang)
	svc, err := newAIService(langCtx(), "", "")
	if err != nil {
		fmt.Fprintln(os.Stderr, red(sprintf(txt.aiInitFail, err.Error())))
		return
	}
	cfg := svc.Config()
	cur := cfg.AI
	reader := bufio.NewReader(os.Stdin)
	ask := func(label, def string) string {
		printf("  %s [%s]: ", label, def)
		line, _ := reader.ReadString('\n')
		return strings.TrimSpace(line)
	}
	fmt.Println(txt.aiConfigTitle)
	next := &cfg.AI
	if v := ask("base_url", cur.BaseURL); v == "." {
		fmt.Println(dim(txt.cancelled))
		return
	} else if v != "" {
		next.BaseURL = v
	}
	if v := ask(txt.aiCfgAPIKey, maskAIKey(cur.APIKey)); v == "." {
		fmt.Println(dim(txt.cancelled))
		return
	} else if v != "" && !strings.Contains(v, "****") {
		next.APIKey = v
	}
	if v := ask("model", cur.Model); v == "." {
		fmt.Println(dim(txt.cancelled))
		return
	} else if v != "" {
		next.Model = v
	}
	if v := ask("temperature", sprintf("%.2f", cur.Temperature)); v == "." {
		fmt.Println(dim(txt.cancelled))
		return
	} else if v != "" {
		if f, err := strconv.ParseFloat(v, 32); err == nil {
			next.Temperature = float32(f)
		}
	}
	if v := ask("max_tokens", strconv.Itoa(cur.MaxTokens)); v == "." {
		fmt.Println(dim(txt.cancelled))
		return
	} else if v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			next.MaxTokens = n
		}
	}
	if v := ask("timeout_sec", strconv.Itoa(cur.TimeoutSec)); v == "." {
		fmt.Println(dim(txt.cancelled))
		return
	} else if v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			next.TimeoutSec = n
		}
	}
	if v := ask(txt.aiCfgMaxSchemaChars, strconv.Itoa(cur.MaxSchemaChars)); v == "." {
		fmt.Println(dim(txt.cancelled))
		return
	} else if v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			next.MaxSchemaChars = n
		}
	}
	if v := ask(txt.aiCfgSystemPrompt, txt.aiCfgDefaultPrompt); v == "." {
		fmt.Println(dim(txt.cancelled))
		return
	} else if strings.EqualFold(v, "clear") {
		next.SystemPrompt = ""
	} else if v != "" {
		next.SystemPrompt = v
	}
	if err := svc.SaveConfig(langCtx(), *cfg); err != nil {
		fmt.Fprintln(os.Stderr, red(sprintf(txt.aiSaveFail, err.Error())))
		return
	}
	fmt.Println(green(txt.aiConfigSaved))
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
	fmt.Println(cliTextsFor(cliLang).aiHelp)
}

// aiGenerate 生成 SQL 到缓冲区（可 \e 编辑后 \g 执行）。
func (s *session) aiGenerate(text string) {
	txt := cliTextsFor(cliLang)
	if strings.TrimSpace(text) == "" {
		fmt.Fprintln(os.Stderr, red(txt.aiGenerateEmpty))
		return
	}
	_, err := s.aiGet()
	if err != nil {
		fmt.Fprintln(os.Stderr, red(err.Error()))
		return
	}
	fmt.Println(dim(txt.aiGenerating))
	content, usage, err := s.aiCall(llm.ActionPrompt(cliLang, "generate", text))
	if err != nil {
		fmt.Fprintln(os.Stderr, red(sprintf(txt.aiGenFail, err.Error())))
		return
	}
	s.aiOutput(content, usage)
}

// aiContinue 基于上文继续补充（追加普通用户消息）。
func (s *session) aiContinue(text string) {
	txt := cliTextsFor(cliLang)
	if strings.TrimSpace(text) == "" {
		fmt.Fprintln(os.Stderr, red(txt.aiContinueUsage))
		return
	}
	fmt.Println(dim(txt.aiGenerating2))
	content, usage, err := s.aiCall(text)
	if err != nil {
		fmt.Fprintln(os.Stderr, red(sprintf(txt.aiGenFail, err.Error())))
		return
	}
	s.aiOutput(content, usage)
}

// aiExplain 解释 SQL。
func (s *session) aiExplain(sql string) {
	txt := cliTextsFor(cliLang)
	if strings.TrimSpace(sql) == "" {
		sql = s.lastSQL
	}
	if strings.TrimSpace(sql) == "" {
		fmt.Fprintln(os.Stderr, red(txt.aiNoSQL))
		return
	}
	fmt.Println(dim(txt.aiExplaining))
	content, _, err := s.aiCall(llm.ActionPrompt(cliLang, "explain", sql))
	if err != nil {
		fmt.Fprintln(os.Stderr, red(sprintf(txt.aiExplainFail, err.Error())))
		return
	}
	fmt.Println(content)
}

// aiFix 修复缓冲区 SQL（携带报错信息；缺省自动附带最近一次执行报错）。
func (s *session) aiFix(errMsg string) {
	txt := cliTextsFor(cliLang)
	if s.lastSQL == "" {
		fmt.Fprintln(os.Stderr, red(txt.aiCopyEmpty))
		return
	}
	detail := txt.aiFixDetailSQL + "\n" + s.lastSQL
	if strings.TrimSpace(errMsg) != "" {
		detail += "\n" + txt.aiFixDetailErr + "\n" + errMsg
	} else if strings.TrimSpace(s.lastErr) != "" {
		detail += "\n" + txt.aiFixDetailErr + "\n" + s.lastErr
	}
	fmt.Println(dim(txt.aiFixing))
	content, usage, err := s.aiCall(llm.ActionPrompt(cliLang, "fix", detail))
	if err != nil {
		fmt.Fprintln(os.Stderr, red(sprintf(txt.aiFixFail, err.Error())))
		return
	}
	s.aiOutput(content, usage)
}

// aiOutput 输出生成结果：提取 SQL、危险检查、写入缓冲区。
func (s *session) aiOutput(content string, usage llm.Usage) {
	txt := cliTextsFor(cliLang)
	sql := llm.ExtractSQL(content)
	if sql == "" {
		fmt.Fprintln(os.Stderr, yellow(txt.aiNoExecSQL))
		fmt.Println(content)
		if usage.TotalTokens > 0 {
			fmt.Println(dim(sprintf(txt.aiTokens, usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens)))
		}
		return
	}

	// 安全链路：危险函数检测（注释已在上游剥离）
	warnings, forbidden := checkDangerous(sql)
	if len(forbidden) > 0 {
		fmt.Fprintln(os.Stderr, red(txt.aiRejected))
		for _, f := range forbidden {
			fmt.Fprintln(os.Stderr, red("  - "+f))
		}
		return
	}
	for _, w := range warnings {
		fmt.Fprintln(os.Stderr, yellow(sprintf(txt.aiWarning, w)))
	}

	fmt.Println(bold(txt.aiGeneratedSQL))
	fmt.Println(sql)
	if usage.TotalTokens > 0 {
		fmt.Println(dim(sprintf(txt.aiTokens, usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens)))
	}
	// 写入缓冲区，可 \e 编辑 / \g 执行 / \ai continue 继续补充
	s.lastSQL = sql
	fmt.Println(dim(txt.aiBufferWritten))
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
