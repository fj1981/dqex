package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/rs/xid"
	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cygin"
)

// PersistMgr 统一持久化管理：连接配置 + 任务配置 + 执行历史（JSON 文件存储于数据根目录）
type PersistMgr struct {
	baseDir                      string
	tmpDir, uploadDir, exportDir string
	mu                           sync.Mutex
}

// 数据根目录（默认 ~/.dbimpex，--data-dir 可覆盖）下的子目录规划：
//   - 根目录：配置存储（connections/tasks/history JSON）
//   - uploads：Web 上传文件临时目录
//   - tmp：任务处理临时目录（如 zip 解压，任务结束自动清理）
//   - exports：最终生成产物目录（导出 zip/目录、对比报告 JSON）
const (
	UploadDirName = "uploads"
	TempDirName   = "tmp"
	ExportDirName = "exports"
)

// NewPersistMgr 创建持久化管理器（默认 ~/.dbimpex/，子目录由 data 目录派生）
func NewPersistMgr(baseDir string) (*PersistMgr, error) {
	return NewPersistMgrWith(ResolveDirs(baseDir, nil))
}

// NewPersistMgrWith 按解析后的四类目录创建持久化管理器
func NewPersistMgrWith(dirs ResolvedDirs) (*PersistMgr, error) {
	for _, d := range []string{dirs.Data, dirs.Tmp, dirs.Uploads, dirs.Exports} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}
	return &PersistMgr{baseDir: dirs.Data, tmpDir: dirs.Tmp, uploadDir: dirs.Uploads, exportDir: dirs.Exports}, nil
}

// BaseDir 返回存储根目录（配置存储）
func (p *PersistMgr) BaseDir() string { return p.baseDir }

// UploadDir 返回 Web 上传文件临时目录
func (p *PersistMgr) UploadDir() string { return p.uploadDir }

// TempDir 返回任务处理临时目录（zip 解压等，任务结束自动清理）
func (p *PersistMgr) TempDir() string { return p.tmpDir }

// ExportDir 返回最终生成产物目录（导出文件、对比报告）
func (p *PersistMgr) ExportDir() string { return p.exportDir }

func (p *PersistMgr) path(name string) string { return filepath.Join(p.baseDir, name) }

func (p *PersistMgr) loadJSON(name string, v any) error {
	data, err := os.ReadFile(p.path(name))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	// 敏感文件：带密文前缀则解密；无前缀视为旧版明文 JSON（直接兼容，下次保存时自动加密）
	if sensitiveFiles[name] && strings.HasPrefix(string(data), encPrefix) {
		if data, err = decryptData(string(data)); err != nil {
			return err
		}
	}
	return json.Unmarshal(data, v)
}

func (p *PersistMgr) saveJSON(name string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	// 敏感文件（含数据库密码）加密后落盘
	if sensitiveFiles[name] {
		s, err := encryptData(data)
		if err != nil {
			return err
		}
		data = []byte(s)
	}
	// 0600：配置含数据库密码等敏感信息，仅允许当前用户读写
	return os.WriteFile(p.path(name), data, 0o600)
}

// ---- 连接配置（connections.json，map[id]ConnRecord） ----

// loadConnRecordsLocked 加载连接记录（调用方需持锁）。
// 兼容旧格式 map[name]DBConnInfo：自动生成 xid 主键并回写新格式
func (p *PersistMgr) loadConnRecordsLocked() (map[string]ConnRecord, error) {
	raw := map[string]json.RawMessage{}
	// 加载失败（如解密失败）时必须中止，避免空数据覆盖原文件
	if err := p.loadJSON("connections.json", &raw); err != nil {
		return nil, err
	}
	recs := make(map[string]ConnRecord, len(raw))
	legacy := false
	for key, v := range raw {
		var rec ConnRecord
		if err := json.Unmarshal(v, &rec); err == nil && rec.ID != "" {
			recs[rec.ID] = rec
			continue
		}
		// 旧格式：key=名称，value=DBConnInfo → 生成主键迁移
		var conn DBConnInfo
		if err := json.Unmarshal(v, &conn); err != nil {
			continue
		}
		rec = ConnRecord{ID: xid.New().String(), Name: key, Conn: conn}
		recs[rec.ID] = rec
		legacy = true
	}
	if legacy {
		_ = p.saveJSON("connections.json", recs)
	}
	return recs, nil
}

// SaveConn 保存连接配置：rec.ID 非空则按主键更新，否则生成新 xid
func (p *PersistMgr) SaveConn(rec ConnRecord) (ConnRecord, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	conns, err := p.loadConnRecordsLocked()
	if err != nil {
		return rec, err
	}
	if rec.ID == "" {
		rec.ID = xid.New().String()
	}
	conns[rec.ID] = rec
	if err := p.saveJSON("connections.json", conns); err != nil {
		return rec, err
	}
	return rec, nil
}

// LoadConns 加载全部连接配置（按 ID 索引）
func (p *PersistMgr) LoadConns() map[string]ConnRecord {
	p.mu.Lock()
	defer p.mu.Unlock()
	recs, _ := p.loadConnRecordsLocked()
	if recs == nil {
		recs = map[string]ConnRecord{}
	}
	return recs
}

// GetConn 按主键 ID 查找连接；兼容按名称查找（旧任务配置引用）
func (p *PersistMgr) GetConn(key string) (ConnRecord, bool) {
	conns := p.LoadConns()
	if rec, ok := conns[key]; ok {
		return rec, true
	}
	for _, rec := range conns {
		if rec.Name == key {
			return rec, true
		}
	}
	return ConnRecord{}, false
}

