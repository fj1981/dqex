package sqlcmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"dbimpex/internal/engine"

	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cydb"
)

// ---- 会话 ----

type session struct {
	connInfo  engine.DBConnInfo
	cli       *cydb.DBCli
	currentDB string
	dbType    string

	tableCache  []string
	lastSQL     string
	lastErr     string   // 最近一次执行报错（供 \ai fix 自动携带）
	displayMode string   // 结果展示模式："" / "auto"=超宽自动降级；"table"=强制表格；"vertical"=强制垂直
	ai          *aiState // AI 会话状态（懒加载；切换数据库时重置）
}

func newSession(info *engine.DBConnInfo) (*session, error) {
	cliDB, err := engine.Connect(*info)
	if err != nil {
		return nil, err
	}
	return &session{
		connInfo:  *info,
		cli:       cliDB,
		currentDB: info.DBName,
		dbType:    info.Type,
	}, nil
}

func (s *session) close() {
	if s.cli != nil {
		s.cli.Close()
	}
}

func (s *session) switchDB(newDB string) error {
	s.cli.Close()
	s.connInfo.DBName = newDB
	cliDB, err := engine.Connect(s.connInfo)
	if err != nil {
		return fmt.Errorf("切换数据库失败: %w", err)
	}
	s.cli = cliDB
	s.currentDB = newDB
	s.tableCache = nil
	s.ai = nil // 换库后 AI 会话（schema/上下文）失效，下次 \ai 自动重建
	return nil
}

func (s *session) reconnect() error {
	s.cli.Close()
	cliDB, err := engine.Connect(s.connInfo)
	if err != nil {
		return err
	}
	s.cli = cliDB
	return nil
}

func (s *session) execute(ctx context.Context, sql string) (*queryResult, error) {
	// 拦截 USE 语句：切换数据库连接池
	if newDB := parseUseDB(sql); newDB != "" {
		if err := s.switchDB(newDB); err != nil {
			return nil, err
		}
		return &queryResult{
			SQL:     sql,
			Elapsed: 0,
		}, nil
	}

	isWrite := classifySQL(sql)
	start := time.Now()

	if !isWrite {
		processedSQL := sql
		if !hasLimitClause(sql) && shouldAddLimit(sql) {
			processedSQL = addLimit(sql, defaultMaxRows)
		}
		rows, columns, err := s.cli.DirectQueryFastContext(ctx, processedSQL)
		if err != nil {
			if isConnErr(err) {
				if re := s.reconnect(); re == nil {
					rows, columns, err = s.cli.DirectQueryFastContext(ctx, processedSQL)
				}
			}
			if err != nil {
				return nil, err
			}
		}
		// 0 行结果时 cydb 可能不返回列信息：兜底为空切片，避免渲染/JSON 输出歧义
		if columns == nil {
			columns = []string{}
		}
		return &queryResult{
			Columns:  columns,
			Rows:     rows,
			RowCount: len(rows),
			Elapsed:  time.Since(start),
			SQL:      processedSQL,
		}, nil
	}

	affected, err := s.cli.DirectExecuteContext(ctx, sql)
	if err != nil {
		if isConnErr(err) {
			if re := s.reconnect(); re == nil {
				affected, err = s.cli.DirectExecuteContext(ctx, sql)
			}
		}
		if err != nil {
			return nil, err
		}
	}
	return &queryResult{
		AffectedRows: affected,
		Elapsed:      time.Since(start),
		SQL:          sql,
		IsWrite:      true,
	}, nil
}

// ---- 元命令 ----

