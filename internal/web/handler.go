package web

import (
	"fmt"
	"mime/multipart"
	"path/filepath"

	"dbimpex/internal/engine"
	"dbimpex/internal/service"

	"github.com/gin-gonic/gin"
	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cygin"
)

// StartResp 异步任务启动响应
type StartResp struct {
	TaskID string `json:"taskID"`
}

// ==================== 连接管理 ====================

// SaveConnReq 新建/更新连接请求（id 非空为按主键更新）
type SaveConnReq struct {
	ID   string             `json:"id"`
	Name string             `json:"name" binding:"required"`
	Conn service.DBConnInfo `json:"conn"`
}

func handleCreateConn(svc *service.Service) gin.HandlerFunc {
	return cygin.Handle(func(c *gin.Context, req SaveConnReq) (any, error) {
		rec, err := svc.AddConnection(service.ConnRecord{ID: req.ID, Name: req.Name, Conn: req.Conn})
		if err != nil {
			return nil, err
		}
		return gin.H{"id": rec.ID, "name": rec.Name}, nil
	})
}

func handleListConns(svc *service.Service) gin.HandlerFunc {
	return cygin.Handle(func(c *gin.Context, req struct{}) ([]service.ConnInfo, error) {
		return svc.ListConnections(), nil
	})
}

// DeleteConnReq 删除连接请求（主键 ID）
type DeleteConnReq struct {
	ID string `uri:"id" binding:"required"`
}

func handleDeleteConn(svc *service.Service) gin.HandlerFunc {
	return cygin.Handle(func(c *gin.Context, req DeleteConnReq) (any, error) {
		return gin.H{"ok": true}, svc.DeleteConnection(req.ID)
	})
}

// TestConnReq 测试连接请求（支持内联连接或已保存连接 ID）
type TestConnReq struct {
	ID   string             `json:"id"`
	Conn service.DBConnInfo `json:"conn"`
}

func handleTestConn(svc *service.Service) gin.HandlerFunc {
	return cygin.Handle(func(c *gin.Context, req TestConnReq) (any, error) {
		conn := req.Conn
		if conn.Type == "" && req.ID != "" {
			saved, ok := svc.Persist().GetConn(req.ID)
			if !ok {
				return nil, cygin.NewError(service.ErrConnNotFound, cygin.WithErrPrint(), cygin.WithErrDetailf("连接配置不存在: %s", req.ID))
			}
			conn = saved.Conn
		}
		if err := svc.TestConnection(conn); err != nil {
			return gin.H{"ok": false}, err
		}
		return gin.H{"ok": true}, nil
	})
}

// GetTablesReq 获取表列表请求（未配置库时返回所有库的树形结构）
type GetTablesReq struct {
	ID string `uri:"id" binding:"required"`
	DB string `query:"db"`
}

func handleGetTables(svc *service.Service) gin.HandlerFunc {
	return cygin.Handle(func(c *gin.Context, req GetTablesReq) (any, error) {
		tree, err := svc.GetTableTree(req.ID, req.DB)
		if err != nil {
			return nil, err
		}
		return gin.H{"databases": tree}, nil
	})
}

// GetTableColumnsReq 获取表列信息请求
type GetTableColumnsReq struct {
	ID    string `uri:"id" binding:"required"`
	DB    string `query:"db"`
	Table string `query:"table" binding:"required"`
}

func handleGetTableColumns(svc *service.Service) gin.HandlerFunc {
	return cygin.Handle(func(c *gin.Context, req GetTableColumnsReq) (any, error) {
		cols, err := svc.GetTableColumns(req.ID, req.DB, req.Table)
		if err != nil {
			return nil, err
		}
		return gin.H{"columns": cols}, nil
	})
}

// ==================== 导出 ====================

// ExportReq 导出请求
type ExportReq struct {
	Options      service.ExportOptions `json:"options"`
	Compress     *bool                 `json:"compress"` // 未指定时默认 true
	TaskConfigID string                `json:"taskConfigId"`
}

