package service

import (
	"os"
	"path/filepath"
	"strings"

	"dqex/internal/store"

	"github.com/fj1981/infrakit/pkg/cylog"
)

// PersistMgr 统一持久化管理：连接配置 + 任务配置 + 执行历史 + SQL 历史 + SQL 审计 + Web 凭证，
// 全部存储于 SQLite（dqex.db），目录类资源（上传/临时/导出/对比/快照）仍为文件系统目录。
type PersistMgr struct {
	baseDir                                               string
	tmpDir, uploadDir, exportDir, compareDir, snapshotDir string
	store                                                 store.Store
}

// 数据根目录（默认 ~/.dqex，--data-dir 可覆盖）下的子目录规划：
//   - 根目录：SQLite 数据库（dqex.db）
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

// NewPersistMgr 创建持久化管理器（默认 ~/.dqex/，子目录由 data 目录派生）
func NewPersistMgr(baseDir string) (*PersistMgr, error) {
	return NewPersistMgrWith(ResolveDirs(baseDir, nil))
}

// NewPersistMgrWith 按解析后的五类目录创建持久化管理器，并打开 SQLite 存储。
func NewPersistMgrWith(dirs ResolvedDirs) (*PersistMgr, error) {
	for _, d := range []string{dirs.Data, dirs.Tmp, dirs.Uploads, dirs.Exports, dirs.Compares, dirs.Snapshots} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}
	st, err := store.OpenSQLite(store.DefaultDBPath(dirs.Data))
	if err != nil {
		return nil, err
	}
	return &PersistMgr{
		baseDir: dirs.Data, tmpDir: dirs.Tmp, uploadDir: dirs.Uploads,
		exportDir: dirs.Exports, compareDir: dirs.Compares, snapshotDir: dirs.Snapshots,
		store: st,
	}, nil
}

// Close 关闭底层存储（进程退出前调用，确保 SQLite 连接释放）。
func (p *PersistMgr) Close() error {
	if p.store != nil {
		return p.store.Close()
	}
	return nil
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

// UpdateDirs 运行时热更新子目录（不修改 baseDir，因 SQLite 已打开）。
// 产物类目录（exports/compares/snapshots）变更时自动迁移原数据到新目录，并同步
// 重写执行历史中的产物路径（OutputPath 保持全路径），保证历史下载始终可用；
// tmp/uploads 为临时目录（任务结束自动清理），不做迁移。
func (p *PersistMgr) UpdateDirs(dirs ResolvedDirs) error {
	for _, d := range []string{dirs.Tmp, dirs.Uploads, dirs.Exports, dirs.Compares, dirs.Snapshots} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	// 迁移产物目录并同步历史路径（旧目录 → 新目录）
	for _, pair := range []struct{ name, oldDir, newDir string }{
		{"exports", p.exportDir, dirs.Exports},
		{"compares", p.compareDir, dirs.Compares},
		{"snapshots", p.snapshotDir, dirs.Snapshots},
	} {
		if filepath.Clean(pair.oldDir) == filepath.Clean(pair.newDir) {
			continue
		}
		if err := migrateDir(pair.oldDir, pair.newDir); err != nil {
			cylog.Warnf("迁移%s目录数据失败（历史路径保持不变，仍指向旧目录）: %v", pair.name, err)
			continue
		}
		p.rewriteHistoryOutputPaths(pair.oldDir, pair.newDir)
	}
	p.tmpDir = dirs.Tmp
	p.uploadDir = dirs.Uploads
	p.exportDir = dirs.Exports
	p.compareDir = dirs.Compares
	p.snapshotDir = dirs.Snapshots
	cylog.Infof("数据目录已热更新: tmp=%s uploads=%s exports=%s compares=%s snapshots=%s",
		dirs.Tmp, dirs.Uploads, dirs.Exports, dirs.Compares, dirs.Snapshots)
	return nil
}

// migrateDir 将 src 目录内容迁移到 dst（目标已存在的同名条目跳过，避免覆盖）。
func migrateDir(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	for _, e := range entries {
		from := filepath.Join(src, e.Name())
		to := filepath.Join(dst, e.Name())
		if _, err := os.Lstat(to); err == nil {
			continue
		}
		if err := os.Rename(from, to); err != nil {
			return err
		}
	}
	return nil
}

// rewriteHistoryOutputPaths 将执行历史中 OutputPath 的旧目录前缀重写为新目录前缀。
func (p *PersistMgr) rewriteHistoryOutputPaths(oldDir, newDir string) {
	oldPrefix := filepath.Clean(oldDir) + string(os.PathSeparator)
	recs := p.store.LoadHistory("", "")
	for _, rec := range recs {
		if rec.OutputPath == "" || !strings.HasPrefix(filepath.Clean(rec.OutputPath), oldPrefix) {
			continue
		}
		rec.OutputPath = filepath.Join(newDir, strings.TrimPrefix(filepath.Clean(rec.OutputPath), oldPrefix))
		if err := p.store.SaveHistory(rec); err != nil {
			cylog.Warnf("重写历史产物路径失败 %s: %v", rec.ID, err)
		}
	}
}

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
		// 避免日志暴露完整服务器路径
		if pe, ok := err.(*os.PathError); ok {
			cylog.Warnf("清理产物文件失败: %v", pe.Err)
		} else {
			cylog.Warnf("清理产物文件失败: %v", err)
		}
	}
}

