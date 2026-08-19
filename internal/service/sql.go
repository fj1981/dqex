package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"dbimpex/internal/engine"

	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cydb"
	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cygin"
	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cylog"
)

// 审计来源标记
const (
	auditSourceManual = "manual" // 用户手写执行
	auditSourceCell   = "cell"   // 单元格内联编辑
)

// 审计日志防膨胀参数
const (
	auditFieldLimit = 2000 // 超长字段（sql/error/newValue）截断上限
)

// nowMillis 当前 Unix 毫秒时间戳
func nowMillis() int64 { return time.Now().UnixMilli() }

// appendSQLAudit 将 SQL 执行记录追加写入审计（只增不删）。失败仅记录日志，不阻塞主流程。
// 超长字段截断（防单条过大）。
func (s *Service) appendSQLAudit(entry SQLAuditEntry) {
	// 超长字段截断，防止单条审计过大
	entry.SQL = truncateAuditString(entry.SQL)
	entry.ErrorMsg = truncateAuditString(entry.ErrorMsg)
	if err := s.persist.AppendSQLAudit(entry); err != nil {
		cylog.Warnf("写入 SQL 审计日志失败: %v", err)
	}
}

// truncateAuditString 截断超长审计字段（保留头部），返回带省略标记的字符串。
func truncateAuditString(s string) string {
	if len(s) <= auditFieldLimit {
		return s
	}
	return s[:auditFieldLimit] + "…[截断]"
}

// UpdateTableCell 更新表浏览视图中的单个单元格（named bind + 标识符引用，防注入）。
// 仅用于对象树表浏览（表结构/主键明确）场景；返回影响行数。
func (s *Service) UpdateTableCell(ctx context.Context, connKey, dbName string, p engine.UpdateCellParams) (int64, error) {
	conn, err := s.resolveConn(connKey, nil)
	if err != nil {
		return 0, err
	}
	var cli *cydb.DBCli
	if dbName != "" {
		cli, err = engine.ConnectDB(*conn, dbName)
	} else {
		cli, err = engine.Connect(*conn)
	}
	if err != nil {
		return 0, cygin.WrapError(err, ErrConnFailed, cygin.WithErrPrint(), cygin.WithErrDetails(err.Error()))
	}
	defer cli.Close()

	affected, err := engine.RunParamUpdate(ctx, cli, p)
	if err != nil {
		// 失败也记审计（真实参数），便于排查失败的改动尝试
		s.appendSQLAudit(s.newCellAuditEntry(connKey, dbName, p, 0, "error", err.Error()))
		return 0, err
	}
	// 单元格编辑：不进「SQL 执行历史」（非用户手写 SQL，不可回填），仅审计记真实参数
	s.appendSQLAudit(s.newCellAuditEntry(connKey, dbName, p, int(affected), "ok", ""))
	return affected, nil
}

// GetCellValue 按主键 + 列名定位单行单列，返回该单元格的完整值（大字段懒加载）。
// 仅用于对象树表浏览场景；列名/主键列用标识符引用，值用命名参数绑定，防注入。
func (s *Service) GetCellValue(ctx context.Context, connKey, dbName, table, column string, pkColumns []string, pkValues []any) (any, error) {
	conn, err := s.resolveConn(connKey, nil)
	if err != nil {
		return nil, err
	}
	var cli *cydb.DBCli
	if dbName != "" {
		cli, err = engine.ConnectDB(*conn, dbName)
	} else {
		cli, err = engine.Connect(*conn)
	}
	if err != nil {
		return nil, cygin.WrapError(err, ErrConnFailed, cygin.WithErrPrint(), cygin.WithErrDetails(err.Error()))
	}
	defer cli.Close()

	return engine.GetCellValue(ctx, cli, table, column, pkColumns, pkValues)
}

