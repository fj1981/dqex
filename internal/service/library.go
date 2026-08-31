package service

// 库模式支持（docs/library-api-design.md 3.5 / 4.1）：
//   - StoreMode 三态：StoreNone（纯内存，库模式默认）/ StoreSQLite（数据目录 + SQLite）/ StoreExternal（触发式，v0.x 仅定义）
//   - ConnProvider：连接列表完全由外部持有（配置中心/密钥系统等），dqex 不落盘、不缓存密码
//   - ConnHooks：连接生命周期回调（审计/监控），失败只告警不影响主流程
//   - NewLibraryService：库模式构建入口，Cli/Web 的 NewServiceWith 行为不变

import (
	"context"
	"fmt"
	"strings"

	"github.com/fj1981/dqex/internal/engine"
	"github.com/fj1981/infrakit/pkg/cydb"
)

// StoreMode 持久化模式（三态，见 4.1）
type StoreMode int

const (
	// StoreNone 纯内存：不建存储、不写历史/任务/连接库（库模式默认——安装工具/环境管理嵌入即此形态）
	StoreNone StoreMode = iota
	// StoreSQLite 数据目录 + SQLite（CLI/Web 用）；等价于 StoreExternal + sqlite 连接的便捷糖
	StoreSQLite
	// StoreExternal 外部注入存储（WithStoreConn / WithStoreDB，触发式，v0.x 仅定义）
	StoreExternal
)

func (m StoreMode) String() string {
	switch m {
	case StoreSQLite:
		return "sqlite"
	case StoreExternal:
		return "external"
	default:
		return "none"
	}
}

// StoreMode 返回当前持久化模式
func (s *Service) StoreMode() StoreMode { return s.storeMode }

// ConnSource 连接解析来源（ConnHooks.OnResolved 参数）
type ConnSource string

const (
	ConnSourceInline   ConnSource = "inline"   // 任务参数直接携带的连接
	ConnSourceMemory   ConnSource = "memory"   // 内存注册表（WithInlineConns / StoreNone 下 AddConnection）
	ConnSourceProvider ConnSource = "provider" // ConnProvider.GetConn 动态外部源
	ConnSourceStore    ConnSource = "store"    // 持久化存储（仅 StoreSQLite）
)

// ConnProvider 连接提供者回调：连接列表完全由外部持有，库内部不维护连接。
// 两个回调都可能被并发调用，实现方必须线程安全。
type ConnProvider struct {
	// ListConns 返回外部连接列表（供 ListConnections / 元数据遍历使用）。
	// 返回的密码字段宿主自行决定是否填充（List 场景可脱敏）。
	ListConns func(ctx context.Context) ([]ConnInfo, error)
	// GetConn 按 connKey 取一个连接用于实际建连，密码必须是可用的明文。
	// 返回 (nil, nil) 表示不认识该 key（dqex 继续尝试后续来源，最终返回 ErrConnNotFound）。
	GetConn func(ctx context.Context, connKey string) (*DBConnInfo, error)
}

// ConnHooks 连接生命周期回调（审计/监控用，宿主可选注册）。
// 契约：回调运行在 dqex 的调用 goroutine 上，OnConnect 在关键路径上必须快速返回；
// 回调内不得再回调 dqex 的 Client 方法；参数中的连接结构由 dqex 持有副本，回调方不得复用修改。
type ConnHooks struct {
	// OnResolved 连接解析成功后触发（source: inline/memory/provider/store）
	OnResolved func(ctx context.Context, connKey string, source ConnSource)
	// OnConnect 每次真实建连后触发（err=失败原因）；宿主日志场景注意遮蔽 Pwd 字段
	OnConnect func(ctx context.Context, conn *DBConnInfo, err error)
	// OnAdded AddConnection 成功后触发（任意模式，内存/落盘均触发）
	OnAdded func(rec ConnRecord)
	// OnDeleted DeleteConnection 成功后触发（任意模式）
	OnDeleted func(key string)
}

// LibraryOptions 库模式构建参数（门面包使用；CLI/Web 不经此路径）
type LibraryOptions struct {
	// DataDir 持久化根目录：非空 = StoreSQLite（快照/历史/连接库落盘）；空 = StoreNone 纯内存
	DataDir string
	// ConfigFile 显式全局配置路径；空 = 不加载全局配置（不自动发现 ~/.dqex/config.yaml）
	ConfigFile string
	// InlineConns 静态注入的连接（便捷糖：内部转成只读内存注册表）
	InlineConns []ConnInfo
	// Provider 连接提供者回调（连接完全外部持有时注入）
	Provider *ConnProvider
	// Hooks 连接生命周期回调（可选）
	Hooks *ConnHooks
	// Contributors 业务对象贡献者模板（代理层）：任务按 Type 引用，回调由此处补齐
	Contributors []Contributor
	// QueryHooks SQL 审计钩子：任务/查询执行逐语句回调（OnQuery），宿主接合规审计
	QueryHooks *engine.QueryHooks
	// DataPreparers 数据前置处理器（代理层，key=目标库名）：.json 数据包导入前回调宿主
	// 做版本合并等业务策略，可修改包内容
	DataPreparers map[string]engine.DataPreparer
}