// ---- 连接配置 ----

// SaveConn 保存连接配置：rec.ID 非空则按主键更新，否则生成新 xid
func (p *PersistMgr) SaveConn(rec ConnRecord) (ConnRecord, error) {
	return p.store.SaveConn(rec)
}

// LoadConns 加载全部连接配置（按 ID 索引）
func (p *PersistMgr) LoadConns() map[string]ConnRecord {
	return p.store.LoadConns()
}

// GetConn 按主键 ID 查找连接；兼容按名称或短名查找（旧任务配置引用）
func (p *PersistMgr) GetConn(key string) (ConnRecord, bool) {
	return p.store.GetConn(key)
}

// DeleteConn 删除连接配置（按主键 ID，兼容名称或短名）
func (p *PersistMgr) DeleteConn(key string) error {
	return p.store.DeleteConn(key)
}

// ---- 任务配置 ----

// SaveTask 保存任务配置（按 ID 更新或新增）
func (p *PersistMgr) SaveTask(task TaskConfig) error {
	return p.store.SaveTask(task)
}

// LoadTasks 加载全部任务配置
func (p *PersistMgr) LoadTasks() []TaskConfig {
	return p.store.LoadTasks()
}

// GetTask 获取指定任务配置
func (p *PersistMgr) GetTask(id string) (TaskConfig, bool) {
	return p.store.GetTask(id)
}

// DeleteTask 删除任务配置
func (p *PersistMgr) DeleteTask(taskID string) error {
	return p.store.DeleteTask(taskID)
}

// MarkLastUsed 标记指定类型为最近使用（同类型其他任务取消标记）
func (p *PersistMgr) MarkLastUsed(taskID, taskType string) error {
	return p.store.MarkLastUsed(taskID, taskType)
}

// GetLastUsed 获取指定类型最近使用的任务配置
func (p *PersistMgr) GetLastUsed(taskType string) *TaskConfig {
	return p.store.GetLastUsed(taskType)
}

// ---- Web 访问凭证 ----

// SaveWebAccess 保存 Web 访问凭证（0600 落盘）
func (p *PersistMgr) SaveWebAccess(info WebAccessInfo) error {
	return p.store.SaveWebAccess(info)
}

// LoadWebAccess 读取 Web 访问凭证；文件不存在或无有效内容时 ok=false
func (p *PersistMgr) LoadWebAccess() (WebAccessInfo, bool) {
	return p.store.LoadWebAccess()
}

// ---- 执行历史 ----

// SaveHistory 保存执行历史（按 ID 更新或新增，超出上限裁剪最旧记录）
func (p *PersistMgr) SaveHistory(record ExecutionRecord) error {
	return p.store.SaveHistory(record)
}

// LoadHistory 加载执行历史（taskType 为空=全部，taskConfigID 为空=不过滤）
func (p *PersistMgr) LoadHistory(taskType, taskConfigID string) []ExecutionRecord {
	return p.store.LoadHistory(taskType, taskConfigID)
}

// GetHistory 获取指定执行记录
func (p *PersistMgr) GetHistory(id string) (ExecutionRecord, error) {
	return p.store.GetHistory(id)
}

// DeleteHistory 删除指定执行记录
func (p *PersistMgr) DeleteHistory(id string) error {
	return p.store.DeleteHistory(id)
}

// ---- SQL 执行历史 ----

// AddSQLHistory 追加一条 SQL 执行历史（每连接环形保留最近 N 条）
func (p *PersistMgr) AddSQLHistory(item SQLHistoryItem) error {
	return p.store.AddSQLHistory(item)
}

