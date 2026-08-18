package web

import (
	"net/http"
	"os/exec"
	"path/filepath"
	"runtime"

	"dbimpex/internal/service"

	"github.com/gin-gonic/gin"
	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cygin"
)

// rawRoutes 原生 gin 路由：SSE 进度推送、取消任务、下载、打开目录
// （这些端点需要直接控制响应流，不走 cygin.Handle 统一包装）
func rawRoutes(svc *service.Service) func(*gin.RouterGroup) {
	return func(g *gin.RouterGroup) {
		g.GET("/progress/:taskID", func(c *gin.Context) { streamProgress(c, svc) })
		g.POST("/cancel/:taskID", func(c *gin.Context) {
			taskID := c.Param("taskID")
			if err := svc.CancelTask(taskID); err != nil {
				cygin.ResponseError(c, err)
				return
			}
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "success": true})
		})
		g.GET("/export/download/:taskID", func(c *gin.Context) { downloadExport(c, svc) })
		g.POST("/export/open-dir/:taskID", func(c *gin.Context) { openExportDir(c, svc) })
		g.POST("/history/del/:taskID", func(c *gin.Context) {
			if err := svc.DeleteHistory(c.Param("taskID")); err != nil {
				cygin.ResponseError(c, err)
				return
			}
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "success": true})
		})
		g.POST("/sql/table-export", func(c *gin.Context) { exportTableExcel(c, svc) })
		g.POST("/ai/chat/stream", func(c *gin.Context) { handleAIChatStream(c, svc) })
	}
}

// streamProgress SSE 推送任务进度
func streamProgress(c *gin.Context, svc *service.Service) {
	taskID := c.Param("taskID")
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	ch, release, err := svc.ProgressCh(taskID)
	if err != nil {
		// 任务已结束或不存在：从执行历史返回终态
		if rec, herr := svc.GetHistory(taskID); herr == nil {
			p := progressFromRecord(rec)
			c.SSEvent("progress", p)
			c.SSEvent("done", p)
		} else {
			c.SSEvent("error", gin.H{"message": err.Error()})
		}
		c.Writer.Flush()
		return
	}
	defer release()

	clientGone := c.Request.Context().Done()
	for {
		select {
		case <-clientGone:
			return
		case p, ok := <-ch:
			if !ok {
				return
			}
			c.SSEvent("progress", p)
			if isTerminalState(p.State) {
				c.SSEvent("done", p)
				c.Writer.Flush()
				return
			}
			c.Writer.Flush()
		}
	}
}

func isTerminalState(state string) bool {
	return state == "done" || state == "error" || state == "cancelled"
}

// progressFromRecord 由执行历史构造终态进度：与实时推送保持同构（真实完成单元数/百分比/日志/耗时）
func progressFromRecord(rec service.ExecutionRecord) service.ProgressInfo {
	msg := ""
	if rec.ErrorMsg != "" {
		msg = rec.ErrorMsg
	} else if rec.Summary != "" {
		msg = rec.Summary
	}
	// 百分比口径与 tracker.calcPercent 一致：成功 100%，失败/取消按实际完成单元数还原
	percent := 0.0
	if rec.Status == "done" {
		percent = 100
	} else if rec.TotalUnits > 0 {
		percent = float64(rec.DoneUnits) / float64(rec.TotalUnits) * 100
	}
	return service.ProgressInfo{
		State:      rec.Status,
		TaskID:     rec.ID,
		TotalUnits: rec.TotalUnits,
		DoneUnits:  rec.DoneUnits,
		DoneRows:   rec.TotalRows,
		Percent:    percent,
		Message:    msg,
		OutputPath: rec.OutputPath,
		DurationMs: rec.Duration,
		Logs:       rec.Logs,
	}
}

// downloadExport 下载导出产物
func downloadExport(c *gin.Context, svc *service.Service) {
	rec, err := svc.GetHistory(c.Param("taskID"))
	if err != nil {
		cygin.ResponseError(c, cygin.WrapError(err, service.ErrTaskNotFound, cygin.WithStatus(http.StatusNotFound)))
		return
	}
	if rec.OutputPath == "" {
		cygin.ResponseError(c, cygin.NewError(service.ErrNoArtifact, cygin.WithErrPrint(),
			cygin.WithStatus(http.StatusNotFound), cygin.WithErrDetailf("任务没有可下载的产物: %s", rec.ID)))
		return
	}
	c.FileAttachment(rec.OutputPath, filepath.Base(rec.OutputPath))
}

// openExportDir 在文件管理器中打开导出产物所在目录
func openExportDir(c *gin.Context, svc *service.Service) {
	rec, err := svc.GetHistory(c.Param("taskID"))
	if err != nil {
		cygin.ResponseError(c, err)
		return
	}
	if rec.OutputPath == "" {
		cygin.ResponseError(c, cygin.NewError(service.ErrNoArtifact, cygin.WithErrPrint(), cygin.WithErrDetailf("任务没有产物路径: %s", rec.ID)))
		return
	}
	dir := filepath.Dir(rec.OutputPath)
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", dir)
	case "windows":
		cmd = exec.Command("explorer", dir)
	default:
		cmd = exec.Command("xdg-open", dir)
	}
	if err := cmd.Start(); err != nil {
		cygin.ResponseError(c, cygin.WrapError(err, service.ErrOpenDirFailed, cygin.WithErrPrint(), cygin.WithErrDetails(err.Error())))
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "success": true})
}

// exportTableExcel 表数据导出 Excel：解析 TablePageReq，内存生成 xlsx 直接返回文件流。
// 复用 service.ExportTableExcel（内部复用 engine.QueryTablePage + excelize），不落盘。
func exportTableExcel(c *gin.Context, svc *service.Service) {
	var req TablePageReq
	if err := c.ShouldBindJSON(&req); err != nil {
		cygin.ResponseError(c, cygin.WrapError(err, cygin.ErrParamsInvalid, cygin.WithStatus(http.StatusBadRequest)))
		return
	}
	maxRows := req.MaxRows
	data, total, truncated, err := svc.ExportTableExcel(c.Request.Context(), req.ConnID, req.DB, req.Table, req.SortSpecs, req.Filters, maxRows)
	if err != nil {
		cygin.ResponseError(c, err)
		return
	}
	filename := req.Table
	if truncated {
		filename = req.Table + "-truncated"
	}
	_ = total
	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Header("Content-Disposition", `attachment; filename="`+filename+`.xlsx"`)
	c.Header("Content-Transfer-Encoding", "binary")
	c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", data)
}
