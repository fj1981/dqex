package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"dqex/internal/engine"

	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cygin"
	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cylog"
)

// ---- 快照管理 ----

// CreateSnapshot 创建快照（同步，CLI 使用）。dbNames 支持多库；空库名回退到连接默认库。
// sampleLimit：每表采样行数上限；<=0 走引擎默认值。lang：进度日志语言（zh/en）。
func (s *Service) CreateSnapshot(ctx context.Context, connID string, dbNames []string, name, description string, includeSamples bool, sampleLimit int, lang string, cb ProgressFunc) (*Snapshot, error) {
	conn, err := s.resolveConn(connID, nil)
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
	if rec, ok := s.persist.GetConn(connID); ok {
		snapshot.ConnLabel = rec.Name
	}

	// 落盘
	if err := s.saveSnapshot(snapshot); err != nil {
		return nil, cygin.WrapError(err, ErrExecFailed, cygin.WithErrPrint())
	}
	if err := s.addSnapshotToIndex(snapshotToInfo(snapshot)); err != nil {
		cylog.Warnf("更新快照索引失败（不影响创建）: %v", err)
	}

	return snapshot, nil
}

// ListSnapshots 列出所有快照摘要（始终返回非 nil 切片，避免 JSON 序列化为 null）
func (s *Service) ListSnapshots() []SnapshotInfo {
	infos, err := s.loadSnapshotIndex()
	if err != nil {
		cylog.Warnf("加载快照索引失败: %v", err)
		return []SnapshotInfo{}
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

// DeleteSnapshot 删除快照（索引 + 数据文件）
func (s *Service) DeleteSnapshot(id string) error {
	if err := s.removeSnapshotFromIndex(id); err != nil {
		return err
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

	target, err := s.resolveConn(opts.TargetConn, opts.Target)
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

	s.runner.Start(taskID, "snapshot_compare", opts.Lang, func(ctx context.Context, publish ProgressFunc) error {
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
			outputPath := filepath.Join(s.persist.CompareDir(), "snapshot-compare-"+taskID+".json")
			if e := saveCompareResult(outputPath, result); e != nil {
				cylog.Errorf("保存快照对比结果失败: %v", e)
			} else {
				r.OutputPath = outputPath
			}
			sm := result.Summary
			r.Summary = fmt.Sprintf("%d项, 一致%d, 结构差异%d, 数据差异%d", sm.Total, sm.Matched, sm.StructureDiff, sm.DataDiff)
		})
		return err
	})
	return taskID, nil
}

// RunSnapshotCompareRecorded 同步执行快照对比并记录历史（CLI 使用）
func (s *Service) RunSnapshotCompareRecorded(ctx context.Context, snap *Snapshot, target *DBConnInfo, opts SnapshotCompareOptions, cb ProgressFunc) (string, *CompareResult, error) {
	taskID := newTaskID()
	record := ExecutionRecord{
		ID: taskID, TaskType: "snapshot_compare", Status: "running", StartedAt: time.Now().UnixMilli(),
		Target: fmt.Sprintf("%s (快照) vs %s · %s", snap.Name, s.connLabel(opts.TargetConn, target), targetTables(nil, opts.Tables)),
	}
	_ = s.persist.SaveHistory(record)

	result, err := engine.RunSnapshotCompareWithConn(ctx, snap, target, opts, cb)

	record.FinishedAt = time.Now().UnixMilli()
	record.Duration = record.FinishedAt - record.StartedAt
	if err != nil {
		record.Status = "error"
		record.ErrorMsg = renderErrFor(err, opts.Lang).Error()
	} else {
		record.Status = "done"
		outputPath := filepath.Join(s.persist.CompareDir(), "snapshot-compare-"+taskID+".json")
		if e := saveCompareResult(outputPath, result); e != nil {
			cylog.Errorf("保存快照对比结果失败: %v", e)
		} else {
			record.OutputPath = outputPath
		}
		sm := result.Summary
		record.Summary = fmt.Sprintf("%d项, 一致%d, 结构差异%d, 数据差异%d", sm.Total, sm.Matched, sm.StructureDiff, sm.DataDiff)
		record.TotalUnits = sm.Total
	}
	if e := s.persist.SaveHistory(record); e != nil {
		cylog.Errorf("保存执行历史失败: %v", e)
	}
	return taskID, result, err
}

// GetSnapshotCompareResult 读取快照对比结果
func (s *Service) GetSnapshotCompareResult(taskID string) (*CompareResult, error) {
	path := ""
	if rec, err := s.persist.GetHistory(taskID); err == nil && rec.OutputPath != "" {
		path = rec.OutputPath
	}
	if path == "" {
		path = filepath.Join(s.persist.CompareDir(), "snapshot-compare-"+taskID+".json")
	}
	data, err := os.ReadFile(path)
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

func (s *Service) snapshotIndexPath() string {
	return filepath.Join(s.persist.SnapshotDir(), "index.json")
}

func (s *Service) snapshotDataPath(id string) string {
	return filepath.Join(s.persist.SnapshotDir(), id+".json")
}

func (s *Service) saveSnapshot(snap *Snapshot) error {
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.snapshotDataPath(snap.ID), data, 0o644)
}

func (s *Service) loadSnapshot(id string) (*Snapshot, error) {
	data, err := os.ReadFile(s.snapshotDataPath(id))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, newSvcErr(ErrTaskNotFound, svcSnapNotFound, id)
		}
		return nil, cygin.WrapError(err, cygin.ErrInternalServer, cygin.WithErrPrint())
	}
	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, cygin.WrapError(err, cygin.ErrInternalServer, cygin.WithErrPrint())
	}
	return &snap, nil
}

func (s *Service) loadSnapshotIndex() ([]SnapshotInfo, error) {
	data, err := os.ReadFile(s.snapshotIndexPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
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
	return os.WriteFile(s.snapshotIndexPath(), data, 0o644)
}

func (s *Service) addSnapshotToIndex(info SnapshotInfo) error {
	infos, err := s.loadSnapshotIndex()
	if err != nil {
		infos = nil
	}
	// 去重：同 ID 更新
	found := false
	for i := range infos {
		if infos[i].ID == info.ID {
			infos[i] = info
			found = true
			break
		}
	}
	if !found {
		infos = append(infos, info)
	}
	return s.saveSnapshotIndex(infos)
}

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
