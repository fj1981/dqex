package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"dbimpex/internal/engine"

	"github.com/rs/xid"

	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cygin"
	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cylog"
)

// Service 业务服务层：Web/CLI 共用，编排引擎 + 连接 + 任务 + 历史
type Service struct {
	persist *PersistMgr
	runner  *TaskRunner
}

// NewService 创建业务服务（自动发现全局配置 config.yaml）
func NewService(dataDirFlag string) (*Service, error) {
	return NewServiceWith(dataDirFlag, "")
}

// NewServiceWith 创建业务服务：configFile 显式指定全局配置，空则按默认顺序发现
func NewServiceWith(dataDirFlag, configFile string) (*Service, error) {
	cfg, err := LoadAppConfig(FindConfigFile(configFile))
	if err != nil {
		return nil, err
	}
	persist, err := NewPersistMgrWith(ResolveDirs(dataDirFlag, cfg))
	if err != nil {
		return nil, err
	}
	return &Service{
		persist: persist,
		runner:  newTaskRunner(),
	}, nil
}

// Persist 返回持久化管理器
func (s *Service) Persist() *PersistMgr { return s.persist }

// ---- 连接管理 ----

// AddConnection 保存连接配置：rec.ID 非空为按主键更新，否则新建（生成 xid）
func (s *Service) AddConnection(rec ConnRecord) (ConnRecord, error) {
	if strings.TrimSpace(rec.Name) == "" {
		return rec, cygin.NewError(cygin.ErrParamsInvalid, cygin.WithErrPrint(), cygin.WithErrDetailf("连接名称不能为空"))
	}
	rec.Name = strings.TrimSpace(rec.Name)
	if rec.Conn.Type == "" {
		return rec, cygin.NewError(cygin.ErrParamsInvalid, cygin.WithErrPrint(), cygin.WithErrDetailf("数据库类型不能为空"))
	}
	if _, ok := SupportedDBTypes[rec.Conn.Type]; !ok {
		return rec, cygin.NewError(ErrUnsupportedType, cygin.WithErrPrint(), cygin.WithErrDetailf("不支持的数据库类型: %s", rec.Conn.Type))
	}
	saved, err := s.persist.SaveConn(rec)
	if err != nil {
		return rec, cygin.WrapError(err, cygin.ErrInternalServer, cygin.WithErrPrint(), cygin.WithErrDetails(err.Error()))
	}
	return saved, nil
}

// ListConnections 列出所有连接（按名称排序，保证前端展示稳定）
func (s *Service) ListConnections() []ConnInfo {
	conns := s.persist.LoadConns()
	ret := make([]ConnInfo, 0, len(conns))
	for _, rec := range conns {
		ret = append(ret, ConnInfo{ID: rec.ID, Name: rec.Name, Conn: rec.Conn, SubTypes: SupportedDBTypes[rec.Conn.Type]})
	}
	sort.Slice(ret, func(i, j int) bool { return ret[i].Name < ret[j].Name })
	return ret
}

// DeleteConnection 删除连接（按主键 ID，兼容名称）
func (s *Service) DeleteConnection(key string) error {
	if err := s.persist.DeleteConn(key); err != nil {
		return cygin.WrapError(err, cygin.ErrInternalServer, cygin.WithErrPrint(), cygin.WithErrDetails(err.Error()))
	}
	return nil
}

// TestConnection 测试连接可用性
func (s *Service) TestConnection(conn DBConnInfo) error {
	cli, err := engine.Connect(conn)
	if err != nil {
		return cygin.WrapError(err, ErrConnFailed, cygin.WithErrPrint(), cygin.WithErrDetails(err.Error()))
	}
	defer cli.Close()
	return nil
}

// GetTableTree 获取指定连接（可覆盖库名）的 库→表 树形结构；
// 连接未配置库时遍历所有库（Oracle 遍历 schema）
func (s *Service) GetTableTree(connKey, dbName string) ([]engine.DBTables, error) {
	conn, err := s.resolveConn(connKey, nil)
	if err != nil {
		return nil, err
	}
	if dbName != "" {
		conn.DBName = dbName
	}
	tree, err := engine.GetTableTree(*conn)
	if err != nil {
		return nil, cygin.WrapError(err, ErrExecFailed, cygin.WithErrPrint(), cygin.WithErrDetails(err.Error()))
	}
	return tree, nil
}

