// Package llm 提供对大模型 OpenAI 兼容协议的薄封装（基于 Eino ChatModel）。
// 该包为纯客户端层，不依赖 engine / service，供 service/ai.go 统一编排调用。
package llm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/schema"
	"github.com/fj1981/infrakit/pkg/cylog"
)

// Config 为创建 LLM 客户端所需的配置（来自 AppConfig.AI，已由 service 层取值）。
type Config struct {
	BaseURL     string
	APIKey      string
	Model       string
	Temperature float32
	MaxTokens   int
	Timeout     time.Duration
}

// Usage 记录单轮/会话累计的 token 消耗（透出模型返回的 usage，不自数）。
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// IsZero 判断 usage 是否为空（模型未返回时记 0）。
func (u Usage) IsZero() bool { return u.TotalTokens == 0 }

// Add 累加一轮消耗到累计值。
func (u *Usage) Add(o Usage) {
	u.PromptTokens += o.PromptTokens
	u.CompletionTokens += o.CompletionTokens
	u.TotalTokens += o.TotalTokens
}

// Client 封装单个 Eino ChatModel 实例（每个配置可复用一个客户端）。
type Client struct {
	cm  *openai.ChatModel
	cfg Config
}

// sanitizeBaseURL 兼容误填：BaseURL 应只到版本前缀（.../v1），
// 具体端点（/chat/completions 等）由客户端自动拼接。
// 若用户误填了完整端点，剥离后缀避免拼出 .../chat/completions/chat/completions。
func sanitizeBaseURL(u string) string {
	u = strings.TrimSpace(u)
	u = strings.TrimRight(u, "/")
	for _, suffix := range []string{"/chat/completions", "/completions", "/chat"} {
		if strings.HasSuffix(strings.ToLower(u), suffix) {
			return strings.TrimRight(u[:len(u)-len(suffix)], "/")
		}
	}
	return u
}

// NewClient 创建 LLM 客户端。ctx 仅用于初始化，不参与后续调用。
func NewClient(ctx context.Context, cfg Config) (*Client, error) {
	cfg.BaseURL = sanitizeBaseURL(cfg.BaseURL)
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, errors.New("llm: base_url 不能为空")
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New("llm: api_key 不能为空")
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return nil, errors.New("llm: model 不能为空")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 60 * time.Second
	}
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = 2048
	}
	// temperature 默认 0.2：生成 SQL 建议偏低温度保证确定性。
	if cfg.Temperature == 0 {
		cfg.Temperature = 0.2
	}
	maxTokens := cfg.MaxTokens
	temperature := cfg.Temperature

	cm, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{
		BaseURL:     cfg.BaseURL,
		APIKey:      cfg.APIKey,
		Model:       cfg.Model,
		Timeout:     cfg.Timeout,
		MaxTokens:   &maxTokens,
		Temperature: &temperature,
	})
	if err != nil {
		return nil, fmt.Errorf("llm: 初始化模型客户端失败: %w", err)
	}
	c := &Client{cm: cm, cfg: cfg}
	c.debugf("[llm] 客户端初始化 base=%s model=%s timeout=%s maxTokens=%d temperature=%.1f",
		cfg.BaseURL, cfg.Model, cfg.Timeout, cfg.MaxTokens, cfg.Temperature)
	return c, nil
}

// debugf 输出 AI 链路调试日志；是否打印由全局日志级别（debug 及以上）控制。
func (c *Client) debugf(format string, args ...any) {
	cylog.Debugf(format, args...)
}

// stripThinkAll 剥离完整文本中的 <think>...</think> 思考块。
// 推理模型（MiniMax M 系列、DeepSeek R1 等）默认在正文前输出思考内容且多数无法关闭，
// SQL 生成场景只需要最终结果；未闭合的思考块（流被截断）整段丢弃。
func stripThinkAll(s string) string {
	for {
		i := strings.Index(s, openThinkTag)
		if i < 0 {
			return s
		}
		j := strings.Index(s[i:], closeThinkTag)
		if j < 0 {
			return s[:i]
		}
		s = s[:i] + s[i+j+len(closeThinkTag):]
	}
}

const (
	openThinkTag  = "<think>"
	closeThinkTag = "</think>"
)

