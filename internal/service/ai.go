package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"dbimpex/internal/engine"
	"dbimpex/internal/llm"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
	"github.com/rs/xid"
	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cylog"
)

// ---- AI 能力状态（Web/CLI 共用，APIKey 永不回显明文） ----

// AIStatus AI 能力状态视图。
type AIStatus struct {
	Enabled         bool    `json:"enabled"`         // 配置齐全（四项必填非空）
	BaseURL         string  `json:"baseUrl"`         // 掩码后端点
	Model           string  `json:"model"`           // 模型名
	Temperature     float32 `json:"temperature"`     // 温度
	MaxTokens       int     `json:"maxTokens"`       // 单次回复上限
	TimeoutSec      int     `json:"timeoutSec"`      // 请求超时（秒）
	MaxSchemaTables int     `json:"maxSchemaTables"` // 注入表结构数量上限
	MaxSchemaChars  int     `json:"maxSchemaChars"`  // 注入表结构文本字符上限
	HasPrompt       bool    `json:"hasPrompt"`       // 是否使用自定义 system prompt
}

// AIEnabled 判断 AI 功能是否可用。
func (s *Service) AIEnabled() bool { return s.cfg.AI.configured() }

// AIStatus 返回 AI 能力状态（掩码脱敏）。
func (s *Service) AIStatus() AIStatus {
	ai := s.cfg.AI
	ai.normalize()
	return AIStatus{
		Enabled:         ai.configured(),
		BaseURL:         maskSecret(ai.BaseURL),
		Model:           ai.Model,
		Temperature:     ai.Temperature,
		MaxTokens:       ai.MaxTokens,
		TimeoutSec:      ai.TimeoutSec,
		MaxSchemaTables: ai.MaxSchemaTables,
		MaxSchemaChars:  ai.MaxSchemaChars,
		HasPrompt:       strings.TrimSpace(ai.SystemPrompt) != "",
	}
}

// aiDebugf 输出 AI 链路调试日志；是否打印由全局日志级别（debug 及以上）控制。
func (s *Service) aiDebugf(format string, args ...any) {
	cylog.Debugf(format, args...)
}

// truncLog 调试日志截断：超长文本（含多行任务描述）只保留前 n 个字符，避免刷屏。
func truncLog(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…(截断)"
	}
	return s
}

// configured 判断必填项是否齐全。
func (c AIConfig) configured() bool {
	return strings.TrimSpace(c.BaseURL) != "" &&
		strings.TrimSpace(c.APIKey) != "" &&
		strings.TrimSpace(c.Model) != ""
}

func maskSecret(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 8 {
		return "****"
	}
	return s[:4] + "****" + s[len(s)-4:]
}

// ---- AI 会话管理（对话历史仅存内存，不落盘；成果经 source=ai 走执行历史） ----

// AISession 一次 AI 对话会话：绑定连接+库，累计 token，消息保留在内存。
// 采用「预注入表结构 + 工具兜底」模式：目标库的表结构（受 max_schema_tables/chars 限制）
// 预加载进 system prompt，模型无需自觉调工具即可用真实字段；超出预算的表仍可经 get_schema 按需查。
type AISession struct {
	ID       string
	ConnKey  string
	TabID    string // 所属 query tab（按 tab 隔离对话；空 = 不隔离，兼容旧调用）
	DBName   string
	Dialect  string
	Messages []*schema.Message
	Usage    llm.Usage
	Created  time.Time
	LastAt   time.Time

	// Agent 会话级 React Agent（独立 ChatModel 实例 + 只读工具，Reset 后复用）。
	Agent *llm.ReactAgent
	// Sys 名录 system prompt（Reset 重建用）。
	Sys string
	// ToolSink 工具调用事件出口（每次 aiChat 前设置，SSE 转发给前端）。
	// 用 atomic.Value 存 func 类型：主 goroutine 写、工具 goroutine 读，避免 data race。
	ToolSink atomic.Value // 存 func(name, args string)

	// schemaQueried 会话级标记：会话内是否已成功调用过 get_schema（查询过真实表结构）。
	// 一旦置 true 即持续（后续轮次复用历史表结构同样可信），仅在「重置会话」时清空。
	// get_schema 工具在 goroutine 中成功调用后置位；工具并发执行，用 atomic.Bool 避免 data race。
	schemaQueried atomic.Bool

	// mu 会话对话互斥锁：同一会话并发对话时串行化，避免 Messages 切片并发读写（data race）。
	mu sync.Mutex
}

const (
	aiMaxMessages   = 40               // 会话消息上限（约 20 轮），超出裁剪最旧对话
	aiIdleTTL       = 30 * time.Minute // 会话空闲回收 TTL
	aiMaxConcurrent = 32               // 并发会话上限，防止内存膨胀

	// aiMaxHistoryChars 对话历史字符预算上限（约 6K token）：超出时按轮次裁剪，
	// 优先保留含 SQL 代码块的关键轮次，避免上下文过度膨胀（未雨绸缪的软上限）。
	aiMaxHistoryChars = 24000

	// aiMaxSessionsPerConn 每连接落盘会话数上限（N）：会话数超过该值时，触发「超期会话」清理。
	// 仅当会话数 > N 且某会话已超过 aiSessionKeepDays 天未活动时才清理；否则永久保留。
	aiMaxSessionsPerConn = 50

	// aiSessionKeepDays 会话保留天数（M）：超过该天数未活动的会话，在「会话数 > N」时被清理。
	aiSessionKeepDays = 7
)