// GetTableColumns 获取指定连接/库下某表的列信息（名称/类型/可空/主键/默认值）
func (s *Service) GetTableColumns(connKey, dbName, tableName string) ([]engine.TableColumnInfo, error) {
	conn, err := s.resolveConn(connKey, nil)
	if err != nil {
		return nil, err
	}
	if dbName != "" {
		conn.DBName = dbName
	}
	cols, err := engine.GetTableColumns(*conn, tableName)
	if err != nil {
		return nil, cygin.WrapError(err, ErrExecFailed, cygin.WithErrPrint(), cygin.WithErrDetails(err.Error()))
	}
	return cols, nil
}

// resolveConn 解析连接：优先使用内联 conn，否则按主键 ID（兼容名称）查找已保存连接
func (s *Service) resolveConn(key string, inline *DBConnInfo) (*DBConnInfo, error) {
	if inline != nil && inline.Type != "" {
		return inline, nil
	}
	if key == "" {
		return nil, cygin.NewError(ErrConnNotSpecified, cygin.WithErrPrint(), cygin.WithErrDetailf("未指定数据库连接"))
	}
	rec, ok := s.persist.GetConn(key)
	if !ok {
		return nil, cygin.NewError(ErrConnNotFound, cygin.WithErrPrint(), cygin.WithErrDetailf("连接配置不存在: %s", key))
	}
	return &rec.Conn, nil
}

// ---- 核心执行（同步，CLI 直接使用；Web 通过 Start* 异步执行） ----

func normalizeBatchSize(n int) int {
	if n <= 0 {
		return engine.DefaultBatchSize
	}
	return n
}

// RunExport 执行导出，outputPath 返回最终产物路径
func (s *Service) RunExport(ctx context.Context, opts ExportOptions, cb ProgressFunc) (string, error) {
	src, err := s.resolveConn(opts.SourceConn, opts.Source)
	if err != nil {
		return "", err
	}
	opts.Source = src
	opts.BatchSize = normalizeBatchSize(opts.BatchSize)
	if opts.OutputDir == "" {
		opts.OutputDir = s.persist.ExportDir()
	}
	result, err := engine.RunExport(ctx, opts, cb)
	if err != nil {
		return "", err
	}
	return result.OutputPath, nil
}

// RunImport 执行导入
func (s *Service) RunImport(ctx context.Context, opts ImportOptions, cb ProgressFunc) error {
	target, err := s.resolveConn(opts.TargetConn, opts.Target)
	if err != nil {
		return err
	}
	opts.Target = target
	opts.BatchSize = normalizeBatchSize(opts.BatchSize)
	opts.ResetMode = normalizeReset(opts.ResetMode)
	opts.TempDir = s.persist.TempDir() // 任务处理临时目录（zip 解压）
	_, err = engine.RunImport(ctx, opts, cb)
	return err
}

// RunMigrate 执行迁移
func (s *Service) RunMigrate(ctx context.Context, opts MigrateOptions, cb ProgressFunc) error {
	src, err := s.resolveConn(opts.SourceConn, opts.Source)
	if err != nil {
		return err
	}
	target, err := s.resolveConn(opts.TargetConn, opts.Target)
	if err != nil {
		return err
	}
	opts.Source = src
	opts.Target = target
	opts.BatchSize = normalizeBatchSize(opts.BatchSize)
	opts.ResetMode = normalizeReset(opts.ResetMode)
	_, err = engine.RunMigrate(ctx, opts, cb)
	return err
}

// RunCompare 执行数据库对比（作用域为单个库对，两侧必须已选定库）
func (s *Service) RunCompare(ctx context.Context, opts CompareOptions, cb ProgressFunc) (*CompareResult, error) {
	src, err := s.resolveConn(opts.SourceConn, opts.Source)
	if err != nil {
		return nil, err
	}
	target, err := s.resolveConn(opts.TargetConn, opts.Target)
	if err != nil {
		return nil, err
	}
	opts.Source = src
	opts.Target = target
	if opts.Threshold <= 0 {
		opts.Threshold = engine.DefaultCompareThreshold
	}
	if (src.DBName == "" && src.Schema == "") || (target.DBName == "" && target.Schema == "") {
		return nil, cygin.NewError(cygin.ErrParamsInvalid, cygin.WithErrPrint(), cygin.WithErrDetailf("请先选择对比的库"))
	}
	return engine.RunCompare(ctx, opts, cb)
}

