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
	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cydb/def"
	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cydb/dialect"
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

// RunCompare 执行数据库对比（多库）：解析库对（按 Databases 索引配对，DBMapping 覆盖），
// 逐库循环对比表清单/结构/数据，结果按库分组汇总到 CompareResult.Databases。
// 表按裸表名匹配（大小写不敏感）；Aliases 别名配对优先于同名匹配；行数超阈值仅比较行数。
func RunCompare(ctx context.Context, opts CompareOptions, cb ProgressFunc) (*CompareResult, error) {
	if opts.Source == nil || opts.Target == nil {
		return nil, NewMsgErr(errCmpNoConn)
	}
	threshold := opts.Threshold
	if threshold <= 0 {
		threshold = DefaultCompareThreshold
	}
	// 忽略列归一化（列名大小写不敏感），数据内容对比时排除这些列
	ignore := normalizeIgnoreColumns(opts.IgnoreColumns)
	// 表级忽略列索引（小写源表名 → 列集合）：与全局忽略列合并后对该表生效
	tableIgnore := make(map[string]map[string]bool, len(opts.Aliases))
	for _, a := range opts.Aliases {
		if len(a.IgnoreColumns) == 0 {
			continue
		}
		tableIgnore[strings.ToLower(strings.TrimSpace(a.Source))] = normalizeIgnoreColumns(a.IgnoreColumns)
	}

	dbPairs, err := resolveCompareDBPairs(opts)
	if err != nil {
		return nil, err
	}

	// 清空表结构元数据缓存：对比要求两侧实时结构，避免复用陈旧/他实例缓存
	cydb.FlushTableInfoCache()
	t := newTracker(cb, opts.Lang)

	// 连接走进程级池化（GetOrCreateCli），相同库复用同一实例，程序退出前无需 Close
	sourceCli, err := ConnectPooled(*opts.Source, opts.Source.DBName)
	if err != nil {
		return nil, NewMsgErrf(errCmpSrcConn, err)
	}
	targetCli, err := ConnectPooled(*opts.Target, opts.Target.DBName)
	if err != nil {
		return nil, NewMsgErrf(errCmpTgtConn, err)
	}

	result := &CompareResult{
		Source:    fmt.Sprintf("%s (%s)", compareScopeDB(opts.Source), sourceCli.DBType()),
		Target:    fmt.Sprintf("%s (%s)", compareScopeDB(opts.Target), targetCli.DBType()),
		Databases: make([]CompareDatabaseResult, 0, len(dbPairs)),
		Tables:    []CompareTableResult{},
	}
	for _, dp := range dbPairs {
		srcConn := *opts.Source
		srcConn.DBName, srcConn.Schema = scopeDBValue(opts.Source, dp.SourceDB)
		tgtConn := *opts.Target
		tgtConn.DBName, tgtConn.Schema = scopeDBValue(opts.Target, dp.TargetDB)
		// 多库对比：每个库对必须用绑定了对应库名的独立连接，否则 GetTableInfo 会查错库导致结构/数据张冠李戴
		// 多库对比：每个库对用绑定了对应库名的独立连接（池化复用，不 Close），
		// 否则 GetTableInfo 会查错库导致结构/数据张冠李戴
		srcCli, err := ConnectPooled(srcConn, dp.SourceDB)
		if err != nil {
			return nil, NewMsgErrf(errCmpSrcConnDB, err, dp.SourceDB)
		}
		tgtCli, err := ConnectPooled(tgtConn, dp.TargetDB)
		if err != nil {
			return nil, NewMsgErrf(errCmpTgtConnDB, err, dp.TargetDB)
		}
		dr, err := runCompareDatabase(ctx, &srcConn, &tgtConn, srcCli, tgtCli, dp.SourceDB, dp.TargetDB, opts, ignore, tableIgnore, t)
		if err != nil {
			return nil, err
		}
		result.Databases = append(result.Databases, *dr)
	}

	// 汇总所有库到顶层（兼容字段 + Summary）
	for _, dr := range result.Databases {
		result.Tables = append(result.Tables, dr.Tables...)
		result.Summary = mergeSummary(result.Summary, dr.Summary)
	}
	t.finish()
	t.log(engineTextsFor(t.lang).cmpDone,
		len(result.Databases), result.Summary.Total, result.Summary.Matched, result.Summary.SourceOnly,
		result.Summary.TargetOnly, result.Summary.StructureDiff, result.Summary.DataDiff)
	return result, nil
}

