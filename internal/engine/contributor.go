package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

// Contributor 业务对象贡献者（代理层扩展点）。
//
// 导出/导入的编排（任务目录、进度、zip 打包、清单）由 dqex 统一负责；
// 宿主业务对象（如流程/面板/规则/数据表）的"取数"与"回写"通过回调代理给宿主实现：
//   - Export：把 IDs 指定的业务对象写入 req.Dir（zip 任务目录下 <Type>/），
//     可输出任意格式（SQL/JSON/YAML），随后与库 SQL 一并打包；
//   - Import：从 req.Dir 读回业务对象（可为 nil，表示该类型仅支持导出）。
//
// 回调契约：Export/Import 运行在任务执行 goroutine 上，回调内阻塞会暂停进度推送；
// 取消统一由 req 内携带的 ctx 控制；回调内不得再回调 dqex 的 Client 方法。
type Contributor struct {
	// Type 类型标识，同时是 zip 包内目录名（如 flow/panel/rule/datatable），须为安全目录名
	Type string `json:"type" yaml:"type"`
	// Title 展示名（进度/日志用），空则回退 Type
	Title string `json:"title,omitempty" yaml:"title,omitempty"`
	// Export 导出回调：生成文件写入 req.Dir，返回清单（文件路径相对 req.Dir）；nil 视为非法注册
	Export func(ctx context.Context, req ContributorRequest) (*ContributorResult, error) `json:"-" yaml:"-"`
	// Import 导入回调：从 req.Dir 读回业务对象；nil 表示仅导出（导入包中该目录被跳过）
	Import func(ctx context.Context, req ContributorImportRequest) error `json:"-" yaml:"-"`
	// IDs 本次任务要导出的业务对象 ID（任务配置层携带；Client 注册的模板留空）
	IDs []string `json:"ids,omitempty" yaml:"ids,omitempty"`
}

// DataPreparer 数据前置处理器（代理层，按目标库名注册）：导入 .json 数据包应用前
// 回调宿主执行业务策略（如流程/表单版本合并），可直接修改包内容后返回。
// 返回值：新 ctx（可为 nil，引擎回退原 ctx；派生的值/取消能力传播到后续数据包应用流程）、
// 修改后的包（nil 表示沿用入参）、错误。
// 回调契约与 Contributor 一致：运行在任务 goroutine，取消由 ctx 控制。
type DataPreparer func(ctx context.Context, req DataPrepareRequest) (context.Context, *DataPackage, error)

// DataPrepareRequest 数据前置处理请求
type DataPrepareRequest struct {
	// Key 目标连接 connKey（宿主据此路由 env 级业务上下文）
	Key string
	// DB 目标库名（注册键相同）
	DB string
	// Package 待应用的数据包（宿主可原地修改）
	Package *DataPackage
}

// ContributorRequest 导出回调请求
type ContributorRequest struct {
	// Key 任务连接 connKey（如 env:prod/db:biz）：业务对象策略是环境级的，
	// 宿主据此路由到对应环境的业务配置（回调无 Key 时宿主无法区分多环境）
	Key string
	// Conn 业务对象所在库连接（即本次导出的源连接；宿主可自行建连或忽略）
	Conn *DBConnInfo
	// DB 业务库名（连接配置库；多库导出时为连接默认库，宿主自定义语义）
	DB string
	// IDs 要导出的业务对象 ID 列表（来自任务配置层）
	IDs []string
	// Dir 写入目录（任务目录下 <Type>/，已存在）
	Dir string
}

// ContributorResult 导出回调结果
type ContributorResult struct {
	// Files 生成的文件清单（相对 Dir）
	Files []string
	// Count 导出的业务对象数（进度/日志展示用）
	Count int
}

// ContributorImportRequest 导入回调请求
type ContributorImportRequest struct {
	// Key 任务目标连接 connKey（宿主据此路由到对应环境的业务配置）
	Key string
	// Conn 目标库连接（即本次导入的目标连接）
	Conn *DBConnInfo
	// DB 目标库名（连接配置库）
	DB string
	// Dir 业务对象目录（解压后 <Type>/）
	Dir string
}

// ctbText 贡献者文案（zh/en 双语，语言回退 zh）
func ctbText(lang, zh, en string) string {
	if lang == "en" {
		return en
	}
	return zh
}

// runContributors 在导出编排中依次调用贡献者回调（库 SQL 导出之后、打包之前）。
// 每个贡献者写入任务目录下 <Type>/ 子目录，与库 SQL 文件同级，随后一并 zip。
func runContributors(ctx context.Context, opts ExportOptions, baseDir string, t *tracker) error {
	for _, c := range opts.Contributors {
		if err := ctx.Err(); err != nil {
			return NewMsgErr(errCancelled)
		}
		if c.Export == nil {
			return NewMsgErrf(errCtbNoExport, nil, c.Type)
		}
		name := c.Type
		if c.Title != "" {
			name = c.Title
		}
		dir := filepath.Join(baseDir, sanitizeName(c.Type))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return NewMsgErrf(errCtbExport, err, c.Type)
		}
		t.p.CurrentTable = name
		t.emit(true)
		res, err := c.Export(ctx, ContributorRequest{
			Key:  opts.SourceConn,
			Conn: opts.Source,
			DB:   opts.Source.DBName,
			IDs:  c.IDs,
			Dir:  dir,
		})
		if err != nil {
			return NewMsgErrf(errCtbExport, err, c.Type)
		}
		t.p.DoneUnits++
		if res != nil {
			// Files 契约字段回显任务日志（宿主可据此核对产物清单）
			if len(res.Files) > 0 {
				t.log(ctbText(t.lang, "业务对象[%s]文件: %s", "contributor[%s] files: %s"), name, strings.Join(res.Files, ", "))
			}
			t.log(ctbText(t.lang, "业务对象[%s]导出完成：%d 项", "contributor[%s] export done: %d items"), name, res.Count)
		} else {
			t.log(ctbText(t.lang, "业务对象[%s]导出完成", "contributor[%s] export done"), name)
		}
	}
	return nil
}

// importContributors 在 zip 导入编排中回读贡献者目录（SQL 导入完成之后）。
// 包内存在 <Type>/ 目录且贡献者注册了 Import 回调时调用；目录不存在则跳过。
func importContributors(ctx context.Context, opts ImportOptions, tempDir string, t *tracker) error {
	if tempDir == "" || len(opts.Contributors) == 0 {
		return nil
	}
	for _, c := range opts.Contributors {
		if c.Import == nil {
			continue
		}
		if err := ctx.Err(); err != nil {
			return NewMsgErr(errCancelled)
		}
		dir := filepath.Join(tempDir, sanitizeName(c.Type))
		if st, err := os.Stat(dir); err != nil || !st.IsDir() {
			continue // 包内无该类型业务对象
		}
		if err := c.Import(ctx, ContributorImportRequest{
			Key:  opts.TargetConn,
			Conn: opts.Target,
			DB:   opts.Target.DBName,
			Dir:  dir,
		}); err != nil {
			return NewMsgErrf(errCtbImport, err, c.Type)
		}
		name := c.Type
		if c.Title != "" {
			name = c.Title
		}
		t.log(ctbText(t.lang, "业务对象[%s]导入完成", "contributor[%s] import done"), name)
	}
	return nil
}