// RunCompareRecorded 执行对比并记录历史：结果落盘 compares/compare-<ID>.json，
// 返回记录 ID，供 `compare show --id <ID>` 回看差异明细（CLI 同步执行路径用）
func (s *Service) RunCompareRecorded(ctx context.Context, opts CompareOptions, cb ProgressFunc, taskConfigID string) (string, *CompareResult, error) {
	taskID := newTaskID()
	record := ExecutionRecord{ID: taskID, TaskType: "compare", TaskConfigID: taskConfigID, Status: "running", StartedAt: time.Now().UnixMilli(),
		Target: fmt.Sprintf("%s → %s · %s", s.connLabel(opts.SourceConn, opts.Source), s.connLabel(opts.TargetConn, opts.Target), targetTables(nil, opts.Tables))}
	_ = s.persist.SaveHistory(record)

	result, err := s.RunCompare(ctx, opts, cb)

	record.FinishedAt = time.Now().UnixMilli()
	record.Duration = record.FinishedAt - record.StartedAt
	if err != nil {
		record.Status = "error"
		record.ErrorMsg = err.Error()
	} else {
		record.Status = "done"
		outputPath := filepath.Join(s.persist.CompareDir(), "compare-"+taskID+".json")
		if e := saveCompareResult(outputPath, result); e != nil {
			cylog.Errorf("保存对比结果失败: %v", e)
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

func normalizeReset(mode ResetMode) ResetMode {
	switch strings.ToLower(string(mode)) {
	case "truncate":
		return ResetTruncate
	case "drop":
		return ResetDrop
	default:
		return ResetNone
	}
}

// ---- 异步任务运行器 ----

// TaskRunner 管理异步执行中的任务（进度广播 + 取消）
type TaskRunner struct {
	mu      sync.RWMutex
	running map[string]*runningTask
}

type runningTask struct {
	cancel    context.CancelFunc
	taskType  string
	latest    ProgressInfo
	hasLatest bool
	subs      map[chan ProgressInfo]struct{}
}

func newTaskRunner() *TaskRunner {
	return &TaskRunner{running: map[string]*runningTask{}}
}

// Start 注册并启动一个异步任务
func (r *TaskRunner) Start(taskID, taskType string, run func(ctx context.Context, publish ProgressFunc) error) {
	ctx, cancel := context.WithCancel(context.Background())
	rt := &runningTask{
		cancel:   cancel,
		taskType: taskType,
		subs:     map[chan ProgressInfo]struct{}{},
	}
	r.mu.Lock()
	r.running[taskID] = rt
	r.mu.Unlock()

	go func() {
		defer cancel()
		publish := func(p ProgressInfo) {
			p.TaskID = taskID
			r.publish(taskID, p)
		}
		// panic 兜底：引擎层 panic 转为任务错误，避免拖垮整个服务
		var err error
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					err = fmt.Errorf("任务执行异常: %v", rec)
					cylog.Errorf("任务执行 panic taskID=%s type=%s: %v", taskID, taskType, rec)
				}
			}()
			err = run(ctx, publish)
		}()
		// 终态兜底推送（引擎出错时可能未推送 error 状态）；ctx 已取消时一律按取消认定，
		// 不依赖错误文案匹配（驱动层报错文案不可控）
		r.mu.RLock()
		t := r.running[taskID]
		r.mu.RUnlock()
		if t != nil {
			final := t.latest
			final.TaskID = taskID
			if err != nil {
				if ctx.Err() != nil || strings.Contains(err.Error(), "任务已取消") {
					final.State = "cancelled"
					final.Message = "任务已取消"
				} else {
					final.State = "error"
					final.Message = err.Error()
					cylog.Errorf("任务执行失败 taskID=%s type=%s: %v", taskID, t.taskType, err)
				}
			} else if final.State != "done" {
				final.State = "done"
				final.Percent = 100
			}
			r.publish(taskID, final)
		}
		// 终态推送后清理运行表，避免 entry/日志快照无限累积；
		// 后续进度查询由 streamProgress 回退到执行历史回放
		r.mu.Lock()
		delete(r.running, taskID)
		r.mu.Unlock()
	}()
}

func (r *TaskRunner) publish(taskID string, p ProgressInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.running[taskID]
	if !ok {
		return
	}
	t.latest = p
	t.hasLatest = true
	for ch := range t.subs {
		select {
		case ch <- p:
		default:
			// 订阅者消费过慢：丢弃最旧一条再写入
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- p:
			default:
			}
		}
	}
}

