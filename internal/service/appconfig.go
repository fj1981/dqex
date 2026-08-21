package service

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/fj1981/infrakit/pkg/cygin"
)

// DefaultConfigName 全局配置文件默认名（位于默认数据目录 ~/.dqex/ 下）
const DefaultConfigName = "config.yaml"

// EnvConfigFile 指定全局配置文件的环境变量
const EnvConfigFile = "dqex_CONFIG"

// DirConfig 六类数据目录配置（留空 = 由 data 目录派生）
type DirConfig struct {
	Data      string `yaml:"data" json:"data"`           // ① 配置保存目录（connections/tasks/history）
	Tmp       string `yaml:"tmp" json:"tmp"`             // ② 任务处理临时目录
	Uploads   string `yaml:"uploads" json:"uploads"`     // ③ Web 上传临时目录
	Exports   string `yaml:"exports" json:"exports"`     // ④ 导出产物目录
	Compares  string `yaml:"compares" json:"compares"`   // ⑤ 对比报告目录
	Snapshots string `yaml:"snapshots" json:"snapshots"` // ⑥ 快照目录
}

// WebConfig Web 服务安全配置
type WebConfig struct {
	Allow []string `yaml:"allow" json:"allow"` // 允许访问的来源白名单（IP/CIDR/域名），留空 = 不限制；本机回环始终放行
}

// AI schema 参考的默认上限（可在 ai.max_schema_chars 中覆盖）
const (
	defaultMaxSchemaChars = 20000 // AI 参考表结构文本的字符上限（约 5K tokens）
)

// AIConfig AI 辅助 SQL 配置（OpenAI 兼容协议，可对接 OpenAI / 国产模型等）。
// 前端编辑后通过 SaveConfig 保存；APIKey 保存后以掩码形式回显。
type AIConfig struct {
	BaseURL        string  `yaml:"base_url" json:"baseUrl"`                          // OpenAI 兼容端点，如 https://api.openai.com/v1 或国内中转
	APIKey         string  `yaml:"api_key,omitempty" json:"apiKey"`                  // API Key（保存后不回显明文）
	Model          string  `yaml:"model" json:"model"`                               // 模型名，如 gpt-4o-mini / deepseek-chat
	Temperature    float32 `yaml:"temperature,omitempty" json:"temperature"`         // 温度 0-2，默认 0.2
	MaxTokens      int     `yaml:"max_tokens,omitempty" json:"maxTokens"`            // 单次回复最大 token，默认 2048
	TimeoutSec     int     `yaml:"timeout_sec,omitempty" json:"timeoutSec"`          // 请求超时（秒），默认 60
	MaxSchemaChars int     `yaml:"max_schema_chars,omitempty" json:"maxSchemaChars"` // AI 可参考的表结构文本字符上限，默认 20000
	SystemPrompt   string  `yaml:"system_prompt,omitempty" json:"systemPrompt"`      // 自定义 system prompt 模板（支持 {dialect}/{schema} 占位符），留空用内置默认
}

// CLIConfig CLI 交互终端配置（dqex sql）
type CLIConfig struct {
	// DisplayMode 查询结果默认显示模式：auto=表格超宽自动降级（默认）；table=强制表格；vertical=强制垂直
	DisplayMode string `yaml:"display_mode,omitempty" json:"displayMode"`
}

// AppConfig 全局独立配置（config.yaml）
type AppConfig struct {
	Dirs DirConfig `yaml:"dirs" json:"dirs"`
	Web  WebConfig `yaml:"web" json:"web"`
	AI   AIConfig  `yaml:"ai" json:"ai"`
	CLI  CLIConfig `yaml:"cli" json:"cli"`
	// Debug 全局 debug 日志开关：开启后输出 debug 及以上级别日志（含 AI 链路）。
	// 等效于命令行 --debug；修改后重启服务生效。
	Debug bool `yaml:"debug,omitempty" json:"debug"`
	// CompatCollation 全局默认：将 MySQL 8.0 特有排序规则（如 utf8mb4_0900_ai_ci）
	// 替换为 MySQL 5.7 兼容的 utf8mb4_unicode_ci，使 DDL 可在低版本 MySQL 上执行。
	// 可在单个迁移/导入任务中覆盖此全局默认值。
	// 不带 omitempty：false 需显式持久化，避免重新加载后被默认值 true 覆盖。
	CompatCollation bool `yaml:"compat_collation" json:"compatCollation"`
}