// DeleteConn 删除连接配置（按主键 ID，兼容按名称）
func (p *PersistMgr) DeleteConn(key string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	conns, err := p.loadConnRecordsLocked()
	if err != nil {
		return err
	}
	if _, ok := conns[key]; ok {
		delete(conns, key)
	} else {
		for id, rec := range conns {
			if rec.Name == key {
				delete(conns, id)
				break
			}
		}
	}
	return p.saveJSON("connections.json", conns)
}

// ---- 任务配置（tasks.json） ----

// SaveTask 保存任务配置（按 ID 更新或新增）
func (p *PersistMgr) SaveTask(task TaskConfig) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	tasks := []TaskConfig{}
	if err := p.loadJSON("tasks.json", &tasks); err != nil {
		return err
	}
	found := false
	for i := range tasks {
		if tasks[i].ID == task.ID {
			tasks[i] = task
			found = true
			break
		}
	}
	if !found {
		tasks = append(tasks, task)
	}
	return p.saveJSON("tasks.json", tasks)
}

// LoadTasks 加载全部任务配置
func (p *PersistMgr) LoadTasks() []TaskConfig {
	p.mu.Lock()
	defer p.mu.Unlock()
	tasks := []TaskConfig{}
	_ = p.loadJSON("tasks.json", &tasks)
	return tasks
}

// GetTask 获取指定任务配置
func (p *PersistMgr) GetTask(id string) (TaskConfig, bool) {
	for _, t := range p.LoadTasks() {
		if t.ID == id {
			return t, true
		}
	}
	return TaskConfig{}, false
}

// DeleteTask 删除任务配置
func (p *PersistMgr) DeleteTask(taskID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	tasks := []TaskConfig{}
	if err := p.loadJSON("tasks.json", &tasks); err != nil {
		return err
	}
	ret := make([]TaskConfig, 0, len(tasks))
	for _, t := range tasks {
		if t.ID != taskID {
			ret = append(ret, t)
		}
	}
	return p.saveJSON("tasks.json", ret)
}

// MarkLastUsed 标记指定类型为最近使用（同类型其他任务取消标记）
func (p *PersistMgr) MarkLastUsed(taskID, taskType string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	tasks := []TaskConfig{}
	if err := p.loadJSON("tasks.json", &tasks); err != nil {
		return err
	}
	for i := range tasks {
		if tasks[i].Type == taskType {
			tasks[i].IsLastUsed = tasks[i].ID == taskID
		}
	}
	return p.saveJSON("tasks.json", tasks)
}

// GetLastUsed 获取指定类型最近使用的任务配置
func (p *PersistMgr) GetLastUsed(taskType string) *TaskConfig {
	tasks := p.LoadTasks()
	var latest *TaskConfig
	for i := range tasks {
		if tasks[i].Type != taskType {
			continue
		}
		if tasks[i].IsLastUsed {
			return &tasks[i]
		}
		if latest == nil || tasks[i].UpdatedAt > latest.UpdatedAt {
			latest = &tasks[i]
		}
	}
	return latest
}

// ---- 执行历史（history.json） ----

const maxHistoryRecords = 200

// SaveHistory 保存执行历史（按 ID 更新或新增，超出上限裁剪最旧记录）
func (p *PersistMgr) SaveHistory(record ExecutionRecord) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	records := []ExecutionRecord{}
	_ = p.loadJSON("history.json", &records)
	found := false
	for i := range records {
		if records[i].ID == record.ID {
			records[i] = record
			found = true
			break
		}
	}
	if !found {
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].StartedAt > records[j].StartedAt })
	if len(records) > maxHistoryRecords {
		records = records[:maxHistoryRecords]
	}
	return p.saveJSON("history.json", records)
}

// LoadHistory 加载执行历史（taskType 为空=全部，taskConfigID 为空=不过滤）
func (p *PersistMgr) LoadHistory(taskType, taskConfigID string) []ExecutionRecord {
	p.mu.Lock()
	defer p.mu.Unlock()
	records := []ExecutionRecord{}
	_ = p.loadJSON("history.json", &records)
	ret := make([]ExecutionRecord, 0, len(records))
	for _, r := range records {
		if taskType != "" && r.TaskType != taskType {
			continue
		}
		if taskConfigID != "" && r.TaskConfigID != taskConfigID {
			continue
		}
		ret = append(ret, r)
	}
	return ret
}

// GetHistory 获取指定执行记录
func (p *PersistMgr) GetHistory(id string) (ExecutionRecord, error) {
	for _, r := range p.LoadHistory("", "") {
		if r.ID == id {
			return r, nil
		}
	}
	return ExecutionRecord{}, cygin.NewError(ErrTaskNotFound, cygin.WithErrPrint(), cygin.WithErrDetailf("执行记录不存在: %s", id))
}

// DeleteHistory 删除指定执行记录
func (p *PersistMgr) DeleteHistory(id string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	records := []ExecutionRecord{}
	if err := p.loadJSON("history.json", &records); err != nil {
		return err
	}
	ret := make([]ExecutionRecord, 0, len(records))
	for _, r := range records {
		if r.ID != id {
			ret = append(ret, r)
		}
	}
	return p.saveJSON("history.json", ret)
}
