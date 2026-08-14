// SQL 查询终端 API：查询 / 写操作 / 历史。
package web

import (
	"dbimpex/internal/engine"
	"dbimpex/internal/service"

	"github.com/gin-gonic/gin"
	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cygin"
)

// SQLRunReq SQL 执行请求（统一查询与写操作，支持多语句批量执行）
type SQLRunReq struct {
	ConnID string `json:"connId" binding:"required"`
	DB     string `json:"db"`    // 目标库名（点对象树查表时传入，覆盖连接默认库）
	SQL    string `json:"sql" binding:"required"`
	Limit  int    `json:"limit"` // 返回行数上限，<=0 走默认（1000），最大 1000
	Offset int    `json:"offset"` // 分页偏移
	Mask   bool   `json:"mask"`  // 结果集脱敏：敏感列（password/token/secret 等）统一打码
	Mode   string `json:"mode"`  // 执行模式：transform（默认，转换+限制）| raw（原始直传）
	// RecordHistory 是否写入「SQL 执行历史」；nil=默认 true（手动执行）。
	// 对象树点开自动生成的查询显式传 false，避免污染历史。
	RecordHistory *bool `json:"recordHistory"`
}

// handleSQLRun 统一执行 SQL（Navicat 式）：按分号分割多语句，逐条判断读写并执行，
// 返回结果集数组。写操作的安全确认由前端在发送前完成。
func handleSQLRun(svc *service.Service) gin.HandlerFunc {
	return cygin.Handle(func(c *gin.Context, req SQLRunReq) ([]*engine.SQLQueryResult, error) {
		mode := req.Mode
		if mode == "" {
			mode = "transform"
		}
		recordHistory := req.RecordHistory == nil || *req.RecordHistory
		results, err := svc.RunSQLScript(c.Request.Context(), req.ConnID, req.DB, req.SQL, req.Limit, req.Offset, mode, recordHistory)
		if err != nil {
			return nil, err
		}
		if req.Mask {
			for _, r := range results {
				engine.MaskResult(r)
			}
		}
		return results, nil
	})
}

// SQLQueryReq 查询类 SQL 执行请求（保留兼容，内部委托 RunSQLScript）
type SQLQueryReq struct {
	ConnID string `json:"connId" binding:"required"`
	DB     string `json:"db"`
	SQL    string `json:"sql" binding:"required"`
	Limit  int    `json:"limit"`
	Offset int    `json:"offset"`
	Mask   bool   `json:"mask"`
	Mode   string `json:"mode"`
	// RecordHistory 是否写入「SQL 执行历史」；nil=默认 true。
	RecordHistory *bool `json:"recordHistory"`
}

func handleSQLQuery(svc *service.Service) gin.HandlerFunc {
	return cygin.Handle(func(c *gin.Context, req SQLQueryReq) (*engine.SQLQueryResult, error) {
		mode := req.Mode
		if mode == "" {
			mode = "transform"
		}
		recordHistory := req.RecordHistory == nil || *req.RecordHistory
		results, err := svc.RunSQLScript(c.Request.Context(), req.ConnID, req.DB, req.SQL, req.Limit, req.Offset, mode, recordHistory)
		if err != nil {
			return nil, err
		}
		if len(results) == 0 {
			return &engine.SQLQueryResult{SQL: req.SQL}, nil
		}
		r := results[0]
		if req.Mask {
			engine.MaskResult(r)
		}
		return r, nil
	})
}

// SQLExecReq 写操作 SQL 执行请求（保留兼容）
type SQLExecReq struct {
	ConnID string `json:"connId" binding:"required"`
	DB     string `json:"db"`
	SQL    string `json:"sql" binding:"required"`
}

func handleSQLExec(svc *service.Service) gin.HandlerFunc {
	return cygin.Handle(func(c *gin.Context, req SQLExecReq) (*engine.SQLQueryResult, error) {
		results, err := svc.RunSQLScript(c.Request.Context(), req.ConnID, req.DB, req.SQL, 0, 0, "raw", true)
		if err != nil {
			return nil, err
		}
		if len(results) == 0 {
			return &engine.SQLQueryResult{SQL: req.SQL, IsWrite: true}, nil
		}
		return results[0], nil
	})
}

// UpdateCellReq 表浏览单元格更新请求（named bind 更新）
type UpdateCellReq struct {
	ConnID    string   `json:"connId" binding:"required"`
	DB        string   `json:"db"`                       // 目标库名
	Table     string   `json:"table" binding:"required"` // 表名
	Column    string   `json:"column" binding:"required"` // 目标列名
	Value     any      `json:"value"`                    // 新值（null 表示 SET NULL）
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
			return nil, err
		}
		return map[string]any{"affectedRows": affected}, nil
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
	OK        bool  `json:"ok"`
	ElapsedMS int64 `json:"elapsedMs"`
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

// SQLDDLReq 对象创建语句查询请求
type SQLDDLReq struct {
	ConnID  string `query:"connId" binding:"required"`
	DB      string `query:"db"`                     // 库名（Oracle 下为 schema）
	Type    string `query:"type" binding:"required"` // table / view / function / procedure
	Name    string `query:"name" binding:"required"`
}

func handleSQLDDL(svc *service.Service) gin.HandlerFunc {
	return cygin.Handle(func(c *gin.Context, req SQLDDLReq) (*service.ObjectDDLResult, error) {
		return svc.GetObjectDDL(req.ConnID, req.DB, req.Type, req.Name)
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
	ConnID string                 `json:"connId" binding:"required"`
	Tabs   []service.WorkspaceTab `json:"tabs" binding:"required"`
	ActiveID string               `json:"activeId"`
}

// handleSaveWorkspace 保存某连接的查询工作区（整体覆盖）。
func handleSaveWorkspace(svc *service.Service) gin.HandlerFunc {
	return cygin.Handle(func(c *gin.Context, req WorkspaceSaveReq) (map[string]any, error) {
		state := service.WorkspaceState{Tabs: req.Tabs, ActiveID: req.ActiveID}
		if err := svc.SaveWorkspace(req.ConnID, state); err != nil {
			return nil, err
		}
		return map[string]any{"ok": true}, nil
	})
}
