package cli

// 点导入：CLI 层大量复用 service 包的模型别名与入口（NewService/选项模型/错误码）
import (
	"context"
	"dbimpex/internal/engine"
	. "dbimpex/internal/service"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cygin"
)

var compareSrc, compareTarget connFlags

var compareCmd = &cobra.Command{
	Use:     "compare",
	Aliases: []string{"cmp"},
	Short:   "对比两个数据库的结构与数据差异",
	Long: `对比两个数据库的结构与数据差异。

独立闭环用法：
  dbx compare --gen-config > compare.yaml   # 生成配置模板
  vi compare.yaml                               # 填写连接与选项
  dbx compare --config compare.yaml         # 执行对比

命令行参数优先于配置文件；也可完全通过 flags 指定连接（--source-* / --target-*）。
连接 flag 提供 mysqldump 风格别名：--source-user/--source-password/--source-database（及 target- 同名系列）`,
	RunE: cliCompare,
}

func init() {
	f := compareCmd.Flags()
	f.String("config", "", "配置文件(yaml)，dbx compare --gen-config 可生成模板")
	f.Bool("gen-config", false, "输出配置文件模板到标准输出")
	f.String("task", "", "已保存任务配置 ID（Web 保存的任务）")
	registerConnFlags(compareCmd, "source", &compareSrc)
	registerConnFlags(compareCmd, "target", &compareTarget)
	f.StringP("source-conn", "s", "", "使用已保存源连接（ID 或名称）")
	f.StringP("target-conn", "t", "", "使用已保存目标连接（ID 或名称）")
	_ = compareCmd.RegisterFlagCompletionFunc("task", completeTaskIDs("compare"))
	_ = compareCmd.RegisterFlagCompletionFunc("source-conn", completeConnNames)
	_ = compareCmd.RegisterFlagCompletionFunc("target-conn", completeConnNames)
	f.StringP("tables", "T", "", "指定对比的表，逗号分隔（默认库内全部表）")
	f.StringArray("alias", nil, "表别名配对，格式 源表=目标表（可重复）")
	f.String("scope", "", "对比范围: both|structure|data")
	_ = compareCmd.RegisterFlagCompletionFunc("scope", fixedCompletion("both", "structure", "data"))
	f.Bool("structure-only", false, "仅对比结构（等价 --scope structure）")
	f.Bool("data-only", false, "仅对比数据（等价 --scope data）")
	f.Int("threshold", 0, "数据逐行比较阈值（默认 1000，超过仅比行数）")
	f.String("ignore-columns", "", "数据内容对比忽略的列，逗号分隔（如 created_at,updated_at）")
	f.Bool("force-data", false, "表结构不一致时仍强制对比数据（默认跳过数据对比）")
	f.String("output", "", "对比报告 JSON 额外保存路径（除历史记录自带的报告外）")
	f.Bool("all", false, "输出全部表（默认仅输出有差异的表）")
	registerConnAliases(compareCmd, "source-", "source-", &compareSrc)
	registerConnAliases(compareCmd, "target-", "target-", &compareTarget)
	compareShowCmd.Flags().StringP("id", "i", "", "对比记录 ID（compare 执行后输出，或 history list --type compare 查看）")
	compareShowCmd.Flags().StringP("table", "t", "", "仅显示指定表的差异明细")
	_ = compareShowCmd.RegisterFlagCompletionFunc("id", completeHistoryIDs("compare"))
	compareCmd.AddCommand(compareShowCmd)
	rootCmd.AddCommand(compareCmd)
}

var compareShowCmd = &cobra.Command{
	Use:   "show --id <记录ID> [表名]",
	Short: "回看历史对比记录的差异明细",
	Long: `回看历史对比记录的差异明细。

dbx cmp show -i <记录ID>                # 全部表明细
dbx cmp show -i <记录ID> t_config       # 单表差异明细（位置参数或 --table/-t 均可，列级对照 + 差异行样例）`,
	RunE: cliCompareShow,
}

