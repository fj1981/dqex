package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/fj1981/dqex/internal/engine"

	"github.com/fj1981/infrakit/pkg/cygin"
	"github.com/fj1981/infrakit/pkg/cylog"
	"github.com/fj1981/infrakit/pkg/cystore"
)

// ---- 快照管理 ----

// CreateSnapshot 创建快照（同步，CLI 使用）。dbNames 支持多库；空库名回退到连接默认库。
// sampleLimit：每表采样行数上限；<=0 走引擎默认值。lang：进度日志语言（zh/en）。
func (s *Service) CreateSnapshot(ctx context.Context, connID string, dbNames []string, name, description string, includeSamples bool, sampleLimit int, lang string, cb ProgressFunc) (*Snapshot, error) {
	conn, err := s.resolveConn(ctx, connID, nil)
	if err != nil {
		return nil, err
	}
	opts := CreateSnapshotOptions{IncludeSamples: includeSamples, SampleLimit: sampleLimit, Lang: lang}

	dbs := make([]engine.SnapshotDatabase, 0, len(dbNames))
	var totalTables int
	var totalRows int64
	for i, dbName := range dbNames {
		perConn := *conn
		if dbName != "" {
			perConn.DBName = dbName
			perConn.Schema = "" // oracle 时由引擎按 type 取 schema
		}
		if perConn.DBName == "" && perConn.Schema == "" {
			return nil, newSvcErr(cygin.ErrParamsInvalid, svcSnapNoDBName, i+1)
		}
		one, err := engine.CreateSnapshot(ctx, &perConn, name, description, opts, cb)
		if err != nil {
			// 空库：多库场景跳过并警告，单库场景返回明确提示（不崩溃）
			if errors.Is(err, engine.ErrEmptyDatabase) {
				if len(dbNames) == 1 {
					return nil, newSvcErr(cygin.ErrParamsInvalid, svcSnapEmptyDB, dbName)
				}
				cylog.Warnf("跳过空库 %s（无表，已忽略）", dbName)
				continue
			}
			wrapped := renderErrFor(err, lang)
			return nil, cygin.WrapError(wrapped, ErrExecFailed, cygin.WithErrPrint())
		}
		dbs = append(dbs, engine.SnapshotDatabase{
			DBName:     one.DBName,
			TableCount: one.TableCount,
			TotalRows:  one.TotalRows,
			Tables:     one.Tables,
		})
		totalTables += one.TableCount
		totalRows += one.TotalRows
	}
	// 全部库都为空时，给出明确提示
	if len(dbNames) > 1 && len(dbs) == 0 {
		return nil, newSvcErr(cygin.ErrParamsInvalid, svcSnapAllEmpty, len(dbNames))
	}

	snapshot := &Snapshot{
		ID:          newSnapshotID(),
		Name:        name,
		Description: description,
		ConnID:      connID,
		DBType:      conn.Type,
		CreatedAt:   time.Now().Unix(),
		TableCount:  totalTables,
		TotalRows:   totalRows,
		Databases:   dbs,
	}
	if rec, ok := s.memGet(connID); ok && rec.Name != "" {
		snapshot.ConnLabel = rec.Name
	} else if s.persist != nil {
		if rec, ok := s.persist.GetConn(connID); ok {
			snapshot.ConnLabel = rec.Name
		}
	}

	// 落盘（StoreNone 库模式且未提供 DataDir 时仅内存返回，调用方自行持久化）
	if s.persist != nil {
		if err := s.saveSnapshot(snapshot); err != nil {
			return nil, cygin.WrapError(err, ErrExecFailed, cygin.WithErrPrint())
		}
		s.ensureSnapshotIndexMigrated()
		if err := s.persist.UpsertSnapshot(snapshotToInfo(snapshot)); err != nil {
			cylog.Warnf("写入快照索引失败（不影响创建）: %v", err)
		}
	}

	return snapshot, nil
}

// metaKeySnapshotIndexMigrated 存量 JSON 索引 → DB 一次性迁移的 KV 标记（meta 表共享，
// 多实例只要任一实例完成迁移即全局生效）。
const metaKeySnapshotIndexMigrated = "snapshot_index_migrated"