// ResolvedDirs 解析后的最终目录
type ResolvedDirs struct {
	Data      string `json:"data"`
	Tmp       string `json:"tmp"`
	Uploads   string `json:"uploads"`
	Exports   string `json:"exports"`
	Compares  string `json:"compares"`
	Snapshots string `json:"snapshots"`
}

// FindConfigFile 确定全局配置文件路径：显式指定 > 环境变量 dqex_CONFIG > ~/.dqex/config.yaml（缺省位置不存在时返回 ""）
func FindConfigFile(explicit string) string {
	if explicit = strings.TrimSpace(explicit); explicit != "" {
		return explicit
	}
	if v := strings.TrimSpace(os.Getenv(EnvConfigFile)); v != "" {
		return v
	}
	if home, err := os.UserHomeDir(); err == nil {
		p := filepath.Join(home, ".dqex", DefaultConfigName)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// LoadAppConfig 加载全局配置；path 为空返回默认配置（首次初始化：兼容排序规则默认开启），
// 文件不存在或解析失败报错。
// 加载完成后会对 AI 数字字段填充默认值（0 视为未设置），保证运行时与展示均使用有效值。
func LoadAppConfig(ctx context.Context, path string) (*AppConfig, error) {
	cfg := &AppConfig{}
	if strings.TrimSpace(path) == "" {
		// 首次初始化：无配置文件，兼容排序规则默认开启
		cfg.CompatCollation = true
		cfg.AI.normalize()
		return cfg, nil
	}
	txt := svcTextsFor(langFrom(ctx))
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, cygin.WrapError(err, cygin.ErrParamsInvalid, cygin.WithErrPrint(), cygin.WithErrDetailf(txt.errCfgRead, path))
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, cygin.WrapError(err, cygin.ErrParamsInvalid, cygin.WithErrPrint(), cygin.WithErrDetailf(txt.errCfgParse, path))
	}
	cfg.AI.normalize()
	return cfg, nil
}

// normalize 将 AI 数字字段的零值填充为合理默认值，避免未配置时展示/使用 0。
func (c *AIConfig) normalize() {
	if c.Temperature == 0 {
		c.Temperature = 0.2
	}
	if c.MaxTokens == 0 {
		c.MaxTokens = 2048
	}
	if c.TimeoutSec == 0 {
		c.TimeoutSec = 60
	}
	if c.MaxSchemaChars == 0 {
		c.MaxSchemaChars = defaultMaxSchemaChars
	}
}

// ConfigInfo 全局配置的完整视图（配置内容 + 解析后目录 + 文件路径 + 派生标记）。
type ConfigInfo struct {
	// Config 当前生效的全局配置内容（可编辑项）
	Config AppConfig `json:"config"`
	// Resolved 解析后的六类最终目录（只读展示；除 data 根目录固定外，其余保存后立即热生效）
	Resolved ResolvedDirs `json:"resolved"`
	// ConfigFile 全局配置文件路径（空 = 未发现，保存时将写到默认位置 ~/.dqex/config.yaml）
	ConfigFile string `json:"configFile"`
	// DataDirOverride 是否由 --data-dir 启动参数覆盖了配置的 dirs.data（此时目录项修改不生效）
	DataDirOverride bool `json:"dataDirOverride"`
}

// GetConfigInfo 返回全局配置的完整视图（配置内容 + 解析目录 + 文件路径）。
func (s *Service) GetConfigInfo() ConfigInfo {
	cfg := *s.cfg
	// nil → 空切片：保证 JSON 序列化为 [] 而非 null，前端可直接 .map/.includes
	if cfg.Web.Allow == nil {
		cfg.Web.Allow = []string{}
	}
	// AI APIKey 脱敏：绝不回显明文（前端仅展示掩码，保存时由 SaveConfig 保留原值）
	if cfg.AI.APIKey != "" {
		cfg.AI.APIKey = maskSecret(cfg.AI.APIKey)
	}
	// Resolved 展示用主目录缩写（~ 形式），避免暴露 /Users/xxx 完整路径
	resolved := ResolveDirs(s.dataDirFlag, s.cfg)
	return ConfigInfo{
		Config: cfg,
		Resolved: ResolvedDirs{
			Data:      tildifyHome(resolved.Data),
			Tmp:       tildifyHome(resolved.Tmp),
			Uploads:   tildifyHome(resolved.Uploads),
			Exports:   tildifyHome(resolved.Exports),
			Compares:  tildifyHome(resolved.Compares),
			Snapshots: tildifyHome(resolved.Snapshots),
		},
		ConfigFile:      s.configFile,
		DataDirOverride: strings.TrimSpace(s.dataDirFlag) != "",
	}
}

// tildifyHome 将用户主目录前缀缩写为 ~（如 /Users/xx/.dqex → ~/.dqex，主目录本身 → ~）。
// 跨平台：Windows 用 \ 分隔且路径匹配大小写不敏感（NTFS 不区分大小写）；
// 主目录为文件系统根（如 / 或 C:\）时无意义，保持原样。仅用于接口展示，不参与任何真实文件操作。
func tildifyHome(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" || p == "" {
		return p
	}
	sep := string(filepath.Separator)
	// 主目录本身即根（/ 或 C:\）：任何路径都在其下，缩写无意义
	if home == sep || strings.HasSuffix(home, ":"+sep) {
		return p
	}
	cleanHome := filepath.Clean(home)
	cleanP := filepath.Clean(p)
	if runtime.GOOS == "windows" {
		if strings.EqualFold(cleanP, cleanHome) {
			return "~"
		}
	} else if cleanP == cleanHome {
		return "~"
	}
	prefix := home
	if !strings.HasSuffix(prefix, sep) {
		prefix += sep
	}
	// Windows 大小写不敏感匹配，其余平台精确匹配
	if runtime.GOOS == "windows" {
		if len(p) <= len(prefix) || !strings.EqualFold(p[:len(prefix)], prefix) {
			return p
		}
		return "~" + sep + p[len(prefix):]
	}
	if rest, ok := strings.CutPrefix(p, prefix); ok {
		return "~" + sep + rest
	}
	return p
}