type aiMgr struct {
	mu        sync.Mutex
	sessions  map[string]*AISession
	procUsage llm.Usage // 进程级累计 token（所有会话共享，不落盘）
}

func newAIMgr() *aiMgr {
	return &aiMgr{
		sessions: map[string]*AISession{},
	}
}

// aiPickTarget 拉库→表树并选定目标库，返回该库全部表名（未截断，供名录注入）。
// 连接未配置库时遍历所有库（engine 已排除系统库）；dbName 为空回退连接配置的库。
func (s *Service) aiPickTarget(ctx context.Context, conn *DBConnInfo, dbName string) (string, []string, error) {
	if dbName == "" {
		dbName = conn.DBName
	}
	tree, err := engine.GetTableTree(*conn)
	if err != nil {
		return "", nil, cyginWrapAI(fmt.Errorf("获取表结构失败: %w", err))
	}
	if err := ctx.Err(); err != nil {
		return "", nil, cyginWrapAI(fmt.Errorf("任务已取消"))
	}
	// 选定目标库：dbName 精确匹配，未匹配则取连接库，再退化为第一个库
	target := ""
	for _, db := range tree {
		if db.Name == dbName {
			target = db.Name
			break
		}
	}
	if target == "" {
		if conn.DBName != "" {
			target = conn.DBName
		} else if len(tree) > 0 {
			target = tree[0].Name
		}
	}
	if target == "" {
		return "", nil, cyginWrapAI(errors.New("未找到可用数据库"))
	}
	var tableNames []string
	for _, db := range tree {
		if db.Name == target {
			tableNames = db.Tables
			break
		}
	}
	if len(tableNames) == 0 {
		return "", nil, cyginWrapAI(fmt.Errorf("目标库 %s 中没有可用的表，请检查连接配置的数据库名，或确认该库下存在表", target))
	}
	return target, tableNames, nil
}

// AINewSession 创建 AI 会话（绑定连接+库+tab，注入轻量库/表名录）。返回会话句柄。
// history 可选：会话重建时回放的历史对话（user/assistant 轮次）。
// sessionID 可选：指定会话 ID（会话失效重建时复用原 ID，前端无需感知）；传 "" 则自动生成。
// tabID 可选：所属 query tab（按 tab 隔离对话；空 = 不隔离）。
func (s *Service) AINewSession(ctx context.Context, connKey, dbName, tabID string, history []*schema.Message, sessionID string) (*AISession, error) {
	if !s.AIEnabled() {
		return nil, cyginWrapAI(errors.New("AI 功能未配置：请先在设置中填写 BaseURL / API Key / Model"))
	}
	conn, err := s.resolveConn(connKey, nil)
	if err != nil {
		return nil, err
	}
	if dbName == "" {
		dbName = conn.DBName
	}
	dialect := dialectLabel(conn.Type, conn.SubType)
	aiCfg := s.cfg.AI
	aiCfg.normalize()

	// 拉取目标库表名录（不注入字段结构，模型经只读工具按需查，避免「答非所问」整段倾倒 schema）
	target, tableNames, err := s.aiPickTarget(ctx, conn, dbName)
	if err != nil {
		return nil, err
	}
	sys := s.agentSystemPrompt(dialect, target, tableNames)

	sid := sessionID
	if sid == "" {
		sid = xid.New().String()
	}
	ses := &AISession{
		ID:       sid,
		ConnKey:  connKey,
		TabID:    tabID,
		DBName:   dbName,
		Dialect:  dialect,
		Messages: []*schema.Message{schema.SystemMessage(sys)},
		Created:  time.Now(),
		LastAt:   time.Now(),
		Sys:      sys,
	}
	// 回放历史对话（会话重建场景）：过滤掉 system 消息，只保留 user/assistant/tool 轮次
	for _, h := range history {
		if h == nil || h.Role == schema.System {
			continue
		}
		ses.Messages = append(ses.Messages, h)
	}
	// 防御性去重：折叠历史中连续相同的 user 消息（前端并发发送/失败重试的残留），
	// 避免重建后重复指令污染模型上下文
	ses.Messages = dedupeConsecutiveUser(ses.Messages)
	ses.Messages = trimMessages(ses.Messages)
	// agent 独立 ChatModel 实例（react 需 BindTools，不能与共享 client 混用避免绑定竞争）
	lc := llm.Config{
		BaseURL:     strings.TrimSpace(aiCfg.BaseURL),
		APIKey:      aiCfg.APIKey,
		Model:       strings.TrimSpace(aiCfg.Model),
		Temperature: aiCfg.Temperature,
		MaxTokens:   aiCfg.MaxTokens,
		Timeout:     time.Duration(aiCfg.TimeoutSec) * time.Second,
	}
	tools, err := s.buildAgentTools(*conn, aiCfg.MaxSchemaChars, ses)
	if err != nil {
		return nil, err
	}
	agent, err := llm.NewReactAgent(ctx, lc, tools, sys)
	if err != nil {
		return nil, cyginWrapAI(err)
	}
	ses.Agent = agent

	m := s.ai
	m.mu.Lock()
	// 并发会话上限 + 空闲会话回收
	if len(m.sessions) >= aiMaxConcurrent {
		now := time.Now()
		for id, old := range m.sessions {
			if now.Sub(old.LastAt) > aiIdleTTL {
				delete(m.sessions, id)
			}
		}
	}
	m.sessions[sid] = ses
	m.mu.Unlock()
	// 创建会话时顺带清理超额落盘记录（懒清理，避免 ai_session 表无限增长）
	s.AIPurgeExcessSessions()
	// 新建会话即落盘（空对话），保证会话可被列表/恢复接口发现
	s.persistSession(ses)
	s.aiDebugf("[ai] 创建会话 id=%s conn=%s db=%s dialect=%s 表数=%d sysChars=%d sys=%q",
		sid, connKey, dbName, dialect, len(tableNames), len(sys), truncLog(sys, 200))
	return ses, nil
}

