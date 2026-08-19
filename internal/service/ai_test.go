package service

import (
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func mkUser(raw string) *schema.Message {
	m := schema.UserMessage(raw)
	m.Extra = map[string]any{"action": "generate", "raw": raw}
	return m
}

// TestTrimMessagesCountLimit 条数上限裁剪：超过 aiMaxMessages 时按轮次裁剪最旧，回写后生效。
func TestTrimMessagesCountLimit(t *testing.T) {
	msgs := []*schema.Message{schema.SystemMessage("sys")}
	for i := 0; i < aiMaxMessages+10; i++ {
		msgs = append(msgs, mkUser("q"+string(rune('a'+i%26))))
		msgs = append(msgs, schema.AssistantMessage("a"+string(rune('a'+i%26)), nil))
	}
	if len(msgs) <= aiMaxMessages {
		t.Fatalf("前置条件不成立：消息数应超过上限")
	}
	msgs = trimMessages(msgs) // 模拟调用方回写
	if len(msgs) > aiMaxMessages {
		t.Fatalf("裁剪后应 ≤ %d 条，实际 %d", aiMaxMessages, len(msgs))
	}
	if len(msgs) < 2 || msgs[0].Role != schema.System {
		t.Fatalf("system 消息应保留在首位，实际 %d 条", len(msgs))
	}
	if last := msgs[len(msgs)-1]; last.Role != schema.Assistant {
		t.Fatalf("最后一条应为 assistant（当前轮保护），实际 %s", last.Role)
	}
}

// TestTrimMessagesCharBudget 字符预算裁剪：超预算时裁剪旧轮次，且保留当前轮。
func TestTrimMessagesCharBudget(t *testing.T) {
	long := strings.Repeat("很长的需求描述", 2000) // 12000 rune
	msgs := []*schema.Message{schema.SystemMessage("sys")}
	for i := 0; i < 5; i++ {
		msgs = append(msgs, mkUser("q"+long))
		msgs = append(msgs, schema.AssistantMessage("a"+long, nil))
	}
	before := len(msgs)
	msgs = trimMessages(msgs)
	if len(msgs) >= before {
		t.Fatalf("超预算应裁剪旧轮次：before=%d after=%d", before, len(msgs))
	}
	// 当前轮（最后一条 user 及其 assistant）必须保留
	if last := msgs[len(msgs)-1]; last.Role != schema.Assistant {
		t.Fatalf("当前轮被误裁，最后一条应为 assistant，实际 %s", last.Role)
	}
}

// TestDedupeConsecutiveUser 连续相同 user 消息折叠为一条（保留最新）。
func TestDedupeConsecutiveUser(t *testing.T) {
	msgs := []*schema.Message{
		schema.SystemMessage("sys"),
		mkUser("公共组织的名字是 public"),
		mkUser("公共组织的名字是 public"),
		mkUser("公共组织的名字是 public"),
		schema.AssistantMessage("回复", nil),
		mkUser("我现在想更新 公共组织的用户了"),
		mkUser("我现在想更新 公共组织的用户了"),
		schema.AssistantMessage("回复2", nil),
		mkUser("我都查到了 public"),
	}
	out := dedupeConsecutiveUser(msgs)
	// 原始 9 条（u1×3 + a1 + u2×2 + a2 + u3）折叠后：sys + u1 + a1 + u2 + a2 + u3 = 6 条
	if len(out) != 6 {
		t.Fatalf("应折叠为 6 条（sys+2轮完整+第3轮悬空），实际 %d: %+v", len(out), out)
	}
	// 折叠后同内容 user 只剩一条
	userCount := 0
	for _, m := range out {
		if m.Role == schema.User {
			userCount++
		}
	}
	if userCount != 3 {
		t.Fatalf("user 消息应为 3 条（去重后），实际 %d", userCount)
	}
	// 折叠保留最后一条（Extra 完整）
	if out[1].Role != schema.User || out[1].Extra["raw"] != "公共组织的名字是 public" {
		t.Fatalf("折叠后的 user 消息内容错误: %+v", out[1])
	}
}

// TestDedupeConsecutiveUserNoExtra 回放历史不带 Extra 时，退回按 content 去重。
func TestDedupeConsecutiveUserNoExtra(t *testing.T) {
	msgs := []*schema.Message{
		schema.SystemMessage("sys"),
		schema.UserMessage("查询用户"),
		schema.UserMessage("查询用户"),
		schema.AssistantMessage("回复", nil),
	}
	out := dedupeConsecutiveUser(msgs)
	if len(out) != 3 {
		t.Fatalf("应按 content 去重为 3 条，实际 %d", len(out))
	}
}

// TestDedupeConsecutiveUserKeepsDistinct 不同内容的连续 user（异常历史）不去重。
func TestDedupeConsecutiveUserKeepsDistinct(t *testing.T) {
	msgs := []*schema.Message{
		schema.SystemMessage("sys"),
		mkUser("需求A"),
		mkUser("需求B"),
		schema.AssistantMessage("回复", nil),
	}
	out := dedupeConsecutiveUser(msgs)
	if len(out) != 4 {
		t.Fatalf("不同内容不应折叠，实际 %d", len(out))
	}
}

// mkUserMsgID 构造带流水号（msg_id）的 user 消息。
func mkUserMsgID(raw, msgID string) *schema.Message {
	m := mkUser(raw)
	m.Extra["msg_id"] = msgID
	return m
}

// TestFindMsgIDUserAnswered 已完整处理：user 后有 assistant 回答 → (idx, true)。
func TestFindMsgIDUserAnswered(t *testing.T) {
	msgs := []*schema.Message{
		schema.SystemMessage("sys"),
		mkUserMsgID("查组织", "1"),
		schema.AssistantMessage("回复1", nil),
		mkUserMsgID("查用户", "2"),
		schema.AssistantMessage("回复2", nil),
	}
	idx, answered := findMsgIDUser(msgs, "2")
	if idx != 3 || !answered {
		t.Fatalf("应命中 (3, true)，实际 (%d, %v)", idx, answered)
	}
}

// TestFindMsgIDUserDangling 悬空：user 后无回答 → (idx, false)，可复用继续。
func TestFindMsgIDUserDangling(t *testing.T) {
	msgs := []*schema.Message{
		schema.SystemMessage("sys"),
		mkUserMsgID("查组织", "1"),
		mkUserMsgID("查用户", "2"),
	}
	idx, answered := findMsgIDUser(msgs, "2")
	if idx != 2 || answered {
		t.Fatalf("应命中 (2, false)，实际 (%d, %v)", idx, answered)
	}
}

// TestFindMsgIDUserMiss 无匹配或流水号为空 → (-1, false)。
func TestFindMsgIDUserMiss(t *testing.T) {
	msgs := []*schema.Message{
		schema.SystemMessage("sys"),
		mkUserMsgID("查组织", "1"),
		schema.AssistantMessage("回复1", nil),
	}
	if idx, _ := findMsgIDUser(msgs, "99"); idx != -1 {
		t.Fatalf("无匹配应返回 -1，实际 %d", idx)
	}
	if idx, _ := findMsgIDUser(msgs, ""); idx != -1 {
		t.Fatalf("空流水号应返回 -1，实际 %d", idx)
	}
	// 无 msg_id 的旧消息（Extra 缺失）：按流水号查不到
	legacy := []*schema.Message{schema.SystemMessage("sys"), mkUser("旧消息")}
	if idx, _ := findMsgIDUser(legacy, "1"); idx != -1 {
		t.Fatalf("无 msg_id 的旧消息不应命中，实际 %d", idx)
	}
}
