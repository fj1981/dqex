package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"dqex/internal/service"

	"github.com/cloudwego/eino/schema"
	"github.com/gin-gonic/gin"
	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cygin"
	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cylog"
)

// ==================== AI 辅助 SQL API ====================

// truncLog 调试日志截断：超长文本只保留前 n 个字符，避免刷屏。
func truncLog(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…(截断)"
	}
	return s
}

// handleAIStatus AI 能力状态（前端据此门控 AI 面板入口）。
func handleAIStatus(svc *service.Service) gin.HandlerFunc {
	return cygin.Handle(func(c *gin.Context, _ struct{}) (service.AIStatus, error) {
		return svc.AIStatus(), nil
	})
}

// handleAIProviders AI 厂商预设列表（前端下拉选择用，标记 builtin 供管理界面展示）。
func handleAIProviders(svc *service.Service) gin.HandlerFunc {
	return cygin.Handle(func(c *gin.Context, _ struct{}) ([]service.AIProvider, error) {
		return service.AIProviders(), nil
	})
}

// handleAISaveProviders 保存自定义厂商配置（前端管理界面提交）。
func handleAISaveProviders(svc *service.Service) gin.HandlerFunc {
	return cygin.Handle(func(c *gin.Context, req struct {
		Providers []service.AIProviderItem `json:"providers" binding:"required"`
	}) (any, error) {
		if err := service.SaveAIProviders(req.Providers); err != nil {
			return nil, err
		}
		return gin.H{"ok": true}, nil
	})
}

// handleAIProcessUsage 查询进程级累计 token（服务启动以来所有会话总消耗）。
func handleAIProcessUsage(svc *service.Service) gin.HandlerFunc {
	return cygin.Handle(func(c *gin.Context, _ struct{}) (any, error) {
		return gin.H{"processUsage": svc.AIProcessUsage()}, nil
	})
}

// HistoryItem 重建回放的历史消息条目：role/content 之外，user 消息携带 msgId 流水号
// （前端按会话递增生成），供重建后幂等检测去重。
type HistoryItem struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	MsgID   string `json:"msgId"`
}

// buildHistory 将请求中的历史消息组装为 schema.Message 列表，user 消息的 msg_id
// 写入 Extra（幂等检测依据）；前端重开还原/后端重启重建时流水号不丢失。
func buildHistory(items []HistoryItem) []*schema.Message {
	history := make([]*schema.Message, 0, len(items))
	for _, h := range items {
		switch h.Role {
		case "user":
			m := schema.UserMessage(h.Content)
			if h.MsgID != "" {
				m.Extra = map[string]any{"msg_id": h.MsgID}
			}
			history = append(history, m)
		case "assistant":
			history = append(history, schema.AssistantMessage(h.Content, nil))
		}
	}
	return history
}

// AISessionReq 创建 AI 会话请求。
type AISessionReq struct {
	ConnID string `json:"connId" binding:"required"`
	DB     string `json:"db"`
	// TabID 可选：所属 query tab（按 tab 隔离对话；空 = 不隔离）。
	TabID string `json:"tabId"`
	// History 可选：会话重建时回放的历史消息（role/content/msgId），保留多轮上下文。
	History []HistoryItem `json:"history"`
}

// handleAICreateSession 创建 AI 会话（绑定连接+库+tab，预载表结构上下文）。
func handleAICreateSession(svc *service.Service) gin.HandlerFunc {
	return cygin.Handle(func(c *gin.Context, req AISessionReq) (any, error) {
		start := time.Now()
		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()
		// 组装历史消息（会话重建场景）
		history := buildHistory(req.History)
		ses, err := svc.AINewSession(ctx, cygin.FromCtx(c), req.ConnID, req.DB, req.TabID, history, "")
		if err != nil {
			cylog.Debugf("[ai] 创建会话失败 conn=%s db=%s tab=%s err=%v", req.ConnID, req.DB, req.TabID, err)
			return nil, renderErr(c, err)
		}
		cylog.Debugf("[ai] 创建会话完成 session=%s db=%s tab=%s dialect=%s 耗时=%s",
			ses.ID, ses.DBName, ses.TabID, ses.Dialect, time.Since(start).Round(time.Millisecond))
		return gin.H{"sessionID": ses.ID, "dbName": ses.DBName, "dialect": ses.Dialect}, nil
	})
}

// AISessionIDReq 以路径参数定位会话。
type AISessionIDReq struct {
	ID string `uri:"id" binding:"required"`
}

// handleAIDeleteSession 删除 AI 会话。
func handleAIDeleteSession(svc *service.Service) gin.HandlerFunc {
	return cygin.Handle(func(c *gin.Context, req AISessionIDReq) (any, error) {
		if err := svc.AIDeleteSession(req.ID); err != nil {
			return nil, renderErr(c, err)
		}
		return gin.H{"ok": true}, nil
	})
}

// handleAIResetSession 重置 AI 会话（清空上下文与 token 统计）。
func handleAIResetSession(svc *service.Service) gin.HandlerFunc {
	return cygin.Handle(func(c *gin.Context, req AISessionIDReq) (any, error) {
		if err := svc.AIResetSession(req.ID); err != nil {
			return nil, renderErr(c, err)
		}
		return gin.H{"ok": true}, nil
	})
}

