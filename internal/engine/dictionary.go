package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/fj1981/infrakit/pkg/cydb/def"
	"github.com/xuri/excelize/v2"
)

// 数据字典 Excel 常量
const (
	dictMaxSheetRows = 1048576 // Excel 单 sheet 行数上限
	dictFontFamily   = "微软雅黑"
)

// dictTexts 数据字典产物文案（按语言索引，新增语言只加 map 条目）
type dictTexts struct {
	overview      string   // 总览 sheet 名
	detailCols    []string // 明细列头（序号/列名/类型/可空/主键/自增/默认值/注释）
	groupFailed   string   // 分组标题后缀：结构获取失败
	groupColCount string   // 分组标题后缀：(%d 列)
	metaFailed    string   // 失败占位行前缀：元数据获取失败
	overviewTitle string   // 封面标题
	overviewMeta  string   // 任务/来源/生成时间（含 %s 占位）
	overviewTotal string   // 共 %d 个数据库，%d 张表
	overviewCols  []string // 总览列头（库名/表名/表注释/列数）
	yes           string   // 布尔“是”
}

// dictTextsMap 语言注册表：缺失语言回退 zh
var dictTextsMap = map[string]dictTexts{
	"zh": {
		overview:      "总览",
		detailCols:    []string{"序号", "列名", "数据类型", "可空", "主键", "自增", "默认值", "注释"},
		groupFailed:   "(结构获取失败)",
		groupColCount: "(%d 列)",
		metaFailed:    "元数据获取失败",
		overviewTitle: "数据字典",
		overviewMeta:  "任务: %s　|　来源: %s　|　生成时间: %s",
		overviewTotal: "共 %d 个数据库，%d 张表",
		overviewCols:  []string{"库名", "表名", "表注释", "列数"},
		yes:           "是",
	},
	"en": {
		overview:      "Overview",
		detailCols:    []string{"No.", "Column", "Data Type", "Nullable", "PK", "Auto Inc", "Default", "Comment"},
		groupFailed:   "(structure fetch failed)",
		groupColCount: "(%d columns)",
		metaFailed:    "Failed to fetch metadata",
		overviewTitle: "Data Dictionary",
		overviewMeta:  "Task: %s | Source: %s | Generated: %s",
		overviewTotal: "%d databases, %d tables in total",
		overviewCols:  []string{"Database", "Table", "Comment", "Columns"},
		yes:           "Yes",
	},
}

// dictTextsFor 按语言代码取文案（zh/en 前缀归一，未知回退 zh）
func dictTextsFor(lang string) dictTexts {
	l := strings.ToLower(strings.TrimSpace(lang))
	switch {
	case strings.HasPrefix(l, "zh"):
		l = "zh"
	case strings.HasPrefix(l, "en"):
		l = "en"
	default:
		l = "zh"
	}
	if t, ok := dictTextsMap[l]; ok {
		return t
	}
	return dictTextsMap["zh"]
}

// dictTable 单表元数据（采集阶段产物）
type dictTable struct {
	name    string
	comment string
	cols    []def.ColumnInfo
	failed  string // 非空=结构获取失败原因（写占位行不阻断）
}

// dictDBInfo 单库元数据
type dictDBInfo struct {
	name   string
	tables []*dictTable
}