// persistSession 将会话落盘（整组消息覆盖写）。失败仅告警，不阻断对话主流程。
func (s *Service) persistSession(ses *AISession) {
	if ses == nil || s.persist == nil {
		return
	}
	rec := AISessionRecord{
		ID:        ses.ID,
		ConnID:    ses.ConnKey,
		TabID:     ses.TabID,
		DB:        ses.DBName,
		Dialect:   ses.Dialect,
		Messages:  messagesToAny(ses.Messages),
		Usage:     ses.Usage,
		CreatedAt: ses.Created.UnixMilli(),
		UpdatedAt: ses.LastAt.UnixMilli(),
	}
	if err := s.persist.SaveAISession(rec); err != nil {
		cylog.Warnf("[ai] 会话落盘失败 session=%s err=%v", ses.ID, err)
	}
}

// messagesToAny 将 schema.Message 切片转 []any（用于 JSON 序列化落盘）。
func messagesToAny(msgs []*schema.Message) []any {
	out := make([]any, 0, len(msgs))
	for _, m := range msgs {
		if m != nil {
			out = append(out, *m)
		}
	}
	return out
}

// anyToMessages 将落盘的 []any 还原为 []*schema.Message（仅保留 role/content/tool 等可重放字段）。
func anyToMessages(items []any) []*schema.Message {
	msgs := make([]*schema.Message, 0, len(items))
	for _, it := range items {
		b, err := json.Marshal(it)
		if err != nil {
			continue
		}
		var m schema.Message
		if err := json.Unmarshal(b, &m); err != nil {
			continue
		}
		if m.Role == "" {
			continue
		}
		msgs = append(msgs, &m)
	}
	return msgs
}

// restoreSession 从库恢复会话（内存会话不存在时）：重建 system + 回放历史 + 复用原 ID。
// 返回恢复后的会话；无记录或恢复失败返回 nil。
func (s *Service) restoreSession(ctx context.Context, sessionID, connKey, dbName, tabID string) *AISession {
	if s.persist == nil || sessionID == "" {
		return nil
	}
	rec, ok := s.persist.LoadAISession(sessionID)
	if !ok {
		return nil
	}
	// 连接不一致（会话被挪到其他连接）时不恢复，避免上下文错乱
	if rec.ConnID != "" && connKey != "" && rec.ConnID != connKey {
		return nil
	}
	history := anyToMessages(rec.Messages)
	ses, err := s.AINewSession(ctx, connKey, dbName, tabID, history, sessionID)
	if err != nil {
		s.aiDebugf("[ai] 从库恢复会话失败 session=%s err=%v", sessionID, err)
		return nil
	}
	// 还原累计 token（供前端 usage 展示）
	if rec.Usage != nil {
		if b, merr := json.Marshal(rec.Usage); merr == nil {
			var u llm.Usage
			if uerr := json.Unmarshal(b, &u); uerr == nil {
				ses.Usage = u
			}
		}
	}
	s.aiDebugf("[ai] 从库恢复会话 session=%s conn=%s db=%s msgs=%d", sessionID, connKey, dbName, len(ses.Messages))
	return ses
}

// AIChat 非流式对话：追加用户消息 → 生成 → 追加助手消息 → 返回文本 + 本轮 usage。
// action 取值 generate/explain/fix/optimize，空视为 generate；task 为需求文本或原始 SQL。
// schemaVerified 表示本轮是否调用过 get_schema（真实表结构已验证），供前端可靠度标记。
func (s *Service) AIChat(ctx context.Context, sessionID, action, task, msgID string) (string, llm.Usage, bool, error) {
	r, err := s.aiChat(ctx, sessionID, action, task, msgID, nil, nil)
	return r.content, r.usage, r.schemaVerified, err
}

// AIChatStream 流式对话：onDelta 每收到一段增量即回调，onTool 每次工具调用开始时回调（agent 模式）。
// 返回本轮累计 usage（模型未提供时记 0）+ 本轮是否已验证真实表结构（schemaVerified）。
func (s *Service) AIChatStream(ctx context.Context, sessionID, action, task, msgID string, onDelta func(string), onTool func(string, string)) (llm.Usage, bool, error) {
	r, err := s.aiChat(ctx, sessionID, action, task, msgID, onDelta, onTool)
	return r.usage, r.schemaVerified, err
}