func (s *session) handleMeta(cmd string) (bool, bool) {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" || cmd[0] != '\\' {
		return false, false
	}
	parts := strings.Fields(cmd)
	name := parts[0]
	args := parts[1:]
	txt := cliTextsFor(cliLang)
	switch name {
	case "\\q", "\\quit":
		return true, true
	case "\\dt", "\\tables":
		s.showTables(args)
		return true, false
	case "\\d":
		if len(args) > 0 {
			s.descTable(args[0])
		}
		return true, false
	case "\\d+":
		if len(args) > 0 {
			s.descTableVerbose(args[0])
		}
		return true, false
	case "\\l", "\\list", "\\databases":
		s.showDatabases()
		return true, false
	case "\\c", "\\use", "\\connect":
		if len(args) > 0 {
			if err := s.switchDB(args[0]); err != nil {
				fprintf(os.Stderr, "%s\n", red(err.Error()))
			}
		} else {
			s.showConnInfo()
		}
		return true, false
	case "\\timing":
		showTiming = !showTiming
		if showTiming {
			fmt.Println(dim(txt.timingOn))
		} else {
			fmt.Println(dim(txt.timingOff))
		}
		return true, false
	case "\\x":
		// psql 风格扩展显示开关：\x [on|off|auto]，无参数时 toggle；切换后写回 config.yaml
		mode := ""
		if len(args) > 0 {
			mode = strings.ToLower(args[0])
		}
		switch mode {
		case "on":
			s.displayMode = "vertical"
		case "off":
			s.displayMode = "table"
		case "auto":
			s.displayMode = "auto"
		default:
			// 无参数：toggle（auto 视为扩展中 → 关闭，与 psql 行为一致）
			if s.displayMode == "vertical" {
				s.displayMode = "table"
			} else {
				s.displayMode = "vertical"
			}
		}
		s.saveDisplayMode()
		fmt.Println(dim(sprintf(txt.xDisplaySaved, displayModeLabel(s.displayMode))))
		return true, false
	case "\\g", "\\G":
		s.runLastSQL(name == "\\G")
		return true, false
	case "\\p", "\\print":
		fmt.Println(s.lastSQL)
		return true, false
	case "\\r", "\\reset":
		s.lastSQL = ""
		return true, false
	case "\\h", "\\help":
		printHelp()
		return true, false
	case "\\e", "\\edit":
		s.editLastSQL()
		return true, false
	case "\\copy":
		if len(args) > 0 {
			exportToFile(s, args[0])
		} else {
			fmt.Fprintln(os.Stderr, red(txt.copyUsage))
		}
		return true, false
	case "\\w", "\\write":
		if len(args) > 0 {
			exportToFile(s, args[0])
		}
		return true, false
	case "\\i", "\\include":
		if len(args) > 0 {
			executeFile(s, args[0])
		}
		return true, false
	case "\\ai":
		s.aiCommand(args)
		return true, false
	}
	return false, false
}

// runLastSQL 重新执行上一条 SQL（缓冲区）；vertical 为 true 时垂直显示（\G）。
func (s *session) runLastSQL(vertical bool) {
	txt := cliTextsFor(cliLang)
	if s.lastSQL == "" {
		return
	}
	ctx := context.Background()
	result, err := s.execute(ctx, s.lastSQL)
	if err != nil {
		s.lastErr = err.Error()
		fprintf(os.Stderr, "%s\n", red(err.Error()))
		fmt.Fprintln(os.Stderr, dim(txt.aiFixHint))
		return
	}
	s.renderResult(result, vertical)
}

// renderResult 按显示模式渲染查询结果；forceVertical 为 \G / SQL 后缀 \G 的单次垂直。
func (s *session) renderResult(r *queryResult, forceVertical bool) {
	if forceVertical || s.displayMode == "vertical" {
		outputVertical(r)
		return
	}
	if s.displayMode == "table" {
		outputTable(r)
		return
	}
	maybePage(r) // auto：表格超宽时自动降级为垂直显示
}

// saveDisplayMode 将当前显示模式写回 config.yaml（cli.display_mode），下次启动作为默认生效。
func (s *session) saveDisplayMode() {
	txt := cliTextsFor(cliLang)
	svc, err := newAIService("", "")
	if err != nil {
		fmt.Fprintln(os.Stderr, dim(sprintf(txt.displayModeSaveFail, err.Error())))
		return
	}
	cfg := *svc.Config()
	cfg.CLI.DisplayMode = s.displayMode
	if err := svc.SaveConfig(cfg); err != nil {
		fmt.Fprintln(os.Stderr, dim(sprintf(txt.displayModeSaveFail, err.Error())))
	}
}

// displayModeLabel 显示模式的友好描述（按 CLI 语言）。
func displayModeLabel(mode string) string {
	txt := cliTextsFor(cliLang)
	switch mode {
	case "vertical":
		return txt.xModeVertical
	case "table":
		return txt.xModeTable
	default:
		return txt.xModeAuto
	}
}

func (s *session) showConnInfo() {
	txt := cliTextsFor(cliLang)
	info := sprintf(txt.bannerConn, s.dbType, s.connInfo.Host, s.connInfo.Port, s.currentDB)
	if s.connInfo.SubType != "" {
		info += sprintf(" (%s)", s.connInfo.SubType)
	}
	fmt.Println(info)
}

