package service

import (
	"errors"
	"os"
	"path/filepath"
	"sync"

	"gopkg.in/yaml.v3"
)

// AIModelInfo 模型信息（名称 + 上下文窗口 + 单次回复上限）。
type AIModelInfo struct {
	Name      string `json:"name"`
	Context   int    `json:"context"`   // 上下文窗口大小（K tokens）
	MaxTokens int    `json:"maxTokens"` // 单次回复最大 token（0 表示未设置）
}

// AIProvider 厂商预设（API 响应结构，Models 含上下文信息）。
type AIProvider struct {
	ID      string        `json:"id"`
	Name    string        `json:"name"`
	BaseURL string        `json:"baseUrl"`
	Models  []AIModelInfo `json:"models"`
	Builtin bool          `json:"builtin,omitempty"` // 是否内置厂商（仅展示用）
}

// AIProviderItem 前端管理接口使用的厂商结构（不含 Builtin 标记）。
type AIProviderItem struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	BaseURL string   `json:"baseUrl"`
	Models  []string `json:"models"`
}

// AIProviderConfig 本地配置文件结构（~/.dqex/ai_providers.yaml）。
type AIProviderConfig struct {
	Providers []AIProviderStored `yaml:"providers"`
}

// AIProviderStored YAML 持久化结构（模型仅为名称列表，上下文由内置映射提供）。
type AIProviderStored struct {
	ID      string   `yaml:"id"`
	Name    string   `yaml:"name"`
	BaseURL string   `yaml:"base_url"`
	Models  []string `yaml:"models"`
}

// modelDef 内置模型定义（上下文窗口 + 单次回复上限，单位 K tokens）。
type modelDef struct {
	Context   int
	MaxTokens int
}

// builtinModels 内置模型定义（上下文窗口 + 单次回复上限，单位 K tokens）。
// 数据来源（2026.08 验证）：
//
//	OpenAI 官方: GPT-5.6 Sol/Terra/Luna 1M context, 128K max output; o3/o4-mini 200K/100K
//	DeepSeek API: V4-Pro/Flash 1M context, 384K max output; R1 128K context, 64K max output
//	阿里云百炼: qwen3.8-max 1M/128K; qwen3.6-plus 1M/64K; qwen-turbo/plus 1M/64K
//	火山引擎: doubao-seed-2.1-pro/turbo 256K/32K
//	月之暗面: kimi-k3 1M context; kimi-k2.6 256K; kimi-k2.7-code 256K/96K
//	智谱 AI: glm-5.3 1M/128K; glm-5.2 1M/128K; glm-5-turbo 128K/8K
//	MiniMax 官方: M3 1M context; M2.5 200K/32K
var builtinModels = map[string]modelDef{
	// OpenAI（官方 2026.07）
	"gpt-5.6-sol":   {1024, 128},
	"gpt-5.6-terra": {1024, 128},
	"gpt-5.6-luna":  {1024, 128},
	"o3":            {200, 100},
	"o4-mini":       {200, 100},
	// DeepSeek（API 文档 2026.08）
	"deepseek-v4-pro":   {1024, 384},
	"deepseek-v4-flash": {1024, 384},
	"deepseek-r1":       {128, 64},
	// 通义千问（阿里云百炼 2026.08）
	"qwen3.8-max":  {1024, 128},
	"qwen3.6-plus": {1024, 64},
	"qwen-turbo":   {1024, 64},
	"qwen-plus":    {1024, 64},
	// 豆包（火山引擎 2026.06）
	"doubao-seed-2.1-pro":   {256, 32},
	"doubao-seed-2.1-turbo": {256, 32},
	"doubao-1.5-pro":        {256, 16},
	// 月之暗面（官方 2026.07）
	"kimi-k3":        {1024, 128},
	"kimi-k2.6":      {256, 32},
	"kimi-k2.7-code": {256, 96},
	// 智谱 AI（官方 2026.08）
	"glm-5.3":     {1024, 128},
	"glm-5.2":     {1024, 128},
	"glm-5-turbo": {128, 8},
	// MiniMax（官方 2026.08）
	"MiniMax-M3":   {1024, 128},
	"MiniMax-M2.5": {200, 32},
	"MiniMax-M1":   {200, 32},
}