// DeleteTableRows 删除表浏览中的整行（按主键定位，支持批量）。
// 逐行执行 DELETE（named bind + 标识符引用，防注入）；返回累计影响行数。
func (s *Service) DeleteTableRows(ctx context.Context, connKey, dbName, table string, pkColumns []string, rows [][]any) (int64, error) {
	conn, err := s.resolveConn(connKey, nil)
	if err != nil {
		return 0, err
	}
	var cli *cydb.DBCli
	if dbName != "" {
		cli, err = engine.ConnectDB(*conn, dbName)
	} else {
		cli, err = engine.Connect(*conn)
	}
	if err != nil {
		return 0, cygin.WrapError(err, ErrConnFailed, cygin.WithErrPrint(), cygin.WithErrDetails(err.Error()))
	}
	defer cli.Close()

	var total int64
	for _, pkValues := range rows {
		p := engine.DeleteRowParams{Table: table, PKColumns: pkColumns, PKValues: pkValues}
		affected, err := engine.RunParamDelete(ctx, cli, p)
		if err != nil {
			// 失败也记审计（真实参数），便于排查失败的删除尝试
			s.appendSQLAudit(s.newDeleteAuditEntry(connKey, dbName, table, pkColumns, pkValues, 0, "error", err.Error()))
			return total, err
		}
		total += affected
		s.appendSQLAudit(s.newDeleteAuditEntry(connKey, dbName, table, pkColumns, pkValues, int(affected), "ok", ""))
	}
	return total, nil
}

// InsertTableRow 表浏览视图新增一行（INSERT，named bind + 标识符引用，防注入）。
// columns/values 为用户显式填写的列（自增主键通常不传）；返回影响行数。
func (s *Service) InsertTableRow(ctx context.Context, connKey, dbName string, p engine.InsertRowParams) (int64, error) {
	conn, err := s.resolveConn(connKey, nil)
	if err != nil {
		return 0, err
	}
	var cli *cydb.DBCli
	if dbName != "" {
		cli, err = engine.ConnectDB(*conn, dbName)
	} else {
		cli, err = engine.Connect(*conn)
	}
	if err != nil {
		return 0, cygin.WrapError(err, ErrConnFailed, cygin.WithErrPrint(), cygin.WithErrDetails(err.Error()))
	}
	defer cli.Close()

	affected, err := engine.RunParamInsert(ctx, cli, p)
	if err != nil {
		// 失败也记审计（真实参数），便于排查失败的插入尝试
		s.appendSQLAudit(s.newInsertAuditEntry(connKey, dbName, p, 0, "error", err.Error()))
		return 0, err
	}
	s.appendSQLAudit(s.newInsertAuditEntry(connKey, dbName, p, int(affected), "ok", ""))
	return affected, nil
}

// GenerateSQL 快速生成 SQL 文本（表浏览的行/单元格/过滤条件 → 方言正确的可执行语句）。
// 复用 engine 的 cydb 构建器（InlineLiterals 内联转义），生成仅产出文本不执行、不写审计。
func (s *Service) GenerateSQL(ctx context.Context, connKey, dbName string, p engine.GenSQLParams) (string, error) {
	conn, err := s.resolveConn(connKey, nil)
	if err != nil {
		return "", err
	}
	var cli *cydb.DBCli
	if dbName != "" {
		cli, err = engine.ConnectDB(*conn, dbName)
	} else {
		cli, err = engine.Connect(*conn)
	}
	if err != nil {
		return "", cygin.WrapError(err, ErrConnFailed, cygin.WithErrPrint(), cygin.WithErrDetails(err.Error()))
	}
	defer cli.Close()

	sql, err := engine.GenerateSQL(ctx, cli, p)
	if err != nil {
		return "", err
	}
	return sql, nil
}

// QueryTablePage 对象树数据浏览：分页查询单表数据并一次返回全表总行数。
// 与 RunSQLQuery 不同：这是系统自动生成的浏览查询，不进「SQL 执行历史」、不写审计，
// 避免每次点开表都产生一条孤立的 SELECT COUNT(*) 审计记录（此前由前端二次 COUNT 造成）。
// filters 为列过滤条件（AND 叠加），由 engine 层复用 cydb 条件构建器（值参数化绑定防注入）。
func (s *Service) QueryTablePage(ctx context.Context, connKey, dbName, table string, page, pageSize int, sortSpecs []engine.SortSpec, excludeColumns []string, filters []engine.ColumnFilter) (*engine.TablePageResult, error) {
	conn, err := s.resolveConn(connKey, nil)
	if err != nil {
		return nil, err
	}
	var cli *cydb.DBCli
	if dbName != "" {
		cli, err = engine.ConnectDB(*conn, dbName)
	} else {
		cli, err = engine.Connect(*conn)
	}
	if err != nil {
		return nil, cygin.WrapError(err, ErrConnFailed, cygin.WithErrPrint(), cygin.WithErrDetails(err.Error()))
	}
	defer cli.Close()

	return engine.QueryTablePage(ctx, cli, table, page, pageSize, sortSpecs, excludeColumns, filters)
}

