// SQL 查询终端 API：查询 / 写操作 / 历史。
package web

import (
	"dqex/internal/engine"
	"dqex/internal/service"

	"github.com/gin-gonic/gin"
	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cygin"
)

// SQLRunReq SQL 执行请求（统一查询与写操作，支持多语句批量执行）
type SQLRunReq struct {
	ConnID string `json:"connId" binding:"required"`
	DB     string `json:"db"` // 目标库名（点对象树查表时传入，覆盖连接默认库）
	SQL    string `json:"sql" binding:"required"`
	Limit  int    `json:"limit"`  // 返回行数上限，<=0 走默认（1000），最大 1000
	Offset int    `json:"offset"` // 分页偏移
	Mask   bool   `json:"mask"`   // 结果集脱敏：敏感列（password/token/secret 等）统一打码
	Mode   string `json:"mode"`   // 执行模式：transform（默认，转换+限制）| raw（原始直传）
}

// handleSQLRun 统一执行 SQL（Navicat 式）：按分号分割多语句，逐条判断读写并执行，
// 返回结果集数组。写操作的安全确认由前端在发送前完成。
func handleSQLRun(svc *service.Service) gin.HandlerFunc {
	return cygin.Handle(func(c *gin.Context, req SQLRunReq) ([]*engine.SQLQueryResult, error) {
		mode := req.Mode
		if mode == "" {
			mode = "transform"
		}
		results, err := svc.RunSQLScript(c.Request.Context(), req.ConnID, req.DB, req.SQL, req.Limit, req.Offset, mode)
		if err != nil {
			return nil, renderErr(c, err)
		}
		if req.Mask {
			for _, r := range results {
				engine.MaskResult(r)
			}
		}
		return results, nil
	})
}

// TablePageReq 对象树数据浏览分页请求：一次返回当前页数据与全表总行数。
// 独立于 /query（查询终端），是系统自动生成的浏览查询，不写审计/历史。
type TablePageReq struct {
	ConnID         string                `json:"connId" binding:"required"`
	DB             string                `json:"db"` // 目标库名（点对象树查表时传入，覆盖连接默认库）
	Table          string                `json:"table" binding:"required"`
	Page           int                   `json:"page"`
	PageSize       int                   `json:"pageSize"`
	SortSpecs      []engine.SortSpec     `json:"sortSpecs"`      // 多列排序（按顺序叠加 ORDER BY）
	ExcludeColumns []string              `json:"excludeColumns"` // 省略的大字段列名（二进制/超长文本，列表不取真实值）
	Filters        []engine.ColumnFilter `json:"filters"`        // 列过滤条件（AND 叠加）
	MaxRows        int                   `json:"maxRows"`        // 导出时行数上限（仅 /table-export 使用，默认 100000）
}

// handleSQLTablePage 对象树数据浏览：分页查表数据 + 全表总行数一次返回。
func handleSQLTablePage(svc *service.Service) gin.HandlerFunc {
	return cygin.Handle(func(c *gin.Context, req TablePageReq) (*engine.TablePageResult, error) {
		return svc.QueryTablePage(c.Request.Context(), req.ConnID, req.DB, req.Table, req.Page, req.PageSize, req.SortSpecs, req.ExcludeColumns, req.Filters)
	})
}

// UpdateCellReq 表浏览单元格更新请求（named bind 更新）
type UpdateCellReq struct {
	ConnID    string   `json:"connId" binding:"required"`
	DB        string   `json:"db"`                           // 目标库名
	Table     string   `json:"table" binding:"required"`     // 表名
	Column    string   `json:"column" binding:"required"`    // 目标列名
	Value     any      `json:"value"`                        // 新值（null 表示 SET NULL）
	PKColumns []string `json:"pkColumns" binding:"required"` // 主键列名
	PKValues  []any    `json:"pkValues" binding:"required"`  // 主键值
}

// handleUpdateCell 更新表浏览中的单个单元格，返回影响行数。
func handleUpdateCell(svc *service.Service) gin.HandlerFunc {
	return cygin.Handle(func(c *gin.Context, req UpdateCellReq) (map[string]any, error) {
		affected, err := svc.UpdateTableCell(c.Request.Context(), req.ConnID, req.DB, engine.UpdateCellParams{
			Table:     req.Table,
			SetColumn: req.Column,
			SetValue:  req.Value,
			PKColumns: req.PKColumns,
			PKValues:  req.PKValues,
		})
		if err != nil {
			return nil, renderErr(c, err)
		}
		return map[string]any{"affectedRows": affected}, nil
	})
}

// DeleteRowsReq 整行删除请求（按主键定位，支持批量删除）
type DeleteRowsReq struct {
	ConnID    string   `json:"connId" binding:"required"`
	DB        string   `json:"db"`                           // 目标库名
	Table     string   `json:"table" binding:"required"`     // 表名
	PKColumns []string `json:"pkColumns" binding:"required"` // 主键列名
	Rows      [][]any  `json:"rows" binding:"required"`      // 每行主键值数组（与 PKColumns 顺序一致）
}