// RunDictionary 生成数据字典：选定库表 → 单个 .xlsx（总览 + 每库字段明细）→ 可选 zip 打包
//
// 产物组织（面向整体交付物）：
//   - Sheet "总览"（固定第一个）：封面标题区 + 全实例表清单（库名/表名/注释/列数），
//     表名经定义名称超链接精确跳转到所属库明细 sheet 的对应表分区
//   - 每库一个明细 sheet：首行全局列头（冻结 + 筛选），表与表之间以分组标题行分隔，
//     斑马纹按表分区重置，主键列高亮
func RunDictionary(ctx context.Context, opts DictionaryOptions, cb ProgressFunc) (*ExportResult, error) {
	if opts.Source == nil {
		return nil, NewMsgErr(errDictNoSrc)
	}
	txt := dictTextsFor(opts.Lang) // 产物文案语言（发起时确定，历史产物不回改）
	t := newTracker(cb, opts.Lang)

	outputDir := opts.OutputDir
	if outputDir == "" {
		// 默认：数据根目录下 exports/（service 层调用时已注入同一默认值）
		home, _ := os.UserHomeDir()
		outputDir = filepath.Join(home, ".dqex", "exports")
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, NewMsgErrf(errDictOutDir, err)
	}

	taskName := sanitizeName(opts.TaskName)
	if taskName == "" {
		taskName = "dictionary"
	}
	ts := time.Now().Format("20060102_150405")
	baseDir := filepath.Join(outputDir, fmt.Sprintf("%s_%s", taskName, ts))
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, NewMsgErrf(errDictExpDir, err)
	}

	// 1. 确定库列表（未选库时明确报错，对齐 compare 的交互）
	databases := opts.Databases
	if len(databases) == 0 {
		if opts.Source.DBName == "" {
			return nil, NewMsgErr(errDictNoDB)
		}
		databases = []string{opts.Source.DBName}
	}
	sort.Strings(databases) // 交付物按库名排序

	// 2. 预扫描：收集各库的表清单（视图不纳入字典）
	type dbTables struct {
		db     string
		tables []string
	}
	var plan []dbTables
	for _, db := range databases {
		if err := ctx.Err(); err != nil {
			return nil, NewMsgErr(errCancelled)
		}
		cli, err := ConnectDB(*opts.Source, db)
		if err != nil {
			return nil, err
		}
		all, err := listSchemaTables(cli, db, &opts.Source.Schema)
		cli.Close()
		if err != nil {
			return nil, NewMsgErrf(errDictListTables, err, db)
		}
		tables := filterTables(all, opts.Tables, db)
		if len(tables) == 0 {
			t.log(engineTextsFor(t.lang).dictNoTables, db)
			continue
		}
		sort.Strings(tables)
		plan = append(plan, dbTables{db: db, tables: tables})
	}
	if len(plan) == 0 {
		return nil, NewMsgErr(errDictNoTables)
	}

	totalTables := 0
	for _, p := range plan {
		totalTables += len(p.tables)
	}
	t.p.TotalUnits = totalTables
	t.log(engineTextsFor(t.lang).dictStart, len(plan), totalTables)

	// 3. 逐库采集表/列元数据（单表失败仅占位不阻断，交付物优先完整性）
	var infos []*dictDBInfo
	for _, p := range plan {
		cli, err := ConnectDB(*opts.Source, p.db)
		if err != nil {
			return nil, err
		}
		info := &dictDBInfo{name: p.db}
		for _, tb := range p.tables {
			if err := ctx.Err(); err != nil {
				cli.Close()
				return nil, NewMsgErr(errCancelled)
			}
			t.p.CurrentTable = p.db + "." + tb
			t.emit(true)

			dt := &dictTable{name: tb}
			ti, err := cli.GetTableInfo(tb)
			if err != nil {
				dt.failed = err.Error()
				t.log(engineTextsFor(t.lang).dictStructFail, p.db, tb, err)
			} else {
				dt.comment = ti.GetComment()
				dt.cols = ti.GetColumns()
				t.log(engineTextsFor(t.lang).dictCols, p.db, tb, len(dt.cols))
			}
			info.tables = append(info.tables, dt)
			t.p.DoneUnits++
		}
		cli.Close()
		infos = append(infos, info)
	}

	// 4. 构建工作簿并落盘
	f := excelize.NewFile()
	defer f.Close()
	srcLabel := fmt.Sprintf("%s@%s:%d", opts.Source.Un, opts.Source.Host, opts.Source.Port)
	if err := buildDictionaryWorkbook(f, infos, taskName, srcLabel, txt, t); err != nil {
		return nil, err
	}

	xlsxPath := filepath.Join(baseDir, fmt.Sprintf("%s_%s.xlsx", taskName, ts))
	if err := f.SaveAs(xlsxPath); err != nil {
		return nil, NewMsgErrf(errDictWrite, err)
	}

	// 5. 打包 zip（可选，OutputPath 语义与导出一致）
	result := &ExportResult{OutputDir: baseDir, TotalTables: totalTables}
	if opts.Compress {
		zipPath := filepath.Join(outputDir, fmt.Sprintf("%s_%s.zip", taskName, ts))
		t.log(engineTextsFor(t.lang).zipPack, filepath.Base(zipPath))
		if err := zipDir(baseDir, zipPath); err != nil {
			return nil, NewMsgErrf(errDictZipPack, err)
		}
		_ = os.RemoveAll(baseDir)
		result.OutputPath = zipPath
	} else {
		result.OutputPath = xlsxPath
	}

	t.p.OutputPath = result.OutputPath
	t.finish()
	t.log(engineTextsFor(t.lang).dictDone, len(infos), totalTables, filepath.Base(result.OutputPath))
	return result, nil
}

