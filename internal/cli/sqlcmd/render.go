package sqlcmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"dqex/internal/engine"

	"github.com/olekukonko/tablewriter"
	"github.com/olekukonko/tablewriter/tw"
	"golang.org/x/term"
)

func outputTable(r *queryResult) {
	if r.Error != "" {
		fprintf(os.Stderr, "%s\n", red(r.Error))
		return
	}
	if len(r.Columns) == 0 {
		// 写语句：Query OK；读语句空结果（cydb 可能不返回列信息）：Empty set
		if !r.IsWrite {
			fmt.Println(dim("Empty set"))
			if showTiming {
				printf("%s\n", dim(sprintf("(%s)", formatDuration(r.Elapsed))))
			}
			return
		}
		if r.AffectedRows > 0 {
			if showTiming {
				printf("Query OK, %d rows affected (%s)\n", r.AffectedRows, formatDuration(r.Elapsed))
			} else {
				printf("Query OK, %d rows affected\n", r.AffectedRows)
			}
		} else if showTiming {
			printf("Query OK (%s)\n", formatDuration(r.Elapsed))
		} else {
			fmt.Println("Query OK")
		}
		return
	}
	if len(r.Rows) == 0 {
		fmt.Println(dim("Empty set"))
		if showTiming {
			printf("%s\n", dim(sprintf("(%s)", formatDuration(r.Elapsed))))
		}
		return
	}

	table := tablewriter.NewWriter(os.Stdout)
	// 保留原始列名（不转换大小写、不替换下划线）
	table.Options(tablewriter.WithHeaderAutoFormat(tw.Off))

	headers := make([]any, len(r.Columns))
	for i, c := range r.Columns {
		headers[i] = c
	}
	table.Header(headers...)

	// 格式化数据行
	explain := isExplainResult(r.Columns)
	for _, row := range r.Rows {
		formatted := make([]any, len(r.Columns))
		for i := range r.Columns {
			if i < len(row) {
				val := formatCell(row[i])
				if explain {
					val = explainFormat(r.Columns[i], row[i])
				}
				formatted[i] = val
			}
		}
		table.Append(formatted...)
	}
	table.Render()

	if showTiming {
		printf("%d rows in set (%s)\n", r.RowCount, formatDuration(r.Elapsed))
	}
}

func formatCell(val any) string {
	if val == nil {
		return dim("(NULL)")
	}
	switch v := val.(type) {
	case []byte:
		return dim(sprintf("<BLOB %s>", formatBytes(len(v))))
	case string:
		if len(v) > 500 {
			return v[:200] + dim(sprintf(cliTextsFor(cliLang).tableTruncChars, len(v)))
		}
		return v
	case int64, int, int32, float64, float32:
		return sprintf("%v", v)
	case bool:
		if v {
			return green("true")
		}
		return red("false")
	case time.Time:
		return v.Format("2006-01-02 15:04:05")
	default:
		return sprintf("%v", v)
	}
}

func formatBytes(n int) string {
	switch {
	case n >= 1<<20:
		return sprintf("%.1fMB", float64(n)/(1<<20))
	case n >= 1<<10:
		return sprintf("%.1fKB", float64(n)/(1<<10))
	default:
		return sprintf("%dB", n)
	}
}

func formatDuration(d time.Duration) string {
	if d < time.Microsecond {
		return "0ms"
	}
	if d < time.Millisecond {
		return sprintf("%.3fms", float64(d.Microseconds())/1000)
	}
	if d < time.Second {
		return sprintf("%.0fms", float64(d.Milliseconds()))
	}
	return sprintf("%.3fs", d.Seconds())
}

// ---- 执行计划着色 ----

func isExplainResult(columns []string) bool {
	if len(columns) == 0 {
		return false
	}
	keyCols := []string{"id", "select_type", "table", "type", "key", "rows", "Extra"}
	lowerCols := make(map[string]bool)
	for _, c := range columns {
		lowerCols[strings.ToLower(c)] = true
	}
	match := 0
	for _, kc := range keyCols {
		if lowerCols[strings.ToLower(kc)] {
			match++
		}
	}
	return match >= 4
}