// handleDeleteRows 删除表浏览中选中的整行（按主键定位，支持批量），返回累计影响行数。
func handleDeleteRows(svc *service.Service) gin.HandlerFunc {
	return cygin.Handle(func(c *gin.Context, req DeleteRowsReq) (map[string]any, error) {
		affected, err := svc.DeleteTableRows(c.Request.Context(), req.ConnID, req.DB, req.Table, req.PKColumns, req.Rows)
		if err != nil {
			return nil, renderErr(c, err)
		}
		return map[string]any{"affectedRows": affected}, nil
	})
}

// InsertRowReq 新增行请求（用户显式填写的列与值）
type InsertRowReq struct {
	ConnID  string   `json:"connId" binding:"required"`
	DB      string   `json:"db"`                         // 目标库名
	Table   string   `json:"table" binding:"required"`   // 表名
	Columns []string `json:"columns" binding:"required"` // 写入列名（与 Values 顺序一致）
	Values  []any    `json:"values" binding:"required"`  // 对应列值（null 表示 NULL）
}

// handleInsertRow 表浏览视图新增一行（INSERT），返回影响行数。
func handleInsertRow(svc *service.Service) gin.HandlerFunc {
	return cygin.Handle(func(c *gin.Context, req InsertRowReq) (map[string]any, error) {
		affected, err := svc.InsertTableRow(c.Request.Context(), req.ConnID, req.DB, engine.InsertRowParams{
			Table:   req.Table,
			Columns: req.Columns,
			Values:  req.Values,
		})
		if err != nil {
			return nil, renderErr(c, err)
		}
		return map[string]any{"affectedRows": affected}, nil
	})
}

// CellValueReq 单个大字段单元格取值请求：按主键 + 列名定位单行单列，返回完整值。
// 用于列表省略的大字段（二进制/超长文本）点击查看，避免列表一次传输大量数据。
type CellValueReq struct {
	ConnID    string   `json:"connId" binding:"required"`
	DB        string   `json:"db"`                           // 目标库名
	Table     string   `json:"table" binding:"required"`     // 表名
	Column    string   `json:"column" binding:"required"`    // 目标列名
	PKColumns []string `json:"pkColumns" binding:"required"` // 主键列名
	PKValues  []any    `json:"pkValues" binding:"required"`  // 主键值
}

// handleCellValue 返回单行单列的完整值（大字段懒加载）。
func handleCellValue(svc *service.Service) gin.HandlerFunc {
	return cygin.Handle(func(c *gin.Context, req CellValueReq) (map[string]any, error) {
		val, err := svc.GetCellValue(c.Request.Context(), req.ConnID, req.DB, req.Table, req.Column, req.PKColumns, req.PKValues)
		if err != nil {
			return nil, renderErr(c, err)
		}
		return map[string]any{"value": val}, nil
	})
}

// SQLHistoryReq 历史记录请求
type SQLHistoryReq struct {
	ConnID string `query:"connId" binding:"required"`
}

func handleSQLHistory(svc *service.Service) gin.HandlerFunc {
	return cygin.Handle(func(c *gin.Context, req SQLHistoryReq) ([]service.SQLHistoryItem, error) {
		return svc.SQLHistory(req.ConnID), nil
	})
}

func handleClearSQLHistory(svc *service.Service) gin.HandlerFunc {
	return cygin.Handle(func(c *gin.Context, req SQLHistoryReq) (any, error) {
		svc.ClearSQLHistory(req.ConnID)
		return gin.H{"ok": true}, nil
	})
}

// FavoriteReq 收藏列表请求（全局共享，无需连接过滤；保留 ConnID 仅作来源标记上下文）
type FavoriteReq struct {
	ConnID string `query:"connId"`
}

// FavoriteAddReq 新增收藏请求
type FavoriteAddReq struct {
	ConnID string `json:"connId" binding:"required"`
	Title  string `json:"title"`
	DB     string `json:"db"`
	Mode   string `json:"mode"`
	SQL    string `json:"sql" binding:"required"`
}

// FavoriteRenameReq 重命名收藏请求
type FavoriteRenameReq struct {
	ID    string `json:"id" binding:"required"`
	Title string `json:"title" binding:"required"`
}

func handleListFavorites(svc *service.Service) gin.HandlerFunc {
	return cygin.Handle(func(c *gin.Context, req FavoriteReq) ([]*service.SQLFavorite, error) {
		return svc.ListFavorites(), nil
	})
}

func handleAddFavorite(svc *service.Service) gin.HandlerFunc {
	return cygin.Handle(func(c *gin.Context, req FavoriteAddReq) (any, error) {
		f := &service.SQLFavorite{
			ConnID: req.ConnID, // 仅作来源标记，便于跨连接回填时提示
			Title:  req.Title,
			DB:     req.DB,
			Mode:   req.Mode,
			SQL:    req.SQL,
		}
		if err := svc.AddFavorite(f); err != nil {
			return nil, renderErr(c, err)
		}
		return gin.H{"ok": true, "id": f.ID}, nil
	})
}

