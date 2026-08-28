package dqex

import (
	"context"
)

// ---- 查询执行（docs/library-api-design.md 3.2 query.go） ----
// 参数对象化：门面方法避免多裸参数（公开契约可读性/可演进性，3.2）。

// ScriptParams RunSQLScript 参数（参数对象化）。
type ScriptParams struct {
	// Limit 结果集单语句最大返回行数（<=0 走服务层默认值）
	Limit int
	// Offset 结果集偏移（分页浏览）
	Offset int
	// Mode 执行模式（保留字段，透传服务层）
	Mode string
}

// PageParams QueryTablePage 参数（参数对象化）。
type PageParams struct {
	// Page 页码（1 起）
	Page int
	// PageSize 每页行数
	PageSize int
	// SortSpecs 多列排序（按顺序叠加 ORDER BY，优先级从高到低）
	SortSpecs []SortSpec
	// ExcludeColumns 需省略的大字段列（二进制/超长文本，查询时 NULL 占位）
	ExcludeColumns []string
	// Filters 列过滤条件（AND 叠加，值参数化绑定防注入）
	Filters []ColumnFilter
}

// RunSQLScript 执行 SQL 脚本（可含多语句），返回各语句结果集。
func (c *Client) RunSQLScript(ctx context.Context, connKey, db, sql string, p ScriptParams) ([]*SQLQueryResult, error) {
	if err := c.ensureOpen(); err != nil {
		return nil, err
	}
	return c.svc.RunSQLScript(c.ctx(ctx), connKey, db, sql, p.Limit, p.Offset, p.Mode)
}

// QueryTablePage 分页查询单表数据并返回全表总行数（对象树数据浏览专用）。
func (c *Client) QueryTablePage(ctx context.Context, connKey, db, table string, p PageParams) (*TablePageResult, error) {
	if err := c.ensureOpen(); err != nil {
		return nil, err
	}
	return c.svc.QueryTablePage(c.ctx(ctx), connKey, db, table, p.Page, p.PageSize, p.SortSpecs, p.ExcludeColumns, p.Filters)
}

// GenerateSQL 由行/单元格/过滤条件生成方言正确的 SQL 文本（只产出不执行）。
func (c *Client) GenerateSQL(ctx context.Context, connKey, db string, p GenSQLParams) (string, error) {
	if err := c.ensureOpen(); err != nil {
		return "", err
	}
	return c.svc.GenerateSQL(c.ctx(ctx), connKey, db, p)
}

// UpdateTableCell 更新单个单元格（named bind + 标识符引用防注入），返回影响行数。
func (c *Client) UpdateTableCell(ctx context.Context, connKey, db string, p UpdateCellParams) (int64, error) {
	if err := c.ensureOpen(); err != nil {
		return 0, err
	}
	return c.svc.UpdateTableCell(c.ctx(ctx), connKey, db, p)
}

// GetCellValue 按主键 + 列名定位单行单列，返回该单元格完整值（大字段懒加载取值）。
func (c *Client) GetCellValue(ctx context.Context, connKey, db, table, column string, pkColumns []string, pkValues []any) (any, error) {
	if err := c.ensureOpen(); err != nil {
		return nil, err
	}
	return c.svc.GetCellValue(c.ctx(ctx), connKey, db, table, column, pkColumns, pkValues)
}

// InsertTableRow 新增一行（named bind 防注入），返回影响行数。
func (c *Client) InsertTableRow(ctx context.Context, connKey, db string, p InsertRowParams) (int64, error) {
	if err := c.ensureOpen(); err != nil {
		return 0, err
	}
	return c.svc.InsertTableRow(c.ctx(ctx), connKey, db, p)
}

// DeleteTableRows 按主键批量删除整行，返回累计影响行数。
func (c *Client) DeleteTableRows(ctx context.Context, connKey, db, table string, pkColumns []string, rows [][]any) (int64, error) {
	if err := c.ensureOpen(); err != nil {
		return 0, err
	}
	return c.svc.DeleteTableRows(c.ctx(ctx), connKey, db, table, pkColumns, rows)
}
