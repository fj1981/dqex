// Package dqex 提供 dqex 的库化公开门面（docs/library-api-design.md 第 3 章）。
//
// 外部 Go 程序只 import 本包即可调用核心能力（迁移/导出/导入/对比/查询/元数据/快照），
// 无需启动 dqex 进程或经 HTTP 调用。门面为薄包装，业务实现复用 internal/service，
// 类型经别名公开：internal/service 的 Options/Result 结构体字段自 v1 起冻结为公开契约。
//
// 基本用法（安装工具场景，进程内完成一次迁移）：
//
//	client, err := dqex.New(
//	    dqex.WithLang("zh"),
//	    dqex.WithInlineConns(
//	        dqex.ConnInfo{ID: "src", Name: "源库", Conn: dqex.NewConn("mysql", "10.0.0.1", 3306, "root", "***", "app")},
//	        dqex.ConnInfo{ID: "tgt", Name: "目标库", Conn: dqex.NewConn("mysql", "10.0.0.2", 3306, "root", "***", "app_new")},
//	    ),
//	)
//	if err != nil { panic(err) }
//	defer client.Close()
//
//	conns := client.ListConnections()
//	err = client.RunMigrate(context.Background(), dqex.MigrateOptions{
//	    SourceConn: conns[0].ID, TargetConn: conns[1].ID,
//	}, func(p dqex.ProgressInfo) { fmt.Printf("%s\n", p.Message) })
//
// 并发契约：Client 可被多个 goroutine 并发使用；Close() 幂等，关闭后调用能力方法返回
// ErrClientClosed。同进程多 Client 实例可用但共享元数据缓存与连接池（v0 语义）。
package dqex

import (
	"github.com/fj1981/dqex/internal/engine"
	"github.com/fj1981/dqex/internal/service"
	"github.com/fj1981/infrakit/pkg/cydb/def"
)

// NewConn 构造网络型数据库连接信息（mysql/postgresql/oracle 及兼容产品）。
// DBConnInfo 内嵌 cydb def.DBConnection（复合字面量不能直接写提升字段），
// 提供此便捷构造器；SubType/Service 等特殊字段对返回值直接赋值即可：
//
//	c := dqex.NewConn("mysql", "10.0.0.1", 3306, "root", "***", "app")
//	c.SubType = "mariadb" // 兼容产品按需指定
func NewConn(dbType, host string, port int, user, pwd, dbName string) DBConnInfo {
	return DBConnInfo{DBConnection: def.DBConnection{
		Type: dbType, Host: host, Port: port, Un: user, Pw: pwd, DBName: dbName,
	}}
}

// ---- 连接与记录类型 ----

type (
	// DBConnInfo 数据库连接信息（含子类型，内嵌 def.DBConnection）
	DBConnInfo = service.DBConnInfo
	// ConnInfo 连接列表展示信息（ID/Name/ShortName/Env + Conn）
	ConnInfo = service.ConnInfo
	// ConnRecord 连接配置存储记录（AddConnection 入参/出参）
	ConnRecord = service.ConnRecord
	// ConnProvider 连接提供者回调：连接列表完全由外部持有（见 options.go WithConnProvider）
	ConnProvider = service.ConnProvider
	// ConnHooks 连接生命周期回调（审计/监控，见 options.go WithConnHooks）
	ConnHooks = service.ConnHooks
	// ConnSource 连接解析来源（ConnHooks.OnResolved 参数：inline/memory/provider/store）
	ConnSource = service.ConnSource
	// StoreMode 持久化模式（StoreNone/StoreSQLite/StoreExternal）
	StoreMode = service.StoreMode
)

// 连接解析来源常量
const (
	ConnSourceInline   = service.ConnSourceInline
	ConnSourceMemory   = service.ConnSourceMemory
	ConnSourceProvider = service.ConnSourceProvider
	ConnSourceStore    = service.ConnSourceStore
)

// 持久化模式常量
const (
	// StoreNone 纯内存：不建存储、不写历史/任务/连接库（默认，库模式）
	StoreNone = service.StoreNone
	// StoreSQLite 数据目录 + SQLite（WithDataDir 便捷糖）
	StoreSQLite = service.StoreSQLite
	// StoreExternal 外部注入存储（触发式，v0.x 仅定义）
	StoreExternal = service.StoreExternal
)

// ---- 任务选项 / 结果类型 ----

