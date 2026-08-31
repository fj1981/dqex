package engine

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"
)

// QueryHooks SQL 审计钩子：宿主在每次语句执行后收到回调，用于合规审计/慢查询采集。
// 注册方式：宿主经 Client 选项注入（见根包 WithQueryHooks），引擎经 context 传递到执行点。
// 回调契约：同步调用、不得阻塞（审计耗时直接累加到语句响应时间）；回调内不得再回调
// Client 方法；多任务并行时 OnQuery 会被多 goroutine 并发调用，宿主回调需自行保证并发安全。
type QueryHooks struct {
	// OnQuery 语句执行完成回调：stmt 为语句文本（超长截断至 4096 字节，截断点回退至
	// UTF-8 字符边界；宿主自行做摘要/脱敏）；
	// costMs 为执行耗时（毫秒）；rowsAffected 写操作为受影响行数，查询为返回行数。
	// 失败语句同样回调（rowsAffected = -1），宿主可据此记录失败审计。
	OnQuery func(ctx context.Context, connKey, stmt string, costMs int64, rowsAffected int64)
}

// queryHooksKey context 键（私有类型防碰撞）
type queryHooksKey struct{}

// CtxWithQueryHooks 将审计钩子注入 context，随任务传递到引擎执行点
func CtxWithQueryHooks(ctx context.Context, hooks *QueryHooks) context.Context {
	if hooks == nil || hooks.OnQuery == nil {
		return ctx
	}
	return context.WithValue(ctx, queryHooksKey{}, hooks)
}

// hooksFromCtx 取出审计钩子（未注册返回 nil）
func hooksFromCtx(ctx context.Context) *QueryHooks {
	hooks, _ := ctx.Value(queryHooksKey{}).(*QueryHooks)
	return hooks
}

// fireQueryHook 在语句执行点回调审计钩子：start 为执行起始时间；
// rows 为受影响/返回行数，执行失败传 -1。回调异常不外泄（审计不得影响业务）。
func fireQueryHook(ctx context.Context, connKey, stmt string, start time.Time, rows int64) {
	hooks := hooksFromCtx(ctx)
	if hooks == nil || hooks.OnQuery == nil || strings.TrimSpace(stmt) == "" {
		return
	}
	stmt = strings.TrimSpace(stmt)
	if len(stmt) > 4096 {
		// 截断超长语句（4096 字节）并回退到完整 UTF-8 字符边界（最多回退 3 字节，O(1)）；
		// 本身非 UTF-8 的内容（如驱动返回的异常字节流）保留截断结果，避免审计记录被剥成空串
		stmt = stmt[:4096]
		for i := 0; i < utf8.UTFMax-1; i++ {
			r, size := utf8.DecodeLastRuneInString(stmt)
			if r != utf8.RuneError || size > 1 {
				break
			}
			stmt = stmt[:len(stmt)-1]
		}
	}
	costMs := time.Since(start).Milliseconds()
	func() {
		defer func() { _ = recover() }()
		hooks.OnQuery(ctx, connKey, stmt, costMs, rows)
	}()
}
