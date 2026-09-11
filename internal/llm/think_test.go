package llm

import (
	"strings"
	"testing"
)

// TestStripThinkAll 非流式剥离：<think>...</think> 思考块。
func TestStripThinkAll(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"无思考块", "SELECT 1;", "SELECT 1;"},
		{"单个思考块", "<think>让我想想</think>SELECT 1;", "SELECT 1;"},
		{"多个思考块", "<think>a</think>S1<think>b</think>;S2", "S1;S2"},
		{"未闭合思考块", "SELECT 1;<think>被截断的思考", "SELECT 1;"},
		{"思考块在中间", "S1<think>x</think>S2", "S1S2"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := stripThinkAll(c.in); got != c.want {
				t.Fatalf("stripThinkAll(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// TestThinkStripperStream 流式剥离：标签被增量拆在任意位置。
func TestThinkStripperStream(t *testing.T) {
	cases := []struct {
		name   string
		chunks []string
		want   string
	}{
		{"整块一次到达", []string{"<think>x</think>S1"}, "S1"},
		{"逐字符到达", strings.Split("<think>思考</think>SELECT 1;", ""), "SELECT 1;"},
		{"标签边界拆分", []string{"<thi", "nk>xx</th", "ink>S1"}, "S1"},
		{"正文先出后思考", []string{"S1<think>裁掉"}, "S1"},
		{"无标签正常透传", []string{"SELECT", " 1;"}, "SELECT 1;"},
		{"孤立尖括号flush", []string{"S1<"}, "S1<"},
		{"两个思考块", []string{"<think>a</th", "ink>S1<think>b</think>S2"}, "S1S2"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var st thinkStripper
			var got strings.Builder
			for _, ch := range c.chunks {
				got.WriteString(st.Feed(ch))
			}
			got.WriteString(st.Flush())
			if got.String() != c.want {
				t.Fatalf("chunks=%q → %q, want %q", c.chunks, got.String(), c.want)
			}
		})
	}
}

// TestThinkStripperUnmatchedUnclosed 未闭合思考块：流结束时丢弃（不输出思考内容）。
func TestThinkStripperUnmatchedUnclosed(t *testing.T) {
	var st thinkStripper
	if out := st.Feed("<think>abc"); out != "" {
		t.Fatalf("未闭合思考块不应输出, got %q", out)
	}
	if rest := st.Flush(); rest != "" {
		t.Fatalf("未闭合思考块 Flush 应丢弃, got %q", rest)
	}
}