// ExportTableExcel 表数据导出 Excel（应用过滤/排序），返回 xlsx 字节流 + 总行数 + 是否截断。
// 复用 engine.QueryTablePage 拿数据 + excelize 内存生成，不落盘。
func (s *Service) ExportTableExcel(ctx context.Context, connKey, dbName, table string, sortSpecs []engine.SortSpec, filters []engine.ColumnFilter, maxRows int) ([]byte, int64, bool, error) {
	conn, err := s.resolveConn(connKey, nil)
	if err != nil {
		return nil, 0, false, err
	}
	var cli *cydb.DBCli
	if dbName != "" {
		cli, err = engine.ConnectDB(*conn, dbName)
	} else {
		cli, err = engine.Connect(*conn)
	}
	if err != nil {
		return nil, 0, false, cygin.WrapError(err, ErrConnFailed, cygin.WithErrPrint(), cygin.WithErrDetails(err.Error()))
	}
	defer cli.Close()

	return engine.ExportTableExcel(ctx, cli, table, sortSpecs, filters, maxRows)
}

// RunSQLScript 批量执行多语句 SQL（Navicat 式）：按分号分割，逐条判断读写并执行，
// 返回结果集数组（顺序与语句一致）。mode 控制执行模式（transform/raw）。
// 写操作的安全确认由调用方（前端）在进入本函数前完成。
// 均写入「SQL 执行历史」并追加审计日志（用户手写，来源 manual）。
func (s *Service) RunSQLScript(ctx context.Context, connKey, dbName, sql string, limit, offset int, mode string) ([]*engine.SQLQueryResult, error) {
	conn, err := s.resolveConn(connKey, nil)
	if err != nil {
		return nil, err
	}
	var cli *cydb.DBCli
	if dbName != "" {
		cli, err = engine.ConnectDB(*conn, dbName)
	} else {
		cli, err = engine.Connect(*conn)
	}
	if err != nil {
		return nil, cygin.WrapError(err, ErrConnFailed, cygin.WithErrPrint(), cygin.WithErrDetails(err.Error()))
	}
	defer cli.Close()

	results, err := engine.RunSQLScript(ctx, cli, sql, limit, offset, mode)
	if err != nil {
		// 连接/分割级失败：记录历史并审计，返回错误（前端展示）
		item := SQLHistoryItem{ConnID: connKey, DB: dbName, Mode: mode, SQL: sql, Status: "error", ErrorMsg: err.Error(), CreatedAt: nowMillis()}
		s.recordSQL(item, dbName, mode)
		return nil, err
	}
	// 语句级失败（结果中含错误占位）：整次执行记为 error，错误信息取第一条失败语句；
	// 已成功的结果集仍随 results 返回，前端在多结果集 tab 上对应展示（部分成功）
	var firstErr string
	for _, r := range results {
		if r.Error != "" {
			firstErr = r.Error
			break
		}
	}
	if firstErr != "" {
		item := SQLHistoryItem{ConnID: connKey, DB: dbName, Mode: mode, SQL: sql, Status: "error", ErrorMsg: firstErr, CreatedAt: nowMillis()}
		s.recordSQL(item, dbName, mode)
		return results, nil
	}
	// 全部成功：记录历史（汇总）
	totalRows := 0
	hasWrite := false
	for _, r := range results {
		if r.IsWrite {
			hasWrite = true
			totalRows += int(r.AffectedRows)
		} else {
			totalRows += r.RowCount
		}
	}
	item := SQLHistoryItem{
		ConnID: connKey, DB: dbName, Mode: mode, SQL: sql, IsWrite: hasWrite, RowCount: totalRows,
		Elapsed: results[len(results)-1].Elapsed, Status: "ok", CreatedAt: nowMillis(),
	}
	s.recordSQL(item, dbName, mode)
	return results, nil
}

// recordSQL 写入 SQL 执行历史并追加审计日志（来源恒为用户手写 manual）。
func (s *Service) recordSQL(item SQLHistoryItem, dbName, mode string) {
	_ = s.persist.AddSQLHistory(item)
	s.appendSQLAudit(s.newAuditEntry(item.ConnID, dbName, mode, auditSourceManual, item.SQL, item.IsWrite, item.RowCount, item.Elapsed, item.Status, item.ErrorMsg))
}