func explainFormat(col string, val any) string {
	colLower := strings.ToLower(col)
	s := sprintf("%v", val)
	switch colLower {
	case "type":
		switch strings.ToLower(s) {
		case "all":
			return red(s)
		case "ref", "eq_ref", "const", "system":
			return green(s)
		case "index", "range":
			return yellow(s)
		}
	case "key":
		if s == "<nil>" || s == "NULL" {
			return red(s)
		}
		return green(s)
	case "rows":
		if n, err := strconv.Atoi(s); err == nil {
			if n > 10000 {
				return red(s)
			}
			if n > 1000 {
				return yellow(s)
			}
			return green(s)
		}
	case "extra":
		lower := strings.ToLower(s)
		if strings.Contains(lower, "using filesort") || strings.Contains(lower, "using temporary") {
			return red(s)
		}
		if strings.Contains(lower, "using index") {
			return green(s)
		}
	}
	return s
}

// showTiming 是否显示耗时信息（\timing 切换，默认开）。
var showTiming = true

// ---- 垂直显示 ----

func outputVertical(r *queryResult) {
	for i, row := range r.Rows {
		printf("*************************** %d. row ***************************\n", i+1)
		for j, col := range r.Columns {
			val := ""
			if j < len(row) {
				val = formatCell(row[j])
			}
			printf("%20s: %s\n", col, val)
		}
	}
	if showTiming {
		printf("%d rows in set (%s)\n", r.RowCount, formatDuration(r.Elapsed))
	}
}

// ---- 宽度检测与垂直降级 ----

// verticalAutoMaxRows 表格超宽时自动切垂直显示的最大行数（再大则只提示用 \G）。
const verticalAutoMaxRows = 30

// stripANSI 剥离 ANSI 转义序列（CSI 模式），用于表格宽度估算。
func stripANSI(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			i += 2
			for i < len(s) && !(s[i] >= 0x40 && s[i] <= 0x7e) {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// estimateTableWidth 估算 tablewriter 渲染后的表格宽度（含列间距）。
func estimateTableWidth(r *queryResult) int {
	total := 1
	for i, col := range r.Columns {
		w := len(stripANSI(col))
		for _, row := range r.Rows {
			if i < len(row) {
				if l := len(stripANSI(formatCell(row[i]))); l > w {
					w = l
				}
			}
		}
		total += w + 3
	}
	return total
}

// maybePage 渲染查询结果：表格宽度超过终端时，
// 行数不多自动切换垂直显示（\G 样式），行数多则提示改用 \G。
func maybePage(r *queryResult) {
	termW := 0
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
		termW = w
	}
	if termW > 0 && len(r.Columns) > 1 && estimateTableWidth(r) > termW {
		if r.RowCount <= verticalAutoMaxRows {
			fmt.Fprintln(os.Stderr, dim(cliTextsFor(cliLang).tableAutoVertical))
			outputVertical(r)
			return
		}
		fprintf(os.Stderr, "%s\n", yellow(sprintf(cliTextsFor(cliLang).tableTooWide, len(r.Columns))))
	}
	outputTable(r)
}

// ---- 审计日志 ----

const (
	auditFileName   = "audit.log"
	auditMaxSize    = 10 * 1024 * 1024
	auditMaxBackups = 5
)

func auditWrite(info *engine.DBConnInfo, sql string, affected int64, elapsed time.Duration) {
	auditSQL := sql
	if len(auditSQL) > 500 {
		auditSQL = auditSQL[:500]
	}
	entry := sprintf("[%s] %s@%s:%d/%s | %s | affected=%d | %s\n",
		time.Now().Format("2006-01-02 15:04:05"),
		info.Un, info.Host, info.Port, info.DBName,
		auditSQL, affected, formatDuration(elapsed),
	)

	path := auditLogPath()
	if path == "" {
		fmt.Fprint(os.Stderr, dim(entry))
		return
	}

	dir := filepath.Dir(path)
	os.MkdirAll(dir, 0700)

	if fi, err := os.Stat(path); err == nil && fi.Size() >= auditMaxSize {
		rotateAuditLogs(path)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		fmt.Fprint(os.Stderr, dim(entry))
		return
	}
	defer f.Close()
	f.WriteString(entry)
}

func auditLogPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".dqex", auditFileName)
}

func rotateAuditLogs(basePath string) {
	oldest := sprintf("%s.%d", basePath, auditMaxBackups)
	os.Remove(oldest)
	for i := auditMaxBackups; i > 1; i-- {
		old := sprintf("%s.%d", basePath, i-1)
		new_ := sprintf("%s.%d", basePath, i)
		os.Rename(old, new_)
	}
	os.Rename(basePath, basePath+".1")
}