func cliCompare(cmd *cobra.Command, args []string) error {
	f := cmd.Flags()
	if v, _ := f.GetBool("gen-config"); v {
		printConfigTemplate("compare")
		return nil
	}

	cfg := &compareConfig{Scope: "both"}
	if v, _ := f.GetString("config"); v != "" {
		var err error
		cfg, err = loadCompareConfig(v)
		if err != nil {
			return err
		}
	}
	opts, output, err := buildCompareOpts(cmd, cfg)
	if err != nil {
		return err
	}

	svc, err := newCliService()
	if err != nil {
		return err
	}
	// 库名覆盖：引用连接解析为内联并写入指定库（引擎要求连接带库）；
	// 命令行 --source-db/--source-database 优先于配置文件 source_database
	srcDB, tgtDB := compareSrc.db, compareTarget.db
	if srcDB == "" {
		srcDB = cfg.SourceDB
	}
	if tgtDB == "" {
		tgtDB = cfg.TargetDB
	}
	if srcDB != "" && opts.SourceConn != "" && opts.Source == nil {
		conn, oerr := overrideConnDB(svc, opts.SourceConn, srcDB)
		if oerr != nil {
			return oerr
		}
		opts.Source, opts.SourceConn = conn, ""
	}
	if tgtDB != "" && opts.TargetConn != "" && opts.Target == nil {
		conn, oerr := overrideConnDB(svc, opts.TargetConn, tgtDB)
		if oerr != nil {
			return oerr
		}
		opts.Target, opts.TargetConn = conn, ""
	}
	// 多库对比：配置文件 databases / db_map（与单库 source_database/target_database 二选一）
	if len(cfg.Databases) > 0 {
		opts.Databases = cfg.Databases
	}
	if len(cfg.DBMap) > 0 {
		opts.DBMapping = cfg.DBMap
	}
	cb, _ := cliProgress()
	fmt.Println("开始对比...")
	taskID, _ := f.GetString("task")
	recordID, result, err := svc.RunCompareRecorded(context.Background(), opts, cb, taskID)
	if err != nil {
		return err
	}

	printCompareReport(result, f.Changed("all"))
	printCompareShowHint(recordID)

	if output != "" {
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(output, data, 0o644); err != nil {
			return cygin.WrapError(err, cygin.ErrInternalServer, cygin.WithErrPrint(), cygin.WithErrDetails(err.Error()))
		}
		fmt.Printf("报告已保存: %s\n", output)
	}
	return nil
}

// buildCompareOpts 合并配置与命令行参数（命令行仅 Changed 的 flag 生效，优先级最高）
func buildCompareOpts(cmd *cobra.Command, cfg *compareConfig) (CompareOptions, string, error) {
	f := cmd.Flags()
	opts, output, err := compareOptsFromConfig(cfg, false)
	if err != nil {
		return opts, output, err
	}

	// 连接：命令行内联/引用 覆盖配置
	if conn := compareSrc.toConn(); conn != nil {
		opts.Source = conn
	}
	if v, _ := f.GetString("source-conn"); v != "" {
		opts.SourceConn = v
	}
	if conn := compareTarget.toConn(); conn != nil {
		opts.Target = conn
	}
	if v, _ := f.GetString("target-conn"); v != "" {
		opts.TargetConn = v
	}

	if f.Changed("tables") {
		v, _ := f.GetString("tables")
		opts.Tables = splitCSV(v)
	}
	if f.Changed("alias") {
		items, _ := f.GetStringArray("alias")
		opts.Aliases = nil // 命令行显式给出时完全覆盖
		for _, item := range items {
			parts := strings.SplitN(item, "=", 2)
			if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
				return opts, "", cygin.NewError(cygin.ErrParamsInvalid, cygin.WithErrPrint(), cygin.WithErrDetailf("别名格式应为 源表=目标表: %s", item))
			}
			opts.Aliases = append(opts.Aliases, TableAlias{Source: strings.TrimSpace(parts[0]), Target: strings.TrimSpace(parts[1])})
		}
	}

	// 对比范围：--scope 统一入口，兼容 --structure-only/--data-only
	if f.Changed("scope") || f.Changed("structure-only") || f.Changed("data-only") {
		scope, _ := f.GetString("scope")
		if f.Changed("structure-only") && mustGetBool(f, "structure-only") {
			scope = "structure"
		}
		if f.Changed("data-only") && mustGetBool(f, "data-only") {
			scope = "data"
		}
		opts.StructureOnly, opts.DataOnly = false, false
		switch strings.ToLower(scope) {
		case "", "both":
		case "structure":
			opts.StructureOnly = true
		case "data":
			opts.DataOnly = true
		default:
			return opts, "", cygin.NewError(cygin.ErrParamsInvalid, cygin.WithErrPrint(), cygin.WithErrDetailf("无效的对比范围: %s（可选 both/structure/data）", scope))
		}
	}

	if f.Changed("threshold") {
		v, _ := f.GetInt("threshold")
		if v < 0 {
			return opts, "", cygin.NewError(cygin.ErrParamsInvalid, cygin.WithErrPrint(), cygin.WithErrDetailf("threshold 不能为负数"))
		}
		opts.Threshold = v
	}
	if f.Changed("ignore-columns") {
		v, _ := f.GetString("ignore-columns")
		opts.IgnoreColumns = splitCSV(v)
	}
	if f.Changed("force-data") {
		opts.ForceData, _ = f.GetBool("force-data")
	}
	if f.Changed("output") {
		output, _ = f.GetString("output")
	}
	if opts.Source == nil && opts.SourceConn == "" {
		return opts, "", cygin.NewError(cygin.ErrParamsInvalid, cygin.WithErrPrint(), cygin.WithErrDetailf("缺少源连接：配置 source/source_ref 段或 --source-*/--source-conn"))
	}
	if opts.Target == nil && opts.TargetConn == "" {
		return opts, "", cygin.NewError(cygin.ErrParamsInvalid, cygin.WithErrPrint(), cygin.WithErrDetailf("缺少目标连接：配置 target/target_ref 段或 --target-*/--target-conn"))
	}
	return opts, output, nil
}