// newAuditEntry 构造审计条目（SQL 执行类）。
func (s *Service) newAuditEntry(connID, dbName, mode, source, sql string, isWrite bool, rowCount int, elapsed int64, status, errMsg string) SQLAuditEntry {
	return SQLAuditEntry{
		ConnID: connID, DB: dbName, Mode: mode, Source: source, SQL: sql, IsWrite: isWrite,
		RowCount: rowCount, Elapsed: elapsed, Status: status, ErrorMsg: errMsg, CreatedAt: nowMillis(),
	}
}

// newCellAuditEntry 构造审计条目（单元格内联编辑，结构化真实参数）。
func (s *Service) newCellAuditEntry(connID, dbName string, p engine.UpdateCellParams, affected int, status, errMsg string) SQLAuditEntry {
	return SQLAuditEntry{
		ConnID: connID, DB: dbName, Source: auditSourceCell,
		SQL:       fmt.Sprintf("UPDATE %s SET %s WHERE (主键)", p.Table, p.SetColumn),
		IsWrite:   true,
		RowCount:  affected,
		Status:    status,
		ErrorMsg:  errMsg,
		CreatedAt: nowMillis(),
		Table:     p.Table,
		Column:    p.SetColumn,
		NewValue:  p.SetValue,
		PKColumns: p.PKColumns,
		PKValues:  p.PKValues,
	}
}

// newDeleteAuditEntry 构造审计条目（整行删除，结构化真实参数）。
func (s *Service) newDeleteAuditEntry(connID, dbName, table string, pkColumns []string, pkValues []any, affected int, status, errMsg string) SQLAuditEntry {
	return SQLAuditEntry{
		ConnID:    connID,
		DB:        dbName,
		Source:    auditSourceCell,
		SQL:       fmt.Sprintf("DELETE FROM %s WHERE (主键)", table),
		IsWrite:   true,
		RowCount:  affected,
		Status:    status,
		ErrorMsg:  errMsg,
		CreatedAt: nowMillis(),
		Table:     table,
		PKColumns: pkColumns,
		PKValues:  pkValues,
	}
}

// newInsertAuditEntry 构造审计条目（新增行，结构化真实参数）。
func (s *Service) newInsertAuditEntry(connID, dbName string, p engine.InsertRowParams, affected int, status, errMsg string) SQLAuditEntry {
	return SQLAuditEntry{
		ConnID:    connID,
		DB:        dbName,
		Source:    auditSourceCell,
		SQL:       fmt.Sprintf("INSERT INTO %s (%s)", p.Table, strings.Join(p.Columns, ", ")),
		IsWrite:   true,
		RowCount:  affected,
		Status:    status,
		ErrorMsg:  errMsg,
		CreatedAt: nowMillis(),
		Table:     p.Table,
		Column:    strings.Join(p.Columns, ", "),
		NewValue:  p.Values,
	}
}

// PingConnection 检测连接可用性（SELECT 1），返回耗时（毫秒）。
func (s *Service) PingConnection(ctx context.Context, connKey string) (int64, error) {
	conn, err := s.resolveConn(connKey, nil)
	if err != nil {
		return 0, err
	}
	cli, err := engine.Connect(*conn)
	if err != nil {
		return 0, cygin.WrapError(err, ErrConnFailed, cygin.WithErrPrint(), cygin.WithErrDetails(err.Error()))
	}
	defer cli.Close()
	elapsed, err := engine.Ping(ctx, cli)
	if err != nil {
		return 0, cygin.WrapError(err, ErrConnFailed, cygin.WithErrPrint(), cygin.WithErrDetails(err.Error()))
	}
	return elapsed, nil
}

// ObjectDDLResult 对象创建语句查询结果
type ObjectDDLResult struct {
	Type string `json:"type"` // table / view / function / procedure
	Name string `json:"name"`
	DDL  string `json:"ddl"`
}

