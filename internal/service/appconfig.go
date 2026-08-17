package service

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cygin"
)

// DefaultConfigName 全局配置文件默认名（位于默认数据目录 ~/.dbimpex/ 下）
const DefaultConfigName = "config.yaml"

// EnvConfigFile 指定全局配置文件的环境变量
const EnvConfigFile = "DBIMPEX_CONFIG"

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

// AppConfig 全局独立配置（config.yaml）
type AppConfig struct {
	Dirs DirConfig `yaml:"dirs" json:"dirs"`
	Web  WebConfig `yaml:"web" json:"web"`
	// CompatCollation 全局默认：将 MySQL 8.0 特有排序规则（如 utf8mb4_0900_ai_ci）
	// 替换为 MySQL 5.7 兼容的 utf8mb4_unicode_ci，使 DDL 可在低版本 MySQL 上执行。
	// 可在单个迁移/导入任务中覆盖此全局默认值。
	CompatCollation bool `yaml:"compat_collation,omitempty" json:"compatCollation"`
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

// FindConfigFile 确定全局配置文件路径：显式指定 > 环境变量 DBIMPEX_CONFIG > ~/.dbimpex/config.yaml（缺省位置不存在时返回 ""）
func FindConfigFile(explicit string) string {
	if explicit = strings.TrimSpace(explicit); explicit != "" {
		return explicit
	}
	if v := strings.TrimSpace(os.Getenv(EnvConfigFile)); v != "" {
		return v
	}
	if home, err := os.UserHomeDir(); err == nil {
		p := filepath.Join(home, ".dbimpex", DefaultConfigName)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// LoadAppConfig 加载全局配置；path 为空返回空配置，文件不存在或解析失败报错
func LoadAppConfig(path string) (*AppConfig, error) {
	cfg := &AppConfig{}
	if strings.TrimSpace(path) == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, cygin.WrapError(err, cygin.ErrParamsInvalid, cygin.WithErrPrint(), cygin.WithErrDetailf("读取全局配置失败: %s", path))
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, cygin.WrapError(err, cygin.ErrParamsInvalid, cygin.WithErrPrint(), cygin.WithErrDetailf("解析全局配置失败: %s", path))
	}
	return cfg, nil
}

// ConfigInfo 全局配置的完整视图（配置内容 + 解析后目录 + 文件路径 + 派生标记）。
type ConfigInfo struct {
	// Config 当前生效的全局配置内容（可编辑项）
	Config AppConfig `json:"config"`
	// Resolved 解析后的六类最终目录（只读展示，配置修改后需重启才生效）
	Resolved ResolvedDirs `json:"resolved"`
	// ConfigFile 全局配置文件路径（空 = 未发现，保存时将写到默认位置 ~/.dbimpex/config.yaml）
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
	return ConfigInfo{
		Config:          cfg,
		Resolved:        ResolveDirs(s.dataDirFlag, s.cfg),
		ConfigFile:      s.configFile,
		DataDirOverride: strings.TrimSpace(s.dataDirFlag) != "",
	}
}

// SaveConfig 将修改后的全局配置写回 config.yaml。
// 目录项与兼容选项需重启服务才生效，访问白名单由服务启动时读取（同样需重启）。
func (s *Service) SaveConfig(cfg AppConfig) error {
	path := s.configFile
	if strings.TrimSpace(path) == "" {
		// 未发现配置文件：写入默认位置 ~/.dbimpex/config.yaml
		home, err := os.UserHomeDir()
		if err != nil {
			return cygin.WrapError(err, cygin.ErrInternalServer, cygin.WithErrPrint(), cygin.WithErrDetailf("无法定位用户主目录: %v", err))
		}
		path = filepath.Join(home, ".dbimpex", DefaultConfigName)
	}
	// 确保目录存在
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return cygin.WrapError(err, cygin.ErrInternalServer, cygin.WithErrPrint(), cygin.WithErrDetailf("创建配置目录失败: %v", err))
	}
	if cfg.Web.Allow == nil {
		cfg.Web.Allow = []string{}
	}
	data, err := yaml.Marshal(&cfg)
	if err != nil {
		return cygin.WrapError(err, cygin.ErrInternalServer, cygin.WithErrPrint(), cygin.WithErrDetailf("序列化全局配置失败: %v", err))
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return cygin.WrapError(err, cygin.ErrInternalServer, cygin.WithErrPrint(), cygin.WithErrDetailf("写入全局配置失败: %v", err))
	}
	// 更新内存中的配置（读取接口立即返回最新值，但目录/白名单等需重启才真正生效）
	s.cfg = &cfg
	s.configFile = path
	return nil
}

// ResolveDirs 计算最终目录。优先级：--data-dir flag > 配置文件 dirs.data > 默认 ~/.dbimpex；
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
		data = filepath.Join(home, ".dbimpex")
	}
	pick := func(v, fallback string) string {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
		return fallback
	}
	return ResolvedDirs{
		Data:      data,
		Tmp:       pick(cfg.Dirs.Tmp, filepath.Join(data, TempDirName)),
		Uploads:   pick(cfg.Dirs.Uploads, filepath.Join(data, UploadDirName)),
		Exports:   pick(cfg.Dirs.Exports, filepath.Join(data, ExportDirName)),
		Compares:  pick(cfg.Dirs.Compares, filepath.Join(data, CompareDirName)),
		Snapshots: pick(cfg.Dirs.Snapshots, filepath.Join(data, SnapshotDirName)),
	}
}