// ensureSnapshotIndexMigrated 存量快照索引迁移（幂等，可安全重试）。
// persist 不可用（StoreNone 降级）时为 no-op，调用方走 JSON 索引路径。
//   - 进程内 atomic.Bool 仅作成功后的快路径；失败不固化，下次调用自动重试；
//   - 纯增量语义：只按 ID 逐条 upsert legacy 条目，绝不以 legacy 为准对齐 DB——
//     迁移期间并发新建、仅存在于 DB 的快照不会被覆盖/清除；
//   - 单条 upsert 失败即中止不置标记（已导入条目无需回滚，重迁移幂等收敛）；
//   - legacy 读取瞬时故障（非 not-found）同样中止（复用 loadSnapshotIndex 的
//     not-found=空索引 语义，该语义防止瞬时故障被误判为空索引后覆盖丢失）；
//   - legacy 文件只读不删不改：回滚旧版本时旧代码仍可读到存量索引。
func (s *Service) ensureSnapshotIndexMigrated() {
	if s.persist == nil || s.snapIdxMig.Load() {
		return
	}
	s.snapIdxMu.Lock()
	defer s.snapIdxMu.Unlock()
	if s.snapIdxMig.Load() { // 双检：等锁期间其他调用已完成迁移
		return
	}
	if _, ok, err := s.persist.GetMeta(metaKeySnapshotIndexMigrated); err != nil {
		cylog.Warnf("读取快照索引迁移标记失败（本次跳过，下次自动重试）: %v", err)
		return
	} else if ok {
		s.snapIdxMig.Store(true)
		return
	}
	legacy, err := s.loadSnapshotIndex()
	if err != nil {
		cylog.Warnf("读取存量快照索引失败（迁移跳过，下次自动重试）: %v", err)
		return
	}
	imported := 0
	for _, info := range legacy {
		if err := s.persist.UpsertSnapshot(info); err != nil {
			cylog.Warnf("迁移快照索引条目 %s 失败（已导入 %d/%d 条，本次中止不置标记）: %v",
				info.ID, imported, len(legacy), err)
			return
		}
		imported++
	}
	if err := s.persist.SetMeta(metaKeySnapshotIndexMigrated, "1"); err != nil {
		cylog.Warnf("写入快照索引迁移标记失败（下次将重复迁移，幂等无害）: %v", err)
		return
	}
	s.snapIdxMig.Store(true)
	if len(legacy) > 0 {
		cylog.Infof("存量快照索引已迁移至数据库：共 %d 条（legacy JSON 索引保留未删，供回滚旧版本读取）", len(legacy))
	}
}

// ListSnapshots 列出快照摘要（始终返回非 nil 切片，避免 JSON 序列化为 null）。
// persist 可用（StoreSQLite/StoreExternal）时走 DB 索引（connID 非空按 conn_id 列过滤）；
// StoreNone 降级时回退 JSON 索引（OSS/本地），connID 在内存过滤。
func (s *Service) ListSnapshots(connID string) []SnapshotInfo {
	if s.persist != nil {
		s.ensureSnapshotIndexMigrated()
		infos, err := s.persist.ListSnapshots(connID)
		if err != nil {
			cylog.Warnf("查询快照索引失败: %v", err)
			return []SnapshotInfo{}
		}
		sort.Slice(infos, func(i, j int) bool { return infos[i].CreatedAt > infos[j].CreatedAt })
		return infos
	}
	infos, err := s.loadSnapshotIndex()
	if err != nil {
		cylog.Warnf("加载快照索引失败: %v", err)
		return []SnapshotInfo{}
	}
	if connID != "" {
		filtered := make([]SnapshotInfo, 0, len(infos))
		for _, info := range infos {
			if info.ConnID == connID {
				filtered = append(filtered, info)
			}
		}
		infos = filtered
	}
	if infos == nil {
		return []SnapshotInfo{}
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].CreatedAt > infos[j].CreatedAt })
	return infos
}

// GetSnapshot 获取单个快照完整数据
func (s *Service) GetSnapshot(id string) (*Snapshot, error) {
	return s.loadSnapshot(id)
}