func mustGetBool(f interface{ GetBool(string) (bool, error) }, name string) bool {
	v, _ := f.GetBool(name)
	return v
}

// printCompareReport 终端输出对比报告：汇总行 + 差异表明细（--all 时输出全部）
func printCompareReport(result *CompareResult, showAll bool) {
	s := result.Summary
	fmt.Println()
	fmt.Printf("%s\n", bold(fmt.Sprintf("对比结果: %s ↔ %s", result.Source, result.Target)))
	fmt.Printf("共 %d 项 | %s %d | %s %d | %s %d | %s %d | %s %d\n",
		s.Total,
		"一致", s.Matched,
		yellow("仅源有"), s.SourceOnly,
		yellow("仅目标有"), s.TargetOnly,
		yellow("结构差异"), s.StructureDiff,
		red("数据差异"), s.DataDiff)

	fmt.Println()
	tables := result.Tables
	if len(result.Databases) > 0 {
		fmt.Printf("%s\n", bold(fmt.Sprintf("%d 个库参与对比", len(result.Databases))))
		tables = nil
		for _, db := range result.Databases {
			fmt.Printf("\n%s %s ↔ %s（共%d项, 一致%d, 结构差异%d, 数据差异%d）\n",
				bold("库"), bold(db.SourceDB), bold(db.TargetDB),
				db.Summary.Total, db.Summary.Matched, db.Summary.StructureDiff, db.Summary.DataDiff)
			tables = append(tables, db.Tables...)
		}
	}
	fmt.Printf("%s  %s  %s\n", padCell("表", 32), padCell("状态", 8), "差异说明")
	fmt.Printf("%s  %s  %s\n", strings.Repeat("-", 32), strings.Repeat("-", 8), strings.Repeat("-", 40))
	shown := 0
	for _, t := range tables {
		status, detail := cliTableSummary(t)
		if !showAll && status == "一致" {
			continue
		}
		name := t.Name
		if dispWidth(name) > 32 {
			name = truncateCell(name, 32)
		}
		fmt.Printf("%s  %s  %s\n", padCell(name, 32), styleStatus(padCell(status, 8)), detail)
		shown++
	}
	if shown == 0 {
		fmt.Println(green("（无差异）"))
	} else if !showAll {
		fmt.Printf("\n%s\n", dim(fmt.Sprintf("仅显示有差异的表（%d）；使用 --all 查看全部 %d 项", shown, s.Total)))
	}
}

// printCompareShowHint 对比完成后提示记录 ID 与回看命令
func printCompareShowHint(recordID string) {
	fmt.Printf("\n%s\n", dim(fmt.Sprintf("记录 ID: %s · 查看差异明细: dbx cmp show -i %s [表名]", recordID, recordID)))
}