// tagPrefixLen 返回 s 结尾与 tag 前缀的最大匹配长度。
// 用于流式场景：缓冲尾部可能是被 chunk 拆开的标签前缀（如 "<thi"），
// 这部分不能输出，需保留等待下一个增量确认。
func tagPrefixLen(s, tag string) int {
	n := len(s)
	if n > len(tag) {
		n = len(tag)
	}
	for i := n; i > 0; i-- {
		if strings.HasSuffix(s, tag[:i]) {
			return i
		}
	}
	return 0
}

// thinkStripper 流式剥离 <think>...</think> 的状态机。
// 流式增量可能把标签拆在任意位置（如 "<thi" + "nk>"），
// 需要缓冲不确定的尾部直到能判定是否为标签。
type thinkStripper struct {
	inThink bool
	buf     strings.Builder
}

// Feed 喂入一段增量，返回应透传给调用方的正文部分。
func (t *thinkStripper) Feed(delta string) string {
	t.buf.WriteString(delta)
	var out strings.Builder
	for {
		s := t.buf.String()
		if t.inThink {
			idx := strings.Index(s, closeThinkTag)
			if idx < 0 {
				// 保留可能是闭合标签前缀的尾部，其余（思考内容）丢弃。
				keep := tagPrefixLen(s, closeThinkTag)
				t.buf.Reset()
				if keep > 0 {
					t.buf.WriteString(s[len(s)-keep:])
				}
				break
			}
			t.inThink = false
			t.buf.Reset()
			t.buf.WriteString(s[idx+len(closeThinkTag):])
		} else {
			idx := strings.Index(s, openThinkTag)
			if idx < 0 {
				keep := tagPrefixLen(s, openThinkTag)
				t.buf.Reset()
				if keep > 0 {
					t.buf.WriteString(s[len(s)-keep:])
				}
				if out.Len() == 0 && len(s) == keep {
					break
				}
				out.WriteString(s[:len(s)-keep])
				break
			}
			out.WriteString(s[:idx])
			t.inThink = true
			t.buf.Reset()
			t.buf.WriteString(s[idx+len(openThinkTag):])
		}
	}
	return out.String()
}

// Flush 流结束时调用，返回缓冲残留（think 外的不完整标签片段）；未闭合的思考块丢弃。
func (t *thinkStripper) Flush() string {
	s := t.buf.String()
	t.buf.Reset()
	if t.inThink {
		return ""
	}
	return s
}

// msgsChars 统计消息列表的字符总量（用于调试日志粗估上下文规模）。
func msgsChars(msgs []*schema.Message) int {
	n := 0
	for _, m := range msgs {
		if m != nil {
			n += len(m.Content)
		}
	}
	return n
}

// truncLog 调试日志截断：超长文本只保留前 n 个字符，避免刷屏。
func truncLog(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…(截断)"
	}
	return s
}

// dumpMsgs 逐条输出消息角色与内容（截断），用于核对注入上下文的实际内容（如表结构）。
func dumpMsgs(msgs []*schema.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		if m == nil {
			continue
		}
		b.WriteString(fmt.Sprintf("\n  [%s] %s", m.Role, truncLog(m.Content, 300)))
	}
	return b.String()
}

// Chat 非流式生成，返回完整文本与 usage。
func (c *Client) Chat(ctx context.Context, msgs []*schema.Message) (string, Usage, error) {
	start := time.Now()
	c.debugf("[llm] Chat 开始 msgs=%d chars=%d%s", len(msgs), msgsChars(msgs), dumpMsgs(msgs))
	out, err := c.cm.Generate(ctx, msgs)
	if err != nil {
		c.debugf("[llm] Chat 失败 耗时=%s err=%v", time.Since(start).Round(time.Millisecond), err)
		return "", Usage{}, normalizeErr(err)
	}
	u := Usage{}
	if out != nil && out.ResponseMeta != nil && out.ResponseMeta.Usage != nil {
		u = fromTokenUsage(out.ResponseMeta.Usage)
	}
	if out == nil {
		return "", u, errors.New("llm: 模型返回空响应")
	}
	content := stripThinkAll(out.Content)
	c.debugf("[llm] Chat 完成 耗时=%s contentChars=%d(think剥离后%d) usage=prompt=%d/completion=%d/total=%d",
		time.Since(start).Round(time.Millisecond), len(out.Content), len(content), u.PromptTokens, u.CompletionTokens, u.TotalTokens)
	return content, u, nil
}