// AIChatStreamWithFallback 流式对话（Web 主链路）：会话不存在时，用 connID/db/tabID/history
// 透明重建会话并继续（复用原 sessionID，前端无需感知会话生命周期、也无需更新 ID）。
// 返回 usage + schemaVerified（本轮是否已验证真实表结构）。
func (s *Service) AIChatStreamWithFallback(ctx context.Context, sessionID, action, task, msgID string, connID, db, tabID string, history []*schema.Message, onDelta func(string), onTool func(string, string)) (llm.Usage, bool, error) {
	r, err := s.aiChat(ctx, sessionID, action, task, msgID, onDelta, onTool)
	if err != nil {
		// 会话不存在且提供了重建信息 → 透明重建（复用原 sessionID，回放历史）
		if connID != "" && isSessionNotFound(err) {
			s.aiDebugf("[ai] 会话失效透明重建 session=%s conn=%s db=%s tab=%s historyMsgs=%d", sessionID, connID, db, tabID, len(history))
			// 优先从库恢复（进程重启/会话被回收后，仍能延续多轮上下文）；
			// 库中无记录时退回前端回传的 history 回放重建。
			if s.restoreSession(ctx, sessionID, connID, db, tabID) == nil {
				if _, nerr := s.AINewSession(ctx, connID, db, tabID, history, sessionID); nerr != nil {
					s.aiDebugf("[ai] 透明重建失败 session=%s err=%v", sessionID, nerr)
				}
			}
			// 用原 sessionID 重跑（历史已回放，task 为当前问题）
			r2, e2 := s.aiChat(ctx, sessionID, action, task, msgID, onDelta, onTool)
			return r2.usage, r2.schemaVerified, e2
		}
		return r.usage, r.schemaVerified, err
	}
	return r.usage, r.schemaVerified, nil
}

// isSessionNotFound 判断错误是否为「会话不存在/已过期」。
func isSessionNotFound(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "会话不存在") || strings.Contains(msg, "已过期") || strings.Contains(msg, "会话已失效")
}

// aiChatResult 一轮对话的结果：文本 + usage + 本轮是否已验证真实表结构。
// schemaVerified 供前端在 SQL 代码块上展示「可靠度」图标（绿✓已验证 / 灰?未验证），
// 不再由后端硬拦截（用户自行判断是否采纳）。
type aiChatResult struct {
	content        string
	usage          llm.Usage
	schemaVerified bool
}

// findMsgIDUser 在历史中定位指定流水号（msg_id）的 user 消息。
// 返回该消息的索引与「其后是否已有 assistant 回答」：
//   - idx >= 0 且 answered=true：该发起已完整处理过（幂等命中，可重放结果）
//   - idx >= 0 且 answered=false：该发起悬空（user 已存在但无回答，可复用继续）
//   - idx < 0：历史中无此流水号
func findMsgIDUser(msgs []*schema.Message, msgID string) (idx int, answered bool) {
	if msgID == "" {
		return -1, false
	}
	for i, m := range msgs {
		if m == nil || m.Role != schema.User {
			continue
		}
		if id, _ := m.Extra["msg_id"].(string); id == msgID {
			return i, i+1 < len(msgs) && msgs[i+1] != nil && msgs[i+1].Role == schema.Assistant
		}
	}
	return -1, false
}

func (s *Service) aiChat(ctx context.Context, sessionID, action, task, msgID string, onDelta func(string), onTool func(string, string)) (aiChatResult, error) {
	ses, err := s.getSession(sessionID)
	if err != nil {
		return aiChatResult{}, err
	}
	if strings.TrimSpace(task) == "" {
		return aiChatResult{}, cyginWrapAI(errors.New("请输入需求描述"))
	}
	// 会话对话互斥：同一会话的并发对话串行化，避免 Messages 切片并发读写（data race）。
	// 锁持有整个对话周期（含 agent 多轮工具调用），并发请求会排队等待而非交错破坏状态。
	ses.mu.Lock()
	defer ses.mu.Unlock()

	// 可靠度追踪：schemaQueried 为「会话级」标记——会话内一旦成功调用过 get_schema 即置 true，
	// 后续轮次复用历史查过的表结构时同样可信（不必每轮重新查询）。
	// 不再每轮复位；仅「重置会话」时清空（见 AIResetSession）。

	// 任务指令固定由后端拼装（用户不直接接触 system prompt 的修改权）。
	// 同时把「原始输入 raw」与「动作类型 action」写入 Extra，供前端恢复历史时
	// 还原纯 SQL / 需求文本 + 展示动作标签（避免恢复后只剩带指令前缀的长文本）。
	userText := llm.ActionPrompt(action, task)
	userMsg := schema.UserMessage(userText)
	userMsg.Extra = map[string]any{"action": action, "raw": task}
	if msgID != "" {
		userMsg.Extra["msg_id"] = msgID
	}

	// 流水号幂等：每个发起（msg_id，前端按会话递增生成）在历史中只允许出现一次。
	// 覆盖：网络重试/重复请求（已处理完 → 重放结果，不写历史）、透明重建回放（悬空 → 复用继续）、
	// 失败残留（尾部不同流水号的悬空 user → 原位替换）。
	if msgID != "" {
		if idx, answered := findMsgIDUser(ses.Messages, msgID); idx >= 0 {
			if answered {
				// 该发起已完整处理过：直接重放已有回答（幂等命中），历史不变
				content := ses.Messages[idx+1].Content
				if onDelta != nil {
					onDelta(content)
				}
				s.aiDebugf("[ai] 流水号幂等命中（已处理），重放结果 session=%s msgID=%s", sessionID, msgID)
				return aiChatResult{content: content, schemaVerified: ses.schemaQueried.Load()}, nil
			}
			// 该发起悬空（重建回放后重复到达）：保留该 user，丢弃其后的未完成残留后继续处理
			ses.Messages = ses.Messages[:idx+1]
			ses.Messages = trimMessages(ses.Messages)
			s.aiDebugf("[ai] 流水号悬空（重建回放重复），复用该 user 继续 session=%s msgID=%s", sessionID, msgID)
			return s.aiAgentChat(ctx, ses, action, onDelta, onTool)
		}
		// 未命中：尾部悬空 user（其他发起失败残留）原位替换，不留悬空
		if n := len(ses.Messages); n > 0 {
			if last := ses.Messages[n-1]; last != nil && last.Role == schema.User {
				s.aiDebugf("[ai] 尾部悬空 user（不同流水号），原位替换 session=%s msgID=%s", sessionID, msgID)
				ses.Messages[n-1] = userMsg
				ses.Messages = trimMessages(ses.Messages)
				return s.aiAgentChat(ctx, ses, action, onDelta, onTool)
			}
		}
		ses.Messages = append(ses.Messages, userMsg)
		ses.Messages = trimMessages(ses.Messages)
		s.aiDebugf("[ai] 对话开始 session=%s action=%s task=%q msgID=%s msgs=%d", sessionID, action, truncLog(task, 200), msgID, len(ses.Messages))
		return s.aiAgentChat(ctx, ses, action, onDelta, onTool)
	}

	// msgID 为空（CLI / 旧调用方）：退回内容比较兜底
	if n := len(ses.Messages); n > 0 {
		if last := ses.Messages[n-1]; last != nil && last.Role == schema.User {
			if raw, _ := last.Extra["raw"].(string); raw == task {
				s.aiDebugf("[ai] 检测到悬空 user 且与当前任务相同，跳过重复追加 session=%s task=%q", sessionID, truncLog(task, 200))
				ses.Messages = trimMessages(ses.Messages)
				return s.aiAgentChat(ctx, ses, action, onDelta, onTool)
			}
			s.aiDebugf("[ai] 检测到悬空 user（失败残留），原位替换为当前任务 session=%s", sessionID)
			ses.Messages[n-1] = userMsg
			ses.Messages = trimMessages(ses.Messages)
			return s.aiAgentChat(ctx, ses, action, onDelta, onTool)
		}
	}
	ses.Messages = append(ses.Messages, userMsg)
	ses.Messages = trimMessages(ses.Messages)
	s.aiDebugf("[ai] 对话开始 session=%s action=%s task=%q taskChars=%d msgs=%d", sessionID, action, truncLog(task, 200), len(task), len(ses.Messages))

	// 统一走 React Agent：模型可调只读工具探索元数据后生成 SQL
	r, err := s.aiAgentChat(ctx, ses, action, onDelta, onTool)
	if err != nil {
		// 失败回滚本轮 user 消息：失败轮次不残留，避免用户重试/重发后同一需求重复累积，
		// 保持后端历史与前端截断重试的一致性
		if n := len(ses.Messages); n > 0 && ses.Messages[n-1] == userMsg {
			ses.Messages = ses.Messages[:n-1]
			s.persistSession(ses)
		}
		return aiChatResult{}, err
	}
	return r, nil
}