// ListSQLHistory 返回某连接的历史（新→旧）
func (p *PersistMgr) ListSQLHistory(connID string) []SQLHistoryItem {
	items, err := p.store.ListSQLHistory(connID)
	if err != nil {
		cylog.Warnf("加载 SQL 执行历史失败: %v", err)
		return []SQLHistoryItem{}
	}
	return items
}

// ClearSQLHistory 清空某连接的历史
func (p *PersistMgr) ClearSQLHistory(connID string) error {
	return p.store.ClearSQLHistory(connID)
}

// ---- SQL 收藏（全局共享，conn_id/db 仅作来源标记） ----

// AddFavorite 新增一条收藏
func (p *PersistMgr) AddFavorite(f *SQLFavorite) error {
	return p.store.AddFavorite(f)
}

// ListFavorites 返回全部收藏（全局共享，不按连接隔离；新→旧）
func (p *PersistMgr) ListFavorites() []*SQLFavorite {
	items, err := p.store.ListFavorites()
	if err != nil {
		cylog.Warnf("加载 SQL 收藏失败: %v", err)
		return []*SQLFavorite{}
	}
	return items
}

// DeleteFavorite 删除收藏（按全局唯一 id 定位）
func (p *PersistMgr) DeleteFavorite(id string) error {
	return p.store.DeleteFavorite(id)
}

// RenameFavorite 重命名收藏（按全局唯一 id 定位）
func (p *PersistMgr) RenameFavorite(id, title string) error {
	return p.store.RenameFavorite(id, title)
}

// ---- SQL 审计（只增不删） ----

// AppendSQLAudit 追加一条 SQL 审计日志（只追加，不提供删除）
func (p *PersistMgr) AppendSQLAudit(entry SQLAuditEntry) error {
	return p.store.AppendSQLAudit(entry)
}

// ListSQLAudit 读取审计日志（倒序，分页）。connID 为空返回全部连接。
func (p *PersistMgr) ListSQLAudit(connID string, limit, offset int) ([]SQLAuditEntry, error) {
	return p.store.ListSQLAudit(connID, limit, offset)
}

// ---- 查询工作区 ----

// SaveWorkspace 保存某连接的工作区（整体覆盖）。
func (p *PersistMgr) SaveWorkspace(connID string, state WorkspaceState) error {
	return p.store.SaveWorkspace(connID, state)
}

// LoadWorkspace 读取某连接的工作区；无记录时 ok=false。
func (p *PersistMgr) LoadWorkspace(connID string) (WorkspaceState, bool) {
	return p.store.LoadWorkspace(connID)
}

// DeleteWorkspace 删除某连接的工作区。
func (p *PersistMgr) DeleteWorkspace(connID string) error {
	return p.store.DeleteWorkspace(connID)
}

// ---- AI 会话（对话历史，按连接持久化） ----

// SaveAISession 保存/更新一个 AI 会话（整组消息覆盖写）。
func (p *PersistMgr) SaveAISession(rec AISessionRecord) error {
	return p.store.SaveAISession(rec)
}

// LoadAISession 读取指定会话；无记录时 ok=false。
func (p *PersistMgr) LoadAISession(sessionID string) (AISessionRecord, bool) {
	return p.store.LoadAISession(sessionID)
}

// ListAISessions 列出某连接（可选指定 tab）的会话（新→旧，仅元信息不含消息）。
func (p *PersistMgr) ListAISessions(connID, tabID string) []AISessionRecord {
	items, err := p.store.ListAISessions(connID, tabID)
	if err != nil {
		cylog.Warnf("加载 AI 会话列表失败: %v", err)
		return []AISessionRecord{}
	}
	return items
}

// DeleteAISession 删除指定会话。
func (p *PersistMgr) DeleteAISession(sessionID string) error {
	return p.store.DeleteAISession(sessionID)
}

// DeleteAISessionByTab 删除某连接下指定 tab 的会话（tab 关闭时调用）。
func (p *PersistMgr) DeleteAISessionByTab(connID, tabID string) error {
	return p.store.DeleteAISessionByTab(connID, tabID)
}

// DeleteAISessionsByConn 删除某连接的全部会话。
func (p *PersistMgr) DeleteAISessionsByConn(connID string) error {
	return p.store.DeleteAISessionsByConn(connID)
}

// PurgeExcessAISessions 清理超额会话（会话数 > maxPerConn 且超过 keepDays 天未活动），返回删除条数。
func (p *PersistMgr) PurgeExcessAISessions(maxPerConn, keepDays int) (int64, error) {
	return p.store.PurgeExcessAISessions(maxPerConn, keepDays)
}
