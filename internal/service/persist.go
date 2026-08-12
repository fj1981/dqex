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
	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cylog"
)

// PersistMgr 统一持久化管理：连接配置 + 任务配置 + 执行历史（JSON 文件存储于数据根目录）
type PersistMgr struct {
	baseDir                                                string
	tmpDir, uploadDir, exportDir, compareDir, snapshotDir  string
	mu                                                     sync.Mutex
}

// 数据根目录（默认 ~/.dbimpex，--data-dir 可覆盖）下的子目录规划：
//   - 根目录：配置存储（connections/tasks/history JSON）
//   - uploads：Web 上传文件临时目录
//   - tmp：任务处理临时目录（如 zip 解压，任务结束自动清理）
//   - exports：导出产物目录（导出 zip/目录）
//   - compares：对比报告目录（compare-<ID>.json）
//   - snapshots：快照目录（index.json + <snapshot-id>.json）
const (
	UploadDirName   = "uploads"
	TempDirName     = "tmp"
	ExportDirName   = "exports"
	CompareDirName  = "compares"
	SnapshotDirName = "snapshots"
)

// NewPersistMgr 创建持久化管理器（默认 ~/.dbimpex/，子目录由 data 目录派生）
func NewPersistMgr(baseDir string) (*PersistMgr, error) {
	return NewPersistMgrWith(ResolveDirs(baseDir, nil))
}

// NewPersistMgrWith 按解析后的五类目录创建持久化管理器
func NewPersistMgrWith(dirs ResolvedDirs) (*PersistMgr, error) {
	for _, d := range []string{dirs.Data, dirs.Tmp, dirs.Uploads, dirs.Exports, dirs.Compares, dirs.Snapshots} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}
	return &PersistMgr{baseDir: dirs.Data, tmpDir: dirs.Tmp, uploadDir: dirs.Uploads, exportDir: dirs.Exports, compareDir: dirs.Compares, snapshotDir: dirs.Snapshots}, nil
}

// BaseDir 返回存储根目录（配置存储）
func (p *PersistMgr) BaseDir() string { return p.baseDir }

// UploadDir 返回 Web 上传文件临时目录
func (p *PersistMgr) UploadDir() string { return p.uploadDir }

// TempDir 返回任务处理临时目录（zip 解压等，任务结束自动清理）
func (p *PersistMgr) TempDir() string { return p.tmpDir }

// ExportDir 返回导出产物目录（导出 zip/目录）
func (p *PersistMgr) ExportDir() string { return p.exportDir }

// CompareDir 返回对比报告目录（compare-<ID>.json）
func (p *PersistMgr) CompareDir() string { return p.compareDir }

// SnapshotDir 返回快照目录
func (p *PersistMgr) SnapshotDir() string { return p.snapshotDir }

func (p *PersistMgr) path(name string) string { return filepath.Join(p.baseDir, name) }

// RemoveArtifact 清理执行记录 OutputPath 指向的产物文件（目录或文件均可）。
// 安全边界：仅清理 exports/compares 受管目录内的产物，用户自定义输出路径
// （export -o / compare --output）不动；文件不存在时静默跳过。
func (p *PersistMgr) RemoveArtifact(outputPath string) {
	path := strings.TrimSpace(outputPath)
	if path == "" {
		return
	}
	cleaned := filepath.Clean(path)
	inDir := func(dir string) bool {
		return dir != "" && strings.HasPrefix(cleaned, filepath.Clean(dir)+string(os.PathSeparator))
	}
	if !inDir(p.exportDir) && !inDir(p.compareDir) {
		return
	}
	if err := os.RemoveAll(path); err != nil {
		cylog.Warnf("清理产物文件失败 %s: %v", path, err)
	}
}

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

// GetConn 按主键 ID 查找连接；兼容按名称或短名查找（旧任务配置引用）
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
	for _, rec := range conns {
		if rec.ShortName == key {
			return rec, true
		}
	}
	return ConnRecord{}, false
}

// DeleteConn 删除连接配置（按主键 ID，兼容名称或短名）
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
		found := false
		for id, rec := range conns {
			if rec.Name == key || rec.ShortName == key {
				delete(conns, id)
				found = true
				break
			}
		}
		if !found {
			return cygin.NewError(ErrConnNotFound, cygin.WithErrPrint(), cygin.WithErrDetailf("未找到连接: %s", key))
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

// ---- Web 访问凭证（web-access.json） ----

// WebAccessInfo Web 访问凭证：持久化后重启可复用（未过期时），dbx url 随时可取
type WebAccessInfo struct {
	Addr     string `json:"addr"`               // 监听地址 host:port
	Token    string `json:"token"`              // 访问令牌（空=启动时禁用了认证）
	IssuedAt int64  `json:"issuedAt,omitempty"` // 令牌签发时间（Unix 毫秒），用于过期判断
}

// SaveWebAccess 保存 Web 访问凭证（0600 落盘）
func (p *PersistMgr) SaveWebAccess(info WebAccessInfo) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.saveJSON("web-access.json", info)
}

// LoadWebAccess 读取 Web 访问凭证；文件不存在或无有效内容时 ok=false
func (p *PersistMgr) LoadWebAccess() (WebAccessInfo, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var info WebAccessInfo
	if err := p.loadJSON("web-access.json", &info); err != nil {
		return WebAccessInfo{}, false
	}
	if info.Addr == "" && info.Token == "" {
		return WebAccessInfo{}, false
	}
	return info, true
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
		// 超限裁剪的最旧记录：同步清理其产物文件，避免磁盘无限增长
		for _, r := range records[maxHistoryRecords:] {
			p.RemoveArtifact(r.OutputPath)
		}
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