// aiAgentChat 对话：React Agent 循环执行，工具事件经 ToolSink 透传，最终答案作为 assistant 消息持久化。
// 返回 schemaVerified：会话内是否已调用过 get_schema（真实表结构已验证过），供前端可靠度标记。
func (s *Service) aiAgentChat(ctx context.Context, ses *AISession, action string, onDelta func(string), onTool func(string, string)) (aiChatResult, error) {
	start := time.Now()
	ses.ToolSink.Store(onTool)
	content, usage, err := ses.Agent.Stream(ctx, ses.Messages, llm.AgentCallbacks{
		OnContent: onDelta,
	})
	ses.ToolSink.Store((func(string, string))(nil))
	if err != nil {
		s.aiDebugf("[ai] Agent 对话失败 session=%s 耗时=%s err=%v", ses.ID, time.Since(start).Round(time.Millisecond), err)
		return aiChatResult{}, cyginWrapAI(err)
	}
	if strings.TrimSpace(content) == "" {
		return aiChatResult{}, cyginWrapAI(errors.New("模型返回空结果"))
	}
	// 校验：仅拦截「只输出思考过程、无有效答案」的无效回复；
	// 「是否查过真实表结构」不再硬拦截，改为随结果返回 schemaVerified，由前端图标提示可靠度。
	if reason := validateAgentResult(content); reason != "" {
		s.aiDebugf("[ai] 结果校验拦截 session=%s action=%s reason=%s content=%q", ses.ID, action, reason, truncLog(content, 200))
		return aiChatResult{}, cyginWrapAI(errors.New(reason))
	}
	// 本轮已验证标记：读取 get_schema 是否在本轮被调用过
	schemaVerified := ses.schemaQueried.Load()

	// 持久化最终答案（工具调用的中间过程不落盘，下一轮由模型按需重新探索）
	ses.Messages = append(ses.Messages, schema.AssistantMessage(content, nil))
	ses.Messages = trimMessages(ses.Messages)
	ses.Usage.Add(usage)
	ses.LastAt = time.Now()
	m := s.ai
	m.mu.Lock()
	m.procUsage.Add(usage)
	m.mu.Unlock()
	// 每轮对话完成后落盘（覆盖写整组消息），保证页面刷新/进程重启后可恢复
	s.persistSession(ses)
	s.aiDebugf("[ai] Agent 对话完成 session=%s 耗时=%s contentChars=%d schemaVerified=%v usage=prompt=%d/completion=%d/total=%d",
		ses.ID, time.Since(start).Round(time.Millisecond), len(content), schemaVerified,
		usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens)
	return aiChatResult{content: content, usage: usage, schemaVerified: schemaVerified}, nil
}