// SaveConfig 将修改后的全局配置写回 config.yaml。
// 目录项中，data 根目录固定（已打开的 SQLite 位置不变）；其余目录保存后立即热生效。
// 访问白名单由 handleSaveConfig 额外刷新过滤器。
func (s *Service) SaveConfig(ctx context.Context, cfg AppConfig) error {
	txt := svcTextsFor(langFrom(ctx))
	path := s.configFile
	if strings.TrimSpace(path) == "" {
		// 未发现配置文件：写入默认位置 ~/.dqex/config.yaml
		home, err := os.UserHomeDir()
		if err != nil {
			return cygin.WrapError(err, cygin.ErrInternalServer, cygin.WithErrPrint(), cygin.WithErrDetails(txt.errCfgHome))
		}
		path = filepath.Join(home, ".dqex", DefaultConfigName)
	}
	// 确保目录存在
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return cygin.WrapError(err, cygin.ErrInternalServer, cygin.WithErrPrint(), cygin.WithErrDetails(txt.errCfgMkdir))
	}
	if cfg.Web.Allow == nil {
		cfg.Web.Allow = []string{}
	}
	// AI APIKey 保护：前端回显的是掩码（含 ****）或空值，保存时保留原密钥，
	// 避免掩码值覆盖真实密钥；仅在用户输入全新完整 key 时更新。
	if strings.TrimSpace(cfg.AI.APIKey) == "" || strings.Contains(cfg.AI.APIKey, "****") {
		cfg.AI.APIKey = s.cfg.AI.APIKey
	}
	// AI 数字字段 0 视为未设置，保存前填充默认值，保证前后端展示一致。
	cfg.AI.normalize()
	data, err := yaml.Marshal(&cfg)
	if err != nil {
		return cygin.WrapError(err, cygin.ErrInternalServer, cygin.WithErrPrint(), cygin.WithErrDetails(txt.errCfgSerialize))
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return cygin.WrapError(err, cygin.ErrInternalServer, cygin.WithErrPrint(), cygin.WithErrDetails(txt.errCfgWrite))
	}
	// 更新内存中的配置（读取接口立即返回最新值）
	s.cfg = &cfg
	s.configFile = path

	// 热更新非 data 目录（data 根目录保持当前运行时 SQLite 所在位置不变）
	dirs := resolveSubDirs(s.persist.BaseDir(), &cfg)
	if err := s.persist.UpdateDirs(dirs); err != nil {
		return cygin.WrapError(err, cygin.ErrInternalServer, cygin.WithErrPrint(), cygin.WithErrDetails(txt.errCfgHotReload))
	}
	return nil
}