// dictStyles 字典工作簿全套样式（一次性创建复用，避免逐单元格建样式）
type dictStyles struct {
	title, meta, header, group int
	normal, zebra              int // 普通/斑马纹
	right, rightZebra          int // 右对齐（序号/列数）
	center, centerZebra        int // 居中（布尔列）
	pk                         int // 主键高亮
	wrap, wrapZebra            int // 自动换行（默认值/注释）
	link                       int // 总览表名超链接
}

// newDictStyles 创建字典样式集
func newDictStyles(f *excelize.File) (*dictStyles, error) {
	font := dictFontFamily
	border := []excelize.Border{
		{Type: "left", Color: "D9D9D9", Style: 1},
		{Type: "top", Color: "D9D9D9", Style: 1},
		{Type: "right", Color: "D9D9D9", Style: 1},
		{Type: "bottom", Color: "D9D9D9", Style: 1},
	}
	fill := func(color string) *excelize.Fill {
		return &excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{color}}
	}
	newStyle := func(s *excelize.Style) (int, error) { return f.NewStyle(s) }

	st := &dictStyles{}
	var err error
	if st.title, err = newStyle(&excelize.Style{
		Font:      &excelize.Font{Family: font, Size: 16, Bold: true, Color: "FFFFFF"},
		Fill:      *fill("1F4E79"),
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	}); err != nil {
		return nil, err
	}
	if st.meta, err = newStyle(&excelize.Style{
		Font:      &excelize.Font{Family: font, Size: 10, Color: "595959"},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	}); err != nil {
		return nil, err
	}
	headerAlign := &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true}
	if st.header, err = newStyle(&excelize.Style{
		Font:      &excelize.Font{Family: font, Size: 11, Bold: true, Color: "FFFFFF"},
		Fill:      *fill("1F4E79"),
		Alignment: headerAlign,
		Border:    border,
	}); err != nil {
		return nil, err
	}
	if st.group, err = newStyle(&excelize.Style{
		Font:      &excelize.Font{Family: font, Size: 11, Bold: true, Color: "FFFFFF"},
		Fill:      *fill("2E75B6"),
		Alignment: &excelize.Alignment{Horizontal: "left", Vertical: "center", Indent: 1},
		Border:    border,
	}); err != nil {
		return nil, err
	}
	// 数据区基础样式及变体
	base := func(extra *excelize.Alignment, zebra bool) *excelize.Style {
		s := &excelize.Style{Font: &excelize.Font{Family: font, Size: 11}, Border: border}
		if extra != nil {
			s.Alignment = extra
		} else {
			s.Alignment = &excelize.Alignment{Vertical: "center"}
		}
		if zebra {
			s.Fill = *fill("F2F7FB")
		}
		return s
	}
	if st.normal, err = newStyle(base(nil, false)); err != nil {
		return nil, err
	}
	if st.zebra, err = newStyle(base(nil, true)); err != nil {
		return nil, err
	}
	if st.right, err = newStyle(base(&excelize.Alignment{Horizontal: "right", Vertical: "center"}, false)); err != nil {
		return nil, err
	}
	if st.rightZebra, err = newStyle(base(&excelize.Alignment{Horizontal: "right", Vertical: "center"}, true)); err != nil {
		return nil, err
	}
	if st.center, err = newStyle(base(&excelize.Alignment{Horizontal: "center", Vertical: "center"}, false)); err != nil {
		return nil, err
	}
	if st.centerZebra, err = newStyle(base(&excelize.Alignment{Horizontal: "center", Vertical: "center"}, true)); err != nil {
		return nil, err
	}
	if st.pk, err = newStyle(&excelize.Style{
		Font:      &excelize.Font{Family: font, Size: 11, Bold: true, Color: "7F6000"},
		Fill:      *fill("FFF2CC"),
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
		Border:    border,
	}); err != nil {
		return nil, err
	}
	if st.wrap, err = newStyle(base(&excelize.Alignment{Vertical: "center", WrapText: true}, false)); err != nil {
		return nil, err
	}
	if st.wrapZebra, err = newStyle(base(&excelize.Alignment{Vertical: "center", WrapText: true}, true)); err != nil {
		return nil, err
	}
	if st.link, err = newStyle(&excelize.Style{
		Font:      &excelize.Font{Family: font, Size: 11, Color: "0563C1", Underline: "single"},
		Alignment: &excelize.Alignment{Vertical: "center"},
		Border:    border,
	}); err != nil {
		return nil, err
	}
	return st, nil
}