// builtinProviderDefs 内置主流厂商定义（OpenAI 兼容协议，2026.08 验证）。
var builtinProviderDefs = []AIProviderStored{
	{ID: "openai", Name: "OpenAI", BaseURL: "https://api.openai.com/v1", Models: []string{"gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna", "o3", "o4-mini"}},
	{ID: "deepseek", Name: "DeepSeek", BaseURL: "https://api.deepseek.com/v1", Models: []string{"deepseek-v4-pro", "deepseek-v4-flash", "deepseek-r1"}},
	{ID: "qwen", Name: "通义千问", BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Models: []string{"qwen3.8-max", "qwen3.6-plus", "qwen-turbo", "qwen-plus"}},
	{ID: "doubao", Name: "豆包（字节）", BaseURL: "https://ark.cn-beijing.volces.com/api/v3", Models: []string{"doubao-seed-2.1-pro", "doubao-seed-2.1-turbo", "doubao-1.5-pro"}},
	{ID: "moonshot", Name: "月之暗面", BaseURL: "https://api.moonshot.cn/v1", Models: []string{"kimi-k3", "kimi-k2.6", "kimi-k2.7-code"}},
	{ID: "zhipu", Name: "智谱 AI", BaseURL: "https://open.bigmodel.cn/api/paas/v4", Models: []string{"glm-5.3", "glm-5.2", "glm-5-turbo"}},
	{ID: "minimax", Name: "MiniMax", BaseURL: "https://api.minimaxi.com/v1", Models: []string{"MiniMax-M3", "MiniMax-M2.5", "MiniMax-M1"}},
	{ID: "siliconflow", Name: "硅基流动", BaseURL: "https://api.siliconflow.cn/v1", Models: []string{"deepseek-v4-pro", "deepseek-v4-flash", "qwen3.8-max", "glm-5.3"}},
	{ID: "custom", Name: "自定义", BaseURL: "", Models: nil},
}

// providersCache 合并后的厂商列表缓存（内置 + 本地配置）。
var (
	providersOnce    sync.Once
	providersCache   []AIProviderStored
	providersDataDir string // 数据目录（用于定位配置文件）
)

// SetProvidersDataDir 设置数据目录（在 Service 初始化时调用），用于加载本地厂商配置。
func SetProvidersDataDir(dir string) {
	providersDataDir = dir
	providersOnce = sync.Once{} // 重置缓存，下次调用 AIProviders 时重新加载
}

// enrichModels 将模型名称列表转换为带上下文窗口和最大 token 的 AIModelInfo 列表。
func enrichModels(names []string) []AIModelInfo {
	models := make([]AIModelInfo, len(names))
	for i, name := range names {
		def := modelDef{Context: 128} // 未知模型默认 128K 上下文，MaxTokens=0
		if v, ok := builtinModels[name]; ok {
			def = v
		}
		models[i] = AIModelInfo{Name: name, Context: def.Context, MaxTokens: def.MaxTokens}
	}
	return models
}

// AIProviders 返回所有厂商列表（内置 + 本地配置合并，模型带上下文信息，标记 builtin）。
func AIProviders() []AIProvider {
	providersOnce.Do(func() {
		providersCache = loadAndMergeProviders(providersDataDir)
	})
	builtinIDs := make(map[string]bool, len(builtinProviderDefs))
	for _, p := range builtinProviderDefs {
		builtinIDs[p.ID] = true
	}
	result := make([]AIProvider, len(providersCache))
	for i, p := range providersCache {
		result[i] = AIProvider{
			ID: p.ID, Name: p.Name, BaseURL: p.BaseURL,
			Models:  enrichModels(p.Models),
			Builtin: builtinIDs[p.ID],
		}
	}
	return result
}

// SaveAIProviders 保存自定义厂商配置到本地 YAML 文件。
// 内置厂商的修改也会被保存（加载时覆盖内置默认值）。
func SaveAIProviders(providers []AIProviderItem) error {
	if providersDataDir == "" {
		return errors.New("data directory not configured")
	}
	if err := os.MkdirAll(providersDataDir, 0755); err != nil {
		return err
	}
	cfg := AIProviderConfig{Providers: make([]AIProviderStored, len(providers))}
	for i, p := range providers {
		cfg.Providers[i] = AIProviderStored{
			ID: p.ID, Name: p.Name, BaseURL: p.BaseURL, Models: p.Models,
		}
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	cfgPath := filepath.Join(providersDataDir, "ai_providers.yaml")
	if err := os.WriteFile(cfgPath, data, 0644); err != nil {
		return err
	}
	// 重置缓存，下次 AIProviders() 重新加载
	providersOnce = sync.Once{}
	return nil
}

// loadAndMergeProviders 加载本地配置并与内置合并。
func loadAndMergeProviders(dataDir string) []AIProviderStored {
	result := make([]AIProviderStored, len(builtinProviderDefs))
	copy(result, builtinProviderDefs)

	if dataDir == "" {
		return result
	}

	cfgPath := filepath.Join(dataDir, "ai_providers.yaml")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return result // 文件不存在或读取失败，返回内置列表
	}

	var cfg AIProviderConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return result // 解析失败，返回内置列表
	}

	if len(cfg.Providers) == 0 {
		return result
	}

	// 构建 ID 索引，用于覆盖和追加
	indexMap := make(map[string]int, len(result))
	for i, p := range result {
		indexMap[p.ID] = i
	}

	for _, p := range cfg.Providers {
		if p.ID == "" {
			continue
		}
		if idx, ok := indexMap[p.ID]; ok {
			// 覆盖内置
			result[idx] = p
		} else {
			// 追加新厂商（插入到 custom 之前）
			customIdx := indexMap["custom"]
			result = append(result[:customIdx+1], result[customIdx:]...)
			result[customIdx] = p
			// 更新索引
			indexMap[p.ID] = customIdx
			indexMap["custom"] = len(result) - 1
		}
	}

	return result
}