// NewLibraryService 创建库模式业务服务：
// DataDir 非空走 StoreSQLite（与 NewServiceWith 等价的持久化链路），
// DataDir 为空走 StoreNone（无 SQLite、无配置发现，连接仅内存/回调提供）。
func NewLibraryService(ctx context.Context, opts LibraryOptions) (*Service, error) {
	cfg, err := LoadAppConfig(ctx, strings.TrimSpace(opts.ConfigFile))
	if err != nil {
		return nil, err
	}
	s := &Service{
		cfg:           cfg,
		configFile:    strings.TrimSpace(opts.ConfigFile),
		runner:        newTaskRunner(),
		ai:            newAIMgr(),
		storeMode:     StoreNone,
		memConns:      map[string]ConnRecord{},
		connProvider:  opts.Provider,
		connHooks:     opts.Hooks,
		contributors:  opts.Contributors,
		queryHooks:    opts.QueryHooks,
		dataPreparers: opts.DataPreparers,
	}
	if strings.TrimSpace(opts.DataDir) != "" {
		s.storeMode = StoreSQLite
		s.dataDirFlag = opts.DataDir
		persist, err := NewPersistMgrWith(ResolveDirs(opts.DataDir, cfg))
		if err != nil {
			return nil, err
		}
		s.persist = persist
		// 厂商配置数据目录（AI providers 本地加载）；StoreNone 不设置（AI 依赖数据目录，库模式未暴露 AI 能力）
		SetProvidersDataDir(persist.BaseDir())
	}
	// InlineConns → 内存注册表（任意模式均可注入；StoreSQLite 下作为额外连接源，StoreNone 下为主来源）
	for i, ci := range opts.InlineConns {
		rec := ConnRecord{ID: strings.TrimSpace(ci.ID), Name: strings.TrimSpace(ci.Name), ShortName: strings.TrimSpace(ci.ShortName), Env: ci.Env, Conn: ci.Conn}
		if rec.ID == "" {
			if rec.Name != "" {
				rec.ID = rec.Name
			} else {
				rec.ID = fmt.Sprintf("conn-%d", i+1)
			}
		}
		if rec.Name == "" {
			rec.Name = rec.ID
		}
		s.memPut(rec)
	}
	return s, nil
}

// ---- 内存连接注册表（StoreNone 为主场景；任意模式下作为解析链最高持久化优先级） ----

// memGet 按 key 查找内存注册表连接（兼容 ID / 名称 / 短名，与持久层 GetConn 口径一致）
func (s *Service) memGet(key string) (ConnRecord, bool) {
	s.memMu.RLock()
	defer s.memMu.RUnlock()
	if rec, ok := s.memConns[key]; ok {
		return rec, true
	}
	for _, rec := range s.memConns {
		if rec.Name == key || (rec.ShortName != "" && rec.ShortName == key) {
			return rec, true
		}
	}
	return ConnRecord{}, false
}

// memPut 按 ID 写入（ID 相同 = 更新，与 SaveConn 语义一致）
func (s *Service) memPut(rec ConnRecord) {
	s.memMu.Lock()
	defer s.memMu.Unlock()
	s.memConns[rec.ID] = rec
}

// memDelete 按 key 删除（兼容 ID / 名称 / 短名）；返回是否删除成功
func (s *Service) memDelete(key string) bool {
	s.memMu.Lock()
	defer s.memMu.Unlock()
	if _, ok := s.memConns[key]; ok {
		delete(s.memConns, key)
		return true
	}
	for id, rec := range s.memConns {
		if rec.Name == key || (rec.ShortName != "" && rec.ShortName == key) {
			delete(s.memConns, id)
			return true
		}
	}
	return false
}

// memList 返回内存注册表全部连接（按 ID 排序，保证展示稳定）
func (s *Service) memList() []ConnRecord {
	s.memMu.RLock()
	defer s.memMu.RUnlock()
	out := make([]ConnRecord, 0, len(s.memConns))
	for _, rec := range s.memConns {
		out = append(out, rec)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].ID < out[j-1].ID; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// allConnRecords 合并内存注册表 + 持久层连接（短名唯一性等全量校验用）
func (s *Service) allConnRecords() []ConnRecord {
	recs := s.memList()
	if s.persist != nil {
		for _, rec := range s.persist.LoadConns() {
			recs = append(recs, rec)
		}
	}
	return recs
}

// ---- 解析链（resolveConn 扩展） ----

// fireResolved 触发 OnResolved 钩子（失败只告警不影响主流程）
func (s *Service) fireResolved(ctx context.Context, key string, source ConnSource) {
	if s.connHooks != nil && s.connHooks.OnResolved != nil {
		s.connHooks.OnResolved(ctx, key, source)
	}
}

// fireAdded 触发 OnAdded 钩子
func (s *Service) fireAdded(rec ConnRecord) {
	if s.connHooks != nil && s.connHooks.OnAdded != nil {
		s.connHooks.OnAdded(rec)
	}
}

// fireDeleted 触发 OnDeleted 钩子
func (s *Service) fireDeleted(key string) {
	if s.connHooks != nil && s.connHooks.OnDeleted != nil {
		s.connHooks.OnDeleted(key)
	}
}

// ---- 建连统一入口（OnConnect 钩子挂载点） ----

// dial 建立短生命周期连接（调用方 Close），统一触发 ConnHooks.OnConnect。
// dbName 非空覆盖连接库名；统一走 PG 锚点逻辑（DBName 为空的 PG 系依次尝试候选锚点库，
// 与元数据枚举/健康检测口径一致），语义与各调用点此前的 Connect/ConnectDB/ConnectPGWithAnchor 组合等价。
func (s *Service) dial(ctx context.Context, conn DBConnInfo, dbName string) (*cydb.DBCli, error) {
	var cli *cydb.DBCli
	var err error
	if dbName != "" {
		cli, err = engine.ConnectDB(conn, dbName)
	} else {
		cli, err = engine.ConnectPGWithAnchor(conn)
	}
	if s.connHooks != nil && s.connHooks.OnConnect != nil {
		cp := conn
		s.connHooks.OnConnect(ctx, &cp, err)
	}
	return cli, err
}
