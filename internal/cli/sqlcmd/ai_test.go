package sqlcmd

import (
	"context"
	"slices"
	"strings"
	"testing"

	"dbimpex/internal/engine"
	"dbimpex/internal/llm"
	"dbimpex/internal/service"

	"github.com/cloudwego/eino/schema"
	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cydb/def"
)

// ---- mock 依赖 ----

// mockChat 记录每次 Chat 收到的消息，返回预设内容。
type mockChat struct {
	reply  string
	usage  llm.Usage
	calls  [][]*schema.Message
	record bool
}

func (m *mockChat) Chat(ctx context.Context, msgs []*schema.Message) (string, llm.Usage, error) {
	if m.record {
		m.calls = append(m.calls, msgs)
	}
	return m.reply, m.usage, nil
}

// mockSvc 实现 aiService 接口。
type mockSvc struct {
	cfg    service.AppConfig
	status service.AIStatus
}

func (m *mockSvc) Config() *service.AppConfig  { return &m.cfg }
func (m *mockSvc) AIEnabled() bool              { return true }
func (m *mockSvc) AIStatus() service.AIStatus   { return m.status }

// newTestSession 构造一个带 mock AI 状态的 session，绕过真实 service/engine/llm。
func newTestSession(reply string) (*session, *mockChat) {
	mc := &mockChat{reply: reply, record: true}
	svc := &mockSvc{
		cfg: service.AppConfig{
			AI: service.AIConfig{
				MaxSchemaTables: 30,
				MaxSchemaChars:  20000,
			},
		},
		status: service.AIStatus{Enabled: true},
	}
	s := &session{
		connInfo:  engine.DBConnInfo{DBConnection: def.DBConnection{Type: "mysql", DBName: "testdb"}},
		currentDB: "testdb",
		dbType:    "mysql",
		ai: &aiState{
			svc:    svc,
			client: mc,
			sys:    "# 预置 system prompt（绕过数据库访问）\n`sys_user`\n",
		},
	}
	return s, mc
}

// 重置包级注入变量（测试隔离），在每个用例开头调用。
func resetAIDeps() {
	newAIService = service.NewServiceWith
	getTableTree = engine.GetTableTree
	getTableMeta = engine.GetTableMeta
}

// 把 getTableTree 换成返回固定树，供 aiTargetTables 相关测试。
func stubTableTree() {
	getTableTree = func(conn engine.DBConnInfo) ([]engine.DBTables, error) {
		return []engine.DBTables{
			{Name: "testdb", Tables: []string{"sys_user", "orders"}},
			{Name: "otherdb", Tables: []string{"logs"}},
		}, nil
	}
	getTableMeta = func(conn engine.DBConnInfo, tableName string) (engine.TableMeta, error) {
		return engine.TableMeta{
			Comment: "测试表",
			Columns: []engine.TableColumnInfo{
				{Name: "id", DataType: "bigint", Comment: "主键"},
				{Name: "name", DataType: "varchar(64)", Nullable: true, Comment: "名称"},
			},
		}, nil
	}
}

// ---- 用例 ----

// aiGenerate 成功时：提取 SQL 写入 lastSQL。
func TestAIGenerateWritesLastSQL(t *testing.T) {
	resetAIDeps()
	s, mc := newTestSession("```sql\nSELECT * FROM sys_user;\n```")
	mc.record = true

	s.aiGenerate("查询所有用户")

	if strings.TrimSpace(s.lastSQL) != "SELECT * FROM sys_user;" {
		t.Fatalf("lastSQL 未正确写入，实际: %q", s.lastSQL)
	}
	if len(mc.calls) != 1 {
		t.Fatalf("应调用 1 次 Chat，实际 %d", len(mc.calls))
	}
}

// aiGenerate 返回危险 SQL：拒绝写入 lastSQL。
func TestAIGenerateRejectsForbidden(t *testing.T) {
	resetAIDeps()
	s, _ := newTestSession("```sql\nSELECT LOAD_FILE('/etc/passwd');\n```")

	s.aiGenerate("读取文件")

	if s.lastSQL != "" {
		t.Fatalf("禁止操作应被拒绝，lastSQL 应保持空，实际: %q", s.lastSQL)
	}
}

