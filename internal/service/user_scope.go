package service

import (
	"context"
	"strings"
)

// ---- 用户作用域（多用户隔离） ----
//
// 嵌入宿主（tl-env）场景：宿主鉴权中间件把登录用户写入 X-DQEX-User 请求头，
// web 层中间件再注入请求 context；持久化数据（工作区/SQL 历史/收藏/审计/AI 会话）
// 以「用户域 + 连接」隔离，避免同环境多用户互相覆盖工作区、互见历史。
//
// 作用域存储键规则（scopeConnID，store 层写路径按同一算法落 conn_id 物理列，
// 见 store.scopeConnKey）：
//   - local 域（独立部署，无身份头）：connID 原样存储 —— 完全兼容既有 SQLite 数据；
//   - 其他用户：user + sep + connID（sep 为控制字符 \x1f，不会出现在正常用户名/连接 key 中）。
//
// 五张隔离表（sql_history/sql_audit/ai_session/workspace/sql_favorites）统一三列同写：
// user（用户域）、conn_key（裸连接 key，跨用户级联删除定位）、conn_id（作用域存储键，
// 物理列名沿用、workspace 表主键）。service/persist 层对外一律传裸连接 key，
// 作用域键的计算收敛在 store 层；本文件保留 scopeConnID 供测试与规则说明。

const (
	// LocalUser 独立部署（无宿主身份头）时的默认用户域。
	LocalUser = "local"
	// userScopeSep 作用域键分隔符。
	userScopeSep = "\x1f"
	// MaxUserLen 用户标识长度上限（超出截断，防列溢出）。
	MaxUserLen = 64
)

// NormalizeUser 规范化用户标识：去空白、限长、剔除作用域分隔符；空回退 local。
func NormalizeUser(u string) string {
	u = strings.TrimSpace(u)
	if u == "" {
		return LocalUser
	}
	if len(u) > MaxUserLen {
		u = u[:MaxUserLen]
	}
	// 剔除作用域分隔符（TrimSpace 与截断之后执行）：防止用户名内嵌 \x1f
	// 构造 "user\x1fconn" 形态的作用域键，跨用户域伪造/读写他人数据
	u = strings.ReplaceAll(u, userScopeSep, "")
	if u == "" {
		return LocalUser
	}
	return u
}

// scopeConnID 生成用户域下的作用域存储键（算法与 store 层 scopeConnKey 一致）。
func scopeConnID(user, connID string) string {
	user = NormalizeUser(user)
	if user == LocalUser || connID == "" {
		return connID
	}
	return user + userScopeSep + connID
}

// ---- 请求上下文中的用户（写侧链路：ctx 由 web 层中间件注入，执行/落盘零签名改动） ----

type userCtxKey struct{}

// WithUser 将用户标识写入 context（web 层中间件调用）。
func WithUser(ctx context.Context, user string) context.Context {
	return context.WithValue(ctx, userCtxKey{}, NormalizeUser(user))
}

// userFromCtx 从 context 取用户标识；无注入时回退 local。
func userFromCtx(ctx context.Context) string {
	if ctx == nil {
		return LocalUser
	}
	if u, ok := ctx.Value(userCtxKey{}).(string); ok && u != "" {
		return u
	}
	return LocalUser
}