// buildDictionaryWorkbook 构建完整工作簿：明细 sheet（收集定义名称）→ 总览 sheet
func buildDictionaryWorkbook(f *excelize.File, infos []*dictDBInfo, taskName, srcLabel string, txt dictTexts, t *tracker) error {
	st, err := newDictStyles(f)
	if err != nil {
		return NewMsgErrf(errDictExcelStyle, err)
	}

	// 默认 Sheet1 重命名为总览（保持其为第一个 sheet），明细 sheet 依次追加
	if err := f.SetSheetName("Sheet1", txt.overview); err != nil {
		return err
	}

	usedSheets := map[string]bool{txt.overview: true}
	usedNames := map[string]bool{}
	// 表 → 定义名称（总览超链接跳转目标），key 为 库名\x00表名
	definedNames := make(map[string]string)

	for _, dbi := range infos {
		sheet := dictSheetName(dbi.name, usedSheets)
		if _, err := f.NewSheet(sheet); err != nil {
			return NewMsgErrf(errDictSheet, err, sheet)
		}
		names, err := writeDictDetailSheet(f, st, sheet, dbi, usedNames, txt)
		if err != nil {
			return NewMsgErrf(errDictSheetDetail, err, dbi.name)
		}
		for tb, name := range names {
			definedNames[dbi.name+"\x00"+tb] = name
		}
	}

	if err := writeDictOverviewSheet(f, st, infos, taskName, srcLabel, definedNames, txt); err != nil {
		return err
	}

	f.SetActiveSheet(0) // 打开即见总览
	return nil
}