func handleExport(svc *service.Service) gin.HandlerFunc {
	return cygin.Handle(func(c *gin.Context, req ExportReq) (StartResp, error) {
		opts := req.Options
		if req.Compress != nil {
			opts.Compress = *req.Compress
		} else {
			opts.Compress = true
		}
		taskID, err := svc.StartExport(opts, req.TaskConfigID)
		if err != nil {
			return StartResp{}, err
		}
		return StartResp{TaskID: taskID}, nil
	})
}

// ==================== 导入 ====================

// ImportReq 导入请求
type ImportReq struct {
	Options      service.ImportOptions `json:"options"`
	Backup       *bool                 `json:"backup"` // 未指定时默认 true
	TaskConfigID string                `json:"taskConfigId"`
}

func handleImport(svc *service.Service) gin.HandlerFunc {
	return cygin.Handle(func(c *gin.Context, req ImportReq) (StartResp, error) {
		opts := req.Options
		if req.Backup != nil {
			opts.Backup = *req.Backup
		} else {
			opts.Backup = true
		}
		taskID, err := svc.StartImport(opts, req.TaskConfigID)
		if err != nil {
			return StartResp{}, err
		}
		return StartResp{TaskID: taskID}, nil
	})
}

// UploadReq 导入文件上传请求
type UploadReq struct {
	File *multipart.FileHeader `file:"file" binding:"required"`
}

func handleImportUpload(svc *service.Service) gin.HandlerFunc {
	return cygin.Handle(func(c *gin.Context, req UploadReq) (any, error) {
		name := filepath.Base(req.File.Filename)
		ext := filepath.Ext(name)
		if ext != ".sql" && ext != ".zip" {
			return nil, cygin.NewError(service.ErrFileType, cygin.WithErrPrint(), cygin.WithErrDetailf("仅支持 .sql 或 .zip 文件: %s", name))
		}
		dir := svc.Persist().UploadDir()
		saveTo := filepath.Join(dir, fmt.Sprintf("%d%s", nowMillis(), ext))
		if err := c.SaveUploadedFile(req.File, saveTo); err != nil {
			return nil, cygin.WrapError(err, cygin.ErrInternalServer, cygin.WithErrPrint(), cygin.WithErrDetailf("保存上传文件失败: %v", err))
		}
		// 同时返回原始文件名 name，供前端展示（隐藏服务器存储路径）
		info, err := engine.InspectImportFile(saveTo)
		if err != nil {
			return gin.H{"path": saveTo, "name": name}, nil
		}
		return gin.H{"path": saveTo, "name": name, "info": info}, nil
	})
}

// InspectReq 导入文件预览请求
type InspectReq struct {
	Path string `json:"path" binding:"required"`
}

func handleImportInspect(svc *service.Service) gin.HandlerFunc {
	return cygin.Handle(func(c *gin.Context, req InspectReq) (any, error) {
		return engine.InspectImportFile(req.Path)
	})
}

// ==================== 迁移 ====================

// MigrateReq 迁移请求
type MigrateReq struct {
	Options      service.MigrateOptions `json:"options"`
	Backup       *bool                  `json:"backup"` // 未指定时默认 true
	TaskConfigID string                 `json:"taskConfigId"`
}

func handleMigrate(svc *service.Service) gin.HandlerFunc {
	return cygin.Handle(func(c *gin.Context, req MigrateReq) (StartResp, error) {
		opts := req.Options
		if req.Backup != nil {
			opts.Backup = *req.Backup
		} else {
			opts.Backup = true
		}
		taskID, err := svc.StartMigrate(opts, req.TaskConfigID)
		if err != nil {
			return StartResp{}, err
		}
		return StartResp{TaskID: taskID}, nil
	})
}

// ==================== 对比 ====================

// CompareReq 对比请求
type CompareReq struct {
	Options      service.CompareOptions `json:"options"`
	TaskConfigID string                 `json:"taskConfigId"`
}

func handleCompare(svc *service.Service) gin.HandlerFunc {
	return cygin.Handle(func(c *gin.Context, req CompareReq) (StartResp, error) {
		taskID, err := svc.StartCompare(req.Options, req.TaskConfigID)
		if err != nil {
			return StartResp{}, err
		}
		return StartResp{TaskID: taskID}, nil
	})
}

