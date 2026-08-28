package dqex

import (
	"context"

	"github.com/fj1981/dqex/internal/service"
)

// ---- 快照（docs/library-api-design.md 3.2 snapshot.go） ----

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
