package service

import (
	"context"
	"fmt"
	"time"

	"dbimpex/internal/engine"

	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cydb"
	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cygin"
	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cylog"
)

// 审计来源标记
const (
	auditSourceManual = "manual" // 用户手写执行
	auditSourceTree   = "tree"   // 对象树点开自动生成的查询（浏览表数据/统计行数）
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

// RunSQLQuery 执行查询类 SQL（SELECT/SHOW 等），自动追加 LIMIT 护栏。
// dbName 覆盖连接默认库（点对象树查表时必传），确保跨库查询时 SQL 有库上下文。
// mode 控制执行模式：transform（解析重构 + 补 LIMIT，默认）/ raw（原始直传，不做转换限制）。
func (s *Service) RunSQLQuery(ctx context.Context, connKey, dbName, sql string, limit, offset int, mode string) (*engine.SQLQueryResult, error) {
	conn, err := s.resolveConn(connKey, nil)
	if err != nil {
		return nil, err
	}
	var cli *cydb.DBCli
	if dbName != "" {
		// 指定库时按库建立连接（覆盖连接默认库），复用 ConnectDB
		cli, err = engine.ConnectDB(*conn, dbName)
	} else {
		cli, err = engine.Connect(*conn)
	}
	if err != nil {
		return nil, cygin.WrapError(err, ErrConnFailed, cygin.WithErrPrint(), cygin.WithErrDetails(err.Error()))
	}
	defer cli.Close()

	result, err := engine.RunSQLQuery(ctx, cli, sql, limit, offset, mode)
	if err != nil {
		// 执行失败作为结果返回（HTTP 200），错误写入结果对象，前端结果区展示
		result = &engine.SQLQueryResult{
			SQL:     sql,
			IsWrite: false,
			Error:   err.Error(),
			Elapsed: 0,
		}
		item := SQLHistoryItem{ConnID: connKey, DB: dbName, Mode: mode, SQL: sql, Status: "error", ErrorMsg: err.Error(), CreatedAt: nowMillis()}
		_ = s.persist.AddSQLHistory(item)
		s.appendSQLAudit(s.newAuditEntry(connKey, dbName, mode, auditSourceManual, sql, false, 0, 0, "error", err.Error()))
		return result, nil
	}
	item := SQLHistoryItem{
		ConnID: connKey, DB: dbName, Mode: mode, SQL: sql, IsWrite: false, RowCount: result.RowCount,
		Elapsed: result.Elapsed, Status: "ok", CreatedAt: nowMillis(),
	}
	_ = s.persist.AddSQLHistory(item)
	s.appendSQLAudit(s.newAuditEntry(connKey, dbName, mode, auditSourceManual, sql, false, result.RowCount, result.Elapsed, "ok", ""))
	return result, nil
}

// RunSQLExec 执行写操作 SQL（INSERT/UPDATE/DELETE/DDL），危险语句已由引擎拦截。
// dbName 覆盖连接默认库（点对象树时必传）。
func (s *Service) RunSQLExec(ctx context.Context, connKey, dbName, sql string) (*engine.SQLQueryResult, error) {
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

	result, err := engine.RunSQLExec(ctx, cli, sql)
	if err != nil {
		// 执行失败作为结果返回（HTTP 200），错误写入结果对象，前端结果区展示
		result = &engine.SQLQueryResult{
			SQL:     sql,
			IsWrite: true,
			Error:   err.Error(),
			Elapsed: 0,
		}
		item := SQLHistoryItem{ConnID: connKey, DB: dbName, SQL: sql, IsWrite: true, Status: "error", ErrorMsg: err.Error(), CreatedAt: nowMillis()}
		_ = s.persist.AddSQLHistory(item)
		s.appendSQLAudit(s.newAuditEntry(connKey, dbName, "", auditSourceManual, sql, true, 0, 0, "error", err.Error()))
		return result, nil
	}
	item := SQLHistoryItem{
		ConnID: connKey, DB: dbName, SQL: sql, IsWrite: true, RowCount: int(result.AffectedRows),
		Elapsed: result.Elapsed, Status: "ok", CreatedAt: nowMillis(),
	}
	_ = s.persist.AddSQLHistory(item)
	s.appendSQLAudit(s.newAuditEntry(connKey, dbName, "", auditSourceManual, sql, true, int(result.AffectedRows), result.Elapsed, "ok", ""))
	return result, nil
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

// RunSQLScript 批量执行多语句 SQL（Navicat 式）：按分号分割，逐条判断读写并执行，
// 返回结果集数组（顺序与语句一致）。mode 控制执行模式（transform/raw）。
// 写操作的安全确认由调用方（前端）在进入本函数前完成。
// recordHistory 控制是否写入「SQL 执行历史」：对象树点开自动生成的查询（浏览表数据/统计行数）
// 传 false，避免系统生成语句污染历史；用户手动执行传 true。审计日志始终保留，不受此开关影响。
func (s *Service) RunSQLScript(ctx context.Context, connKey, dbName, sql string, limit, offset int, mode string, recordHistory bool) ([]*engine.SQLQueryResult, error) {
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
		// 执行失败：记录历史（受 recordHistory 开关控制），返回错误（前端展示）
		item := SQLHistoryItem{ConnID: connKey, DB: dbName, Mode: mode, SQL: sql, Status: "error", ErrorMsg: err.Error(), CreatedAt: nowMillis()}
		s.recordSQL(item, recordHistory, dbName, mode)
		return nil, err
	}
	// 记录历史（汇总）
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
	s.recordSQL(item, recordHistory, dbName, mode)
	return results, nil
}

// recordSQL 写入 SQL 执行历史（受 recordHistory 开关控制）并始终追加审计日志。
// 来源由 recordHistory 推断：true=用户手写（manual），false=对象树自动查询（tree）。
func (s *Service) recordSQL(item SQLHistoryItem, recordHistory bool, dbName, mode string) {
	source := auditSourceManual
	if !recordHistory {
		source = auditSourceTree
	}
	if recordHistory {
		_ = s.persist.AddSQLHistory(item)
	}
	s.appendSQLAudit(s.newAuditEntry(item.ConnID, dbName, mode, source, item.SQL, item.IsWrite, item.RowCount, item.Elapsed, item.Status, item.ErrorMsg))
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
		SQL:      fmt.Sprintf("UPDATE %s SET %s WHERE (主键)", p.Table, p.SetColumn),
		IsWrite:  true,
		RowCount: affected,
		Status:   status,
		ErrorMsg: errMsg,
		CreatedAt: nowMillis(),
		Table:    p.Table,
		Column:   p.SetColumn,
		NewValue: p.SetValue,
		PKColumns: p.PKColumns,
		PKValues:  p.PKValues,
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