// aiGenerate 空需求：直接报错，不调用 Chat。
func TestAIGenerateEmptyInput(t *testing.T) {
	resetAIDeps()
	s, mc := newTestSession("")

	s.aiGenerate("   ")

	if len(mc.calls) != 0 {
		t.Fatalf("空需求不应调用 Chat，实际调用 %d 次", len(mc.calls))
	}
}

// aiFix 应把原始 SQL 与报错信息一并交给模型。
func TestAIFixCarriesSQLAndError(t *testing.T) {
	resetAIDeps()
	s, mc := newTestSession("```sql\nSELECT * FROM fixed;\n```")

	s.lastSQL = "SELECT * FROM bad_table;"
	s.aiFix("Unknown column 'x'")

	if len(mc.calls) != 1 {
		t.Fatalf("应调用 1 次 Chat，实际 %d", len(mc.calls))
	}
	last := mc.calls[0][len(mc.calls[0])-1]
	if !strings.Contains(last.Content, "SELECT * FROM bad_table;") {
		t.Fatalf("修复请求应携带原始 SQL，实际: %q", last.Content)
	}
	if !strings.Contains(last.Content, "Unknown column 'x'") {
		t.Fatalf("修复请求应携带报错信息，实际: %q", last.Content)
	}
}

// aiFix 缓冲区为空：直接报错，不调用 Chat。
func TestAIFixEmptyBuffer(t *testing.T) {
	resetAIDeps()
	s, mc := newTestSession("")

	s.aiFix("some error")

	if len(mc.calls) != 0 {
		t.Fatalf("缓冲区为空不应调用 Chat，实际 %d 次", len(mc.calls))
	}
}

// aiExplain 缺省 SQL：回退到 lastSQL。
func TestAIExplainFallsBackToLastSQL(t *testing.T) {
	resetAIDeps()
	s, mc := newTestSession("解释文本")

	s.lastSQL = "SELECT * FROM orders;"
	s.aiExplain("")

	if len(mc.calls) != 1 {
		t.Fatalf("应调用 1 次 Chat，实际 %d", len(mc.calls))
	}
	last := mc.calls[0][len(mc.calls[0])-1]
	if !strings.Contains(last.Content, "SELECT * FROM orders;") {
		t.Fatalf("解释应回退到 lastSQL，实际: %q", last.Content)
	}
}

// aiExplain 显式提供 SQL：优先用传入的，而非 lastSQL。
func TestAIExplainUsesProvidedSQL(t *testing.T) {
	resetAIDeps()
	s, mc := newTestSession("解释文本")

	s.lastSQL = "SELECT * FROM old;"
	s.aiExplain("SELECT * FROM new;")

	last := mc.calls[0][len(mc.calls[0])-1]
	if !strings.Contains(last.Content, "SELECT * FROM new;") {
		t.Fatalf("应优先使用传入 SQL，实际: %q", last.Content)
	}
	if strings.Contains(last.Content, "SELECT * FROM old;") {
		t.Fatalf("不应混入 lastSQL，实际: %q", last.Content)
	}
}

// aiReset 清空消息与 token 累计。
func TestAIResetClearsContext(t *testing.T) {
	resetAIDeps()
	s, mc := newTestSession("```sql\nSELECT 1;\n```")

	s.aiGenerate("生成")
	if len(s.ai.msgs) == 0 {
		t.Fatal("generate 后应有上下文消息")
	}
	s.ai.usage = llm.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}
	s.aiReset()
	if len(s.ai.msgs) != 0 {
		t.Fatalf("reset 后消息应清空，实际 %d", len(s.ai.msgs))
	}
	if s.ai.usage.TotalTokens != 0 {
		t.Fatalf("reset 后 usage 应清零，实际 %d", s.ai.usage.TotalTokens)
	}
	_ = mc
}