// scopeDBValue 根据连接类型（oracle 的库= schema）返回应设置到 DBName/Schema 字段的值对
func scopeDBValue(conn *DBConnInfo, db string) (dbName string, schema string) {
	if strings.EqualFold(conn.Type, "oracle") {
		return "", db
	}
	return db, ""
}

// resolveCompareDBPairs 解析对比库对：优先按 Databases 索引一一配对；
// 未提供时回退到 opts.DBMapping（源库→目标库），再回退到源连接库=目标连接库（单库）
func resolveCompareDBPairs(opts CompareOptions) ([]CompareDBPair, error) {
	if len(opts.Databases) > 0 {
		pairs := make([]CompareDBPair, 0, len(opts.Databases))
		for _, p := range opts.Databases {
			srcDB := p.SourceDB
			tgtDB := p.TargetDB
			if mapped, ok := opts.DBMapping[srcDB]; ok && tgtDB == "" {
				tgtDB = mapped
			}
			pairs = append(pairs, CompareDBPair{SourceDB: srcDB, TargetDB: tgtDB})
		}
		return pairs, nil
	}
	if len(opts.DBMapping) > 0 {
		pairs := make([]CompareDBPair, 0, len(opts.DBMapping))
		for src, tgt := range opts.DBMapping {
			pairs = append(pairs, CompareDBPair{SourceDB: src, TargetDB: tgt})
		}
		sort.Slice(pairs, func(i, j int) bool { return pairs[i].SourceDB < pairs[j].SourceDB })
		return pairs, nil
	}
	srcDB := compareScopeDB(opts.Source)
	tgtDB := compareScopeDB(opts.Target)
	if srcDB == "" || tgtDB == "" {
		return nil, NewMsgErr(errCmpNoDB)
	}
	return []CompareDBPair{{SourceDB: srcDB, TargetDB: tgtDB}}, nil
}