// handleAISessionUsage 查询会话累计 token。
func handleAISessionUsage(svc *service.Service) gin.HandlerFunc {
	return cygin.Handle(func(c *gin.Context, req AISessionIDReq) (any, error) {
		usage, ok := svc.AISessionUsage(req.ID)
		if !ok {
			return nil, cygin.NewError(service.ErrAISessionNotFound)
		}
		return gin.H{"usage": usage}, nil
	})
}

// handleAIListSessions 列出某连接（可选指定 tab）的会话（新→旧，仅元信息），供前端恢复历史对话。
func handleAIListSessions(svc *service.Service) gin.HandlerFunc {
	return cygin.Handle(func(c *gin.Context, req struct {
		ConnID string `form:"connId" binding:"required"`
		TabID  string `form:"tabId"`
	}) (any, error) {
		return gin.H{"sessions": svc.AIListSessions(req.ConnID, req.TabID)}, nil
	})
}

// handleAIDeleteSessionByTab 删除某连接下指定 tab 的会话（tab 关闭时调用）。
func handleAIDeleteSessionByTab(svc *service.Service) gin.HandlerFunc {
	return cygin.Handle(func(c *gin.Context, req struct {
		ConnID string `form:"connId" binding:"required"`
		TabID  string `form:"tabId" binding:"required"`
	}) (any, error) {
		svc.AIDeleteSessionByTab(req.ConnID, req.TabID)
		return gin.H{"ok": true}, nil
	})
}

// handleAISessionHistory 读取某会话的对话历史（含 role/content 消息与 usage），供前端恢复展示。
func handleAISessionHistory(svc *service.Service) gin.HandlerFunc {
	return cygin.Handle(func(c *gin.Context, req AISessionIDReq) (any, error) {
		recs := svc.AILoadSessionHistory(req.ID)
		if recs == nil {
			return nil, cygin.NewError(service.ErrAISessionNotFound)
		}
		return gin.H{"session": recs[0]}, nil
	})
}

// AIChatReq 对话请求。
type AIChatReq struct {
	SessionID string `json:"sessionID" binding:"required"`
	Text      string `json:"text" binding:"required"`
	// Action 任务类型：generate（默认）/ explain / fix / optimize / continue
	Action string `json:"action"`
	// MsgID 本次发起的唯一流水号（前端按会话递增），后端据此幂等去重：
	// 同一流水号已处理时重放结果、悬空时复用继续，杜绝重复消息污染上下文。
	MsgID string `json:"msgId"`
	// ConnID / DB / TabID：会话失效时后端据此透明重建（保留多轮上下文，按 tab 隔离）。
	ConnID string `json:"connId"`
	DB     string `json:"db"`
	TabID  string `json:"tabId"`
	// History 可选：当前对话历史（会话重建时回放，保留多轮上下文）。
	History []HistoryItem `json:"history"`
}

// handleAIChat 非流式对话（供降级使用；Web 主链路走 SSE）。
func handleAIChat(svc *service.Service) gin.HandlerFunc {
	return cygin.Handle(func(c *gin.Context, req AIChatReq) (any, error) {
		action := req.Action
		if action == "" {
			action = "generate"
		}
		content, usage, schemaVerified, err := svc.AIChat(c.Request.Context(), req.SessionID, action, req.Text, req.MsgID)
		if err != nil {
			return nil, renderErr(c, err)
		}
		return gin.H{"content": content, "usage": usage, "schemaVerified": schemaVerified}, nil
	})
}

