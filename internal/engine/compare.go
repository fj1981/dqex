package engine

import (
	"context"
	"encoding/hex"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cydb"
)

// DefaultCompareThreshold 数据逐行比较默认阈值：行数不超过该值的表逐行比较，超过仅比较行数
const DefaultCompareThreshold = 1000

// compareSampleLimit 行级差异明细每侧最多保留条数
const compareSampleLimit = 20

// 对比状态
const (
	compareStatusBoth       = "both"
	compareStatusSourceOnly = "source_only"
	compareStatusTargetOnly = "target_only"
)

// comparePair 一个比较配对（同名匹配或别名配对）
type comparePair struct {
	Name       string // 展示名
	SourceName string // 源侧实际表名（源侧不存在时为空）
	TargetName string // 目标侧实际表名
	Status     string
}

// RunCompare 执行数据库对比：表清单/结构差异 + 数据差异。
// 作用域为单个库对（Source.DBName ↔ Target.DBName），表按裸表名匹配（大小写不敏感）；
// Aliases 别名配对优先于同名匹配，数据行数超阈值仅比较行数
func RunCompare(ctx context.Context, opts CompareOptions, cb ProgressFunc) (*CompareResult, error) {
	if opts.Source == nil || opts.Target == nil {
		return nil, fmt.Errorf("未提供源或目标数据库连接")
	}
	if compareScopeDB(opts.Source) == "" || compareScopeDB(opts.Target) == "" {
		return nil, fmt.Errorf("请先选择对比的库")
	}
	threshold := opts.Threshold
	if threshold <= 0 {
		threshold = DefaultCompareThreshold
	}
	t := newTracker(cb)

	sourceCli, err := Connect(*opts.Source)
	if err != nil {
		return nil, fmt.Errorf("源库连接失败: %w", err)
	}
	defer sourceCli.Close()
	targetCli, err := Connect(*opts.Target)
	if err != nil {
		return nil, fmt.Errorf("目标库连接失败: %w", err)
	}
	defer targetCli.Close()

	srcTables, err := listCompareTables(sourceCli, opts.Source, opts.Tables)
	if err != nil {
		return nil, fmt.Errorf("获取源库表列表失败: %w", err)
	}
	tgtTables, err := listCompareTables(targetCli, opts.Target, opts.Tables)
	if err != nil {
		return nil, fmt.Errorf("获取目标库表列表失败: %w", err)
	}

	pairs, err := buildComparePairs(srcTables, tgtTables, opts.Aliases)
	if err != nil {
		return nil, err
	}
	t.p.TotalUnits = len(pairs)
	t.log("开始对比: %d 组表配对 (%s ↔ %s), 数据阈值=%d", len(pairs), compareScopeDB(opts.Source), compareScopeDB(opts.Target), threshold)

	result := &CompareResult{Source: compareScopeDB(opts.Source), Target: compareScopeDB(opts.Target), Tables: []CompareTableResult{}}
	for _, pair := range pairs {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("任务已取消")
		}
		t.p.CurrentTable = pair.Name
		t.emit(true)

		tr := CompareTableResult{
			Name:       pair.Name,
			SourceName: pair.SourceName,
			TargetName: pair.TargetName,
			Status:     pair.Status,
		}
		if pair.Status == compareStatusBoth {
			if !opts.DataOnly {
				cols, err := compareColumns(sourceCli, targetCli, pair.SourceName, pair.TargetName)
				if err != nil {
					t.log("表 %s 结构对比失败（已跳过）: %v", pair.Name, err)
				} else {
					tr.Columns = cols
				}
			}
			if !opts.StructureOnly {
				data, err := compareTableData(ctx, sourceCli, targetCli, pair.SourceName, pair.TargetName, threshold, t)
				if err != nil {
					t.log("表 %s 数据对比失败（已跳过）: %v", pair.Name, err)
				} else {
					tr.Data = data
				}
			}
		}
		result.Tables = append(result.Tables, tr)
		t.p.DoneUnits++
		t.log("%s: %s", pair.Name, tableResultDesc(&tr))
	}

	result.Summary = buildCompareSummary(result.Tables)
	t.finish()
	t.log("对比完成: 共%d项, 一致%d, 仅源有%d, 仅目标有%d, 结构差异%d, 数据差异%d",
		result.Summary.Total, result.Summary.Matched, result.Summary.SourceOnly,
		result.Summary.TargetOnly, result.Summary.StructureDiff, result.Summary.DataDiff)
	return result, nil
}