func (s *session) showTables(filter []string) {
	txt := cliTextsFor(cliLang)
	tables, err := s.cli.GetTables(s.currentDB, nil, nil)
	if err != nil {
		fprintf(os.Stderr, "%s\n", red(sprintf(txt.failListTables, err)))
		return
	}
	filtered := tables
	if len(filter) > 0 {
		pattern := filter[0]
		filtered = nil
		for _, t := range tables {
			if matchPattern(t, pattern) {
				filtered = append(filtered, t)
			}
		}
	}
	s.tableCache = filtered
	if len(filtered) == 0 {
		fmt.Println(dim(txt.noTables))
		return
	}
	sort.Strings(filtered)
	for _, t := range filtered {
		fmt.Println(t)
	}
}

func (s *session) descTable(name string) {
	txt := cliTextsFor(cliLang)
	info, err := s.cli.GetTableInfo(name)
	if err != nil {
		fprintf(os.Stderr, "%s\n", red(sprintf(txt.failTableMeta, err)))
		return
	}
	columns := info.GetColumns()
	if len(columns) == 0 {
		fmt.Println(dim(txt.noColumns))
		return
	}
	cols := []string{txt.colName, txt.colType, txt.colNullable, txt.colDefault, txt.colComment}
	rows := make([][]any, 0, len(columns))
	for _, c := range columns {
		nullable := "YES"
		if c.IsNotNull() {
			nullable = "NO"
		}
		defaultVal := ""
		if d := c.GetDefault(); d != nil {
			defaultVal = *d
		}
		rows = append(rows, []any{c.GetName(), c.GetOrginalDataType(), nullable, defaultVal, c.GetComment()})
	}
	outputTable(&queryResult{Columns: cols, Rows: rows, RowCount: len(rows)})
}

func (s *session) descTableVerbose(name string) {
	txt := cliTextsFor(cliLang)
	s.descTable(name)
	// 补充索引信息
	fmt.Println()
	indexes, err := s.cli.GetIndexes(name)
	if err != nil {
		fmt.Fprintln(os.Stderr, dim(sprintf(txt.failIndexInfo, err)))
		return
	}
	if len(indexes) == 0 {
		fmt.Println(dim(txt.noIndexes))
		return
	}
	fmt.Println(bold(txt.indexesTitle))
	for _, idx := range indexes {
		marker := ""
		if idx.IsPrimary {
			marker = " " + green("PRIMARY")
		} else if idx.IsUnique {
			marker = " " + yellow("UNIQUE")
		}
		cols := strings.Join(idx.Columns, ", ")
		if cols == "" {
			cols = idx.Name
		}
		printf("  %s%s: %s\n", idx.Name, marker, cols)
	}
}

func (s *session) showDatabases() {
	txt := cliTextsFor(cliLang)
	dbs, err := s.cli.GetDatabases()
	if err != nil {
		fprintf(os.Stderr, "%s\n", red(sprintf(txt.failListDBs, err)))
		return
	}
	for _, db := range dbs {
		marker := ""
		if strings.EqualFold(db, s.currentDB) {
			marker = " " + green("*")
		}
		printf("%s%s\n", db, marker)
	}
}

func matchPattern(s, pattern string) bool {
	pattern = strings.ReplaceAll(pattern, "*", ".*")
	pattern = strings.ReplaceAll(pattern, "?", ".")
	trimmed := strings.Trim(pattern, ".*")
	return strings.Contains(strings.ToLower(s), strings.ToLower(trimmed))
}

// ---- 文件操作 ----

func exportToFile(s *session, path string) {
	txt := cliTextsFor(cliLang)
	if s.lastSQL == "" {
		fmt.Fprintln(os.Stderr, red(txt.noExportSQL))
		return
	}
	ctx := context.Background()
	result, err := s.execute(ctx, s.lastSQL)
	if err != nil {
		fprintf(os.Stderr, "%s\n", red(err.Error()))
		return
	}
	f, err := os.Create(path)
	if err != nil {
		fprintf(os.Stderr, "%s\n", red(err.Error()))
		return
	}
	defer f.Close()
	fmt.Fprintln(f, strings.Join(csvFields(result.Columns), ","))
	for _, row := range result.Rows {
		vals := make([]string, len(row))
		for i, v := range row {
			vals[i] = csvField(sprintf("%v", v))
		}
		fmt.Fprintln(f, strings.Join(vals, ","))
	}
	printf(txt.exportedRows+"\n", result.RowCount, path)
}

