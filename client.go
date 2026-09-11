package dqex

import (
	"context"
	"sync"

	"github.com/fj1981/dqex/internal/engine"
	"github.com/fj1981/dqex/internal/service"
	"github.com/fj1981/infrakit/pkg/cygin"
)

// Client dqex 库客户端：进程内调用核心能力的统一入口。
//
// 并发契约（3.6）：
//  1. Client 可被多个 goroutine 并发使用：并发调用任意能力方法（含同一方法）安全。
//  2. 连接池为进程级共享（engine cliPool）；Close() 幂等，之后调用能力方法返回 ErrClientClosed。
//     注意：Close 会释放进程级连接池，同进程其他 Client 实例的池化连接一并释放（进程级语义）。
//  3. 同进程多 Client 实例可用但共享元数据缓存与连接池（v0 语义，安全无冲突）。
type Client struct {
	svc  *service.Service
	lang string

	mu     sync.Mutex
	closed bool
}

// New 创建库客户端。零依赖即可用：不传任何 Option 时为"库模式"（StoreNone），
// 不发现全局配置、不建 SQLite 存储；连接经 WithInlineConns/WithConnProvider 注入。
func New(opts ...Option) (*Client, error) {
	o := &options{}
	for _, opt := range opts {
		if opt != nil {
			opt(o)
		}
	}
	// 触发式能力（4.4）：v0.2 门面即接受参数并校验，具体实现随场景触发落地
	if err := o.validateTriggered(); err != nil {
		return nil, err
	}
	svc, err := service.NewLibraryService(context.Background(), service.LibraryOptions{
		DataDir:        o.dataDir,
		StoreConn:      o.storeConn,
		ConfigFile:     o.configFile,
		AI:             o.ai,
		InlineConns:    o.inlineConns,
		Provider:       o.provider,
		Hooks:          o.hooks,
		Contributors:   o.contribs,
		QueryHooks:     o.queryHooks,
		DataPreparers:  o.preparers,
		TaskHooks:      o.taskHooks,
		ArtifactStore:  o.artifactStore,
		ArtifactBucket: o.artifactBucket,
	})
	if err != nil {
		return nil, err
	}
	return &Client{svc: svc, lang: o.lang}, nil
}

// Service 返回内部业务服务句柄（仅供同模块挂载层 dqexweb 使用；一般使用方请走公开能力方法）。
func (c *Client) Service() *service.Service {
	return c.svc
}

// Close 释放资源（幂等）：关闭 SQLite 存储（StoreSQLite）+ 释放进程级连接池。
// Close 之后再调用能力方法返回 ErrClientClosed。
func (c *Client) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()
	err := c.svc.Close()
	engine.CloseAllCliPool() // 进程级连接池释放（进程级语义，见 docs/library.md）
	return err
}

// StoreMode 返回当前持久化模式（StoreNone / StoreSQLite）。
func (c *Client) StoreMode() StoreMode { return c.svc.StoreMode() }

// ctx 返回注入了请求语言的 ctx（WithLang 决定所有 SvcError 消息的语言，3.3 i18n 内聚）
func (c *Client) ctx(ctx context.Context) context.Context {
	if c.lang == "" {
		return ctx
	}
	return service.WithLang(ctx, c.lang)
}

// ensureOpen 校验客户端未关闭
func (c *Client) ensureOpen() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return cygin.NewError(ErrClientClosed, cygin.WithErrDetailf("client is closed"))
	}
	return nil
}

// ---- 连接管理 ----
// 方法与 service.Service 一一对应（薄包装，不新增逻辑）：
//   - StoreSQLite：AddConnection / DeleteConnection 落盘持久化；
//   - StoreNone 库模式：允许调用且仅内存注册表生效（适合安装工具动态注册连接）；
//   - TestConnection / PingConnection 在库模式下接受注册 key 或完整连接信息语义由调用方选择。

// AddConnection 保存连接配置：rec.ID 非空为按主键更新，否则新建（自动生成 ID）。
// StoreNone 库模式下不落盘、仅内存注册表生效；连接配置变更会使元数据缓存失效。
func (c *Client) AddConnection(ctx context.Context, rec ConnRecord) (ConnRecord, error) {
	if err := c.ensureOpen(); err != nil {
		return rec, err
	}
	return c.svc.AddConnection(c.ctx(ctx), rec)
}

// ListConnections 列出所有连接（按名称排序，展示稳定）。
// 来源合并：内存注册表 + 持久层（StoreSQLite）+ ConnProvider.ListConns（外部持有）。
// 该方法为只读视图，Close 后仍可安全调用（返回当前内存快照）。
func (c *Client) ListConnections() []ConnInfo { return c.svc.ListConnections() }

// DeleteConnection 删除连接（按主键 ID，兼容名称/短名）。
// StoreNone 库模式下仅删除内存注册表连接；StoreSQLite 落盘删除并级联清理。
func (c *Client) DeleteConnection(key string) error {
	if err := c.ensureOpen(); err != nil {
		return err
	}
	return c.svc.DeleteConnection(key)
}

// TestConnection 测试连接可用性（拨号即断开，不进入连接池）。
func (c *Client) TestConnection(ctx context.Context, conn DBConnInfo) error {
	if err := c.ensureOpen(); err != nil {
		return err
	}
	return c.svc.TestConnection(c.ctx(ctx), conn)
}

// PingConnection 对已注册连接执行 SELECT 探活，返回往返耗时（毫秒）。
func (c *Client) PingConnection(ctx context.Context, connKey string) (int64, error) {
	if err := c.ensureOpen(); err != nil {
		return 0, err
	}
	return c.svc.PingConnection(c.ctx(ctx), connKey)
}

// ---- 异步任务 ----

// CancelTask 取消运行中的 dqex 任务（任务不存在或已结束返回错误，宿主自行决定
// 是否忽略）。库模式宿主经 TaskHooks 镜像任务时，取消应先经本方法转发到引擎，
// 再更新镜像状态（仅改镜像不取消引擎会导致引擎仍在写库而环境并发保护已放开）。
func (c *Client) CancelTask(taskID string) error {
	if err := c.ensureOpen(); err != nil {
		return err
	}
	return c.svc.CancelTask(taskID)
}