// compareScopeDB 返回对比作用域的库/schema 名（Oracle 的"库"语义为 schema），空表示未指定
func compareScopeDB(conn *DBConnInfo) string {
	if strings.EqualFold(conn.Type, "oracle") {
		if conn.Schema != "" {
			return conn.Schema
		}
		return conn.DBName
	}
	return conn.DBName
}

// listCompareTables 获取连接对应库内的表清单（剔除视图），按裸表名过滤选中项
func listCompareTables(cli *cydb.DBCli, conn *DBConnInfo, wanted []string) ([]string, error) {
	dbName := conn.DBName
	var schemaPtr *string
	if strings.EqualFold(cli.DBType(), "oracle") {
		schema := conn.Schema
		if schema == "" {
			schema = conn.DBName
		}
		schemaPtr = &schema
		dbName = ""
	}
	all, err := cli.GetTables(dbName, schemaPtr, nil)
	if err != nil {
		return nil, err
	}
	all = excludeViews(cli, conn.DBName, conn.Schema, all)
	return filterTablesBare(all, wanted, compareScopeDB(conn)), nil
}

// filterTablesBare 按裸表名过滤：nil=全部，空数组=无表。
// "库.表" 限定名条目剥离库前缀后按裸名比较（仅库名匹配时生效），避免源/目标库名不同时失配
func filterTablesBare(all []string, wanted []string, db string) []string {
	if wanted == nil {
		return all
	}
	bare := make(map[string]bool, len(wanted))
	for _, w := range wanted {
		w = strings.TrimSpace(w)
		if d, tbl, ok := splitQualifiedName(w); ok {
			if strings.EqualFold(d, db) {
				bare[strings.ToLower(tbl)] = true
			}
		} else if w != "" {
			bare[strings.ToLower(w)] = true
		}
	}
	ret := make([]string, 0, len(all))
	for _, tb := range all {
		if bare[strings.ToLower(tb)] {
			ret = append(ret, tb)
		}
	}
	return ret
}

// buildComparePairs 构建比较配对（大小写不敏感）：
// 别名配对优先于同名匹配；同一张表不允许出现在多个别名配对中（重复配置直接报错）；
// 别名映射的表在对应库不存在时降级为 source_only/target_only，两侧都不存在则跳过
func buildComparePairs(srcTables, tgtTables []string, aliases []TableAlias) ([]comparePair, error) {
	srcMap := make(map[string]string, len(srcTables)) // 小写名 → 实际名
	for _, tb := range srcTables {
		srcMap[strings.ToLower(tb)] = tb
	}
	tgtMap := make(map[string]string, len(tgtTables))
	for _, tb := range tgtTables {
		tgtMap[strings.ToLower(tb)] = tb
	}

	pairs := make([]comparePair, 0, len(srcTables)+len(tgtTables))
	usedSrc := make(map[string]bool, len(aliases))
	usedTgt := make(map[string]bool, len(aliases))

	seenSrc := make(map[string]bool, len(aliases))
	seenTgt := make(map[string]bool, len(aliases))
	for _, a := range aliases {
		s := strings.ToLower(strings.TrimSpace(a.Source))
		tg := strings.ToLower(strings.TrimSpace(a.Target))
		if s == "" || tg == "" {
			continue
		}
		if seenSrc[s] || seenTgt[tg] {
			return nil, fmt.Errorf("别名配对配置重复: %s ↔ %s", a.Source, a.Target)
		}
		seenSrc[s] = true
		seenTgt[tg] = true

		srcName, srcOK := srcMap[s]
		tgtName, tgtOK := tgtMap[tg]
		status := compareStatusBoth
		switch {
		case srcOK && tgtOK:
		case srcOK:
			status = compareStatusSourceOnly
		case tgtOK:
			status = compareStatusTargetOnly
		default:
			continue // 两侧都不存在该表，别名无效直接跳过
		}
		pairs = append(pairs, newComparePair(srcName, tgtName, status))
		usedSrc[s] = true
		usedTgt[tg] = true
	}

	// 剩余表按同名匹配
	for _, key := range sortedKeys(srcMap) {
		if usedSrc[key] {
			continue
		}
		usedSrc[key] = true
		if tgtName, ok := tgtMap[key]; ok && !usedTgt[key] {
			usedTgt[key] = true
			pairs = append(pairs, newComparePair(srcMap[key], tgtName, compareStatusBoth))
			continue
		}
		pairs = append(pairs, newComparePair(srcMap[key], "", compareStatusSourceOnly))
	}
	for _, key := range sortedKeys(tgtMap) {
		if usedTgt[key] {
			continue
		}
		pairs = append(pairs, newComparePair("", tgtMap[key], compareStatusTargetOnly))
	}
	return pairs, nil
}