// runCompareDatabase 对比单库对（源库 srcDB ↔ 目标库 tgtDB），返回该库分组结果
func runCompareDatabase(ctx context.Context, srcConn, tgtConn *DBConnInfo, sourceCli, targetCli *cydb.DBCli,
	srcDB, tgtDB string, opts CompareOptions, ignore map[string]bool, tableIgnore map[string]map[string]bool, t *tracker) (*CompareDatabaseResult, error) {
	threshold := opts.Threshold
	if threshold <= 0 {
		threshold = DefaultCompareThreshold
	}
	srcTables, err := listCompareTables(sourceCli, srcConn, opts.Tables)
	if err != nil {
		return nil, NewMsgErrf(errCmpSrcListTables, err, srcDB)
	}
	tgtTables, err := listCompareTables(targetCli, tgtConn, opts.Tables)
	if err != nil {
		return nil, NewMsgErrf(errCmpTgtListTables, err, tgtDB)
	}

	pairs, err := buildComparePairs(srcTables, tgtTables, opts.Aliases, srcDB)
	if err != nil {
		return nil, err
	}
	t.p.TotalUnits += len(pairs)
	t.log(engineTextsFor(t.lang).cmpStart, srcDB, tgtDB, len(pairs), threshold)

	dr := &CompareDatabaseResult{SourceDB: srcDB, TargetDB: tgtDB, Tables: []CompareTableResult{}}
	for _, pair := range pairs {
		if err := ctx.Err(); err != nil {
			return nil, NewMsgErr(errCancelled)
		}
		t.p.CurrentTable = pair.Name
		t.emit(true)

		tr := CompareTableResult{
			Name:       pair.Name,
			SourceName: pair.SourceName,
			TargetName: pair.TargetName,
			SourceDB:   srcDB,
			TargetDB:   tgtDB,
			Status:     pair.Status,
		}
		if pair.Status == compareStatusBoth {
			structDiff := false
			if !opts.DataOnly {
				cols, err := compareColumns(sourceCli, targetCli, pair.SourceName, pair.TargetName)
				if err != nil {
					t.log(engineTextsFor(t.lang).cmpStructFail, pair.Name, err)
				} else {
					tr.Columns = cols
					structDiff = !cols.Matched
				}
			}
			if !opts.StructureOnly {
				if structDiff && !opts.ForceData {
					// 结构不一致时默认不对比数据（列定义都不同，数据对比意义有限）；--force-data 可强制
					tr.Data = &DataDiff{Mode: "skipped", SkippedReason: engineTextsFor(t.lang).skipStructDiff}
				} else {
					// 表级忽略列与全局合并；无表级配置时直接用全局集合（避免逐表拷贝）
					tblIgnore := ignore
					if extra, ok := tableIgnore[strings.ToLower(pair.SourceName)]; ok {
						tblIgnore = make(map[string]bool, len(ignore)+len(extra))
						for c := range ignore {
							tblIgnore[c] = true
						}
						for c := range extra {
							tblIgnore[c] = true
						}
					}
					data, err := compareTableData(ctx, sourceCli, targetCli, pair.SourceName, pair.TargetName, threshold, tblIgnore, t)
					if err != nil {
						t.log(engineTextsFor(t.lang).cmpDataFail, pair.Name, err)
					} else {
						tr.Data = data
					}
				}
			}
		}
		dr.Tables = append(dr.Tables, tr)
		t.p.DoneUnits++
		t.log("%s: %s", pair.Name, tableResultDesc(&tr, t.lang))
	}
	dr.Summary = buildCompareSummary(dr.Tables)
	return dr, nil
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
// "库.表" 限定名条目：库前缀匹配时按裸名比较；前缀不匹配时降级按裸名比较
// （对比作用域为单个库对，目标独有表的限定前缀可能与源侧库名不同，不能因此失配）
func filterTablesBare(all []string, wanted []string, db string) []string {
	if wanted == nil {
		return all
	}
	bare := make(map[string]bool, len(wanted))
	for _, w := range wanted {
		w = strings.TrimSpace(w)
		if _, tbl, ok := splitQualifiedName(w); ok {
			bare[strings.ToLower(tbl)] = true
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
// Source 支持限定名 "库.表"，仅当库名与当前 sourceDB 匹配时生效；裸表名在所有库生效（兼容旧配置）。
// 别名映射的表在对应库不存在时降级为 source_only/target_only，两侧都不存在则跳过
func buildComparePairs(srcTables, tgtTables []string, aliases []TableAlias, sourceDB string) ([]comparePair, error) {
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
		srcRaw := strings.TrimSpace(a.Source)
		tg := strings.ToLower(strings.TrimSpace(a.Target))
		if srcRaw == "" || tg == "" {
			continue
		}
		// Source 支持限定名 "库.表"，仅匹配当前源库时生效
		srcKey := srcRaw
		if db, tbl, ok := splitQualifiedName(srcRaw); ok {
			if !strings.EqualFold(db, sourceDB) {
				continue
			}
			srcKey = tbl
		}
		s := strings.ToLower(srcKey)
		if seenSrc[s] || seenTgt[tg] {
			return nil, NewMsgErr(errCmpAliasDup, a.Source, a.Target)
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

// compareColumns 对比两侧表的列结构（列名大小写不敏感匹配，类型按各方言归一化后对比）
// srcTable/tgtTable 为实际表名（cli 已绑定具体库，裸表名即可正确取列）
func compareColumns(sourceCli, targetCli *cydb.DBCli, srcTable, tgtTable string) (*ColumnDiff, error) {
	srcCols, err := tableColumns(sourceCli, srcTable)
	if err != nil {
		return nil, NewMsgErrf(errCmpSrcCols, err)
	}
	tgtCols, err := tableColumns(targetCli, tgtTable)
	if err != nil {
		return nil, NewMsgErrf(errCmpTgtCols, err)
	}
	return diffColumns(srcCols, tgtCols, typeNormalizer(sourceCli), typeNormalizer(targetCli)), nil
}

// typeNormalizer 返回 cli 方言的列类型归一化函数（对比语义：如 MySQL 剥离整数显示宽度，
// bigint(20) ≡ bigint），与 cydb 自动迁移共用同一实现；方言不可得时退化为恒等
func typeNormalizer(cli *cydb.DBCli) func(string) string {
	if cli == nil {
		return func(dt string) string { return dt }
	}
	return typeNormalizerByDBType(cli.DBType())
}

// typeNormalizerByDBType 根据数据库类型返回列类型归一化函数（不依赖 live cli）
func typeNormalizerByDBType(dbType string) func(string) string {
	md, ok := dialect.GetMigrationDialect(dbType)
	if !ok {
		return func(dt string) string { return dt }
	}
	return func(dt string) string { return string(md.NormalizeColumnType(def.StandardFieldType(dt))) }
}

func tableColumns(cli *cydb.DBCli, table string) ([]ColumnItem, error) {
	info, err := cli.GetTableInfo(table)
	if err != nil {
		return nil, err
	}
	norm := typeNormalizer(cli)
	cols := info.GetColumns()
	ret := make([]ColumnItem, 0, len(cols))
	for _, col := range cols {
		dt := col.GetOrginalDataType()
		ret = append(ret, ColumnItem{
			Name:           col.GetName(),
			DataType:       dt,
			NormalizedType: norm(dt), // 写入时固化归一类型，diff 优先用此比对
			Nullable:       !col.IsNotNull(),
			PrimaryKey:     col.IsPrimaryKey(),
		})
	}
	return ret, nil
}

// diffColumns 列级差异计算（纯函数）：类型按 normSrc/normTgt 归一化后的字符串对比
// （剩余差异为参考性结论），列项保留原始类型供明细展示；normalizer 为 nil 时不归一化
func diffColumns(srcCols, tgtCols []ColumnItem, normSrc, normTgt func(string) string) *ColumnDiff {
	if normSrc == nil {
		normSrc = func(dt string) string { return dt }
	}
	if normTgt == nil {
		normTgt = normSrc
	}
	srcMap := make(map[string]ColumnItem, len(srcCols))
	for _, c := range srcCols {
		srcMap[strings.ToLower(c.Name)] = c
	}
	tgtMap := make(map[string]ColumnItem, len(tgtCols))
	for _, c := range tgtCols {
		tgtMap[strings.ToLower(c.Name)] = c
	}
	d := &ColumnDiff{SourceOnly: []ColumnItem{}, TargetOnly: []ColumnItem{}, Different: []ColumnItemDiff{}}
	// 优先用列项已固化的 NormalizedType 比对（写入时按方言归一，无需运行时推断）；
	// 若未固化（兼容历史数据）则退回 normSrc/normTgt 实时归一
	normOf := func(c ColumnItem, fallback func(string) string) string {
		if c.NormalizedType != "" {
			return c.NormalizedType
		}
		return fallback(c.DataType)
	}
	for _, c := range srcCols {
		tc, ok := tgtMap[strings.ToLower(c.Name)]
		if !ok {
			d.SourceOnly = append(d.SourceOnly, c)
			continue
		}
		if normOf(c, normSrc) != normOf(tc, normTgt) || c.Nullable != tc.Nullable || c.PrimaryKey != tc.PrimaryKey {
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

// normalizeIgnoreColumns 忽略列归一化：去空白、小写（列名大小写不敏感）
func normalizeIgnoreColumns(cols []string) map[string]bool {
	ret := make(map[string]bool, len(cols))
	for _, c := range cols {
		if c = strings.TrimSpace(c); c != "" {
			ret[strings.ToLower(c)] = true
		}
	}
	return ret
}

// compareTableData 单表数据对比：超阈值仅比较行数；有主键时走 PK 模式
// （主键判断有无：缺失/多出，内容对比判断变化），无主键回退整行归一化多重集比较；
// ignore 为数据内容对比忽略的列（小写归一）
func compareTableData(ctx context.Context, sourceCli, targetCli *cydb.DBCli, srcTable, tgtTable string, threshold int, ignore map[string]bool, t *tracker) (*DataDiff, error) {
	srcRows, err := countTableRows(sourceCli, srcTable)
	if err != nil {
		return nil, NewMsgErrf(errCmpSrcRows, err)
	}
	tgtRows, err := countTableRows(targetCli, tgtTable)
	if err != nil {
		return nil, NewMsgErrf(errCmpTgtRows, err)
	}
	dd := &DataDiff{SourceRows: srcRows, TargetRows: tgtRows}
	// 任一侧超过阈值即走 count 模式（阈值判断取两侧最大值）
	if srcRows > int64(threshold) || tgtRows > int64(threshold) {
		dd.Mode = "count"
		dd.Equal = srcRows == tgtRows
		dd.SkippedReason = fmt.Sprintf(engineTextsFor(t.lang).skipRowsThresh, srcRows, tgtRows, threshold)
		return dd, nil
	}
	dd.Mode = "rows"
	t.p.TotalRows += srcRows + tgtRows
	t.emit(true)

	// 行比较基于公共列交集：非公共列差异已由结构对比报告，避免整行 key 必然失配的噪声
	srcCols, err := tableColumns(sourceCli, srcTable)
	if err != nil {
		return nil, NewMsgErrf(errCmpSrcCols, err)
	}
	tgtCols, err := tableColumns(targetCli, tgtTable)
	if err != nil {
		return nil, NewMsgErrf(errCmpTgtCols, err)
	}
	common := commonColumns(srcCols, tgtCols)
	if len(common) == 0 {
		dd.SkippedReason = engineTextsFor(t.lang).skipNoCols
		return dd, nil
	}
	// 内容列 = 公共列 − 忽略列（主键列不可忽略，否则无法判断有无）
	pk := matchPKColumns(sourceCli, targetCli, srcTable, tgtTable, common)
	content := make([]string, 0, len(common))
	for _, c := range common {
		if !ignore[c] || isPKColumn(c, pk) {
			content = append(content, c)
		}
	}
	if len(content) == 0 {
		dd.SkippedReason = engineTextsFor(t.lang).skipAllIgnored
		return dd, nil
	}

	// PK 模式：主键判断有无（missing/extra），内容对比判断变化（changed）
	if len(pk) > 0 {
		if err := compareByKey(ctx, sourceCli, targetCli, srcTable, tgtTable, dd, content, pk, t); err != nil {
			return nil, err
		}
		return dd, nil
	}

	// 无主键回退：整行归一化多重集比较（无法区分「变化」与「缺失+多出」）
	srcSet, srcSamples, err := loadRowMultiset(ctx, sourceCli, srcTable, content, t)
	if err != nil {
		return nil, NewMsgErrf(errCmpSrcData, err)
	}
	tgtSet, tgtSamples, err := loadRowMultiset(ctx, targetCli, tgtTable, content, t)
	if err != nil {
		return nil, NewMsgErrf(errCmpTgtData, err)
	}
	missing, extra := multisetDiff(srcSet, tgtSet)
	dd.Missing = sumCounts(missing)
	dd.Extra = sumCounts(extra)
	dd.MissingSamples = collectSamples(missing, srcSamples)
	dd.ExtraSamples = collectSamples(extra, tgtSamples)
	if dd.MissingSamples != nil || dd.ExtraSamples != nil {
		dd.SampleColumns = content
	}
	dd.Equal = dd.Missing == 0 && dd.Extra == 0
	return dd, nil
}

// matchPKColumns 两侧主键集一致（列数/顺序/列名大小写不敏感）且均在公共列中时返回主键列（小写），
// 否则返回空（回退整行比较）；复合主键按约束定义顺序整体作为存在性键
func matchPKColumns(sourceCli, targetCli *cydb.DBCli, srcTable, tgtTable string, common []string) []string {
	srcPKs, err1 := sourceCli.GetPrimaryKeys(srcTable)
	tgtPKs, err2 := targetCli.GetPrimaryKeys(tgtTable)
	if err1 != nil || err2 != nil || len(srcPKs) == 0 || len(srcPKs) != len(tgtPKs) {
		return nil
	}
	commonSet := make(map[string]bool, len(common))
	for _, c := range common {
		commonSet[c] = true
	}
	pk := make([]string, 0, len(srcPKs))
	for i, c := range srcPKs {
		lc := strings.ToLower(c)
		if lc != strings.ToLower(tgtPKs[i]) || !commonSet[lc] {
			return nil
		}
		pk = append(pk, lc)
	}
	return pk
}

func isPKColumn(col string, pk []string) bool {
	for _, c := range pk {
		if c == col {
			return true
		}
	}
	return false
}

// compareByKey PK 模式数据对比：主键判断有无（missing=源有目标无，extra=目标有源无），
// 内容列逐列归一化对比判断变化（changed），结果写入 dd
func compareByKey(ctx context.Context, sourceCli, targetCli *cydb.DBCli, srcTable, tgtTable string, dd *DataDiff, content, pk []string, t *tracker) error {
	dd.KeyColumns = pk
	srcRows, err := loadRowsByPK(ctx, sourceCli, srcTable, pk, content, t)
	if err != nil {
		return NewMsgErrf(errCmpSrcData, err)
	}
	tgtRows, err := loadRowsByPK(ctx, targetCli, tgtTable, pk, content, t)
	if err != nil {
		return NewMsgErrf(errCmpTgtData, err)
	}

	missingKeys := make([]string, 0)
	extraKeys := make([]string, 0)
	changedKeys := make([]string, 0)
	changedDiffs := make(map[string][]ValueDiff)
	for k, srcRow := range srcRows {
		tgtRow, ok := tgtRows[k]
		if !ok {
			missingKeys = append(missingKeys, k)
			continue
		}
		if diffs := diffRowValues(srcRow, tgtRow, content); len(diffs) > 0 {
			changedKeys = append(changedKeys, k)
			changedDiffs[k] = diffs
		}
	}
	for k := range tgtRows {
		if _, ok := srcRows[k]; !ok {
			extraKeys = append(extraKeys, k)
		}
	}

	dd.Missing = len(missingKeys)
	dd.Extra = len(extraKeys)
	dd.Changed = len(changedKeys)
	dd.MissingSamples = collectKeySamples(missingKeys, srcRows)
	dd.ExtraSamples = collectKeySamples(extraKeys, tgtRows)
	if len(changedKeys) > 0 {
		sort.Strings(changedKeys)
		samples := make([]ChangedRow, 0, compareSampleLimit)
		for _, k := range changedKeys {
			if len(samples) >= compareSampleLimit {
				break
			}
			key := make(map[string]any, len(pk))
			for _, c := range pk {
				key[c] = srcRows[k][c]
			}
			samples = append(samples, ChangedRow{Key: key, Diffs: changedDiffs[k]})
		}
		dd.ChangedSamples = samples
	}
	if dd.MissingSamples != nil || dd.ExtraSamples != nil || dd.ChangedSamples != nil {
		dd.SampleColumns = content
	}
	dd.Equal = dd.Missing == 0 && dd.Extra == 0 && dd.Changed == 0
	return nil
}

// loadRowsByPK 流式读取全表，以主键归一化值为索引（主键唯一，无需计数），
// 保留 content 列行数据供采样展示与内容对比
func loadRowsByPK(ctx context.Context, cli *cydb.DBCli, table string, pk, content []string, t *tracker) (map[string]map[string]any, error) {
	rows := make(map[string]map[string]any)
	selectSQL := fmt.Sprintf("SELECT * FROM %s", EscapeTable(cli.DBType(), cli.DBSubType(), table))
	err := cli.ForEachQuery(table, selectSQL, func(rd cydb.RowData) error {
		if err := ctx.Err(); err != nil {
			return NewMsgErr(errCancelled)
		}
		obj, err := rd.AsObject()
		if err != nil {
			return err
		}
		lower := make(map[string]any, len(obj))
		for k, v := range obj {
			lower[strings.ToLower(k)] = v
		}
		key := pkKey(lower, pk)
		if _, ok := rows[key]; !ok {
			sample := make(map[string]any, len(content))
			for _, c := range content {
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
		return nil, err
	}
	return rows, nil
}

// pkKey 由主键列（含复合主键）构造存在性键
func pkKey(row map[string]any, pk []string) string {
	var sb strings.Builder
	for i, c := range pk {
		if i > 0 {
			sb.WriteByte(0x1f)
		}
		sb.WriteString(normalizeValue(row[c]))
	}
	return sb.String()
}

// diffRowValues 逐列归一化对比，返回取值不一致的列（主键列在匹配行中必然一致，无需排除）
func diffRowValues(src, tgt map[string]any, content []string) []ValueDiff {
	var diffs []ValueDiff
	for _, c := range content {
		if normalizeValue(src[c]) != normalizeValue(tgt[c]) {
			diffs = append(diffs, ValueDiff{Column: c, Source: src[c], Target: tgt[c]})
		}
	}
	return diffs
}

// collectKeySamples PK 模式差异采样：按主键键排序保证确定性，最多取 compareSampleLimit 条
func collectKeySamples(keys []string, rows map[string]map[string]any) []map[string]any {
	if len(keys) == 0 {
		return nil
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

// countTableRows 统计表行数（COUNT 结果在不同驱动下可能是数值/字符串/[]byte，统一按文本解析）
func countTableRows(cli *cydb.DBCli, table string) (int64, error) {
	sql := fmt.Sprintf("SELECT COUNT(*) FROM %s", EscapeTable(cli.DBType(), cli.DBSubType(), table))
	rows, err := cli.DirectQuery(sql)
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, NewMsgErr(errCmpRowCountEmpty)
	}
	for _, v := range rows[0] {
		if v == nil {
			continue
		}
		if n, err := strconv.ParseInt(fmt.Sprint(v), 10, 64); err == nil {
			return n, nil
		}
	}
	return 0, NewMsgErr(errCmpRowCountParse)
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
			return NewMsgErr(errCancelled)
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
		// 跳过类（结构不一致/无公共列等）不计入数据差异，避免与结构差异重复计数
		if tr.Data != nil && !tr.Data.Equal && tr.Data.Mode != "skipped" {
			s.DataDiff++
		}
		if structOK && dataOK {
			s.Matched++
		}
	}
	return s
}

// mergeSummary 累加两个汇总计数（多库对比顶层汇总用）
func mergeSummary(a, b CompareSummary) CompareSummary {
	return CompareSummary{
		Total:         a.Total + b.Total,
		Matched:       a.Matched + b.Matched,
		SourceOnly:    a.SourceOnly + b.SourceOnly,
		TargetOnly:    a.TargetOnly + b.TargetOnly,
		StructureDiff: a.StructureDiff + b.StructureDiff,
		DataDiff:      a.DataDiff + b.DataDiff,
	}
}

// tableResultDesc 单表结论摘要（进度日志用）
func tableResultDesc(tr *CompareTableResult, lang string) string {
	txt := engineTextsFor(lang)
	if tr.Status == compareStatusSourceOnly {
		return txt.descSrcOnly
	}
	if tr.Status == compareStatusTargetOnly {
		return txt.descTgtOnly
	}
	parts := []string{}
	if tr.Columns != nil {
		if tr.Columns.Matched {
			parts = append(parts, txt.descStructSame)
		} else {
			parts = append(parts, fmt.Sprintf(txt.descStructDiff,
				len(tr.Columns.SourceOnly), len(tr.Columns.TargetOnly), len(tr.Columns.Different)))
		}
	}
	if tr.Data != nil {
		switch {
		case tr.Data.SkippedReason != "" && tr.Data.Mode != "count":
			parts = append(parts, tr.Data.SkippedReason)
		case tr.Data.Equal:
			parts = append(parts, fmt.Sprintf(txt.descDataSame, tr.Data.SourceRows))
		case tr.Data.Mode == "count":
			parts = append(parts, fmt.Sprintf(txt.descRowDiff, tr.Data.SourceRows, tr.Data.TargetRows))
		default:
			parts = append(parts, fmt.Sprintf(txt.descDataDiff, tr.Data.Missing, tr.Data.Extra))
		}
	}
	return strings.Join(parts, ", ")
}
