package dqex

import (
	"github.com/fj1981/infrakit/pkg/cydb"
	"github.com/fj1981/infrakit/pkg/cydb/def"
	"github.com/fj1981/infrakit/pkg/cydist"
	"github.com/fj1981/infrakit/pkg/cygin"
	"github.com/fj1981/infrakit/pkg/cystore"
)

// Option 库客户端配置项（options 模式，见 3.1）。
// 未设置的项均为零依赖默认：不发现全局配置、不建存储（StoreNone 库模式）。

type Option func(*options)

type options struct {
	dataDir    string
	configFile string
	lang       string

	inlineConns []ConnInfo
	provider    *ConnProvider
	hooks       *ConnHooks
	contribs    []Contributor
	queryHooks  *QueryHooks
	preparers   map[string]DataPreparer

	// 触发式能力（4.4）：v0.2 门面即接受参数并校验，具体实现随场景触发落地
	triggered string // 首个被注入的触发式能力名（用于报错提示）
	storeConn *def.DBConnection
	cacheAddr string
}

// WithDataDir 设置持久化根目录（快照/历史/连接库），空 = 纯内存。
// 非空即 StoreSQLite 便捷糖（内部等价 sqlite 连接，CLI/Web 同款行为）。
func WithDataDir(dir string) Option {
	return func(o *options) { o.dataDir = dir }
}

// WithConfigFile 显式指定全局配置 config.yaml，空 = 不加载全局配置（不自动发现 ~/.dqex/config.yaml）。
func WithConfigFile(path string) Option {
	return func(o *options) { o.configFile = path }
}

// WithLang 设置错误消息语言（zh/en），默认 zh。
// 决定所有 SvcError 消息与引擎 MsgError 的渲染语言（3.3 i18n 内聚）。
func WithLang(lang string) Option {
	return func(o *options) { o.lang = lang }
}

// WithInlineConns 便捷方式：静态注入少量连接（内部转成只读内存注册表）。
// 连接 key 取 ID，缺省回退 Name，均为空时自动生成（conn-1/conn-2/...）。
func WithInlineConns(conns ...ConnInfo) Option {
	return func(o *options) { o.inlineConns = append(o.inlineConns, conns...) }
}

// WithConnProvider 注入连接提供者回调：连接列表完全由外部持有（3.5）。
// 连接可来自宿主的配置中心、密钥管理系统（Vault/KMS）、自身数据库等任何地方，
// dqex 不落盘、不缓存密码；每次真实建连都走 GetConn 取最新凭证（密码轮换天然支持）。
//
// 回调契约：ListConns/GetConn 可能被并发调用，实现方必须线程安全；
// GetConn 返回 (nil, nil) 表示不认识该 key（继续尝试后续解析来源）。
func WithConnProvider(p ConnProvider) Option {
	return func(o *options) { o.provider = &p }
}

// WithConnHooks 注入连接生命周期回调（审计/监控，3.5）。
//
// 回调契约：回调运行在 dqex 的调用 goroutine 上，OnConnect 在关键路径上必须快速返回
// （重活自己起 goroutine）；回调内不得再回调 Client 方法；回调参数中的连接结构由
// dqex 持有副本，回调方不得复用修改；OnResolved/OnConnect 参数含 Pwd 字段，日志注意遮蔽。
func WithConnHooks(hooks ConnHooks) Option {
	return func(o *options) { o.hooks = &hooks }
}

// WithContributors 注册业务对象贡献者模板（代理层扩展点）。
// 宿主业务对象（流程/面板/规则/数据表等）的"取数"与"回写"经此回调代理给宿主实现，
// 导出/导入的编排（任务目录、进度、zip 打包、<Type>/ 目录约定）由 dqex 统一负责。
// 任务侧仅需在 ExportOptions/ImportOptions.Contributors 填 Type（+ IDs）即可引用。
//
// 回调契约：Export/Import 运行在任务执行 goroutine 上，阻塞会暂停进度推送；
// 取消由回调参数 ctx 控制；回调内不得再回调 Client 方法。
func WithContributors(ctbs ...Contributor) Option {
	return func(o *options) { o.contribs = append(o.contribs, ctbs...) }
}