// DeleteSnapshot 删除快照（索引 + 数据文件/对象）。
// persist 可用时先触发存量迁移（否则 CLI 直接删除存量快照会因 DB 未导入而误报不存在），
// 再删 DB 索引行；StoreNone 降级走 JSON 索引读-改-写。
func (s *Service) DeleteSnapshot(id string) error {
	if s.persist != nil {
		s.ensureSnapshotIndexMigrated()
		if err := s.persist.DeleteSnapshot(id); err != nil {
			return cygin.WrapError(err, cygin.ErrInternalServer, cygin.WithErrPrint())
		}
	} else {
		s.snapIdxMu.Lock()
		err := s.removeSnapshotFromIndex(id)
		s.snapIdxMu.Unlock()
		if err != nil {
			return err
		}
	}
	if s.artifactStore != nil {
		// 对象存储模式：删除对象；同步清理本地存量（升级前创建的本地快照回落读取场景）
		if err := s.artifactRemove(s.snapshotObjectKey(id + ".json")); err != nil {
			return cygin.WrapError(err, cygin.ErrInternalServer, cygin.WithErrPrint())
		}
		_ = os.Remove(s.snapshotDataPath(id))
		return nil
	}
	dataPath := s.snapshotDataPath(id)
	if err := os.Remove(dataPath); err != nil && !os.IsNotExist(err) {
		return cygin.WrapError(err, cygin.ErrInternalServer, cygin.WithErrPrint())
	}
	return nil
}

// ---- 快照对比 ----

// StartSnapshotCompare 异步启动快照对比任务
func (s *Service) StartSnapshotCompare(opts SnapshotCompareOptions, taskConfigID string) (string, error) {
	snap, err := s.loadSnapshot(opts.SnapshotID)
	if err != nil {
		return "", newSvcErr(ErrTaskNotFound, svcSnapNotFound, opts.SnapshotID)
	}

	target, err := s.resolveConn(context.Background(), opts.TargetConn, opts.Target)
	if err != nil {
		return "", err
	}
	// 默认目标库回退：优先快照首库（多库快照 DBName 可能为空），供 DBMapping 未指定时的同名配对
	if target.DBName == "" && target.Schema == "" {
		firstDB := snap.DBName
		if firstDB == "" && len(snap.Databases) > 0 {
			firstDB = snap.Databases[0].DBName
		}
		target.DBName = firstDB
	}
	opts.Target = target

	taskID := newTaskID()
	record := ExecutionRecord{
		ID: taskID, TaskType: "snapshot_compare", TaskConfigID: taskConfigID,
		Status: "running", StartedAt: time.Now().UnixMilli(),
		Target: fmt.Sprintf("%s (快照) vs %s · %s", snap.Name, s.connLabel(opts.TargetConn, target), targetTables(nil, opts.Tables)),
	}
	_ = s.persist.SaveHistory(record)

	s.runner.Start(taskID, "snapshot_compare", opts.TargetConn, record.Target, opts.Lang, opts.User, func(ctx context.Context, publish ProgressFunc) error {
		var last ProgressInfo
		wrapped := func(p ProgressInfo) { last = p; publish(p) }
		result, err := engine.RunSnapshotCompareWithConn(ctx, snap, target, opts, wrapped)
		if err != nil {
			err = renderErrFor(err, opts.Lang) // 按任务语言渲染 engine.MsgError
		}
		s.finishRecord(ctx, &record, err, last, func(r *ExecutionRecord) {
			r.TotalUnits = last.TotalUnits
			r.TotalRows = last.DoneRows
			if result == nil {
				return
			}
			outPath, e := s.saveCompareResultArtifact("snapshot-compare-"+taskID+".json", result)
			if e != nil {
				cylog.Errorf("保存快照对比结果失败: %v", e)
			} else {
				r.OutputPath = outPath
			}
			sm := result.Summary
			r.Summary = fmt.Sprintf("%d项, 一致%d, 结构差异%d, 数据差异%d", sm.Total, sm.Matched, sm.StructureDiff, sm.DataDiff)
		})
		return err
	})
	return taskID, nil
}