// writeDictDetailSheet 写入单库字段明细 sheet，返回 表名 → 定义名称 映射
func writeDictDetailSheet(f *excelize.File, st *dictStyles, sheet string, dbi *dictDBInfo, usedNames map[string]bool, txt dictTexts) (map[string]string, error) {
	const cols = "H" // 明细列数固定 8 列（A~H）
	names := make(map[string]string, len(dbi.tables))

	// 首行全局列头（冻结 + 筛选依赖其固定在首行）
	if err := f.SetSheetRow(sheet, "A1", &txt.detailCols); err != nil {
		return nil, err
	}
	if err := f.SetCellStyle(sheet, "A1", cols+"1", st.header); err != nil {
		return nil, err
	}
	_ = f.SetRowHeight(sheet, 1, 22)

	// 列宽按内容估算：各列最小宽度（序号/列名/类型/可空/主键/自增/默认值/注释）
	minWidths := [8]float64{6, 24, 18, 6, 6, 8, 18, 40}
	widths := minWidths
	track := func(idx int, v string) {
		if w := float64(dictDisplayWidth(v)) + 2; w > widths[idx] {
			widths[idx] = w
		}
	}

	row := 1
	for _, tb := range dbi.tables {
		// 超大规模防护：提前检查 Excel 行数上限，避免生成到一半失败
		if row+1+len(tb.cols) > dictMaxSheetRows {
			return nil, NewMsgErr(errDictRowLimit, dbi.name, dictMaxSheetRows)
		}

		// ---- 分组标题行（合并单元格：表名 + 注释 + 列数）----
		row++
		title := tb.name
		if tb.comment != "" {
			title += "　" + tb.comment
		}
		if tb.failed != "" {
			title += "　" + txt.groupFailed
		} else {
			title += "　" + fmt.Sprintf(txt.groupColCount, len(tb.cols))
		}
		groupCell := fmt.Sprintf("A%d", row)
		if err := f.SetCellValue(sheet, groupCell, title); err != nil {
			return nil, err
		}
		if err := f.MergeCell(sheet, groupCell, cols+fmt.Sprint(row)); err != nil {
			return nil, err
		}
		if err := f.SetCellStyle(sheet, groupCell, cols+fmt.Sprint(row), st.group); err != nil {
			return nil, err
		}
		_ = f.SetRowHeight(sheet, row, 20)

		// 定义名称指向分区标题单元格，供总览超链接精确跳转（工作簿级唯一）
		dn := dictDefinedName(dbi.name, tb.name, usedNames)
		if err := f.SetDefinedName(&excelize.DefinedName{
			Name:     dn,
			RefersTo: fmt.Sprintf("'%s'!$A$%d", sheet, row),
		}); err != nil {
			return nil, err
		}
		names[tb.name] = dn

		// ---- 字段行（占位行：结构获取失败时标注原因）----
		if tb.failed != "" {
			row++
			writeDictRow(f, sheet, row, st, 0, []any{"", "—", txt.metaFailed + ": " + tb.failed, "", "", "", "", ""}, txt)
			track(2, txt.metaFailed+": "+tb.failed)
			continue
		}
		for i, col := range tb.cols {
			row++
			defVal := ""
			if d := col.GetDefault(); d != nil {
				defVal = *d
			}
			pk := col.IsPrimaryKey()
			writeDictRow(f, sheet, row, st, i, []any{
				i + 1,
				col.GetName(),
				col.GetOrginalDataType(),
				boolMark(!col.IsNotNull(), txt),
				boolMark(pk, txt),
				boolMark(col.IsAutoIncrement(), txt),
				defVal,
				col.GetComment(),
			}, txt)
			track(1, col.GetName())
			track(2, col.GetOrginalDataType())
			track(6, defVal)
			track(7, col.GetComment())
		}
	}

	// 列宽（估算值限幅 8~60）
	colNames := []string{"A", "B", "C", "D", "E", "F", "G", "H"}
	for i, cn := range colNames {
		w := widths[i]
		if w < 8 {
			w = 8
		}
		if w > 60 {
			w = 60
		}
		_ = f.SetColWidth(sheet, cn, cn, w)
	}

	// 冻结首行列头 + 自动筛选
	if err := f.SetPanes(sheet, &excelize.Panes{
		Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft",
		Selection: []excelize.Selection{{Pane: "bottomLeft", ActiveCell: "A2", SQRef: "A2"}},
	}); err != nil {
		return nil, err
	}
	if err := f.AutoFilter(sheet, fmt.Sprintf("A1:%s%d", cols, row), []excelize.AutoFilterOptions{}); err != nil {
		return nil, err
	}
	return names, nil
}