// cliCompareShow 回看历史对比记录：不带表名输出全部表，带表名（--table/-t 或位置参数）输出单表差异明细
func cliCompareShow(cmd *cobra.Command, args []string) error {
	f := cmd.Flags()
	id, _ := f.GetString("id")
	if id == "" {
		return cygin.NewError(cygin.ErrParamsInvalid, cygin.WithErrPrint(), cygin.WithErrDetailf("缺少 --id（对比记录 ID）"))
	}
	svc, err := newCliService()
	if err != nil {
		return err
	}
	result, err := svc.GetCompareResult(id)
	if err != nil {
		return err
	}
	table, _ := f.GetString("table")
	if len(args) > 0 {
		if table != "" && table != args[0] {
			return cygin.NewError(cygin.ErrParamsInvalid, cygin.WithErrPrint(), cygin.WithErrDetailf("表名指定冲突：--table %s 与位置参数 %s", table, args[0]))
		}
		table = args[0]
	}
	if table == "" {
		printCompareReport(result, true)
		return nil
	}
	tables := result.Tables
	if len(result.Databases) > 0 {
		tables = nil
		for _, db := range result.Databases {
			tables = append(tables, db.Tables...)
		}
	}
	for _, t := range tables {
		// 兼容三种指定方式：展示名 / 源表名 / 目标表名（别名配对场景）
		if t.Name == table || t.SourceName == table || t.TargetName == table {
			printTableDetail(t)
			return nil
		}
	}
	return cygin.NewError(cygin.ErrParamsInvalid, cygin.WithErrPrint(), cygin.WithErrDetailf("记录 %s 中未找到表: %s", id, table))
}

// printTableDetail 单表差异明细：列级结构对照 + 数据差异行样例
func printTableDetail(t engine.CompareTableResult) {
	name := t.Name
	if t.SourceName != "" && t.TargetName != "" && t.SourceName != t.TargetName {
		name = fmt.Sprintf("%s（%s ↔ %s）", t.Name, t.SourceName, t.TargetName)
	}
	fmt.Printf("%s\n", bold("表: "+name))

	if t.Columns != nil {
		fmt.Println(bold("\n── 结构 ──"))
		if t.Columns.Matched {
			fmt.Println(green("结构一致"))
		} else {
			for _, c := range t.Columns.SourceOnly {
				fmt.Printf("  %s %-30s %s\n", green("+"), c.Name, dim(columnDesc(c)+"  （仅源有）"))
			}
			for _, c := range t.Columns.TargetOnly {
				fmt.Printf("  %s %-30s %s\n", red("-"), c.Name, dim(columnDesc(c)+"  （仅目标有）"))
			}
			for _, d := range t.Columns.Different {
				fmt.Printf("  %s %-30s 定义不一致\n", yellow("±"), d.Name)
				fmt.Printf("      源:   %s\n", dim(columnDesc(d.Source)))
				fmt.Printf("      目标: %s\n", dim(columnDesc(d.Target)))
			}
		}
	}

	if t.Data != nil {
		fmt.Println(bold("\n── 数据 ──"))
		d := t.Data
		if d.SkippedReason != "" {
			fmt.Printf("未逐行比较: %s\n", d.SkippedReason)
		}
		fmt.Printf("行数: 源 %d / 目标 %d", d.SourceRows, d.TargetRows)
		if len(d.KeyColumns) > 0 {
			fmt.Printf("%s", dim(fmt.Sprintf("  （按主键 %s 判断有无）", strings.Join(d.KeyColumns, ","))))
		} else if d.Mode == "rows" {
			fmt.Printf("%s", dim("  （无主键，整行对比）"))
		}
		fmt.Println()
		switch {
		case d.Equal:
			fmt.Println(green("数据一致"))
		case d.Mode == "count":
			fmt.Println(red("行数不一致（超过阈值，仅比行数；可调大 --threshold 后重跑逐行对比）"))
		default:
			if d.Missing > 0 {
				fmt.Printf("%s（源有目标无）\n", red(fmt.Sprintf("缺失 %d 行", d.Missing)))
			}
			if d.Extra > 0 {
				fmt.Printf("%s（目标有源无）\n", yellow(fmt.Sprintf("多出 %d 行", d.Extra)))
			}
			if d.Changed > 0 {
				fmt.Printf("%s（主键匹配但内容不同）\n", yellow(fmt.Sprintf("变化 %d 行", d.Changed)))
			}
			printRowSamples("缺失行样例", d.MissingSamples, d.SampleColumns)
			printRowSamples("多出行样例", d.ExtraSamples, d.SampleColumns)
			printChangedSamples(d.ChangedSamples)
		}
	}
}

// columnDesc 列定义一行描述：类型 + 可空 + 主键
func columnDesc(c engine.ColumnItem) string {
	s := c.DataType
	if !c.Nullable {
		s += " NOT NULL"
	}
	if c.PrimaryKey {
		s += " PK"
	}
	return s
}

