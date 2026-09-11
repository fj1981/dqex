package service

import (
	"context"
	"strings"
	"testing"
)

// TestScopeConnID 验证用户域存储键规则：local 域原样（兼容既有数据），其他用户加前缀。
func TestScopeConnID(t *testing.T) {
	cases := []struct {
		user, connID, want string
	}{
		{"", "env:abc", "env:abc"},
		{LocalUser, "env:abc", "env:abc"},
		{"  ", "env:abc", "env:abc"},
		{"jun.fan", "env:abc", "jun.fan\x1fenv:abc"},
		{"jun.fan", "", ""}, // 空 connID 不加前缀
	}
	for _, c := range cases {
		if got := scopeConnID(c.user, c.connID); got != c.want {
			t.Errorf("scopeConnID(%q,%q)=%q want %q", c.user, c.connID, got, c.want)
		}
	}
}

// TestNormalizeUser 验证用户标识规范化。
func TestNormalizeUser(t *testing.T) {
	if NormalizeUser("") != LocalUser || NormalizeUser("  ") != LocalUser {
		t.Error("empty should fall back to local")
	}
	if got := NormalizeUser(" jun.fan "); got != "jun.fan" {
		t.Errorf("trim failed: %q", got)
	}
	long := string(make([]byte, 200))
	if got := NormalizeUser(long); len(got) != MaxUserLen {
		t.Errorf("length limit failed: %d", len(got))
	}
}

// TestNormalizeUserStripSep 验证作用域分隔符剔除与作用域键规则：
// 防止用户名内嵌 \x1f 伪造 "user\x1fconn" 形态的跨域存储键。
func TestNormalizeUserStripSep(t *testing.T) {
	u := NormalizeUser("a\x1fb")
	if strings.Contains(u, userScopeSep) {
		t.Errorf("作用域分隔符应被剔除，实际 %q", u)
	}
	// 剔除后为空回退 local
	if got := NormalizeUser("\x1f"); got != LocalUser {
		t.Errorf("仅分隔符的用户应回退 local，实际 %q", got)
	}
	// 该用户的作用域键含分隔符前缀，且与 local 域裸 key 形态可区分
	stored := scopeConnID(u, "env:abc")
	if !strings.HasPrefix(stored, u+userScopeSep) {
		t.Errorf("作用域键应带用户域前缀，实际 %q", stored)
	}
	if stored == "env:abc" {
		t.Error("登录用户的作用域键不应与 local 域裸 key 相同")
	}
}

// TestUserFromCtx 验证 context 注入与回退。
func TestUserFromCtx(t *testing.T) {
	if userFromCtx(context.Background()) != LocalUser {
		t.Error("default should be local")
	}
	ctx := WithUser(context.Background(), "jun.fan")
	if userFromCtx(ctx) != "jun.fan" {
		t.Error("injected user lost")
	}
	if userFromCtx(nil) != LocalUser {
		t.Error("nil ctx should be local")
	}
}