// WithQueryHooks 注入 SQL 审计钩子（代理层扩展点）：任务（导出/导入/迁移）与
// RunSQLScript 执行的每条语句都会回调 OnQuery，宿主接合规审计/慢查询采集。
//
// 回调契约：同步调用且必须快速返回（重活自行起 goroutine，审计耗时直接计入语句
// 响应时间）；回调内不得再回调 Client 方法；失败语句同样回调（rowsAffected=-1）；
// 超长语句会被截断至 4096 字节后回调（截断点回退至 UTF-8 字符边界）。
func WithQueryHooks(hooks QueryHooks) Option {
	return func(o *options) { o.queryHooks = &hooks }
}

// WithDataPreparers 注册数据前置处理器（代理层扩展点，key=目标库名）：
// .json 数据包（DataPackage）导入应用前回调宿主执行业务策略（如业务对象版本
// 合并），宿主可直接修改包内容后返回。
func WithDataPreparers(preparers map[string]DataPreparer) Option {
	return func(o *options) {
		if o.preparers == nil {
			o.preparers = map[string]DataPreparer{}
		}
		for db, p := range preparers {
			if p != nil {
				o.preparers[db] = p
			}
		}
	}
}

// ---- 触发式能力（4.4）：v0.2 门面即接受参数并校验，具体实现随 4.4 场景触发落地 ----
// 注入后 New 返回 ErrNotImplemented（参数已校验，实现随多副本/对象存储场景确认后交付）。

func (o *options) markTriggered(name string) {
	if o.triggered == "" {
		o.triggered = name
	}
}

func (o *options) validateTriggered() error {
	if o.triggered == "" {
		return nil
	}
	return cygin.NewError(ErrNotImplemented, cygin.WithErrPrint(),
		cygin.WithErrDetailf("triggered capability %q is validated but not implemented yet (see docs/library-api-design.md 4.4)", o.triggered))
}

// WithStoreConn 注入内部存储连接（cydb 多数据库：sqlite/mysql/postgresql/oracle，见 4.4.1，触发式）。
func WithStoreConn(conn def.DBConnection) Option {
	return func(o *options) {
		if conn.Type == "" {
			return
		}
		o.markTriggered("WithStoreConn")
		o.storeConn = &conn
	}
}

// WithStoreDB 实例注入内部存储（宿主复用已有 cydb client/连接池，触发式）。
func WithStoreDB(cli *cydb.DBCli) Option {
	return func(o *options) {
		if cli == nil {
			return
		}
		o.markTriggered("WithStoreDB")
	}
}

// WithCacheRedis 元数据缓存走 Redis（参数注入，见 4.4.2，触发式）。
func WithCacheRedis(addr, pwd string) Option {
	return func(o *options) {
		if addr == "" {
			return
		}
		o.markTriggered("WithCacheRedis")
		o.cacheAddr = addr
		_ = pwd
	}
}

// WithCacheClient 实例注入元数据缓存 Redis（宿主复用已有 Redis，触发式）。
func WithCacheClient(rc *cydist.RedisClient) Option {
	return func(o *options) {
		if rc == nil {
			return
		}
		o.markTriggered("WithCacheClient")
	}
}

// WithArtifactStore 注入产物存储（复用 infrakit cystore：本地/MinIO/S3/OBS/OSS，见 4.4.3，触发式）。
func WithArtifactStore(store *cystore.Store, bucket string) Option {
	return func(o *options) {
		if store == nil {
			return
		}
		o.markTriggered("WithArtifactStore")
		_ = bucket
	}
}

// WithMinio 产物落 MinIO（参数便捷糖，触发式）。
func WithMinio(endpoint, accessKey, secretKey, bucket string) Option {
	return func(o *options) {
		if endpoint == "" {
			return
		}
		o.markTriggered("WithMinio")
		_, _, _ = accessKey, secretKey, bucket
	}
}

// WithS3 产物落 S3（参数便捷糖，触发式）。
func WithS3(region, bucket string) Option {
	return func(o *options) {
		if region == "" {
			return
		}
		o.markTriggered("WithS3")
		_ = bucket
	}
}
