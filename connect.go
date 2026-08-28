package dqex

import (
	"context"
)

// ---- 连接管理（docs/library-api-design.md 3.2 connect.go） ----
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