// csvFields 批量将字段转为 CSV 转义文本。
func csvFields(cols []string) []string {
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = csvField(c)
	}
	return out
}

// csvField 标准 CSV 字段转义：含逗号/引号/换行时用双引号包裹（内部双引号翻倍）。
func csvField(s string) string {
	if strings.ContainsAny(s, ",\"\n\r") {
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return s
}

func executeFile(s *session, path string) {
	txt := cliTextsFor(cliLang)
	data, err := os.ReadFile(path)
	if err != nil {
		fprintf(os.Stderr, "%s\n", red(err.Error()))
		return
	}
	sql := string(data)
	printf(txt.execFile+"\n", path)
	ctx := context.Background()
	result, err := s.execute(ctx, sql)
	if err != nil {
		fprintf(os.Stderr, "%s\n", red(err.Error()))
		return
	}
	outputTable(result)
}

// ---- 自动补全 ----

func (s *session) complete(line string, pos int) (head string, completions []string, tail string) {
	if pos <= 0 {
		return "", nil, ""
	}
	word, start := currentWord(line, pos)
	prefix := line[:start]
	head = prefix

	// 元命令补全
	if strings.HasPrefix(word, "\\") {
		return head, completeMeta(word), ""
	}

	// 上下文感知补全
	prevWord := lastWord(prefix)
	upperPrev := strings.ToUpper(prevWord)
	upperWord := strings.ToUpper(word)

	switch upperPrev {
	case "USE":
		// USE 后面补全数据库名
		return head, s.completeDatabases(upperWord), ""
	case "SHOW":
		// SHOW 后面补全子命令
		return head, completeShowSub(upperWord), ""
	case "FROM", "JOIN", "INNER", "LEFT", "RIGHT", "OUTER", "INTO", "UPDATE", "TABLE":
		// 表名上下文：缓存为空时同步加载
		if s.tableCache == nil {
			s.refreshMetadata()
		}
		if s.tableCache != nil {
			return head, completeTables(s.tableCache, word), ""
		}
	case "DESC", "DESCRIBE":
		// 表名上下文：缓存为空时同步加载
		if s.tableCache == nil {
			s.refreshMetadata()
		}
		if s.tableCache != nil {
			return head, completeTables(s.tableCache, word), ""
		}
	}

	// 默认：关键字 + 表名
	completions = completeKeywords(word)
	if s.tableCache != nil {
		completions = append(completions, completeTables(s.tableCache, word)...)
	}
	return head, deduplicate(completions), ""
}

// lastWord 获取前缀中最后一个单词。
func lastWord(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return ""
	}
	parts := strings.Fields(prefix)
	return parts[len(parts)-1]
}

// completeShowSub SHOW 子命令补全。
func completeShowSub(word string) []string {
	subs := []string{
		"TABLES", "DATABASES", "COLUMNS", "INDEX", "CREATE TABLE",
		"TABLE STATUS", "PROCESSLIST", "VARIABLES", "STATUS",
		"ENGINES", "WARNINGS", "ERRORS", "GRANTS",
		"FULL TABLES", "FULL COLUMNS", "FULL PROCESSLIST",
	}
	var result []string
	for _, s := range subs {
		if strings.HasPrefix(s, word) {
			result = append(result, s)
		}
	}
	return result
}

// completeDatabases 数据库名补全。
func (s *session) completeDatabases(word string) []string {
	dbs, err := s.cli.GetDatabases()
	if err != nil {
		return nil
	}
	var result []string
	upper := strings.ToUpper(word)
	for _, db := range dbs {
		if strings.HasPrefix(strings.ToUpper(db), upper) {
			result = append(result, db)
		}
	}
	return result
}

func currentWord(line string, pos int) (string, int) {
	if pos > len(line) {
		pos = len(line)
	}
	start := pos - 1
	for start >= 0 && !isWordBoundary(line[start]) {
		start--
	}
	start++
	return line[start:pos], start
}

func isWordBoundary(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == ',' || b == '(' || b == ')'
}

func completeMeta(word string) []string {
	cmds := []string{
		"\\q", "\\quit", "\\dt", "\\tables", "\\d", "\\desc", "\\d+",
		"\\l", "\\list", "\\databases", "\\c", "\\use", "\\connect",
		"\\timing", "\\g", "\\G", "\\x", "\\p", "\\print", "\\r", "\\reset",
		"\\h", "\\help", "\\copy", "\\w", "\\write", "\\i", "\\include",
		"\\ai",
	}
	var result []string
	for _, c := range cmds {
		if strings.HasPrefix(c, word) {
			result = append(result, c)
		}
	}
	return result
}