// RunSnapshotCompareRecorded 同步执行快照对比并记录历史（CLI 使用）。
// StoreNone 库模式（persist 为 nil）下跳过历史与结果落盘，结果直接由内存返回。
func (s *Service) RunSnapshotCompareRecorded(ctx context.Context, snap *Snapshot, target *DBConnInfo, opts SnapshotCompareOptions, cb ProgressFunc) (string, *CompareResult, error) {
	taskID := newTaskID()
	record := ExecutionRecord{
		ID: taskID, TaskType: "snapshot_compare", Status: "running", StartedAt: time.Now().UnixMilli(),
		Target: fmt.Sprintf("%s (快照) vs %s · %s", snap.Name, s.connLabel(opts.TargetConn, target), targetTables(nil, opts.Tables)),
	}
	if s.persist != nil {
		_ = s.persist.SaveHistory(record)
	}

	result, err := engine.RunSnapshotCompareWithConn(ctx, snap, target, opts, cb)

	record.FinishedAt = time.Now().UnixMilli()
	record.Duration = record.FinishedAt - record.StartedAt
	if err != nil {
		record.Status = "error"
		record.ErrorMsg = renderErrFor(err, opts.Lang).Error()
	} else {
		record.Status = "done"
		if s.persist != nil {
			outPath, e := s.saveCompareResultArtifact("snapshot-compare-"+taskID+".json", result)
			if e != nil {
				cylog.Errorf("保存快照对比结果失败: %v", e)
			} else {
				record.OutputPath = outPath
			}
		}
		sm := result.Summary
		record.Summary = fmt.Sprintf("%d项, 一致%d, 结构差异%d, 数据差异%d", sm.Total, sm.Matched, sm.StructureDiff, sm.DataDiff)
		record.TotalUnits = sm.Total
	}
	if s.persist != nil {
		if e := s.persist.SaveHistory(record); e != nil {
			cylog.Errorf("保存执行历史失败: %v", e)
		}
	}
	return taskID, result, err
}

// GetSnapshotCompareResult 读取快照对比结果
func (s *Service) GetSnapshotCompareResult(taskID string) (*CompareResult, error) {
	name := "snapshot-compare-" + taskID + ".json"
	if rec, err := s.persist.GetHistory(taskID); err == nil && rec.OutputPath != "" {
		name = rec.OutputPath
	}
	data, err := s.loadCompareResultArtifact(name)
	if err != nil {
		return nil, newSvcErr(ErrTaskNotFound, svcSnapCmpNotFound, taskID)
	}
	var result CompareResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, cygin.WrapError(err, cygin.ErrInternalServer, cygin.WithErrPrint())
	}
	return &result, nil
}

// ---- 内部持久化 ----

// LoadSnapshotFile 从任意路径读取快照完整数据（离线读文件，不需要连接）。
// 库模式 LoadSnapshot 门面入口使用；应用模式内的按 ID 读取仍走 loadSnapshot。
func LoadSnapshotFile(path string) (*Snapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, newSvcErr(ErrTaskNotFound, svcSnapNotFound, path)
		}
		return nil, cygin.WrapError(err, cygin.ErrInternalServer, cygin.WithErrPrint())
	}
	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, cygin.WrapError(err, cygin.ErrInternalServer, cygin.WithErrPrint())
	}
	return &snap, nil
}

func (s *Service) snapshotIndexPath() string {
	return filepath.Join(s.persist.SnapshotDir(), "index.json")
}

func (s *Service) snapshotDataPath(id string) string {
	return filepath.Join(s.persist.SnapshotDir(), id+".json")
}

// ---- 快照持久化：统一走产物存取器（WithArtifactStore 注入 → 对象存储；nil → 本地目录） ----

// snapshotObjectPrefix 快照对象 key 前缀（与本地目录名一致，便于按前缀浏览/清理）
const snapshotObjectPrefix = "snapshots"

func (s *Service) snapshotObjectKey(name string) string {
	return snapshotObjectPrefix + "/" + name
}

// isSnapshotObjectNotFound 判断对象存储读取错误是否为「对象不存在」：
// local/nfs/sftp provider 返回 cystore.ErrObjectNotFound 哨兵；
// minio（S3 语义）返回含 NoSuchKey 的响应错误。
// 必须与瞬时故障（网络/鉴权问题）区分：索引读-改-写把瞬时故障当作空索引
// 会在下次保存时用单条目覆盖，导致既有索引整体丢失。
func isSnapshotObjectNotFound(err error) bool {
	return errors.Is(err, cystore.ErrObjectNotFound) ||
		strings.Contains(err.Error(), "NoSuchKey")
}

func (s *Service) saveSnapshot(snap *Snapshot) error {
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	if s.artifactStore != nil {
		_, err = s.artifactPut(snapshotObjectPrefix, snap.ID+".json", data)
		return err
	}
	return os.WriteFile(s.snapshotDataPath(snap.ID), data, 0o644)
}

