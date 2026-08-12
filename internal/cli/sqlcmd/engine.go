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

	tableCache []string
	lastSQL    string
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
				fmt.Fprintf(os.Stderr, "%s\n", red(err.Error()))
			}
		} else {
			s.showConnInfo()
		}
		return true, false
	case "\\timing":
		return true, false
	case "\\g":
		if s.lastSQL != "" {
			ctx := context.Background()
			result, err := s.execute(ctx, s.lastSQL)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s\n", red(err.Error()))
			} else {
				outputTable(result)
			}
		}
		return true, false
	case "\\G":
		return false, false
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
	}
	return false, false
}

func (s *session) showConnInfo() {
	info := fmt.Sprintf("连接信息: %s @ %s:%d/%s",
		s.dbType, s.connInfo.Host, s.connInfo.Port, s.currentDB)
	if s.connInfo.SubType != "" {
		info += fmt.Sprintf(" (%s)", s.connInfo.SubType)
	}
	fmt.Println(info)
}

func (s *session) showTables(filter []string) {
	tables, err := s.cli.GetTables(s.currentDB, nil, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", red(fmt.Sprintf("获取表列表失败: %v", err)))
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
		fmt.Println(dim("(无匹配表)"))
		return
	}
	sort.Strings(filtered)
	for _, t := range filtered {
		fmt.Println(t)
	}
}

func (s *session) descTable(name string) {
	info, err := s.cli.GetTableInfo(name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", red(fmt.Sprintf("获取表结构失败: %v", err)))
		return
	}
	columns := info.GetColumns()
	if len(columns) == 0 {
		fmt.Println(dim("(无列信息)"))
		return
	}
	cols := []string{"列名", "类型", "可空", "默认值", "说明"}
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
	s.descTable(name)
}

func (s *session) showDatabases() {
	dbs, err := s.cli.GetDatabases()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", red(fmt.Sprintf("获取数据库列表失败: %v", err)))
		return
	}
	for _, db := range dbs {
		marker := ""
		if strings.EqualFold(db, s.currentDB) {
			marker = " " + green("*")
		}
		fmt.Printf("%s%s\n", db, marker)
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
	if s.lastSQL == "" {
		fmt.Fprintln(os.Stderr, red("没有上一条查询结果可导出"))
		return
	}
	ctx := context.Background()
	result, err := s.execute(ctx, s.lastSQL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", red(err.Error()))
		return
	}
	f, err := os.Create(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", red(err.Error()))
		return
	}
	defer f.Close()
	fmt.Fprintln(f, strings.Join(result.Columns, ","))
	for _, row := range result.Rows {
		vals := make([]string, len(row))
		for i, v := range row {
			vals[i] = fmt.Sprintf("%v", v)
		}
		fmt.Fprintln(f, strings.Join(vals, ","))
	}
	fmt.Printf("已导出 %d 行到 %s\n", result.RowCount, path)
}

func executeFile(s *session, path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", red(err.Error()))
		return
	}
	sql := string(data)
	fmt.Printf("执行 %s...\n", path)
	ctx := context.Background()
	result, err := s.execute(ctx, sql)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", red(err.Error()))
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
		"\\timing", "\\g", "\\G", "\\p", "\\print", "\\r", "\\reset",
		"\\h", "\\help", "\\w", "\\write", "\\i", "\\include",
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
	if s.lastSQL == "" {
		fmt.Fprintln(os.Stderr, red("没有上一条 SQL 可编辑"))
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
		fmt.Fprintf(os.Stderr, "%s\n", red(err.Error()))
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
		fmt.Fprintf(os.Stderr, "%s\n", red(err.Error()))
		return
	}

	// 读取编辑后的内容
	data, err := os.ReadFile(tmpFile.Name())
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", red(err.Error()))
		return
	}
	edited := strings.TrimSpace(string(data))
	if edited != "" && edited != s.lastSQL {
		s.lastSQL = edited
		fmt.Println(dim("SQL 已更新"))
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
	fmt.Println(`元命令:
  \q, \quit           退出
  \dt, \tables [pat]  列出表（支持通配符 * ?）
  \d, \desc  <表名>    查看表结构
  \d+ <表名>           查看表结构（含索引/约束）
  \l, \list            列出数据库
  \c, \use  <库名>     切换数据库
  \c                   查看当前连接信息
  \timing              切换耗时显示
  \g                   再次执行上一条 SQL
  \G                   垂直显示（每行一个字段）
  \p, \print           打印当前缓冲区
  \r, \reset           清空缓冲区
  \h, \help            显示此帮助
  \e, \edit           用外部编辑器编辑上一条 SQL
  \w <文件>             导出结果到 CSV
  \i <文件>             执行文件中的 SQL

快捷键:
  Enter (分号结尾)      执行 SQL
  Enter (无分号)        多行续写
  Ctrl+R               搜索历史
  Tab                  自动补全
  Ctrl+C               取消输入（空行退出）
  Ctrl+D               退出`)
}