type (
	// TableCondition 表级过滤条件（迁移/导出/对比取数）
	TableCondition = service.TableCondition
	// TableAlias 表别名配对
	TableAlias = service.TableAlias
	// ExportOptions 导出选项
	ExportOptions = service.ExportOptions
	// ImportOptions 导入选项
	ImportOptions = service.ImportOptions
	// Contributor 业务对象贡献者（代理层扩展点）：宿主注册取数/回写回调，
	// 导出/导入编排（目录、进度、打包、清单）由 dqex 统一负责
	Contributor = service.Contributor
	// ContributorRequest 贡献者导出回调请求
	ContributorRequest = service.ContributorRequest
	// ContributorResult 贡献者导出回调结果
	ContributorResult = service.ContributorResult
	// ContributorImportRequest 贡献者导入回调请求
	ContributorImportRequest = service.ContributorImportRequest
	// DataPreparer 数据前置处理器（代理层扩展点，key=目标库名）：.json 数据包
	// 导入应用前回调宿主执行业务策略（如流程/表单版本合并），可修改包内容
	DataPreparer = service.DataPreparer
	// DataPrepareRequest 数据前置处理请求
	DataPrepareRequest = service.DataPrepareRequest
	// DataPackage 数据交换包（dqex 数据格式契约，字段冻结）：
	// FormatJSON 导出 / .json 导入 / 精确回滚的载体
	DataPackage = service.DataPackage
	// DataEntry 数据包条目（type: 0=建表 1=按 PK 行数据 2=成对 SQL）
	DataEntry = service.DataEntry
	// QueryHooks SQL 审计钩子（逐语句回调 OnQuery，经 WithQueryHooks 注册）
	QueryHooks = service.QueryHooks
	// ExportFormat 导出产物格式
	ExportFormat = service.ExportFormat
	// MigrateOptions 迁移选项
	MigrateOptions = service.MigrateOptions
	// CompareOptions 对比选项
	CompareOptions = service.CompareOptions
	// CompareDBPair 对比的库对（源库 ↔ 目标库，按索引配对）
	CompareDBPair = engine.CompareDBPair
	// DictionaryOptions 数据字典导出选项
	DictionaryOptions = service.DictionaryOptions
	// CompareResult 对比结果
	CompareResult = service.CompareResult
	// Snapshot 快照完整数据
	Snapshot = service.Snapshot
	// SnapshotInfo 快照摘要
	SnapshotInfo = service.SnapshotInfo
	// SnapshotCompareOptions 快照对比选项
	SnapshotCompareOptions = service.SnapshotCompareOptions
	// ObjectDDLResult 对象创建语句查询结果
	ObjectDDLResult = service.ObjectDDLResult
)

// 重置数据模式常量（ImportOptions/MigrateOptions.ResetMode）
const (
	ResetNone     = service.ResetNone
	ResetTruncate = service.ResetTruncate
	ResetDrop     = service.ResetDrop
	// FormatSQL / FormatJSON 导出产物格式（ExportOptions.Format）
	FormatSQL  = service.FormatSQL
	FormatJSON = service.FormatJSON
)

// ---- 进度类型 ----

type (
	// ProgressInfo 任务进度信息（ProgressFunc 回调参数）
	ProgressInfo = service.ProgressInfo
	// ProgressFunc 进度回调。
	// 契约（3.6）：回调在 dqex 执行 goroutine 上同步调用，回调内阻塞会暂停任务；
	// 回调返回不是取消机制——取消统一由 ctx 控制；不保证串行、不保证每阶段恰好触发一次，
	// 消费方按幂等展示设计。
	ProgressFunc = service.ProgressFunc
)

// ---- 元数据 / 查询类型（engine 层） ----

type (
	// SchemaSummary schema 摘要（PG 系）
	SchemaSummary = engine.SchemaSummary
	// DBSchema 指定库/schema 的对象清单
	DBSchema = engine.DBSchema
	// DBTables 指定库的对象清单（MySQL/Oracle 无 schema 层）
	DBTables = engine.DBTables
	// TableColumnInfo 表列信息（名称/类型/可空/主键/默认值）
	TableColumnInfo = engine.TableColumnInfo
	// SQLQueryResult SQL 语句执行结果集
	SQLQueryResult = engine.SQLQueryResult
	// TablePageResult 表浏览分页查询结果
	TablePageResult = engine.TablePageResult
	// SortSpec 表浏览排序规格
	SortSpec = engine.SortSpec
	// ColumnFilter 表浏览列过滤条件（AND 叠加，值参数化绑定防注入）
	ColumnFilter = engine.ColumnFilter
	// GenSQLParams 快速生成 SQL 的参数（行/单元格/过滤条件 → 方言正确语句）
	GenSQLParams = engine.GenSQLParams
	// UpdateCellParams 单元格更新参数（named bind 防注入）
	UpdateCellParams = engine.UpdateCellParams
	// InsertRowParams 新增行参数
	InsertRowParams = engine.InsertRowParams
	// DeleteRowParams 删除行参数
	DeleteRowParams = engine.DeleteRowParams
)