// writeDictRow 写入一行字段明细并按列套用样式（斑马纹按表分区重置：idx 为表内行序）
func writeDictRow(f *excelize.File, sheet string, row int, st *dictStyles, idx int, values []any, txt dictTexts) {
	zebra := idx%2 == 1
	cell := func(col string) string { return fmt.Sprintf("%s%d", col, row) }
	for i, v := range values {
		_ = f.SetCellValue(sheet, cell(string(rune('A'+i))), v)
	}
	// 各列样式：A 序号右对齐 / BC 常规 / D 可空、F 自增居中 / E 主键高亮 / GH 换行
	pair := func(col string, normal, zebraStyle int) {
		s := normal
		if zebra {
			s = zebraStyle
		}
		_ = f.SetCellStyle(sheet, cell(col), cell(col), s)
	}
	pair("A", st.right, st.rightZebra)
	pair("B", st.normal, st.zebra)
	pair("C", st.normal, st.zebra)
	pair("D", st.center, st.centerZebra)
	if len(values) > 4 && values[4] == txt.yes {
		_ = f.SetCellStyle(sheet, cell("E"), cell("E"), st.pk) // 主键高亮优先于斑马纹
	} else {
		pair("E", st.center, st.centerZebra)
	}
	pair("F", st.center, st.centerZebra)
	pair("G", st.wrap, st.wrapZebra)
	pair("H", st.wrap, st.wrapZebra)
}

// writeDictOverviewSheet 写入总览 sheet：封面标题区 + 全实例表清单（超链接跳转明细）
func writeDictOverviewSheet(f *excelize.File, st *dictStyles, infos []*dictDBInfo, taskName, srcLabel string, definedNames map[string]string, txt dictTexts) error {
	sheet := txt.overview
	now := time.Now().Format("2006-01-02 15:04:05")

	totalTables := 0
	for _, dbi := range infos {
		totalTables += len(dbi.tables)
	}

	// 封面标题区（交付物首页）
	_ = f.SetCellValue(sheet, "A1", txt.overviewTitle)
	if err := f.MergeCell(sheet, "A1", "D1"); err != nil {
		return err
	}
	if err := f.SetCellStyle(sheet, "A1", "D1", st.title); err != nil {
		return err
	}
	_ = f.SetRowHeight(sheet, 1, 36)

	_ = f.SetCellValue(sheet, "A2", fmt.Sprintf(txt.overviewMeta, taskName, srcLabel, now))
	if err := f.MergeCell(sheet, "A2", "D2"); err != nil {
		return err
	}
	_ = f.SetCellValue(sheet, "A3", fmt.Sprintf(txt.overviewTotal, len(infos), totalTables))
	if err := f.MergeCell(sheet, "A3", "D3"); err != nil {
		return err
	}
	for r := 2; r <= 3; r++ {
		if err := f.SetCellStyle(sheet, fmt.Sprintf("A%d", r), fmt.Sprintf("D%d", r), st.meta); err != nil {
			return err
		}
	}
	_ = f.SetRowHeight(sheet, 2, 18)
	_ = f.SetRowHeight(sheet, 3, 18)

	// 表清单列头（第 5 行）
	const headerRow = 5
	if err := f.SetSheetRow(sheet, fmt.Sprintf("A%d", headerRow), &txt.overviewCols); err != nil {
		return err
	}
	if err := f.SetCellStyle(sheet, fmt.Sprintf("A%d", headerRow), fmt.Sprintf("D%d", headerRow), st.header); err != nil {
		return err
	}
	_ = f.SetRowHeight(sheet, headerRow, 22)

	widths := [4]float64{18, 30, 40, 8}
	row := headerRow
	for _, dbi := range infos {
		for _, tb := range dbi.tables {
			row++
			_ = f.SetCellValue(sheet, fmt.Sprintf("A%d", row), dbi.name)
			_ = f.SetCellValue(sheet, fmt.Sprintf("B%d", row), tb.name)
			_ = f.SetCellValue(sheet, fmt.Sprintf("C%d", row), tb.comment)
			if tb.failed == "" {
				_ = f.SetCellValue(sheet, fmt.Sprintf("D%d", row), len(tb.cols))
			}
			_ = f.SetCellStyle(sheet, fmt.Sprintf("A%d", row), fmt.Sprintf("A%d", row), st.normal)
			_ = f.SetCellStyle(sheet, fmt.Sprintf("C%d", row), fmt.Sprintf("C%d", row), st.wrap)
			_ = f.SetCellStyle(sheet, fmt.Sprintf("D%d", row), fmt.Sprintf("D%d", row), st.right)
			// 表名超链接：通过定义名称精确跳转到明细 sheet 的表分区标题行
			if dn, ok := definedNames[dbi.name+"\x00"+tb.name]; ok {
				if err := f.SetCellHyperLink(sheet, fmt.Sprintf("B%d", row), dn, "Location"); err != nil {
					return err
				}
				_ = f.SetCellStyle(sheet, fmt.Sprintf("B%d", row), fmt.Sprintf("B%d", row), st.link)
			} else {
				_ = f.SetCellStyle(sheet, fmt.Sprintf("B%d", row), fmt.Sprintf("B%d", row), st.normal)
			}
			if w := float64(dictDisplayWidth(dbi.name)) + 2; w > widths[0] {
				widths[0] = w
			}
			if w := float64(dictDisplayWidth(tb.name)) + 2; w > widths[1] {
				widths[1] = w
			}
			if w := float64(dictDisplayWidth(tb.comment)) + 2; w > widths[2] {
				widths[2] = w
			}
		}
	}

	for i, cn := range []string{"A", "B", "C", "D"} {
		w := widths[i]
		if w > 60 {
			w = 60
		}
		_ = f.SetColWidth(sheet, cn, cn, w)
	}

	// 冻结封面 + 列头，开启筛选
	if err := f.SetPanes(sheet, &excelize.Panes{
		Freeze: true, YSplit: headerRow, TopLeftCell: fmt.Sprintf("A%d", headerRow+1), ActivePane: "bottomLeft",
		Selection: []excelize.Selection{{Pane: "bottomLeft", ActiveCell: fmt.Sprintf("A%d", headerRow+1), SQRef: fmt.Sprintf("A%d", headerRow+1)}},
	}); err != nil {
		return err
	}
	return f.AutoFilter(sheet, fmt.Sprintf("A%d:D%d", headerRow, row), []excelize.AutoFilterOptions{})
}

