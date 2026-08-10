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

// DirConfig 五类数据目录配置（留空 = 由 data 目录派生）
type DirConfig struct {
	Data     string `yaml:"data"`     // ① 配置保存目录（connections/tasks/history）
	Tmp      string `yaml:"tmp"`      // ② 任务处理临时目录
	Uploads  string `yaml:"uploads"`  // ③ Web 上传临时目录
	Exports  string `yaml:"exports"`  // ④ 导出产物目录
	Compares string `yaml:"compares"` // ⑤ 对比报告目录
}

// WebConfig Web 服务安全配置
type WebConfig struct {
	Allow []string `yaml:"allow"` // 允许访问的来源白名单（IP/CIDR/域名），留空 = 不限制；本机回环始终放行
}

// AppConfig 全局独立配置（config.yaml）
type AppConfig struct {
	Dirs DirConfig `yaml:"dirs"`
	Web  WebConfig `yaml:"web"`
}

// ResolvedDirs 解析后的最终目录
type ResolvedDirs struct {
	Data     string
	Tmp      string
	Uploads  string
	Exports  string
	Compares string
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
		Data:     data,
		Tmp:      pick(cfg.Dirs.Tmp, filepath.Join(data, TempDirName)),
		Uploads:  pick(cfg.Dirs.Uploads, filepath.Join(data, UploadDirName)),
		Exports:  pick(cfg.Dirs.Exports, filepath.Join(data, ExportDirName)),
		Compares: pick(cfg.Dirs.Compares, filepath.Join(data, CompareDirName)),
	}
}