// ChatStream 流式生成：每收到一段增量回调 onDelta，结束后返回流尾 usage（模型未提供则记 0）。
// 客户端断开 ctx 即可终止上游调用。
func (c *Client) ChatStream(ctx context.Context, msgs []*schema.Message, onDelta func(string)) (Usage, error) {
	start := time.Now()
	c.debugf("[llm] Stream 开始 msgs=%d chars=%d%s", len(msgs), msgsChars(msgs), dumpMsgs(msgs))
	sr, err := c.cm.Stream(ctx, msgs)
	if err != nil {
		c.debugf("[llm] Stream 启动失败 耗时=%s err=%v", time.Since(start).Round(time.Millisecond), err)
		return Usage{}, normalizeErr(err)
	}
	defer sr.Close()

	var u Usage
	var b strings.Builder
	// 思考内容剥离状态机：标签可能被流式增量拆在任意位置
	var stripper thinkStripper
	first := true
	for {
		m, err := sr.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			c.debugf("[llm] Stream 中断 已收字符=%d 耗时=%s err=%v",
				b.Len(), time.Since(start).Round(time.Millisecond), err)
			return u, normalizeErr(err)
		}
		if m == nil {
			continue
		}
		if first {
			c.debugf("[llm] Stream 首字节 耗时=%s", time.Since(start).Round(time.Millisecond))
			first = false
		}
		// 流尾消息通常携带 usage（各厂商实现不同，缺失时记 0）。
		if m.ResponseMeta != nil && m.ResponseMeta.Usage != nil {
			u = fromTokenUsage(m.ResponseMeta.Usage)
		}
		if m.Content != "" {
			b.WriteString(m.Content)
			if onDelta != nil {
				if clean := stripper.Feed(m.Content); clean != "" {
					onDelta(clean)
				}
			}
		}
	}
	// 流结束：输出缓冲中残留的不完整标签片段（think 外）
	if onDelta != nil {
		if rest := stripper.Flush(); rest != "" {
			onDelta(rest)
		}
	}
	c.debugf("[llm] Stream 完成 耗时=%s contentChars=%d usage=prompt=%d/completion=%d/total=%d",
		time.Since(start).Round(time.Millisecond), b.Len(), u.PromptTokens, u.CompletionTokens, u.TotalTokens)
	return u, nil
}

func fromTokenUsage(tu *schema.TokenUsage) Usage {
	if tu == nil {
		return Usage{}
	}
	return Usage{
		PromptTokens:     tu.PromptTokens,
		CompletionTokens: tu.CompletionTokens,
		TotalTokens:      tu.TotalTokens,
	}
}

// normalizeErr 将底层错误归一化为可读信息（超时/取消/网络/鉴权/限流）。
func normalizeErr(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, context.Canceled):
		return fmt.Errorf("生成已取消: %w", err)
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("请求超时（超过配置的 timeout）: %w", err)
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return fmt.Errorf("请求超时（超过配置的 timeout）: %w", err)
	}

	var apiErr *openai.APIError
	if errors.As(err, &apiErr) {
		msg := apiErr.Message
		if msg == "" {
			msg = err.Error()
		}
		switch apiErr.HTTPStatusCode {
		case 401:
			return fmt.Errorf("API Key 无效或未授权（HTTP 401）: %s", msg)
		case 403:
			return fmt.Errorf("无权限访问该模型（HTTP 403）: %s", msg)
		case 404:
			return fmt.Errorf("模型或端点不存在（HTTP 404）: %s", msg)
		case 429:
			return fmt.Errorf("请求过于频繁或额度不足（HTTP 429）: %s", msg)
		case 500, 502, 503, 504:
			return fmt.Errorf("模型服务暂不可用（HTTP %d）: %s", apiErr.HTTPStatusCode, msg)
		default:
			return fmt.Errorf("模型接口错误（HTTP %d）: %s", apiErr.HTTPStatusCode, msg)
		}
	}
	return err
}