// boolMark 布尔列展示文案：真=是（按语言），假=—
func boolMark(b bool, txt dictTexts) string {
	if b {
		return txt.yes
	}
	return "—"
}

// dictSheetName 生成合法且唯一的 sheet 名（Excel 限制 31 字符与 []:*?/\ 字符）
func dictSheetName(name string, used map[string]bool) string {
	replacer := strings.NewReplacer("[", "_", "]", "_", ":", "_", "*", "_", "?", "_", "/", "_", "\\", "_")
	s := strings.TrimSpace(replacer.Replace(name))
	if s == "" {
		s = "sheet"
	}
	if rs := []rune(s); len(rs) > 31 {
		s = string(rs[:31])
	}
	base := s
	for i := 2; used[s]; i++ {
		sfx := fmt.Sprintf("~%d", i)
		rs := []rune(base)
		if len(rs) > 31-len([]rune(sfx)) {
			s = string(rs[:31-len([]rune(sfx))]) + sfx
		} else {
			s = base + sfx
		}
	}
	used[s] = true
	return s
}

// dictDefinedName 生成合法且唯一的定义名称（字母/数字/下划线/点，不以数字开头）
func dictDefinedName(db, table string, used map[string]bool) string {
	clean := func(s string) string {
		var b strings.Builder
		for _, r := range s {
			switch {
			case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '.':
				b.WriteRune(r)
			default:
				b.WriteRune('_')
			}
		}
		out := b.String()
		if out == "" {
			out = "x"
		}
		if out[0] >= '0' && out[0] <= '9' {
			out = "_" + out
		}
		return out
	}
	base := clean(db) + "_" + clean(table)
	name := base
	for i := 2; used[name]; i++ {
		name = fmt.Sprintf("%s_%d", base, i)
	}
	used[name] = true
	return name
}

// dictDisplayWidth 估算显示宽度（中文等宽字符按 2 计），用于列宽自适应
func dictDisplayWidth(s string) int {
	w := 0
	for _, r := range s {
		if r > 0x2E7F {
			w += 2
		} else {
			w++
		}
	}
	return w
}