func handleDeleteFavorite(svc *service.Service) gin.HandlerFunc {
	return cygin.Handle(func(c *gin.Context, req FavoriteReq) (any, error) {
		id := c.Query("id")
		if id == "" {
			return nil, cygin.NewError(cygin.ErrParamsInvalid)
		}
		if err := svc.DeleteFavorite(id); err != nil {
			return nil, renderErr(c, err)
		}
		return gin.H{"ok": true}, nil
	})
}

func handleRenameFavorite(svc *service.Service) gin.HandlerFunc {
	return cygin.Handle(func(c *gin.Context, req FavoriteRenameReq) (any, error) {
		if err := svc.RenameFavorite(req.ID, req.Title); err != nil {
			return nil, renderErr(c, err)
		}
		return gin.H{"ok": true}, nil
	})
}

// SQLAuditReq 审计日志查询请求（分页，按连接过滤）
type SQLAuditReq struct {
	ConnID string `query:"connId"` // 空 = 全部连接
	Limit  int    `query:"limit"`  // <=0 默认 100，上限 500
	Offset int    `query:"offset"`
}

func handleSQLAudit(svc *service.Service) gin.HandlerFunc {
	return cygin.Handle(func(c *gin.Context, req SQLAuditReq) ([]service.SQLAuditEntry, error) {
		return svc.SQLAudit(req.ConnID, req.Limit, req.Offset)
	})
}

// SQLPingReq 连接健康检测请求
type SQLPingReq struct {
	ConnID string `json:"connId" binding:"required"`
}

// PingResult 连接健康检测结果
type PingResult struct {
	OK        bool   `json:"ok"`
	ElapsedMS int64  `json:"elapsedMs"`
	Error     string `json:"error,omitempty"`
}

func handleSQLPing(svc *service.Service) gin.HandlerFunc {
	return cygin.Handle(func(c *gin.Context, req SQLPingReq) (PingResult, error) {
		elapsed, err := svc.PingConnection(c.Request.Context(), req.ConnID)
		if err != nil {
			return PingResult{OK: false, Error: err.Error()}, nil
		}
		return PingResult{OK: true, ElapsedMS: elapsed}, nil
	})
}

// SQLGenReq 快速生成 SQL 请求（表浏览右键生成，字段平铺自 GenSQLParams）
type SQLGenReq struct {
	ConnID string `json:"connId" binding:"required"`
	DB     string `json:"db"` // 目标库名（点对象树查表时传入，覆盖连接默认库）
	engine.GenSQLParams
}

// handleSQLGen 快速生成 SQL 文本：行/单元格/过滤条件 → 方言正确的可执行语句，
// 供表浏览右键菜单生成预览（生成不执行，不写审计）。
func handleSQLGen(svc *service.Service) gin.HandlerFunc {
	return cygin.Handle(func(c *gin.Context, req SQLGenReq) (map[string]string, error) {
		sql, err := svc.GenerateSQL(c.Request.Context(), req.ConnID, req.DB, req.GenSQLParams)
		if err != nil {
			return nil, renderErr(c, err)
		}
		return map[string]string{"sql": sql}, nil
	})
}

// SQLDDLReq 对象创建语句查询请求
type SQLDDLReq struct {
	ConnID string `query:"connId" binding:"required"`
	DB     string `query:"db"`                      // 库名（Oracle 下为 schema）
	Type   string `query:"type" binding:"required"` // table / view / function / procedure
	Name   string `query:"name" binding:"required"`
}

func handleSQLDDL(svc *service.Service) gin.HandlerFunc {
	return cygin.Handle(func(c *gin.Context, req SQLDDLReq) (*service.ObjectDDLResult, error) {
		return svc.GetObjectDDL(c.Request.Context(), req.ConnID, req.DB, req.Type, req.Name)
	})
}

// WorkspaceGetReq 查询工作区读取请求（connId 走 query 参数）
type WorkspaceGetReq struct {
	ConnID string `query:"connId" binding:"required"`
}

// handleGetWorkspace 读取某连接的查询工作区。
func handleGetWorkspace(svc *service.Service) gin.HandlerFunc {
	return cygin.Handle(func(c *gin.Context, req WorkspaceGetReq) (service.WorkspaceState, error) {
		state, _ := svc.LoadWorkspace(req.ConnID)
		if state.Tabs == nil {
			state.Tabs = []service.WorkspaceTab{}
		}
		return state, nil
	})
}

// WorkspaceSaveReq 查询工作区保存请求（整体覆盖）
type WorkspaceSaveReq struct {
	ConnID   string                 `json:"connId" binding:"required"`
	Tabs     []service.WorkspaceTab `json:"tabs" binding:"required"`
	ActiveID string                 `json:"activeId"`
}

// handleSaveWorkspace 保存某连接的查询工作区（整体覆盖）。
func handleSaveWorkspace(svc *service.Service) gin.HandlerFunc {
	return cygin.Handle(func(c *gin.Context, req WorkspaceSaveReq) (map[string]any, error) {
		state := service.WorkspaceState{Tabs: req.Tabs, ActiveID: req.ActiveID}
		if err := svc.SaveWorkspace(req.ConnID, state); err != nil {
			return nil, renderErr(c, err)
		}
		return map[string]any{"ok": true}, nil
	})
}
