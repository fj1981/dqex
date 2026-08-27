package engine

import (
	"fmt"
	"time"
)

// tracker 进度跟踪器：累积日志、节流回调
type tracker struct {
	cb       ProgressFunc
	p        Progress
	lang     string // 进度日志语言（引擎文本注册表 key，默认 zh）
	lastSend time.Time
	finished bool      // 任务已全部完成（含打包等收尾），才允许展示 100%
	startAt  time.Time // 任务开始时间，用于实时计算 DurationMs
}

func newTracker(cb ProgressFunc, lang string) *tracker {
	if cb == nil {
		cb = func(Progress) {}
	}
	return &tracker{cb: cb, lang: lang, p: Progress{State: "running", Logs: []string{}}, startAt: time.Now()}
}

// log 追加日志并立即推送
func (t *tracker) log(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	t.p.Message = msg
	t.p.Logs = append(t.p.Logs, fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), msg))
	// 只保留最近 500 条日志，避免内存膨胀
	if len(t.p.Logs) > 500 {
		t.p.Logs = t.p.Logs[len(t.p.Logs)-500:]
	}
	t.emit(true)
}

// emit 推送进度，force=false 时节流（100ms 一次）
func (t *tracker) emit(force bool) {
	t.calcPercent()
	if !force && time.Since(t.lastSend) < 100*time.Millisecond {
		return
	}
	t.lastSend = time.Now()
	t.p.DurationMs = time.Since(t.startAt).Milliseconds()
	t.cb(t.p)
}

// finish 标记任务完成：仅此时进度才置 100%（全部单元完成后往往还有对象导出/zip 打包等收尾工作）
func (t *tracker) finish() {
	t.finished = true
	t.p.Percent = 100
}

func (t *tracker) calcPercent() {
	if t.finished {
		return
	}
	if t.p.TotalUnits <= 0 {
		t.p.Percent = 0
		return
	}
	// 单元级进度（表 + 对象；总行数导出前未知，不做行级加权）；未完成时封顶 99%，避免收尾阶段误导为已完成
	p := float64(t.p.DoneUnits) / float64(t.p.TotalUnits) * 100
	if p > 99 {
		p = 99
	}
	t.p.Percent = p
}