// CompareResultReq 对比结果查询请求
type CompareResultReq struct {
	TaskID string `query:"taskID" binding:"required"`
}

func handleCompareResult(svc *service.Service) gin.HandlerFunc {
	return cygin.Handle(func(c *gin.Context, req CompareResultReq) (*service.CompareResult, error) {
		return svc.GetCompareResult(req.TaskID)
	})
}

// ==================== 任务配置 ====================

// ListTasksReq 任务列表请求
type ListTasksReq struct {
	Type string `query:"type"`
}

func handleListTasks(svc *service.Service) gin.HandlerFunc {
	return cygin.Handle(func(c *gin.Context, req ListTasksReq) ([]service.TaskConfig, error) {
		return svc.ListTasks(req.Type), nil
	})
}

func handleSaveTask(svc *service.Service) gin.HandlerFunc {
	return cygin.Handle(func(c *gin.Context, req service.TaskConfig) (any, error) {
		if err := svc.SaveTask(&req); err != nil {
			return nil, err
		}
		return req, nil
	})
}

// GetTaskReq 获取任务请求
type GetTaskReq struct {
	ID string `query:"id" binding:"required"`
}

func handleGetTask(svc *service.Service) gin.HandlerFunc {
	return cygin.Handle(func(c *gin.Context, req GetTaskReq) (service.TaskConfig, error) {
		return svc.GetTask(req.ID)
	})
}

func handleUpdateTask(svc *service.Service) gin.HandlerFunc {
	return cygin.Handle(func(c *gin.Context, req service.TaskConfig) (any, error) {
		if req.ID == "" {
			return nil, cygin.NewError(cygin.ErrParamsInvalid, cygin.WithErrPrint(), cygin.WithErrDetailf("缺少任务 ID"))
		}
		if err := svc.SaveTask(&req); err != nil {
			return nil, err
		}
		return req, nil
	})
}

func handleDeleteTask(svc *service.Service) gin.HandlerFunc {
	return cygin.Handle(func(c *gin.Context, req GetTaskReq) (any, error) {
		return gin.H{"ok": true}, svc.DeleteTask(req.ID)
	})
}

// GetLastTaskReq 获取最近使用任务请求
type GetLastTaskReq struct {
	Type string `uri:"type" binding:"required"`
}

func handleGetLastTask(svc *service.Service) gin.HandlerFunc {
	return cygin.Handle(func(c *gin.Context, req GetLastTaskReq) (any, error) {
		task := svc.GetLastTask(req.Type)
		if task == nil {
			return gin.H{"task": nil}, nil
		}
		return gin.H{"task": task}, nil
	})
}

// RunTaskReq 执行任务请求
type RunTaskReq struct {
	ID string `json:"id" binding:"required"`
}

func handleRunTask(svc *service.Service) gin.HandlerFunc {
	return cygin.Handle(func(c *gin.Context, req RunTaskReq) (StartResp, error) {
		taskID, err := svc.RunTaskByID(req.ID)
		if err != nil {
			return StartResp{}, err
		}
		return StartResp{TaskID: taskID}, nil
	})
}

// ==================== 执行历史 ====================

// ListHistoryReq 历史列表请求
type ListHistoryReq struct {
	Type         string `query:"type"`
	TaskConfigID string `query:"taskConfigId"`
}

func handleListHistory(svc *service.Service) gin.HandlerFunc {
	return cygin.Handle(func(c *gin.Context, req ListHistoryReq) ([]service.ExecutionRecord, error) {
		return svc.ListHistory(req.Type, req.TaskConfigID), nil
	})
}

// GetHistoryReq 获取历史请求
type GetHistoryReq struct {
	TaskID string `uri:"taskID" binding:"required"`
}

func handleGetHistory(svc *service.Service) gin.HandlerFunc {
	return cygin.Handle(func(c *gin.Context, req GetHistoryReq) (service.ExecutionRecord, error) {
		return svc.GetHistory(req.TaskID)
	})
}

// ==================== 元数据 ====================

func handleDBTypes() gin.HandlerFunc {
	return cygin.Handle(func(c *gin.Context, req struct{}) (any, error) {
		return gin.H{"types": service.SupportedDBTypes}, nil
	})
}