// newComparePair 构造配对：同名配对展示名为表名，别名配对展示名为 "源表 ↔ 目标表"
func newComparePair(srcName, tgtName, status string) comparePair {
	p := comparePair{SourceName: srcName, TargetName: tgtName, Status: status}
	switch status {
	case compareStatusSourceOnly:
		p.Name = srcName
	case compareStatusTargetOnly:
		p.Name = tgtName
	default:
		p.Name = srcName
		if !strings.EqualFold(srcName, tgtName) {
			p.Name = srcName + " ↔ " + tgtName
		}
	}
	return p
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ---- 结构对比 ----

// compareColumns 对比两侧表的列结构（列名大小写不敏感匹配）
func compareColumns(sourceCli, targetCli *cydb.DBCli, srcTable, tgtTable string) (*ColumnDiff, error) {
	srcCols, err := tableColumns(sourceCli, srcTable)
	if err != nil {
		return nil, fmt.Errorf("获取源表列信息失败: %w", err)
	}
	tgtCols, err := tableColumns(targetCli, tgtTable)
	if err != nil {
		return nil, fmt.Errorf("获取目标表列信息失败: %w", err)
	}
	return diffColumns(srcCols, tgtCols), nil
}

func tableColumns(cli *cydb.DBCli, table string) ([]ColumnItem, error) {
	info, err := cli.GetTableInfo(table)
	if err != nil {
		return nil, err
	}
	cols := info.GetColumns()
	ret := make([]ColumnItem, 0, len(cols))
	for _, col := range cols {
		ret = append(ret, ColumnItem{
			Name:       col.GetName(),
			DataType:   col.GetOrginalDataType(),
			Nullable:   !col.IsNotNull(),
			PrimaryKey: col.IsPrimaryKey(),
		})
	}
	return ret, nil
}

// diffColumns 列级差异计算（纯函数）：类型按原始字符串对比，跨库类型时为参考性结论
func diffColumns(srcCols, tgtCols []ColumnItem) *ColumnDiff {
	srcMap := make(map[string]ColumnItem, len(srcCols))
	for _, c := range srcCols {
		srcMap[strings.ToLower(c.Name)] = c
	}
	tgtMap := make(map[string]ColumnItem, len(tgtCols))
	for _, c := range tgtCols {
		tgtMap[strings.ToLower(c.Name)] = c
	}
	d := &ColumnDiff{SourceOnly: []ColumnItem{}, TargetOnly: []ColumnItem{}, Different: []ColumnItemDiff{}}
	for _, c := range srcCols {
		tc, ok := tgtMap[strings.ToLower(c.Name)]
		if !ok {
			d.SourceOnly = append(d.SourceOnly, c)
			continue
		}
		if c.DataType != tc.DataType || c.Nullable != tc.Nullable || c.PrimaryKey != tc.PrimaryKey {
			d.Different = append(d.Different, ColumnItemDiff{Name: c.Name, Source: c, Target: tc})
		}
	}
	for _, c := range tgtCols {
		if _, ok := srcMap[strings.ToLower(c.Name)]; !ok {
			d.TargetOnly = append(d.TargetOnly, c)
		}
	}
	d.Matched = len(d.SourceOnly) == 0 && len(d.TargetOnly) == 0 && len(d.Different) == 0
	return d
}

// ---- 数据对比 ----

// compareTableData 单表数据对比：超阈值仅比较行数；否则按两侧公共列交集逐行归一化后做多重集比较
func compareTableData(ctx context.Context, sourceCli, targetCli *cydb.DBCli, srcTable, tgtTable string, threshold int, t *tracker) (*DataDiff, error) {
	srcRows, err := countTableRows(sourceCli, srcTable)
	if err != nil {
		return nil, fmt.Errorf("统计源表行数失败: %w", err)
	}
	tgtRows, err := countTableRows(targetCli, tgtTable)
	if err != nil {
		return nil, fmt.Errorf("统计目标表行数失败: %w", err)
	}
	dd := &DataDiff{SourceRows: srcRows, TargetRows: tgtRows}
	// 任一侧超过阈值即走 count 模式（阈值判断取两侧最大值）
	if srcRows > int64(threshold) || tgtRows > int64(threshold) {
		dd.Mode = "count"
		dd.Equal = srcRows == tgtRows
		dd.SkippedReason = fmt.Sprintf("行数（源 %d / 目标 %d）超过阈值 %d，仅比较行数", srcRows, tgtRows, threshold)
		return dd, nil
	}
	dd.Mode = "rows"
	t.p.TotalRows += srcRows + tgtRows
	t.emit(true)

	// 行比较基于公共列交集：非公共列差异已由结构对比报告，避免整行 key 必然失配的噪声
	srcCols, err := tableColumns(sourceCli, srcTable)
	if err != nil {
		return nil, fmt.Errorf("获取源表列信息失败: %w", err)
	}
	tgtCols, err := tableColumns(targetCli, tgtTable)
	if err != nil {
		return nil, fmt.Errorf("获取目标表列信息失败: %w", err)
	}
	common := commonColumns(srcCols, tgtCols)
	if len(common) == 0 {
		dd.SkippedReason = "无公共列，跳过数据对比"
		return dd, nil
	}

	srcSet, srcSamples, err := loadRowMultiset(ctx, sourceCli, srcTable, common, t)
	if err != nil {
		return nil, fmt.Errorf("读取源表数据失败: %w", err)
	}
	tgtSet, tgtSamples, err := loadRowMultiset(ctx, targetCli, tgtTable, common, t)
	if err != nil {
		return nil, fmt.Errorf("读取目标表数据失败: %w", err)
	}
	missing, extra := multisetDiff(srcSet, tgtSet)
	dd.Missing = sumCounts(missing)
	dd.Extra = sumCounts(extra)
	dd.MissingSamples = collectSamples(missing, srcSamples)
	dd.ExtraSamples = collectSamples(extra, tgtSamples)
	if dd.MissingSamples != nil || dd.ExtraSamples != nil {
		dd.SampleColumns = common
	}
	dd.Equal = dd.Missing == 0 && dd.Extra == 0
	return dd, nil
}

// countTableRows 统计表行数（COUNT 结果在不同驱动下可能是数值/字符串/[]byte，统一按文本解析）
func countTableRows(cli *cydb.DBCli, table string) (int64, error) {
	sql := fmt.Sprintf("SELECT COUNT(*) FROM %s", EscapeTable(cli.DBType(), cli.DBSubType(), table))
	rows, err := cli.DirectQuery(sql)
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, fmt.Errorf("行数查询结果为空")
	}
	for _, v := range rows[0] {
		if v == nil {
			continue
		}
		if n, err := strconv.ParseInt(fmt.Sprint(v), 10, 64); err == nil {
			return n, nil
		}
	}
	return 0, fmt.Errorf("行数解析失败")
}