func completeKeywords(word string) []string {
	keywords := []string{
		"SELECT", "FROM", "WHERE", "AND", "OR", "NOT", "IN", "BETWEEN", "LIKE",
		"INSERT", "INTO", "VALUES", "UPDATE", "SET", "DELETE",
		"CREATE", "ALTER", "DROP", "TABLE", "INDEX",
		"JOIN", "LEFT", "RIGHT", "INNER", "OUTER", "ON",
		"GROUP", "BY", "ORDER", "ASC", "DESC", "HAVING",
		"LIMIT", "OFFSET", "UNION", "ALL", "DISTINCT",
		"AS", "IS", "NULL", "TRUE", "FALSE",
		"CASE", "WHEN", "THEN", "ELSE", "END",
		"COUNT", "SUM", "AVG", "MAX", "MIN",
		"SHOW", "DESC", "DESCRIBE", "EXPLAIN", "USE",
	}
	upper := strings.ToUpper(word)
	var result []string
	for _, kw := range keywords {
		if strings.HasPrefix(kw, upper) {
			result = append(result, kw)
		}
	}
	return result
}

func completeTables(tables []string, word string) []string {
	maxCandidates := 500
	if len(tables) > maxCandidates {
		tables = tables[:maxCandidates]
	}
	word = strings.ToLower(strings.Trim(word, "`\" "))
	var result []string
	added := 0
	for _, t := range tables {
		if strings.Contains(strings.ToLower(t), word) {
			result = append(result, t)
			added++
			if added >= 200 {
				break
			}
		}
	}
	return result
}

func isAfterFromOrJoin(prefix string) bool {
	upper := strings.ToUpper(strings.TrimSpace(prefix))
	return strings.HasSuffix(upper, "FROM") ||
		strings.HasSuffix(upper, "JOIN") ||
		strings.HasSuffix(upper, "INNER JOIN") ||
		strings.HasSuffix(upper, "LEFT JOIN") ||
		strings.HasSuffix(upper, "RIGHT JOIN") ||
		strings.HasSuffix(upper, "OUTER JOIN") ||
		strings.HasSuffix(upper, ",")
}

func deduplicate(items []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, item := range items {
		if !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}
	return result
}

// ---- 辅助 ----

const defaultMaxRows = 1000

func isConnErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "bad connection") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "EOF")
}

// editLastSQL 用外部编辑器打开上一条 SQL。
func (s *session) editLastSQL() {
	txt := cliTextsFor(cliLang)
	if s.lastSQL == "" {
		fmt.Fprintln(os.Stderr, red(txt.noEditSQL))
		return
	}
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor == "" {
		// Windows 降级
		if _, err := exec.LookPath("notepad"); err == nil {
			editor = "notepad"
		} else {
			editor = "vim"
		}
	}

	// 写入临时文件
	tmpFile, err := os.CreateTemp("", "dbx-edit-*.sql")
	if err != nil {
		fprintf(os.Stderr, "%s\n", red(err.Error()))
		return
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.WriteString(s.lastSQL)
	tmpFile.Close()

	// 启动编辑器
	cmd := exec.Command(editor, tmpFile.Name())
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fprintf(os.Stderr, "%s\n", red(err.Error()))
		return
	}

	// 读取编辑后的内容
	data, err := os.ReadFile(tmpFile.Name())
	if err != nil {
		fprintf(os.Stderr, "%s\n", red(err.Error()))
		return
	}
	edited := strings.TrimSpace(string(data))
	if edited != "" && edited != s.lastSQL {
		s.lastSQL = edited
		fmt.Println(dim(txt.sqlUpdated))
	}
}

// parseUseDB 从 SQL 中提取 USE 目标库名，非 USE 语句返回空。
func parseUseDB(sql string) string {
	upper := strings.ToUpper(strings.TrimSpace(sql))
	if !strings.HasPrefix(upper, "USE ") {
		return ""
	}
	// 去掉 USE 前缀，去掉分号和反引号
	db := strings.TrimPrefix(upper, "USE ")
	db = strings.TrimSuffix(db, ";")
	db = strings.Trim(db, "`\" ")
	return db
}

func printHelp() {
	fmt.Println(cliTextsFor(cliLang).metaHelp)
}