// Subscribe 订阅任务进度，返回进度通道与释放函数
func (r *TaskRunner) Subscribe(taskID string) (<-chan ProgressInfo, func(), error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.running[taskID]
	if !ok {
		return nil, nil, fmt.Errorf("任务不存在或已结束: %s", taskID)
	}
	ch := make(chan ProgressInfo, 16)
	if t.hasLatest {
		ch <- t.latest
	}
	t.subs[ch] = struct{}{}
	return ch, func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if t, ok := r.running[taskID]; ok {
			delete(t.subs, ch)
		}
	}, nil
}

// Cancel 取消任务
func (r *TaskRunner) Cancel(taskID string) error {
	r.mu.RLock()
	t, ok := r.running[taskID]
	r.mu.RUnlock()
	if !ok {
		return cygin.NewError(ErrTaskNotFound, cygin.WithErrPrint(), cygin.WithErrDetailf("任务不存在: %s", taskID))
	}
	t.cancel()
	return nil
}

// ---- Web 异步任务入口（启动 + 记录执行历史） ----

// StartExport 异步启动导出任务，返回 taskID
func (s *Service) StartExport(opts ExportOptions, taskConfigID string) (string, error) {
	if _, err := s.resolveConn(opts.SourceConn, opts.Source); err != nil {
		return "", err
	}
	taskID := newTaskID()
	record := ExecutionRecord{ID: taskID, TaskType: "export", TaskConfigID: taskConfigID, Status: "running", StartedAt: time.Now().UnixMilli(),
		Target: fmt.Sprintf("%s · %s", s.connLabel(opts.SourceConn, opts.Source), targetTables(opts.Databases, opts.Tables))}
	_ = s.persist.SaveHistory(record)

	s.runner.Start(taskID, "export", func(ctx context.Context, publish ProgressFunc) error {
		var last ProgressInfo
		wrapped := func(p ProgressInfo) { last = p; publish(p) }
		outputPath, err := s.RunExport(ctx, opts, wrapped)
		s.finishRecord(ctx, &record, err, last, func(r *ExecutionRecord) {
			r.TotalUnits = last.TotalUnits
			r.TotalRows = last.DoneRows
			r.Summary = buildSummary(last.TotalUnits, last.DoneRows, record.FileSize)
			if outputPath != "" {
				r.OutputPath = outputPath
				if st, statErr := os.Stat(outputPath); statErr == nil {
					r.FileSize = st.Size()
					r.Summary = buildSummary(last.TotalUnits, last.DoneRows, r.FileSize)
				}
			}
		})
		return err
	})
	return taskID, nil
}

// StartImport 异步启动导入任务，返回 taskID
func (s *Service) StartImport(opts ImportOptions, taskConfigID string) (string, error) {
	if _, err := s.resolveConn(opts.TargetConn, opts.Target); err != nil {
		return "", err
	}
	taskID := newTaskID()
	record := ExecutionRecord{ID: taskID, TaskType: "import", TaskConfigID: taskConfigID, Status: "running", StartedAt: time.Now().UnixMilli(),
		Target: fmt.Sprintf("%s · %s", s.connLabel(opts.TargetConn, opts.Target), filepath.Base(opts.InputPath))}
	_ = s.persist.SaveHistory(record)

	s.runner.Start(taskID, "import", func(ctx context.Context, publish ProgressFunc) error {
		var last ProgressInfo
		wrapped := func(p ProgressInfo) { last = p; publish(p) }
		err := s.RunImport(ctx, opts, wrapped)
		s.finishRecord(ctx, &record, err, last, func(r *ExecutionRecord) {
			r.TotalUnits = last.TotalUnits
			r.TotalRows = last.DoneRows
			r.Summary = buildSummary(last.TotalUnits, last.DoneRows, 0)
		})
		return err
	})
	return taskID, nil
}