func (s *Service) loadSnapshot(id string) (*Snapshot, error) {
	var data []byte
	if s.artifactStore != nil {
		d, err := s.artifactGet(s.snapshotObjectKey(id + ".json"))
		if err != nil {
			// 瞬时故障（网络/鉴权）直接报错，不误判为「快照不存在」
			if !isSnapshotObjectNotFound(err) {
				return nil, cygin.WrapError(err, cygin.ErrInternalServer, cygin.WithErrPrint())
			}
			// 对象未命中：回落本地文件（升级前创建的存量本地快照），仍无则按不存在处理
			cylog.Debugf("快照对象不存在，回落本地文件 %s: %v", id, err)
			d2, e2 := os.ReadFile(s.snapshotDataPath(id))
			if e2 != nil {
				return nil, newSvcErr(ErrTaskNotFound, svcSnapNotFound, id)
			}
			d = d2
		}
		data = d
	} else {
		d, err := os.ReadFile(s.snapshotDataPath(id))
		if err != nil {
			if os.IsNotExist(err) {
				return nil, newSvcErr(ErrTaskNotFound, svcSnapNotFound, id)
			}
			return nil, cygin.WrapError(err, cygin.ErrInternalServer, cygin.WithErrPrint())
		}
		data = d
	}
	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, cygin.WrapError(err, cygin.ErrInternalServer, cygin.WithErrPrint())
	}
	return &snap, nil
}

func (s *Service) loadSnapshotIndex() ([]SnapshotInfo, error) {
	var data []byte
	if s.artifactStore != nil {
		d, err := s.artifactGet(s.snapshotObjectKey("index.json"))
		if err != nil {
			// 索引对象不存在（尚未创建过快照）视为空索引；
			// 瞬时故障必须返回错误——否则移除/迁移会以空列表覆盖保存，既有索引整体丢失
			if !isSnapshotObjectNotFound(err) {
				return nil, fmt.Errorf("读取快照索引对象失败: %w", err)
			}
			return nil, nil
		}
		data = d
	} else {
		d, err := os.ReadFile(s.snapshotIndexPath())
		if err != nil {
			if os.IsNotExist(err) {
				return nil, nil
			}
			return nil, err
		}
		data = d
	}
	var infos []SnapshotInfo
	if err := json.Unmarshal(data, &infos); err != nil {
		return nil, err
	}
	return infos, nil
}

func (s *Service) saveSnapshotIndex(infos []SnapshotInfo) error {
	data, err := json.MarshalIndent(infos, "", "  ")
	if err != nil {
		return err
	}
	if s.artifactStore != nil {
		_, err = s.artifactPut(snapshotObjectPrefix, "index.json", data)
		return err
	}
	return os.WriteFile(s.snapshotIndexPath(), data, 0o644)
}

// removeSnapshotFromIndex 索引读-改-写移除（调用方持有 snapIdxMu 或为单线程 CLI 场景）。
// 仅 StoreNone 降级路径使用（persist 可用时直接删 DB 行）。
func (s *Service) removeSnapshotFromIndex(id string) error {
	infos, err := s.loadSnapshotIndex()
	if err != nil {
		return cygin.WrapError(err, cygin.ErrInternalServer, cygin.WithErrPrint())
	}
	ret := make([]SnapshotInfo, 0, len(infos))
	for _, info := range infos {
		if info.ID != id {
			ret = append(ret, info)
		}
	}
	return s.saveSnapshotIndex(ret)
}

func snapshotToInfo(snap *Snapshot) SnapshotInfo {
	names := make([]string, 0, len(snap.Databases))
	for _, d := range snap.Databases {
		if d.DBName != "" {
			names = append(names, d.DBName)
		}
	}
	if len(names) == 0 && snap.DBName != "" {
		names = []string{snap.DBName} // 兼容 v1 单库快照
	}
	return SnapshotInfo{
		ID:          snap.ID,
		Name:        snap.Name,
		Description: snap.Description,
		ConnID:      snap.ConnID,
		ConnLabel:   snap.ConnLabel,
		DBNames:     names,
		DBName:      snap.DBName,
		DBType:      snap.DBType,
		TableCount:  snap.TableCount,
		TotalRows:   snap.TotalRows,
		CreatedAt:   snap.CreatedAt,
	}
}
