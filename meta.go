package dqex

import (
	"context"

	"github.com/fj1981/dqex/internal/service"
)

// ---- 元数据 ----
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

// ---- 快照 ----

// SnapshotParams CreateSnapshot 参数（参数对象化）。
type SnapshotParams struct {
	// Name 快照名称（展示用）
	Name string
	// Description 快照描述
	Description string
	// IncludeSamples 是否采样表数据行
	IncludeSamples bool
	// SampleLimit 每表采样行数上限（<=0 走引擎默认值）
	SampleLimit int
}

// CreateSnapshot 创建快照（同步）：dbs 支持多库，空库名回退到连接默认库。
// StoreSQLite（WithDataDir）下自动落盘到快照目录；StoreNone 库模式下仅内存返回，
// 调用方自行持久化（可用 json.Marshal 序列化 *Snapshot，LoadSnapshot 读回）。
func (c *Client) CreateSnapshot(ctx context.Context, connKey string, dbs []string, opts SnapshotParams, cb ProgressFunc) (*Snapshot, error) {
	if err := c.ensureOpen(); err != nil {
		return nil, err
	}
	return c.svc.CreateSnapshot(c.ctx(ctx), connKey, dbs, opts.Name, opts.Description, opts.IncludeSamples, opts.SampleLimit, c.lang, cb)
}

// LoadSnapshot 离线读快照文件，不需要连接（Close 后仍可安全调用）。
func (c *Client) LoadSnapshot(path string) (*Snapshot, error) {
	return service.LoadSnapshotFile(path)
}

// CompareSnapshot 快照与目标库对比，返回结构 + 数据差异结果。
// target 用完整连接信息而非 connKey：对比目标常为临时环境，未必已注册（3.2）。
func (c *Client) CompareSnapshot(ctx context.Context, snap *Snapshot, target *DBConnInfo, opts SnapshotCompareOptions, cb ProgressFunc) (*CompareResult, error) {
	if err := c.ensureOpen(); err != nil {
		return nil, err
	}
	_, result, err := c.svc.RunSnapshotCompareRecorded(c.ctx(ctx), snap, target, opts, cb)
	return result, err
}