// StartMigrate 异步启动迁移任务，返回 taskID
func (s *Service) StartMigrate(opts MigrateOptions, taskConfigID string) (string, error) {
	if _, err := s.resolveConn(opts.SourceConn, opts.Source); err != nil {
		return "", err
	}
	if _, err := s.resolveConn(opts.TargetConn, opts.Target); err != nil {
		return "", err
	}
	taskID := newTaskID()
	record := ExecutionRecord{ID: taskID, TaskType: "migrate", TaskConfigID: taskConfigID, Status: "running", StartedAt: time.Now().UnixMilli(),
		Target: fmt.Sprintf("%s → %s · %s", s.connLabel(opts.SourceConn, opts.Source), s.connLabel(opts.TargetConn, opts.Target), targetTables(nil, opts.Tables))}
	_ = s.persist.SaveHistory(record)

	s.runner.Start(taskID, "migrate", func(ctx context.Context, publish ProgressFunc) error {
		var last ProgressInfo
		wrapped := func(p ProgressInfo) { last = p; publish(p) }
		err := s.RunMigrate(ctx, opts, wrapped)
		s.finishRecord(ctx, &record, err, last, func(r *ExecutionRecord) {
			r.TotalUnits = last.TotalUnits
			r.TotalRows = last.DoneRows
			r.Summary = buildSummary(last.TotalUnits, last.DoneRows, 0)
		})
		return err
	})
	return taskID, nil
}

