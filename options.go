package dqex

import (
	"github.com/fj1981/dqex/internal/service"
	"github.com/fj1981/infrakit/pkg/cydb"
	"github.com/fj1981/infrakit/pkg/cydb/def"
	"github.com/fj1981/infrakit/pkg/cydist"
	"github.com/fj1981/infrakit/pkg/cygin"
	"github.com/fj1981/infrakit/pkg/cystore"

	// cystore provider 自注册：保证独立二进制/任意宿主注入的 *cystore.Store
	// 所需 provider 均可用（宿主自行 import 注册亦可，此处兜底去依赖宿主）
	_ "github.com/fj1981/infrakit/pkg/cystore/local"
	_ "github.com/fj1981/infrakit/pkg/cystore/minio"
)

// Option 库客户端配置项（options 模式，见 3.1）。
// 未设置的项均为零依赖默认：不发现全局配置、不建存储（StoreNone 库模式）。

type Option func(*options)

type options struct {
	dataDir    string
	configFile string
	lang       string
	ai         *service.AIConfig

	inlineConns []ConnInfo
	provider    *ConnProvider
	hooks       *ConnHooks
	contribs    []Contributor
	queryHooks  *QueryHooks
	preparers   map[string]DataPreparer
	taskHooks   *TaskHooks

	// 触发式能力（4.4）：v0.2 门面即接受参数并校验，具体实现随场景触发落地
	triggered      string // 首个被注入的触发式能力名（用于报错提示）
	storeConn      *def.DBConnection
	cacheAddr      string
	artifactStore  *cystore.Store // 快照对象存储（WithArtifactStore 已落地）
	artifactBucket string         // 对象存储 bucket（空 = store 默认 bucket）
}

// WithDataDir 设置持久化根目录（快照/历史/连接库），空 = 纯内存。
// 非空即 StoreSQLite 便捷糖（内部等价 sqlite 连接，CLI/Web 同款行为）。
func WithDataDir(dir string) Option {
	return func(o *options) { o.dataDir = dir }
}

// IsArtifactLogicalPath 判断 dqex 任务产物路径是否为逻辑路径（exports/、compares/ 前缀）。
// 逻辑路径产物在库模式（WithArtifactStore 注入）下已由 dqex 直传对象存储，
// 宿主登记引用即可，无需再从本地文件搬运。
func IsArtifactLogicalPath(p string) bool {
	return service.IsArtifactLogicalPath(p)
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

// WithAIConfig 注入 AI 辅助 SQL 配置（OpenAI 兼容协议）。
// BaseURL / APIKey / Model 三项非空即启用 AI 助手（前端 AI 入口随之显示）；
// 配置由宿主持有（如宿主 config.yml），dqex 不落盘、不持久化 APIKey。
// 与 WithConfigFile 同时提供时，本注入优先（覆盖配置文件中的 ai 段）。
func WithAIConfig(ai AIConfig) Option {
	return func(o *options) { c := ai; o.ai = &c }
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

// WithTaskHooks 注入任务生命周期回调（代理层扩展点）：Web 异步任务（导出/字典/
// 导入/迁移/对比）启动与进度推送（含终态）时回调宿主，用于任务镜像/通知等。
//
// 回调契约：OnTaskStart 在任务登记完成后、执行 goroutine 启动前同步调用（宿主
// 落库应快速返回，重活自行起 goroutine）；OnTaskProgress 在任务执行 goroutine
// 中随进度推送调用，宿主需自行保证并发安全；回调内不得再回调 Client 方法。
// 未注入（nil）时零开销，CLI/Web 独立形态不注入、行为不变。
func WithTaskHooks(h TaskHooks) Option {
	return func(o *options) { o.taskHooks = &h }
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

// WithStoreConn 注入内部存储连接（已落地，4.4.1）：元数据（连接/任务/历史/审计/
// 工作区/AI 会话）经 cydb 跨方言落入宿主数据库（sqlite/mysql/postgresql/oracle，
// 表自动迁移；MySQL 需 EnsureDB=true 预建 database），不写本地 SQLite。
// 产物类目录资源仍为本地目录（无 DataDir 时回退系统临时目录；产物对象存储化见 WithArtifactStore）。
func WithStoreConn(conn def.DBConnection) Option {
	return func(o *options) {
		if conn.Type == "" {
			return
		}
		c := conn
		o.storeConn = &c
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

// WithArtifactStore 注入对象存储（复用 infrakit cystore，见 4.4.3）。
// 现阶段用于快照持久化：snapshots/index.json 与 <id>.json 落对象存储
// （key 前缀 snapshots/），解决容器化部署本地盘易失导致的快照丢失；
// exports/compares 仍为本地工作目录（最终交付由宿主自行处理）。
// 未注入时快照落本地目录（CLI/独立部署默认行为不变）；产物对象化
// （ArtifactRef 对象存储化）仍为后续触发式规划，届时复用本 store。
// bucket 为空时使用 store 自带的默认 bucket；所需 provider（minio/local）
// 已由本包 blank import 注册，独立二进制无需宿主侧注册。
func WithArtifactStore(store *cystore.Store, bucket string) Option {
	return func(o *options) {
		if store == nil {
			return
		}
		o.artifactStore = store
		o.artifactBucket = bucket
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
