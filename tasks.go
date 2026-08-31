package dqex

import (
	"context"
	"os"
)

// ---- 任务型能力（docs/library-api-design.md 3.2 tasks.go） ----
// 同步 API：ctx 控制取消，ProgressFunc 回调进度；后台并发由调用方自行起 goroutine。
// 服务层不暴露 Start* + taskID 异步任务模型（那是 Web UI 的需求，3.3）。

// ArtifactRef 产物引用（阶段二即引入，避免未来签名破坏）：
// 本地模式 Storage="local"、Ref=文件路径；对象存储模式 Ref=object key
// （存储实现见设计文档 4.4.3，触发式）。
type ArtifactRef struct {
	// Storage 产物存储后端：local / minio / s3 / obs / oss
	Storage string
	// Ref 本地文件路径或对象存储 object key
	Ref string
	// Size 字节数；目录产物为 0
	Size int64
}

// ArtifactStorageLocal 本地产物存储常量（ArtifactRef.Storage 取值）
const ArtifactStorageLocal = "local"

// RunExport 执行导出任务，返回产物引用。
// 产物落位：ExportOptions.OutputDir 显式指定则用之；为空时回填持久化 export 目录；
// StoreNone 库模式且未设 WithDataDir 时必须显式指定，否则返回 ErrExpOutDir。
func (c *Client) RunExport(ctx context.Context, opts ExportOptions, cb ProgressFunc) (*ArtifactRef, error) {
	if err := c.ensureOpen(); err != nil {
		return nil, err
	}
	path, err := c.svc.RunExport(c.ctx(ctx), opts, cb)
	if err != nil {
		return nil, err
	}
	return localArtifactRef(path), nil
}

// ImportResult 导入任务结果
type ImportResult struct {
	// TotalDatabases 导入的库数（.sql/.zip/.json 合计）
	TotalDatabases int
	// TotalStmts 执行的语句数
	TotalStmts int64
	// RollbackRef 精确回滚产物引用（仅 .json 数据包导入且 ImportOptions.Rollback=true 时非空；
	// .sql 导入为盲执行，无行级语义不生成回滚产物）
	RollbackRef *ArtifactRef
	// SkippedTables 跳过的表（无主键，无法精确导入/回滚；宿主侧应告警）
	SkippedTables []string
	// Unrollback 执行了但无法生成精确回滚的语句（如目标方言不支持的 ALTER 列变更；
	// 宿主侧应告警，必要时人工补偿）
	Unrollback []string
}

// RunImport 执行导入任务（目标连接可传 connKey 或完整连接信息）：
// 支持 .sql / .zip / .json（DataPackage 数据包）输入；Rollback=true 时返回精确回滚产物。
func (c *Client) RunImport(ctx context.Context, opts ImportOptions, cb ProgressFunc) (*ImportResult, error) {
	if err := c.ensureOpen(); err != nil {
		return nil, err
	}
	res, err := c.svc.RunImport(c.ctx(ctx), opts, cb)
	if err != nil {
		return nil, err
	}
	out := &ImportResult{TotalDatabases: res.TotalDatabases, TotalStmts: res.TotalStmts, SkippedTables: res.SkippedTables, Unrollback: res.Unrollback}
	if res.RollbackPath != "" {
		out.RollbackRef = localArtifactRef(res.RollbackPath)
	}
	return out, nil
}

// RunMigrate 执行结构 + 数据迁移任务。
func (c *Client) RunMigrate(ctx context.Context, opts MigrateOptions, cb ProgressFunc) error {
	if err := c.ensureOpen(); err != nil {
		return err
	}
	return c.svc.RunMigrate(c.ctx(ctx), opts, cb)
}

// RunDictionary 执行数据字典生成，返回产物引用（落位规则同 RunExport）。
func (c *Client) RunDictionary(ctx context.Context, opts DictionaryOptions, cb ProgressFunc) (*ArtifactRef, error) {
	if err := c.ensureOpen(); err != nil {
		return nil, err
	}
	path, err := c.svc.RunDictionary(c.ctx(ctx), opts, cb)
	if err != nil {
		return nil, err
	}
	return localArtifactRef(path), nil
}

// RunCompare 执行数据库对比（作用域为单个库对，或经 Databases/DBMapping 多库对比），
// 返回结构 + 数据差异结果。
func (c *Client) RunCompare(ctx context.Context, opts CompareOptions, cb ProgressFunc) (*CompareResult, error) {
	if err := c.ensureOpen(); err != nil {
		return nil, err
	}
	return c.svc.RunCompare(c.ctx(ctx), opts, cb)
}

// localArtifactRef 将服务层返回的本地产物路径包装为 ArtifactRef（文件取大小，目录为 0）。
func localArtifactRef(path string) *ArtifactRef {
	ref := &ArtifactRef{Storage: ArtifactStorageLocal, Ref: path}
	if st, err := os.Stat(path); err == nil && st.Mode().IsRegular() {
		ref.Size = st.Size()
	}
	return ref
}