// aiEnsure 首次调用时注入预置 system prompt。
func TestAIEnsureInjectsSystemPrompt(t *testing.T) {
	resetAIDeps()
	stubTableTree()
	s, _ := newTestSession("")

	if err := s.aiEnsure(); err != nil {
		t.Fatalf("aiEnsure 失败: %v", err)
	}
	if len(s.ai.msgs) != 1 {
		t.Fatalf("应恰好 1 条 system 消息，实际 %d", len(s.ai.msgs))
	}
	sys := s.ai.msgs[0]
	if sys.Role != schema.System {
		t.Fatalf("首条消息应为 system，实际 role=%s", sys.Role)
	}
	if !strings.Contains(sys.Content, "sys_user") {
		t.Fatalf("system prompt 应包含预置表名，实际: %q", sys.Content)
	}
}

// aiEnsure 二次调用幂等：不重复追加 system 消息。
func TestAIEnsureIdempotent(t *testing.T) {
	resetAIDeps()
	stubTableTree()
	s, _ := newTestSession("")

	if err := s.aiEnsure(); err != nil {
		t.Fatalf("首次 aiEnsure 失败: %v", err)
	}
	if err := s.aiEnsure(); err != nil {
		t.Fatalf("二次 aiEnsure 失败: %v", err)
	}
	if len(s.ai.msgs) != 1 {
		t.Fatalf("应保持 1 条消息，实际 %d", len(s.ai.msgs))
	}
}

// aiTargetTables 目标库降级：当前库不存在时退化为第一个库。
func TestAITargetTablesFallsBackToFirstDB(t *testing.T) {
	resetAIDeps()
	stubTableTree()
	s, _ := newTestSession("")
	s.currentDB = "nonexistent" // 当前库在树中不存在

	target, tables, err := s.aiTargetTables()
	if err != nil {
		t.Fatalf("aiTargetTables 失败: %v", err)
	}
	// 应退化到第一个库 testdb
	if target != "testdb" {
		t.Fatalf("应退化到 testdb，实际: %q", target)
	}
	if !slices.Contains(tables, "sys_user") {
		t.Fatalf("表名录应包含 sys_user，实际: %v", tables)
	}
}

// aiTargetTables 目标库精确匹配：优先当前库，不混入其他库的表。
func TestAITargetTablesMatchesCurrentDB(t *testing.T) {
	resetAIDeps()
	stubTableTree()
	s, _ := newTestSession("")
	s.currentDB = "otherdb"

	target, tables, err := s.aiTargetTables()
	if err != nil {
		t.Fatalf("aiTargetTables 失败: %v", err)
	}
	if target != "otherdb" {
		t.Fatalf("应匹配 otherdb，实际: %q", target)
	}
	if !slices.Contains(tables, "logs") {
		t.Fatalf("表名录应包含 logs，实际: %v", tables)
	}
	if slices.Contains(tables, "sys_user") {
		t.Fatalf("不应混入其他库的表，实际: %v", tables)
	}
}

// cliAgentSystemPrompt 默认模板：包含表名录与「必须先查表结构」约束。
func TestCLIAgentSystemPrompt(t *testing.T) {
	resetAIDeps()
	sys := cliAgentSystemPrompt(service.AIConfig{}, "mysql", "testdb", []string{"sys_user", "orders"})

	if !strings.Contains(sys, "mysql") {
		t.Fatalf("system prompt 应包含方言，实际: %q", sys)
	}
	if !strings.Contains(sys, "testdb") {
		t.Fatalf("system prompt 应包含目标库，实际: %q", sys)
	}
	if !strings.Contains(sys, "sys_user, orders") {
		t.Fatalf("system prompt 应包含表名录，实际: %q", sys)
	}
	if !strings.Contains(sys, "get_schema") {
		t.Fatalf("system prompt 应包含工具使用约束，实际: %q", sys)
	}
}

// cliAgentSystemPrompt 自定义模板：替换 {dialect} 占位符，工具约束仍保留。
func TestCLIAgentSystemPromptCustom(t *testing.T) {
	resetAIDeps()
	sys := cliAgentSystemPrompt(service.AIConfig{SystemPrompt: "你是 {dialect} 专家"}, "tidb", "testdb", nil)

	if !strings.Contains(sys, "你是 tidb 专家") {
		t.Fatalf("应替换 {dialect} 占位符，实际: %q", sys)
	}
	if !strings.Contains(sys, "get_schema") {
		t.Fatalf("自定义模板下工具约束仍应保留，实际: %q", sys)
	}
}