// commonColumns 两侧公共列（小写归一，按源表列定义顺序，供采样展示按真实列序）
func commonColumns(srcCols, tgtCols []ColumnItem) []string {
	tgtSet := make(map[string]bool, len(tgtCols))
	for _, c := range tgtCols {
		tgtSet[strings.ToLower(c.Name)] = true
	}
	common := make([]string, 0)
	for _, c := range srcCols {
		lc := strings.ToLower(c.Name)
		if tgtSet[lc] {
			common = append(common, lc)
		}
	}
	return common
}

// loadRowMultiset 流式读取全表构建「归一化行 key → 出现次数」多重集，
// 同时记录每个 key 首次出现的行（仅公共列），供差异明细采样
func loadRowMultiset(ctx context.Context, cli *cydb.DBCli, table string, common []string, t *tracker) (map[string]int, map[string]map[string]any, error) {
	set := make(map[string]int)
	rows := make(map[string]map[string]any)
	selectSQL := fmt.Sprintf("SELECT * FROM %s", EscapeTable(cli.DBType(), cli.DBSubType(), table))
	err := cli.ForEachQuery(table, selectSQL, func(rd cydb.RowData) error {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("任务已取消")
		}
		obj, err := rd.AsObject()
		if err != nil {
			return err
		}
		// 列名大小写不敏感：统一小写
		lower := make(map[string]any, len(obj))
		for k, v := range obj {
			lower[strings.ToLower(k)] = v
		}
		key := rowKey(lower, common)
		set[key]++
		if _, ok := rows[key]; !ok {
			sample := make(map[string]any, len(common))
			for _, c := range common {
				sample[c] = lower[c]
			}
			rows[key] = sample
		}
		t.p.DoneRows++
		if t.p.DoneRows%200 == 0 {
			t.emit(false)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return set, rows, nil
}

// rowKey 由公共列（已小写排序）构造行归一化 key
func rowKey(row map[string]any, common []string) string {
	var sb strings.Builder
	for i, c := range common {
		if i > 0 {
			sb.WriteByte(0x1f)
		}
		sb.WriteString(c)
		sb.WriteByte('=')
		sb.WriteString(normalizeValue(row[c]))
	}
	return sb.String()
}

// multisetDiff 多重集差集：missing=源有目标无（key→多出次数），extra=目标有源无
func multisetDiff(src, tgt map[string]int) (missing, extra map[string]int) {
	missing = make(map[string]int)
	extra = make(map[string]int)
	for k, n := range src {
		if m := tgt[k]; n > m {
			missing[k] = n - m
		}
	}
	for k, n := range tgt {
		if m := src[k]; n > m {
			extra[k] = n - m
		}
	}
	return missing, extra
}

func sumCounts(m map[string]int) int {
	n := 0
	for _, c := range m {
		n += c
	}
	return n
}

// collectSamples 差异明细采样：按 key 排序保证确定性，最多取 compareSampleLimit 条
func collectSamples(diff map[string]int, rows map[string]map[string]any) []map[string]any {
	if len(diff) == 0 {
		return nil
	}
	keys := make([]string, 0, len(diff))
	for k := range diff {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	ret := make([]map[string]any, 0, compareSampleLimit)
	for _, k := range keys {
		if len(ret) >= compareSampleLimit {
			break
		}
		if row, ok := rows[k]; ok {
			ret = append(ret, row)
		}
	}
	return ret
}

// ---- 值归一化（防跨库假阳性） ----

var numericPattern = regexp.MustCompile(`^[+-]?(\d+(\.\d*)?|\.\d+)$`)

// 常见时间格式：命中后统一归一为 UTC RFC3339Nano
var timeLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02 15:04:05.999999999",
	"2006-01-02T15:04:05.999999999",
	"2006-01-02 15:04:05",
	"2006-01-02",
}

// normalizeValue 列值归一化为可比较字符串（规则见规划「行归一化规则」）
func normalizeValue(v any) string {
	switch val := v.(type) {
	case nil:
		return "\x00NULL\x00"
	case bool:
		if val {
			return "true"
		}
		return "false"
	case int:
		return strconv.FormatInt(int64(val), 10)
	case int64:
		return strconv.FormatInt(val, 10)
	case uint64:
		return strconv.FormatUint(val, 10)
	case float32:
		return formatFloat(float64(val))
	case float64:
		return formatFloat(val)
	case time.Time:
		return normalizeTime(val)
	case []byte:
		// 可打印 UTF-8 按字符串归一（decimal/text），二进制（如 MySQL bit）hex 编码
		if utf8.Valid(val) && isPrintable(val) {
			return normalizeString(string(val))
		}
		return hex.EncodeToString(val)
	case string:
		return normalizeString(val)
	default:
		return fmt.Sprint(val)
	}
}

// formatFloat 浮点归一：固定有效位数去精度噪声，统一 -0
func formatFloat(f float64) string {
	if f == 0 || math.IsNaN(f) || math.IsInf(f, 0) {
		if f == 0 {
			return "0"
		}
		return fmt.Sprint(f)
	}
	return strconv.FormatFloat(f, 'g', 9, 64)
}

func normalizeTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

// normalizeString 字符串归一：先尝试时间格式，再尝试数值形态（decimal 等），均不命中保持原样
func normalizeString(s string) string {
	trimmed := strings.TrimSpace(s)
	for _, layout := range timeLayouts {
		if tm, err := time.Parse(layout, trimmed); err == nil {
			return normalizeTime(tm)
		}
	}
	if c, ok := canonicalDecimal(trimmed); ok {
		return c
	}
	return s
}

// canonicalDecimal 数值字符串归一为标准十进制（去前导零/尾零、统一负零）；非数值返回 ok=false
func canonicalDecimal(s string) (string, bool) {
	if !numericPattern.MatchString(s) {
		return "", false
	}
	sign := ""
	body := s
	if strings.HasPrefix(body, "-") {
		sign = "-"
		body = body[1:]
	} else if strings.HasPrefix(body, "+") {
		body = body[1:]
	}
	intPart, fracPart := body, ""
	if i := strings.IndexByte(body, '.'); i >= 0 {
		intPart, fracPart = body[:i], body[i+1:]
	}
	intPart = strings.TrimLeft(intPart, "0")
	fracPart = strings.TrimRight(fracPart, "0")
	if intPart == "" {
		intPart = "0"
	}
	out := intPart
	if fracPart != "" {
		out += "." + fracPart
	}
	if out == "0" {
		return "0", true
	}
	return sign + out, true
}

func isPrintable(b []byte) bool {
	for _, r := range string(b) {
		if r == utf8.RuneError || (!unicode.IsPrint(r) && !unicode.IsSpace(r)) {
			return false
		}
	}
	return true
}

// ---- 结果汇总 ----

// buildCompareSummary 汇总对比结论
func buildCompareSummary(tables []CompareTableResult) CompareSummary {
	var s CompareSummary
	s.Total = len(tables)
	for i := range tables {
		tr := &tables[i]
		switch tr.Status {
		case compareStatusSourceOnly:
			s.SourceOnly++
			continue
		case compareStatusTargetOnly:
			s.TargetOnly++
			continue
		}
		structOK := tr.Columns == nil || tr.Columns.Matched
		dataOK := tr.Data == nil || tr.Data.Equal
		if tr.Columns != nil && !tr.Columns.Matched {
			s.StructureDiff++
		}
		if tr.Data != nil && !tr.Data.Equal {
			s.DataDiff++
		}
		if structOK && dataOK {
			s.Matched++
		}
	}
	return s
}

// tableResultDesc 单表结论摘要（进度日志用）
func tableResultDesc(tr *CompareTableResult) string {
	if tr.Status == compareStatusSourceOnly {
		return "仅源库存在"
	}
	if tr.Status == compareStatusTargetOnly {
		return "仅目标库存在"
	}
	parts := []string{}
	if tr.Columns != nil {
		if tr.Columns.Matched {
			parts = append(parts, "结构一致")
		} else {
			parts = append(parts, fmt.Sprintf("结构差异(源独有%d列/目标独有%d列/%d列不一致)",
				len(tr.Columns.SourceOnly), len(tr.Columns.TargetOnly), len(tr.Columns.Different)))
		}
	}
	if tr.Data != nil {
		switch {
		case tr.Data.SkippedReason != "" && tr.Data.Mode == "rows":
			parts = append(parts, tr.Data.SkippedReason)
		case tr.Data.Equal:
			parts = append(parts, fmt.Sprintf("数据一致(%d行)", tr.Data.SourceRows))
		case tr.Data.Mode == "count":
			parts = append(parts, fmt.Sprintf("行数不一致(源%d/目标%d)", tr.Data.SourceRows, tr.Data.TargetRows))
		default:
			parts = append(parts, fmt.Sprintf("数据差异(缺失%d行/多出%d行)", tr.Data.Missing, tr.Data.Extra))
		}
	}
	return strings.Join(parts, ", ")
}
