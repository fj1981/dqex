package dqex

import (
	"context"
)

// ---- 元数据（docs/library-api-design.md 3.2 meta.go） ----
// 对象树/选表器分级接口：库列表 → schema 列表 → 对象清单，服务层带 TTL 缓存；
// force=true 绕过缓存直查并回写（界面刷新语义）；连接增删改时缓存自动失效。

// Databases 列出连接可见的数据库（库名列表）。
func (c *Client) Databases(ctx context.Context, connKey string, force bool) ([]string, error) {
	if err := c.ensureOpen(); err != nil {
		return nil, err
	}
	return c.svc.GetDatabaseList(c.ctx(ctx), connKey, force)
}

// Schemas 列出指定库下的 schema 摘要（PG 系；MySQL/Oracle 返回空）。
func (c *Client) Schemas(ctx context.Context, connKey, db string, force bool) ([]SchemaSummary, error) {
	if err := c.ensureOpen(); err != nil {
		return nil, err
	}
	return c.svc.GetDbSchemas(c.ctx(ctx), connKey, db, force)
}

// Objects 列出指定库/schema 的对象清单（表/视图等分组）。
func (c *Client) Objects(ctx context.Context, connKey, db, schema string, force bool) (*DBSchema, error) {
	if err := c.ensureOpen(); err != nil {
		return nil, err
	}
	return c.svc.GetSchemaObjects(c.ctx(ctx), connKey, db, schema, force)
}

// TableColumns 获取表的列信息（名称/类型/可空/主键/默认值）。
func (c *Client) TableColumns(ctx context.Context, connKey, db, table string) ([]TableColumnInfo, error) {
	if err := c.ensureOpen(); err != nil {
		return nil, err
	}
	return c.svc.GetTableColumns(c.ctx(ctx), connKey, db, table)
}

// ObjectDDL 查询对象的创建语句（表/视图/索引等）。
func (c *Client) ObjectDDL(ctx context.Context, connKey, db, objType, name string) (*ObjectDDLResult, error) {
	if err := c.ensureOpen(); err != nil {
		return nil, err
	}
	return c.svc.GetObjectDDL(c.ctx(ctx), connKey, db, objType, name)
}