// validateAgentResult 校验 agent 最终结果，拦截模型「抢答」导致的无效输出。
// 返回非空字符串表示校验失败（即错误信息），空串表示通过。
// 只拦截一类失败：思考标签泄漏但无有效答案（模型只吐了 <think>/<thinking>/<reasoning> 思考段、没给结果）。
//
// 「是否查过真实表结构」不再硬拦截：字段是否臆造由用户结合可靠度图标自行判断
// （见 aiAgentChat 返回的 schemaVerified）。
//
// 注意：若模型同时输出了思考段和有效答案（思考段在前、答案在后），属于可容忍的「违规但可用」，
// 不再整体拒绝——前端会用 parseThinking 剥离思考段，只展示答案部分。
func validateAgentResult(content string) string {
	// 剥离思考段后，若已无有效答案（只剩思考/空），判定为失败
	answer := stripThinking(content)
	if strings.TrimSpace(answer) == "" {
		return "模型只输出了思考过程，未给出结果，请重试"
	}
	return ""
}

// stripThinking 剥离模型输出中的思考段，返回剩余答案部分。
// 支持：<thinking>/<think>/<reasoning>/<thought> 成对或未闭合标签，以及 MiniMax 的 <｜...｜> 标记。
// 仅用于「判断是否还有有效答案」，不修改最终展示内容（展示层由前端 parseThinking 处理）。
func stripThinking(content string) string {
	s := content
	// 成对标签：移除 <tag>任意内容</tag>
	paired := regexp.MustCompile(`(?is)<(?:think|thinking|reasoning|thought)>[\s\S]*?</(?:think|thinking|reasoning|thought)>`)
	s = paired.ReplaceAllString(s, "")
	// MiniMax 特殊标记：<｜...｜> 段整体移除（含  start/end of thinking）
	mm := regexp.MustCompile(`(?is)<｜[^>]*?｜>`)
	s = mm.ReplaceAllString(s, "")
	// 未闭合的开标签（流式截断常见）：从开标签位置截断到末尾（其后内容视为思考）
	open := regexp.MustCompile(`(?is)<(?:think|thinking|reasoning|thought)>`)
	if loc := open.FindStringIndex(s); loc != nil {
		s = s[:loc[0]]
	}
	return s
}