// GetObjectDDL 获取指定对象（表/视图/函数/存储过程）的创建语句。
// dbName 覆盖连接默认库（Oracle 下即 schema）；连接复用短生命周期、调用方无需关心。
func (s *Service) GetObjectDDL(connKey, dbName, objType, name string) (*ObjectDDLResult, error) {
	conn, err := s.resolveConn(connKey, nil)
	if err != nil {
		return nil, err
	}
	if dbName != "" {
		conn.DBName = dbName
	}
	cli, err := engine.Connect(*conn)
	if err != nil {
		return nil, cygin.WrapError(err, ErrConnFailed, cygin.WithErrPrint(), cygin.WithErrDetails(err.Error()))
	}
	defer cli.Close()

	ddl, err := engine.GetObjectDDL(cli, engine.ObjectDDLType(objType), name)
	if err != nil {
		return nil, cygin.WrapError(err, ErrExecFailed, cygin.WithErrPrint(), cygin.WithErrDetails(err.Error()))
	}
	return &ObjectDDLResult{Type: objType, Name: name, DDL: ddl}, nil
}

// SQLHistory 返回某连接的历史记录
func (s *Service) SQLHistory(connID string) []SQLHistoryItem {
	return s.persist.ListSQLHistory(connID)
}

// ClearSQLHistory 清空某连接的历史记录
func (s *Service) ClearSQLHistory(connID string) {
	_ = s.persist.ClearSQLHistory(connID)
}

// ---- SQL 收藏（独立表，按连接隔离） ----

// AddFavorite 新增一条收藏。校验 SQL 非空、长度合理，标题缺省时取首行。
func (s *Service) AddFavorite(f *SQLFavorite) error {
	if f == nil || f.ConnID == "" {
		return errors.New("connId 必填")
	}
	if strings.TrimSpace(f.SQL) == "" {
		return errors.New("SQL 不可为空")
	}
	if len(f.SQL) > 64*1024 {
		return errors.New("SQL 过长（≤64KB）")
	}
	if f.Title == "" {
		f.Title = defaultFavoriteTitle(f.SQL)
	}
	if len(f.Title) > 256 {
		f.Title = f.Title[:256]
	}
	if f.CreatedAt == 0 {
		f.CreatedAt = nowMillis()
	}
	return s.persist.AddFavorite(f)
}

// ListFavorites 返回全部收藏（全局共享，不按连接隔离；新→旧）
func (s *Service) ListFavorites() []*SQLFavorite {
	return s.persist.ListFavorites()
}

// DeleteFavorite 删除收藏（按全局唯一 id 定位）
func (s *Service) DeleteFavorite(id string) error {
	if id == "" {
		return errors.New("id 必填")
	}
	return s.persist.DeleteFavorite(id)
}

// RenameFavorite 重命名收藏（按全局唯一 id 定位）
func (s *Service) RenameFavorite(id, title string) error {
	if id == "" {
		return errors.New("id 必填")
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return errors.New("标题不可为空")
	}
	if len(title) > 256 {
		title = title[:256]
	}
	return s.persist.RenameFavorite(id, title)
}

// defaultFavoriteTitle 取 SQL 去注释后首行前 40 字符作为默认标题。
func defaultFavoriteTitle(sql string) string {
	var first string
	for _, line := range strings.Split(sql, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "--") || strings.HasPrefix(t, "#") || strings.HasPrefix(t, "/*") {
			continue
		}
		first = t
		break
	}
	if first == "" {
		first = strings.TrimSpace(sql)
	}
	first = strings.TrimSuffix(first, ";")
	if len([]rune(first)) > 40 {
		first = string([]rune(first)[:40]) + "…"
	}
	return first
}

// SQLAudit 读取某连接的审计日志（倒序，分页）。审计只读、不提供删除。
// connID 为空时返回全部连接；limit<=0 时默认 100，上限 500。
func (s *Service) SQLAudit(connID string, limit, offset int) ([]SQLAuditEntry, error) {
	return s.persist.ListSQLAudit(connID, limit, offset)
}

// SaveWorkspace 保存某连接的工作区（整体覆盖）。
func (s *Service) SaveWorkspace(connID string, state WorkspaceState) error {
	return s.persist.SaveWorkspace(connID, state)
}

// LoadWorkspace 读取某连接的工作区；无记录时返回空状态。
func (s *Service) LoadWorkspace(connID string) (WorkspaceState, bool) {
	return s.persist.LoadWorkspace(connID)
}

// DeleteWorkspace 删除某连接的工作区。
func (s *Service) DeleteWorkspace(connID string) error {
	return s.persist.DeleteWorkspace(connID)
}
