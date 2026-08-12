package sqlcmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"dbimpex/internal/engine"

	"github.com/olekukonko/tablewriter"
	"github.com/olekukonko/tablewriter/tw"
	"golang.org/x/term"
)

func outputTable(r *queryResult) {
	if r.Error != "" {
		fmt.Fprintf(os.Stderr, "%s\n", red(r.Error))
		return
	}
	if len(r.Columns) == 0 {
		if r.AffectedRows > 0 {
			fmt.Printf("Query OK, %d rows affected (%s)\n", r.AffectedRows, formatDuration(r.Elapsed))
		} else {
			fmt.Printf("Query OK (%s)\n", formatDuration(r.Elapsed))
		}
		return
	}
	if len(r.Rows) == 0 {
		fmt.Println(dim("Empty set"))
		fmt.Printf("%s\n", dim(fmt.Sprintf("(%s)", formatDuration(r.Elapsed))))
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

	fmt.Printf("%d rows in set (%s)\n", r.RowCount, formatDuration(r.Elapsed))
}

func formatCell(val any) string {
	if val == nil {
		return dim("(NULL)")
	}
	switch v := val.(type) {
	case []byte:
		return dim(fmt.Sprintf("<BLOB %s>", formatBytes(len(v))))
	case string:
		if len(v) > 500 {
			return v[:200] + dim(fmt.Sprintf("…(共 %d 字符)", len(v)))
		}
		return v
	case int64, int, int32, float64, float32:
		return fmt.Sprintf("%v", v)
	case bool:
		if v {
			return green("true")
		}
		return red("false")
	case time.Time:
		return v.Format("2006-01-02 15:04:05")
	default:
		return fmt.Sprintf("%v", v)
	}
}

func formatBytes(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1fKB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%dB", n)
	}
}

func formatDuration(d time.Duration) string {
	if d < time.Microsecond {
		return "0ms"
	}
	if d < time.Millisecond {
		return fmt.Sprintf("%.3fms", float64(d.Microseconds())/1000)
	}
	if d < time.Second {
		return fmt.Sprintf("%.0fms", float64(d.Milliseconds()))
	}
	return fmt.Sprintf("%.3fs", d.Seconds())
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
	s := fmt.Sprintf("%v", val)
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

// ---- 垂直显示 ----

func outputVertical(r *queryResult) {
	for i, row := range r.Rows {
		fmt.Printf("*************************** %d. row ***************************\n", i+1)
		for j, col := range r.Columns {
			val := ""
			if j < len(row) {
				val = fmt.Sprintf("%v", row[j])
			}
			fmt.Printf("%20s: %s\n", col, val)
		}
	}
	fmt.Printf("%d rows in set (%s)\n", r.RowCount, formatDuration(r.Elapsed))
}

// ---- 分页器 ----

func maybePage(r *queryResult) {
	termH := 40
	if _, h, err := term.GetSize(int(os.Stdout.Fd())); err == nil && h > 0 {
		termH = h
	}
	if r.RowCount <= termH-5 {
		outputTable(r)
		return
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
	entry := fmt.Sprintf("[%s] %s@%s:%d/%s | %s | affected=%d | %s\n",
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
	return filepath.Join(home, ".dbimpex", auditFileName)
}

func rotateAuditLogs(basePath string) {
	oldest := fmt.Sprintf("%s.%d", basePath, auditMaxBackups)
	os.Remove(oldest)
	for i := auditMaxBackups; i > 1; i-- {
		old := fmt.Sprintf("%s.%d", basePath, i-1)
		new_ := fmt.Sprintf("%s.%d", basePath, i)
		os.Rename(old, new_)
	}
	os.Rename(basePath, basePath+".1")
}