// ResolveDirs 计算最终目录。优先级：--data-dir flag > 配置文件 dirs.data > 默认 ~/.dqex；
// 其余目录：配置文件显式值 > 由 data 目录派生
func ResolveDirs(dataDirFlag string, cfg *AppConfig) ResolvedDirs {
	if cfg == nil {
		cfg = &AppConfig{}
	}
	data := strings.TrimSpace(dataDirFlag)
	if data == "" {
		data = strings.TrimSpace(cfg.Dirs.Data)
	}
	if data == "" {
		home, _ := os.UserHomeDir()
		data = filepath.Join(home, ".dqex")
	}
	return resolveSubDirs(data, cfg)
}

// resolveSubDirs 在已知 data 根目录 base 的前提下，解析其余五类目录。
func resolveSubDirs(base string, cfg *AppConfig) ResolvedDirs {
	base = strings.TrimSpace(base)
	pick := func(v, fallback string) string {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
		return fallback
	}
	return ResolvedDirs{
		Data:      base,
		Tmp:       pick(cfg.Dirs.Tmp, filepath.Join(base, TempDirName)),
		Uploads:   pick(cfg.Dirs.Uploads, filepath.Join(base, UploadDirName)),
		Exports:   pick(cfg.Dirs.Exports, filepath.Join(base, ExportDirName)),
		Compares:  pick(cfg.Dirs.Compares, filepath.Join(base, CompareDirName)),
		Snapshots: pick(cfg.Dirs.Snapshots, filepath.Join(base, SnapshotDirName)),
	}
}

// ==================== 目录浏览（设置页目录选择器） ====================

// DirEntry 目录浏览结果中的单个子目录项
type DirEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// DirBrowseResult 目录浏览结果（仅目录，不含文件）
type DirBrowseResult struct {
	Path    string     `json:"path"`    // 当前浏览路径
	Parent  string     `json:"parent"`  // 上级目录路径（已到浏览范围边界时为空）
	Root    string     `json:"root"`    // 浏览范围根目录（用户主目录，不可超出）
	Entries []DirEntry `json:"entries"` // 子目录列表（目录在前，隐藏目录排后）
}

// browseRoot 返回目录浏览的根目录（用户主目录）；失败时兜底到当前工作目录。
func browseRoot() string {
	if home, err := os.UserHomeDir(); err == nil {
		return home
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return string(os.PathSeparator)
}

// BrowseDirs 列出 path 下的子目录，供设置页目录选择器使用。
// 浏览范围限制在用户主目录内：path 为空、不存在或超出范围时回退到主目录。
func (s *Service) BrowseDirs(ctx context.Context, path string) (DirBrowseResult, error) {
	root := browseRoot()
	path = strings.TrimSpace(path)
	dir := path
	if dir == "" {
		dir = root
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	// 范围限制：必须在主目录内（含主目录本身）
	if rel, err := filepath.Rel(root, dir); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		dir = root
	} else if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		dir = root
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return DirBrowseResult{}, cygin.WrapError(err, cygin.ErrInternalServer, cygin.WithErrPrint(), cygin.WithErrDetailf(svcTextsFor(langFrom(ctx)).errDirRead, dir))
	}
	dirs := make([]DirEntry, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dirs = append(dirs, DirEntry{Name: e.Name(), Path: filepath.Join(dir, e.Name())})
	}
	// 排序：隐藏目录（. 开头）排后，其余按名称（不区分大小写）
	sort.Slice(dirs, func(i, j int) bool {
		hi := strings.HasPrefix(dirs[i].Name, ".")
		hj := strings.HasPrefix(dirs[j].Name, ".")
		if hi != hj {
			return !hi
		}
		return strings.ToLower(dirs[i].Name) < strings.ToLower(dirs[j].Name)
	})
	parent := filepath.Dir(dir)
	if filepath.Clean(parent) == filepath.Clean(dir) {
		parent = "" // 已到根，无上级
	}
	return DirBrowseResult{Path: dir, Parent: parent, Root: root, Entries: dirs}, nil
}