// handleAIChatStream SSE 流式对话：
//   - event: delta   data: {"delta":"..."}   （打字机增量）
//   - event: tool    data: {"name":"...","args":"..."} （agent 模式工具调用开始）
//   - event: done    data: {"usage":{...}}   （流结束，携带会话累计 usage）
//   - event: error   data: {"message":"..."} （出错）
//
// 客户端断开连接时通过 ctx 取消上游调用。
func handleAIChatStream(c *gin.Context, svc *service.Service) {
	var req AIChatReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "msg": err.Error()})
		return
	}
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "msg": cyginMsg(c, service.ErrStreamUnsupported)})
		return
	}
	// 显式超时兜底：上游模型无响应（TTFB 过长/服务挂起）时不能无限挂起，
	// 超时后走下方 error 分支通知前端复位加载态。时长取 ai.timeout_sec 配置。
	timeout := time.Duration(svc.AIStatus().TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
	defer cancel()

	action := req.Action
	if action == "" {
		action = "generate"
	}
	start := time.Now()
	firstDelta := true
	cylog.Debugf("[ai] SSE 收到请求 session=%s action=%s text=%q textChars=%d", req.SessionID, action, truncLog(req.Text, 200), len(req.Text))

	// 工具可被 agent 并行调用，多个 goroutine 会并发触发 onTool/onDelta。这里用
	// 「事件队列 + 单一 writer goroutine」解耦：生产者只往 channel 投递事件（不碰网络），
	// 由独立 goroutine 串行写 SSE，天然无竞态、不阻塞工具执行，也避免慢客户端拖垮工具。
	//
	// 结束语义保证：done/error 结束事件走「必达」通道，绝不因队列满被丢弃；
	// 且由 defer 兜底，任何 return 路径（成功/失败/超时/panic）都会发一个结束事件，
	// 确保前端不会一直停留在加载态。
	type sseEvent struct {
		event   string
		payload any
	}
	events := make(chan sseEvent, 256)

	// push 投递中间态事件（delta/tool）：队列满时丢弃，不阻塞工具执行，也不影响结束事件。
	// 用 recover 防 close(events) 后仍被 push（理论上 finish 后不会再有 push，防御性兜底）。
	push := func(event string, payload any) {
		defer func() {
			if recover() != nil {
				// events 已 close：忽略
			}
		}()
		select {
		case events <- sseEvent{event: event, payload: payload}:
		default:
		}
	}

	// 单 writer：串行消费队列写 SSE。退出时机只有两种：写完 done/error 结束事件、
	// 或写失败（客户端断开）。ctx 取消不直接退出——因为 finish 保证会投递结束事件，
	// 若 writer 因 ctx.Done 抢跑退出，结束事件会写不出去（前端卡在加载态）。
	// 独立 goroutine 必须 recover：未捕获的 goroutine panic 会导致整个进程崩溃。
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		defer func() {
			if r := recover(); r != nil {
				cylog.Errorf("[ai] SSE writer panic session=%s err=%v", req.SessionID, r)
				cancel()
			}
		}()
		for {
			ev, ok := <-events
			if !ok {
				// 队列关闭且已排空：无结束事件可发（异常兜底路径会补发，此处仅防御）
				return
			}
			data, merr := json.Marshal(ev.payload)
			if merr != nil {
				continue
			}
			if _, werr := fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", ev.event, data); werr != nil {
				cancel() // 写失败即客户端已断开
				return
			}
			flusher.Flush()
			if ev.event == "done" || ev.event == "error" {
				return // 结束事件已写出，writer 退出
			}
		}
	}()

	// finish 投递结束事件：用「必达」通道（阻塞投递，直到 writer 消费或超时兜底），
	// 确保结束事件绝不被丢弃。任何路径退出前都必须调用一次。
	// 投递后 close(events) 让 writer 排空队列后正常退出；等待 writer 有超时兜底，
	// 避免 writer 卡在 Flush（半开连接）时 handler 死锁。
	var finishOnce sync.Once
	finish := func(event string, payload any) {
		finishOnce.Do(func() {
			select {
			case events <- sseEvent{event: event, payload: payload}:
			case <-time.After(5 * time.Second):
				// writer 长时间未消费（已死/卡死）：无需再等，直接放弃
			}
			close(events) // 通知 writer 队列已结束
			select {
			case <-writerDone:
			case <-time.After(5 * time.Second):
				// writer 卡在 Flush 阻塞：放弃等待，避免 handler 死锁
			}
		})
	}

	// finished 语义：已成功调用 finish（而非"进入结束分支"），
	// 确保「设置了 finished 但未发事件」的窗口不存在。
	var finished bool
	defer func() {
		if r := recover(); r != nil {
			cylog.Errorf("[ai] SSE panic session=%s err=%v", req.SessionID, r)
		}
		if !finished {
			// 异常兜底：panic 或提前 return 时补发 error 结束事件
			finish("error", gin.H{"message": cyginMsg(c, service.ErrServiceException)})
		}
	}()

	// 组装历史消息（会话失效透明重建时回放，保留多轮上下文）
	history := buildHistory(req.History)

	usage, schemaVerified, err := svc.AIChatStreamWithFallback(ctx, cygin.FromCtx(c), req.SessionID, action, req.Text, req.MsgID, req.ConnID, req.DB, req.TabID, history, func(delta string) {
		if firstDelta {
			firstDelta = false
			cylog.Debugf("[ai] SSE 首字节 耗时=%s deltaChars=%d", time.Since(start).Round(time.Millisecond), len(delta))
		}
		push("delta", gin.H{"delta": delta})
	}, func(name, args string) {
		// agent 模式工具调用开始：转发给前端展示中间态（如"正在查询 xxx 表结构…"）
		cylog.Debugf("[ai] SSE 工具调用 session=%s tool=%s args=%s", req.SessionID, name, truncLog(args, 200))
		push("tool", gin.H{"name": name, "args": args})
	})

	if err != nil {
		cylog.Debugf("[ai] SSE 失败 session=%s action=%s 耗时=%s err=%v",
			req.SessionID, action, time.Since(start).Round(time.Millisecond), err)
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			finish("error", gin.H{"message": cyginMsg(c, service.ErrAITimeout)})
		} else {
			finish("error", gin.H{"message": err.Error()})
		}
		finished = true
		return
	}
	cylog.Debugf("[ai] SSE 完成 session=%s action=%s 耗时=%s schemaVerified=%v", req.SessionID, action, time.Since(start).Round(time.Millisecond), schemaVerified)
	finish("done", gin.H{"usage": usage, "schemaVerified": schemaVerified})
	finished = true
}
