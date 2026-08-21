package llm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/flow/agent/react"
	"github.com/cloudwego/eino/schema"
	"github.com/fj1981/infrakit/pkg/cylog"
)

// maxAgentSteps agent 循环步数上限（默认 12；大库探索 + 1 轮余量）。
const maxAgentSteps = 16

// AgentCallbacks agent 执行期间的对外回调（SSE 工具事件 / 文本增量）。
type AgentCallbacks struct {
	// OnContent 文本增量（等同普通流式的 onDelta）。
	// 工具调用事件由 service 层经工具函数内的 ToolSink 透传，不在这里处理。
	OnContent func(delta string)
}

// ReactAgent 封装 Eino react.Agent：会话级复用（每次 Stream 传入完整历史）。
// 注意：内部持有独立 ChatModel 实例（react 会对其 BindTools 切换绑定），
// 不可与全局共享的 aiClient 混用，避免多会话工具绑定相互覆盖。
type ReactAgent struct {
	ag  *react.Agent
	cfg Config
}

// NewReactAgent 创建 React Agent。
// systemPrompt 由 MessageModifier 注入：若 msgs 已含 system 消息则不重复注入（幂等）。
func NewReactAgent(ctx context.Context, cfg Config, tools []tool.InvokableTool, systemPrompt string) (*ReactAgent, error) {
	c, err := NewClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	modifier := func(ctx context.Context, msgs []*schema.Message) []*schema.Message {
		for _, m := range msgs {
			if m != nil && m.Role == schema.System {
				return msgs
			}
		}
		return append([]*schema.Message{schema.SystemMessage(systemPrompt)}, msgs...)
	}
	baseTools := make([]tool.BaseTool, 0, len(tools))
	for _, t := range tools {
		baseTools = append(baseTools, t)
	}
	ag, err := react.NewAgent(ctx, &react.AgentConfig{
		ToolCallingModel: c.cm,
		ToolsConfig: compose.ToolsNodeConfig{
			Tools: baseTools,
		},
		MessageModifier: modifier,
		MaxStep:         maxAgentSteps,
		// 自定义 checker：扫描整个模型输出流直到 EOF，只要出现任意 tool call 即判定为
		// 需要调工具（继续 agent 循环）。默认 firstChunkStreamToolCallChecker 只看首个非空
		// chunk，对 MiniMax-M3 这类「先吐文本再吐 tool call」的模型会误判为不调工具、
		// 直接结束，导致工具探索被跳过。参考 Eino 官方 demo 中被注释的 toolCallChecker。
		StreamToolCallChecker: func(ctx context.Context, sr *schema.StreamReader[*schema.Message]) (bool, error) {
			defer sr.Close()
			for {
				msg, err := sr.Recv()
				if err != nil {
					if errors.Is(err, io.EOF) {
						return false, nil
					}
					return false, err
				}
				if msg != nil && len(msg.ToolCalls) > 0 {
					return true, nil
				}
			}
		},
	})
	if err != nil {
		return nil, fmt.Errorf("llm: 初始化 React Agent 失败: %w", err)
	}
	return &ReactAgent{ag: ag, cfg: cfg}, nil
}

// Stream 执行一轮 agent 对话（msgs 需以用户消息结尾，可含 system）。
// 参照 Eino 官方 react demo 的简洁写法：直接消费 agent.Stream 返回的最终输出流，
// 工具调用的中间过程由框架内部自动处理，外部不主动 drain（避免扇出背压死锁）。
//
// 返回：最终回复内容、本轮累计 usage（模型末轮 usage，工具轮次的 usage 框架不直接透出）、
// 以及最终答案消息（供会话持久化）。
func (a *ReactAgent) Stream(ctx context.Context, msgs []*schema.Message, cb AgentCallbacks) (string, Usage, error) {
	start := time.Now()
	cylog.Debugf("[llm] Agent Stream 开始 msgs=%d chars=%d", len(msgs), msgsChars(msgs))

	sr, err := a.ag.Stream(ctx, msgs)
	if err != nil {
		return "", Usage{}, normalizeErr(err)
	}
	defer sr.Close()

	var b strings.Builder
	var u Usage
	for {
		m, err := sr.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			cylog.Debugf("[llm] Agent Stream 中断 已收字符=%d 耗时=%s err=%v",
				b.Len(), time.Since(start).Round(time.Millisecond), err)
			return "", Usage{}, normalizeErr(err)
		}
		if m == nil {
			continue
		}
		if m.ResponseMeta != nil && m.ResponseMeta.Usage != nil {
			u = fromTokenUsage(m.ResponseMeta.Usage)
		}
		if m.Content != "" {
			b.WriteString(m.Content)
			if cb.OnContent != nil {
				cb.OnContent(m.Content)
			}
		}
	}
	content := b.String()
	hasThinking := hasThinkingTag(content)
	cylog.Debugf("[llm] Agent Stream 完成 耗时=%s contentChars=%d usage=prompt=%d/completion=%d/total=%d hasThinking=%v content=%q",
		time.Since(start).Round(time.Millisecond), len(content),
		u.PromptTokens, u.CompletionTokens, u.TotalTokens, hasThinking, truncStr(content, 300))
	return content, u, nil
}

// hasThinkingTag 检测输出中是否残留思考标签（含 MiniMax 的 <｜end▁of▁thinking｜> 等变体），
// 仅用于日志标记，不参与业务判定（业务拦截由 service 层 validateAgentResult 负责）。
func hasThinkingTag(s string) bool {
	for _, tag := range []string{
		"<thinking", "<think", "<reasoning", "<thought",
		"<｜end▁of▁thinking｜>", // MiniMax 思考结束标记
	} {
		if strings.Contains(s, tag) {
			return true
		}
	}
	return false
}

// truncStr 截断字符串用于日志（按 rune 安全，避免截断多字节字符产生乱码）。
func truncStr(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