// printRowSamples 按 SampleColumns 列序渲染差异行样例
func printRowSamples(title string, rows []map[string]any, cols []string) {
	if len(rows) == 0 {
		return
	}
	fmt.Printf("%s（%d）:\n", title, len(rows))
	for i, row := range rows {
		parts := make([]string, 0, len(cols))
		for _, c := range cols {
			parts = append(parts, fmt.Sprintf("%s=%v", c, row[c]))
		}
		fmt.Printf("  %d. %s\n", i+1, strings.Join(parts, "  "))
	}
}

// printChangedSamples 渲染变化行样例：主键取值 + 差异列源/目标取值对照
func printChangedSamples(changed []engine.ChangedRow) {
	if len(changed) == 0 {
		return
	}
	fmt.Printf("变化行样例（%d）:\n", len(changed))
	for i, c := range changed {
		keys := make([]string, 0, len(c.Key))
		for k, v := range c.Key {
			keys = append(keys, fmt.Sprintf("%s=%v", k, v))
		}
		sort.Strings(keys)
		fmt.Printf("  %d. [%s]\n", i+1, strings.Join(keys, "  "))
		for _, d := range c.Diffs {
			fmt.Printf("     %s: 源=%v  目标=%v\n", d.Column, d.Source, d.Target)
		}
	}
}

// styleStatus 状态着色
func styleStatus(status string) string {
	switch strings.TrimSpace(status) {
	case "一致":
		return green(status)
	case "有差异":
		return red(status)
	case "仅源有", "仅目标有":
		return yellow(status)
	}
	return status
}

// dispWidth 终端显示宽度（CJK 按 2 列）
func dispWidth(s string) int {
	w := 0
	for _, r := range s {
		if r >= 0x1100 && (r <= 0x115F || r == 0x2329 || r == 0x232A ||
			(r >= 0x2E80 && r <= 0xA4CF) || (r >= 0xAC00 && r <= 0xD7A3) ||
			(r >= 0xF900 && r <= 0xFAFF) || (r >= 0xFE30 && r <= 0xFE4F) ||
			(r >= 0xFF00 && r <= 0xFF60) || (r >= 0xFFE0 && r <= 0xFFE6)) {
			w += 2
		} else {
			w++
		}
	}
	return w
}

// padCell 按显示宽度右补空格
func padCell(s string, width int) string {
	if n := dispWidth(s); n < width {
		s += strings.Repeat(" ", width-n)
	}
	return s
}

// truncateCell 按显示宽度截断（超出补 …）
func truncateCell(s string, width int) string {
	w := 0
	for i, r := range s {
		rw := dispWidth(string(r))
		if w+rw > width-1 {
			return s[:i] + "…"
		}
		w += rw
	}
	return s
}

// cliTableSummary 单表状态与差异说明（与 Web 报告摘要口径一致）
func cliTableSummary(t engine.CompareTableResult) (string, string) {
	switch t.Status {
	case "source_only":
		return "仅源有", ""
	case "target_only":
		return "仅目标有", ""
	}
	parts := []string{}
	if t.Columns != nil {
		if t.Columns.Matched {
			parts = append(parts, "结构一致")
		} else {
			parts = append(parts, fmt.Sprintf("结构: +%d -%d ±%d",
				len(t.Columns.SourceOnly), len(t.Columns.TargetOnly), len(t.Columns.Different)))
		}
	}
	if t.Data != nil {
		switch {
		case t.Data.Mode == "count":
			parts = append(parts, fmt.Sprintf("行数 %d vs %d", t.Data.SourceRows, t.Data.TargetRows))
		case t.Data.SkippedReason != "":
			parts = append(parts, t.Data.SkippedReason)
		case t.Data.Equal:
			parts = append(parts, fmt.Sprintf("数据一致 (%d行)", t.Data.SourceRows))
		default:
			detail := []string{}
			if t.Data.Missing > 0 {
				detail = append(detail, fmt.Sprintf("缺失%d行", t.Data.Missing))
			}
			if t.Data.Extra > 0 {
				detail = append(detail, fmt.Sprintf("多出%d行", t.Data.Extra))
			}
			if t.Data.Changed > 0 {
				detail = append(detail, fmt.Sprintf("变化%d行", t.Data.Changed))
			}
			parts = append(parts, strings.Join(detail, "/"))
		}
	}
	status := "一致"
	if (t.Columns != nil && !t.Columns.Matched) || (t.Data != nil && !t.Data.Equal) {
		status = "有差异"
	}
	return status, strings.Join(parts, " · ")
}