// agentSystemPrompt 构建 system prompt：库/表名录 + 完整表结构（schemaText）+ 工具使用约束。
// schemaComplete 表示表结构是否完整注入（无截断）：
//   - true：表结构已全部提供，模型应直接使用其中的真实字段，禁止再臆造；
//   - false：表结构超预算被截断，仍需强调先调 get_schema 按需查询。
//
// 用户自定义 prompt（若配置）作为 base 前缀注入（替换 {dialect}），工具约束与元数据始终追加，
// 保证 agent 模式必要的工具调用规则不被自定义 prompt 覆盖或遗漏。
func (s *Service) agentSystemPrompt(dialect, target string, tableNames []string) string {
	const maxListTables = 30
	var b strings.Builder
	custom := strings.TrimSpace(s.cfg.AI.SystemPrompt)
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

// ---- Agent 只读工具（复用 engine 元数据，无 SQL 执行能力） ----

// agentToolArgsListTables list_tables 工具参数。
type agentToolArgsListTables struct {
	DB string `json:"db" jsonschema:"description=数据库名（Oracle 为 schema 名）,required"`
}

// agentToolArgsSchema get_schema 工具参数。
type agentToolArgsSchema struct {
	DB    string `json:"db" jsonschema:"description=数据库名（Oracle 为 schema 名）,required"`
	Table string `json:"table" jsonschema:"description=表名,required"`
}

// buildAgentTools 构建三个只读探索工具（闭包捕获会话，用于工具事件透传）。
func (s *Service) buildAgentTools(conn DBConnInfo, maxSchemaChars int, ses *AISession) ([]tool.InvokableTool, error) {
	notify := func(name, args string) {
		if fn, ok := ses.ToolSink.Load().(func(string, string)); ok && fn != nil {
			fn(name, args)
		}
	}

	listDBs, err := utils.InferTool("list_databases",
		"列出当前连接可访问的所有数据库（Oracle 为 schema 列表）。仅当确认需要跨库查询时才调用，默认应优先使用当前库。",
		func(ctx context.Context, _ struct{}) (string, error) {
			notify("list_databases", "")
			tree, err := engine.GetTableTree(conn)
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
		return nil, cyginWrapAI(fmt.Errorf("构建工具 list_databases 失败: %w", err))
	}

	listTables, err := utils.InferTool("list_tables",
		"列出指定数据库中的全部表名。",
		func(ctx context.Context, args agentToolArgsListTables) (string, error) {
			notify("list_tables", args.DB)
			sub := conn
			sub.DBName = args.DB
			tree, err := engine.GetTableTree(sub)
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
		return nil, cyginWrapAI(fmt.Errorf("构建工具 list_tables 失败: %w", err))
	}

	getSchema, err := utils.InferTool("get_schema",
		"获取指定表的结构摘要（表注释 + 字段名/类型/可空/注释）。",
		func(ctx context.Context, args agentToolArgsSchema) (string, error) {
			notify("get_schema", args.DB+"."+args.Table)
			// 先校验库名（大小写不敏感），避免模型拼错库名时拿到含糊的 not found
			tree, err := engine.GetTableTree(conn)
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
			meta, err := engine.GetTableMeta(sub, args.Table)
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
			// 成功拿到真实表结构后置位标记，供结果校验判断模型是否跳过工具探索
			ses.schemaQueried.Store(true)
			// 用完整版渲染（不过滤敏感列）：工具语义是返回真实表结构，
			// 过滤会让模型误判字段不存在（如 email/mobile/password_*），导致臆造或报错
			return llm.BuildSchemaTextFull([]llm.TableInfo{ti}, 1, maxSchemaChars), nil
		})
	if err != nil {
		return nil, cyginWrapAI(fmt.Errorf("构建工具 get_schema 失败: %w", err))
	}
	return []tool.InvokableTool{listDBs, listTables, getSchema}, nil
}

// AIProcessUsage 返回进程级累计 token（服务启动以来所有会话的总消耗）。
func (s *Service) AIProcessUsage() llm.Usage {
	m := s.ai
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.procUsage
}

// AISessionUsage 返回会话累计 token 消耗。
func (s *Service) AISessionUsage(sessionID string) (llm.Usage, bool) {
	m := s.ai
	m.mu.Lock()
	defer m.mu.Unlock()
	ses, ok := m.sessions[sessionID]
	if !ok {
		return llm.Usage{}, false
	}
	return ses.Usage, true
}

// AIResetSession 清空会话消息（重建 system prompt）与累计 token。
func (s *Service) AIResetSession(sessionID string) error {
	m := s.ai
	m.mu.Lock()
	defer m.mu.Unlock()
	ses, ok := m.sessions[sessionID]
	if !ok {
		return cyginWrapAI(fmt.Errorf("会话不存在: %s", sessionID))
	}
	// 重建名录 system，复用已有 Agent（model + tools 不变）
	ses.mu.Lock()
	ses.Messages = []*schema.Message{schema.SystemMessage(ses.Sys)}
	ses.Usage = llm.Usage{}
	ses.LastAt = time.Now()
	ses.schemaQueried.Store(false)
	ses.mu.Unlock()
	s.persistSession(ses)
	return nil
}

// AIDeleteSession 删除会话。
func (s *Service) AIDeleteSession(sessionID string) error {
	m := s.ai
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[sessionID]; !ok {
		return cyginWrapAI(fmt.Errorf("会话不存在: %s", sessionID))
	}
	delete(m.sessions, sessionID)
	// 同步删除落盘记录
	if s.persist != nil {
		_ = s.persist.DeleteAISession(sessionID)
	}
	return nil
}

func (s *Service) getSession(sessionID string) (*AISession, error) {
	m := s.ai
	m.mu.Lock()
	defer m.mu.Unlock()
	ses, ok := m.sessions[sessionID]
	if !ok {
		return nil, cyginWrapAI(fmt.Errorf("会话不存在或已过期，请重新创建"))
	}
	return ses, nil
}

// ---- AI 会话持久化对外接口（供 Web 层） ----

// AIListSessions 列出某连接（可选指定 tab）的会话（新→旧，仅元信息，供前端恢复历史对话选择）。
func (s *Service) AIListSessions(connID, tabID string) []AISessionRecord {
	if s.persist == nil {
		return []AISessionRecord{}
	}
	return s.persist.ListAISessions(connID, tabID)
}

// AILoadSessionHistory 读取某会话的对话历史（role/content 序列，供前端恢复展示）。
// 返回 nil 表示无记录。system 消息与空内容消息已过滤，仅返回 user/assistant 轮次。
func (s *Service) AILoadSessionHistory(sessionID string) []AISessionRecord {
	if s.persist == nil {
		return nil
	}
	rec, ok := s.persist.LoadAISession(sessionID)
	if !ok {
		return nil
	}
	return []AISessionRecord{rec}
}

// AIDeleteSessionByTab 删除某连接下指定 tab 的会话（tab 关闭时调用），同步清理内存会话。
func (s *Service) AIDeleteSessionByTab(connID, tabID string) {
	if s.persist != nil {
		_ = s.persist.DeleteAISessionByTab(connID, tabID)
	}
	// 同步清理内存中该 tab 的会话（若有）
	m := s.ai
	m.mu.Lock()
	for id, ses := range m.sessions {
		if ses.ConnKey == connID && ses.TabID == tabID {
			delete(m.sessions, id)
		}
	}
	m.mu.Unlock()
}

// AIPurgeExcessSessions 清理超额会话：
// 仅当某连接会话数 > aiMaxSessionsPerConn 且某会话超过 aiSessionKeepDays 天未活动时清理；
// 否则（tab 未关闭）永久保留。在会话创建等时机调用，防止孤儿会话无限增长。
func (s *Service) AIPurgeExcessSessions() int64 {
	if s.persist == nil {
		return 0
	}
	n, err := s.persist.PurgeExcessAISessions(aiMaxSessionsPerConn, aiSessionKeepDays)
	if err != nil {
		cylog.Warnf("[ai] 清理超额会话失败: %v", err)
		return 0
	}
	if n > 0 {
		s.aiDebugf("[ai] 清理超额会话 %d 条", n)
	}
	return n
}

// trimMessages 控制会话上下文规模，避免历史过度膨胀。分两级：
//  1. 字符预算（aiMaxHistoryChars，约 6K token）：超预算时按轮次裁剪最旧一组，
//     但跳过含 SQL 代码块的关键轮次（SQL 是精确信息，不能先丢）。
//  2. 条数上限（aiMaxMessages）：最终兜底，防止极端情况下消息条数失控。
//
// 一组 = 一条 User 到下一个 User 之前的所有消息（agent 模式可含工具调用与结果）。
// 保留索引 0 的 system 消息。
// 返回裁剪后的切片（必须由调用方回写，否则裁剪不生效且可能覆盖底层数组）。
func trimMessages(msgs []*schema.Message) []*schema.Message {
	// 第 1 级：字符预算裁剪（优先丢「无 SQL 的旧轮次」，保留含 SQL 的关键轮次）
	for historyChars(msgs) > aiMaxHistoryChars && len(msgs) > 2 {
		s, e := trimTarget(msgs)
		if s < 0 {
			break // 没有可裁剪的旧轮次（只剩当前轮）
		}
		msgs = append(msgs[:s], msgs[e:]...)
	}
	// 第 2 级：条数兜底（超限时按轮次裁剪最旧一组，无论是否含 SQL）
	for len(msgs) > aiMaxMessages {
		cut := trimOldestRound(msgs)
		if cut < 0 {
			break
		}
		msgs = append(msgs[:1], msgs[cut:]...)
	}
	return msgs
}

// dedupeConsecutiveUser 折叠历史中「连续相同内容」的 user 消息，只保留最后一条。
// 背景：前端并发发送/失败重试会在历史中残留多条相同 user（中间无 assistant），
// 模型看到连续重复的用户指令会困惑并复读旧回答。仅折叠相邻相同消息，不跨轮次去重。
func dedupeConsecutiveUser(msgs []*schema.Message) []*schema.Message {
	out := make([]*schema.Message, 0, len(msgs))
	for _, m := range msgs {
		if m == nil {
			continue
		}
		if len(out) > 0 && m.Role == schema.User && out[len(out)-1] != nil && out[len(out)-1].Role == schema.User {
			if sameUserText(m, out[len(out)-1]) {
				out[len(out)-1] = m // 内容相同：替换为新的（保留最新字段）
				continue
			}
		}
		out = append(out, m)
	}
	return out
}

// sameUserText 比较两条 user 消息是否为同一需求：优先用 extra.raw（原始输入），
// 缺失时退回比较完整 content（回放历史可能不带 Extra）。
func sameUserText(a, b *schema.Message) bool {
	ar, _ := a.Extra["raw"].(string)
	br, _ := b.Extra["raw"].(string)
	if ar != "" && br != "" {
		return ar == br
	}
	return a.Content == b.Content
}

// historyChars 累计所有消息的字符数（近似 token 预算，中文 1 字≈1 token，英文偏保守）。
func historyChars(msgs []*schema.Message) int {
	total := 0
	for _, m := range msgs {
		if m != nil {
			total += len([]rune(m.Content))
		}
	}
	return total
}

// trimTarget 找到「最旧的、不含 SQL 的旧轮次」的起止索引 [start, end)，
// 优先裁剪这类轮次（它们是纯对话，信息价值低）；若所有旧轮次都含 SQL，
// 则退回裁剪最旧一轮（含 SQL，兜底防膨胀）。
// 返回 [start, end)；start<0 表示没有可裁剪的旧轮次（只剩当前轮，保护当前上下文）。
func trimTarget(msgs []*schema.Message) (int, int) {
	// 收集所有「旧轮次」的 [start, end) 区间（不含当前轮）
	type rng struct{ s, e int }
	var rounds []rng
	start := 1
	for start < len(msgs) {
		// 找该轮起点（下一个 User）
		for start < len(msgs) && msgs[start].Role != schema.User {
			start++
		}
		if start >= len(msgs) {
			break
		}
		s := start
		// 该轮结束：下一个 User
		end := start + 1
		for end < len(msgs) && msgs[end].Role != schema.User {
			end++
		}
		// 该轮就是当前轮（最后一个 user 轮次）：不纳入可裁剪范围
		if end >= len(msgs) {
			break
		}
		rounds = append(rounds, rng{s, end})
		start = end
	}
	if len(rounds) == 0 {
		return -1, -1
	}
	// 优先找第一个「不含 SQL」的旧轮次
	for _, r := range rounds {
		if !groupHasSQL(msgs[r.s:r.e]) {
			return r.s, r.e
		}
	}
	// 所有旧轮次都含 SQL：裁最旧一轮（兜底）
	return rounds[0].s, rounds[0].e
}

// groupHasSQL 判断一组消息（一个轮次）是否包含「高价值内容」：SQL 代码块，或 get_schema 工具结果。
//
// 背景：get_schema 工具返回的真实表结构是后续多轮对话「不再幻觉字段」的唯一依据。
// 若仅以「是否含 ```sql」判定价值，get_schema 查询轮次会被当作「无 SQL 的低价值轮次」
// 在字符预算超限时优先裁掉，导致模型丢失真实表结构、转而幻觉复读错误 SQL。
// 因此把「含 get_schema 工具结果」的轮次同样视为高价值，避免被优先裁剪。
func groupHasSQL(group []*schema.Message) bool {
	for _, m := range group {
		if m == nil {
			continue
		}
		if strings.Contains(m.Content, "```sql") {
			return true
		}
		// get_schema 工具结果消息：role=tool 且 tool_name=get_schema，内容为真实表结构
		if m.Role == schema.Tool && m.ToolName == "get_schema" {
			return true
		}
	}
	return false
}

// trimOldestRound 裁剪最旧一轮（无论是否含 SQL），返回裁剪后保留消息的起始索引（不含 system）。
// 返回 -1 表示只剩当前轮，不裁剪。
func trimOldestRound(msgs []*schema.Message) int {
	i := 1
	for ; i < len(msgs); i++ {
		if msgs[i].Role == schema.User {
			break
		}
	}
	if i >= len(msgs) {
		return -1
	}
	j := i + 1
	for ; j < len(msgs); j++ {
		if msgs[j].Role == schema.User {
			break
		}
	}
	if j >= len(msgs) {
		return -1
	}
	return j
}

// dialectLabel 生成 prompt 中使用的方言标签。
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

// cyginWrapAI 错误统一出口（当前直接透传；后续如需附加上下文在此扩展）。
func cyginWrapAI(err error) error {
	return err
}