// StartCompare 异步启动对比任务，返回 taskID；完成后结果报告落盘 CompareDir/compare-<taskID>.json
func (s *Service) StartCompare(opts CompareOptions, taskConfigID string) (string, error) {
	src, err := s.resolveConn(opts.SourceConn, opts.Source)
	if err != nil {
		return "", err
	}
	target, err := s.resolveConn(opts.TargetConn, opts.Target)
	if err != nil {
		return "", err
	}
	opts.Source = src
	opts.Target = target
	if (src.DBName == "" && src.Schema == "") || (target.DBName == "" && target.Schema == "") {
		return "", cygin.NewError(cygin.ErrParamsInvalid, cygin.WithErrPrint(), cygin.WithErrDetailf("请先选择对比的库"))
	}
	taskID := newTaskID()
	record := ExecutionRecord{ID: taskID, TaskType: "compare", TaskConfigID: taskConfigID, Status: "running", StartedAt: time.Now().UnixMilli(),
		Target: fmt.Sprintf("%s → %s · %s", s.connLabel(opts.SourceConn, src), s.connLabel(opts.TargetConn, target), targetTables(nil, opts.Tables))}
	_ = s.persist.SaveHistory(record)

	s.runner.Start(taskID, "compare", func(ctx context.Context, publish ProgressFunc) error {
		var last ProgressInfo
		wrapped := func(p ProgressInfo) { last = p; publish(p) }
		result, err := s.RunCompare(ctx, opts, wrapped)
		s.finishRecord(ctx, &record, err, last, func(r *ExecutionRecord) {
			r.TotalUnits = last.TotalUnits
			r.TotalRows = last.DoneRows
			if result == nil {
				return
			}
			outputPath := filepath.Join(s.persist.CompareDir(), "compare-"+taskID+".json")
			if e := saveCompareResult(outputPath, result); e != nil {
				cylog.Errorf("保存对比结果失败: %v", e)
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

// saveCompareResult 对比结果序列化落盘（带缩进便于人工查看）
func saveCompareResult(path string, result *CompareResult) error {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// GetCompareResult 按 taskID 读取已落盘的对比结果报告
func (s *Service) GetCompareResult(taskID string) (*CompareResult, error) {
	path := ""
	if rec, err := s.persist.GetHistory(taskID); err == nil && rec.OutputPath != "" {
		path = rec.OutputPath
	}
	if path == "" {
		path = filepath.Join(s.persist.CompareDir(), "compare-"+taskID+".json")
		// 兼容：旧版对比报告存于 exports/，新路径不存在时回退旧路径读取
		if _, serr := os.Stat(path); serr != nil {
			path = filepath.Join(s.persist.ExportDir(), "compare-"+taskID+".json")
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, cygin.NewError(ErrTaskNotFound, cygin.WithErrPrint(), cygin.WithErrDetailf("对比结果不存在: %s", taskID))
	}
	var result CompareResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, cygin.WrapError(err, cygin.ErrInternalServer, cygin.WithErrPrint(), cygin.WithErrDetails(err.Error()))
	}
	return &result, nil
}

// ProgressCh 订阅任务进度（供 SSE 使用）
func (s *Service) ProgressCh(taskID string) (<-chan ProgressInfo, func(), error) {
	return s.runner.Subscribe(taskID)
}

// CancelTask 取消运行中的任务
func (s *Service) CancelTask(taskID string) error {
	return s.runner.Cancel(taskID)
}

// finishRecord 落盘终态记录：last 为最后一次进度快照，统一持久化完成单元数与日志快照（供终态回放与实时展示一致）；
// ctx 已取消时一律记为 cancelled，不依赖错误文案匹配（驱动层报错文案不可控）
func (s *Service) finishRecord(ctx context.Context, record *ExecutionRecord, err error, last ProgressInfo, fill func(*ExecutionRecord)) {
	record.FinishedAt = time.Now().UnixMilli()
	record.Duration = record.FinishedAt - record.StartedAt
	if err != nil {
		if ctx.Err() != nil || strings.Contains(err.Error(), "任务已取消") {
			record.Status = "cancelled"
		} else {
			record.Status = "error"
			record.ErrorMsg = err.Error()
		}
	} else {
		record.Status = "done"
	}
	record.DoneUnits = last.DoneUnits
	record.Logs = trimLogs(last.Logs)
	if fill != nil {
		fill(record)
	}
	if err := s.persist.SaveHistory(*record); err != nil {
		cylog.Errorf("保存执行历史失败: %v", err)
	}
}

// trimLogs 只保留最近 200 条日志，控制历史记录文件体积
func trimLogs(logs []string) []string {
	const keep = 200
	if len(logs) <= keep {
		return logs
	}
	return logs[len(logs)-keep:]
}

// ---- 任务配置管理 ----

// SaveTask 保存任务配置（新增或更新），并标记为最近使用
func (s *Service) SaveTask(task *TaskConfig) error {
	if strings.TrimSpace(task.Name) == "" {
		return cygin.NewError(cygin.ErrParamsInvalid, cygin.WithErrPrint(), cygin.WithErrDetailf("任务名称不能为空"))
	}
	if task.Type == "" {
		return cygin.NewError(cygin.ErrParamsInvalid, cygin.WithErrPrint(), cygin.WithErrDetailf("任务类型不能为空"))
	}
	now := time.Now().UnixMilli()
	if task.ID == "" {
		task.ID = newTaskID()
		task.CreatedAt = now
	}
	task.UpdatedAt = now
	if err := s.persist.SaveTask(*task); err != nil {
		return cygin.WrapError(err, cygin.ErrInternalServer, cygin.WithErrPrint(), cygin.WithErrDetails(err.Error()))
	}
	if err := s.persist.MarkLastUsed(task.ID, task.Type); err != nil {
		return cygin.WrapError(err, cygin.ErrInternalServer, cygin.WithErrPrint(), cygin.WithErrDetails(err.Error()))
	}
	return nil
}

// ListTasks 列出任务配置（taskType 为空=全部）
func (s *Service) ListTasks(taskType string) []TaskConfig {
	tasks := s.persist.LoadTasks()
	if taskType == "" {
		return tasks
	}
	ret := make([]TaskConfig, 0, len(tasks))
	for _, t := range tasks {
		if t.Type == taskType {
			ret = append(ret, t)
		}
	}
	return ret
}

// GetTask 获取任务配置
func (s *Service) GetTask(id string) (TaskConfig, error) {
	t, ok := s.persist.GetTask(id)
	if !ok {
		return TaskConfig{}, cygin.NewError(ErrTaskNotFound, cygin.WithErrPrint(), cygin.WithErrDetailf("任务配置不存在: %s", id))
	}
	return t, nil
}

// DeleteTask 删除任务配置
func (s *Service) DeleteTask(id string) error {
	if err := s.persist.DeleteTask(id); err != nil {
		return cygin.WrapError(err, cygin.ErrInternalServer, cygin.WithErrPrint(), cygin.WithErrDetails(err.Error()))
	}
	return nil
}

// GetLastTask 获取指定类型最近使用的任务配置
func (s *Service) GetLastTask(taskType string) *TaskConfig {
	return s.persist.GetLastUsed(taskType)
}

// RunTaskByID 以保存的任务配置异步执行，返回 taskID
func (s *Service) RunTaskByID(taskID string) (string, error) {
	task, err := s.GetTask(taskID)
	if err != nil {
		return "", err
	}
	_ = s.persist.MarkLastUsed(task.ID, task.Type)
	switch task.Type {
	case "export":
		if task.ExportOpts == nil {
			return "", cygin.NewError(ErrTaskInvalid, cygin.WithErrPrint(), cygin.WithErrDetailf("任务配置缺少导出选项: %s", task.ID))
		}
		return s.StartExport(*task.ExportOpts, task.ID)
	case "import":
		if task.ImportOpts == nil {
			return "", cygin.NewError(ErrTaskInvalid, cygin.WithErrPrint(), cygin.WithErrDetailf("任务配置缺少导入选项: %s", task.ID))
		}
		return s.StartImport(*task.ImportOpts, task.ID)
	case "migrate":
		if task.MigrateOpts == nil {
			return "", cygin.NewError(ErrTaskInvalid, cygin.WithErrPrint(), cygin.WithErrDetailf("任务配置缺少迁移选项: %s", task.ID))
		}
		return s.StartMigrate(*task.MigrateOpts, task.ID)
	case "compare":
		if task.CompareOpts == nil {
			return "", cygin.NewError(ErrTaskInvalid, cygin.WithErrPrint(), cygin.WithErrDetailf("任务配置缺少对比选项: %s", task.ID))
		}
		return s.StartCompare(*task.CompareOpts, task.ID)
	default:
		return "", cygin.NewError(ErrTaskInvalid, cygin.WithErrPrint(), cygin.WithErrDetailf("未知任务类型: %s", task.Type))
	}
}

// ---- 执行历史 ----

// ListHistory 列出执行历史
func (s *Service) ListHistory(taskType, taskConfigID string) []ExecutionRecord {
	return s.persist.LoadHistory(taskType, taskConfigID)
}

// GetHistory 获取执行记录
func (s *Service) GetHistory(taskID string) (ExecutionRecord, error) {
	return s.persist.GetHistory(taskID)
}

// DeleteHistory 删除执行记录并同步清理其产物文件（仅受管目录内）；运行中的任务不允许删除
func (s *Service) DeleteHistory(taskID string) error {
	rec, err := s.persist.GetHistory(taskID)
	if err != nil {
		return err
	}
	if rec.Status == "running" {
		return cygin.NewError(ErrHistoryRunning, cygin.WithErrPrint(), cygin.WithErrDetailf("任务运行中，无法删除记录: %s", taskID))
	}
	if err := s.persist.DeleteHistory(taskID); err != nil {
		return err
	}
	// 记录删除成功后清理对应产物（导出目录/zip、对比报告），用户自定义输出路径不受影响
	s.persist.RemoveArtifact(rec.OutputPath)
	return nil
}

// newTaskID 生成任务主键：xid（内嵌时间戳，字典序即时间序，全局唯一），与连接配置主键风格一致
func newTaskID() string {
	return xid.New().String()
}

// buildSummary 生成执行摘要，如 "3项, 40000行, 15.3MB"（项 = 表 + 对象）
func buildSummary(units int, rows int64, fileSize int64) string {
	parts := []string{fmt.Sprintf("%d项", units), fmt.Sprintf("%d行", rows)}
	if fileSize > 0 {
		parts = append(parts, humanSize(fileSize))
	}
	return strings.Join(parts, ", ")
}

// connLabel 连接展示名：优先取已保存连接的名称（可标识环境），未找到时回退 Host
func (s *Service) connLabel(connID string, fallback *DBConnInfo) string {
	if rec, ok := s.persist.GetConn(connID); ok && rec.Name != "" {
		return rec.Name
	}
	if fallback != nil && fallback.Host != "" {
		return fallback.Host
	}
	return connID
}

// targetTables 按库聚合表数为 "db1(3表) db2(5表)" 列表；表名为 库.表 限定名；均空时视为整库
func targetTables(databases []string, tables []string) string {
	counts := map[string]int{}
	order := []string{}
	addDB := func(db string) {
		if _, ok := counts[db]; !ok {
			order = append(order, db)
			counts[db] = 0
		}
	}
	for _, t := range tables {
		db := t
		if i := strings.Index(t, "."); i > 0 {
			db = t[:i]
		}
		addDB(db)
		counts[db]++
	}
	for _, db := range databases {
		addDB(db)
	}
	if len(order) == 0 {
		return "整库"
	}
	parts := make([]string, 0, len(order))
	for _, db := range order {
		if counts[db] > 0 {
			parts = append(parts, fmt.Sprintf("%s(%d表)", db, counts[db]))
		} else {
			parts = append(parts, db)
		}
	}
	return strings.Join(parts, " ")
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(n)/float64(div), "KMGTPE"[exp])
}
