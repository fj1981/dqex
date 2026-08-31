package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/fj1981/infrakit/pkg/cydb"
	"github.com/fj1981/infrakit/pkg/cydb/dialect"
)

// 数据包条目类型（JSON 序列化值与 tl-env DataHolder/DataStruc 格式兼容，契约冻结）
const (
	DataEntryCreateTable = 0 // 建表：SQL 为 CREATE TABLE，回滚为 DROP TABLE（仅当表原本不存在时生成）
	DataEntryUpsertData  = 1 // 行数据：按 PK 幂等 upsert（先 DELETE 再 REPLACE），回滚为旧行还原
	DataEntryExecSQL     = 2 // 成对 SQL：Data 中每项为 {"SQL":"...", "rollback":"..."}，逐对执行并收集回滚
)

// rollbackJSONKey 成对 SQL 条目中回滚语句的键名（兼容 tl-env 格式：map 仅有非 rollback 键时为执行语句）
const rollbackJSONKey = "rollback"

// DataEntry 数据包条目（对应 tl-env DataStruc，JSON 字段名兼容）
type DataEntry struct {
	Type        int                      `json:"type"`
	Table       string                   `json:"table,omitempty"`
	PK          []string                 `json:"pk,omitempty"`
	SQL         string                   `json:"sql,omitempty"`
	RollbackSQL string                   `json:"rollback_sql,omitempty"`
	Data        []map[string]interface{} `json:"data,omitempty"`
}

// DataPackage 数据交换包：dqex 数据格式契约（JSON 结构与 tl-env DataHolder 兼容，
// 格式冻结后写入 docs/library.md）。适用于业务配置类中小数据量的精确导入与回滚，
// 不适用于大批量数据（整包驻留内存）。
type DataPackage struct {
	DB       string         `json:"db"`
	Entries  []*DataEntry   `json:"datas"`
	MapIndex map[string]int `json:"index"`

	// mergeIndex (表,类型) 复合键 -> 条目下标（内部合并索引，不入 JSON；
	// LoadDataPackage 重建，Add 懒初始化）
	mergeIndex map[string]int
}

// LoadDataPackage 从 JSON 字节解析数据包。
// 使用 UseNumber：数字解码为 json.Number（string kind）而非 float64——雪花 ID 等
// 超过 float64 尾数精度（2^53）的整数主键往返不失真，且不会以科学计数法进入回滚
// SQL 的 IN 匹配；json.Number 经 %v 输出原文，参数化写入时由 database/sql 转为
// string 落库（数字列隐式转换），行为安全。
// datas 数组中的 null 条目（外部不可信输入）直接丢弃，防止应用时 nil 解引用 panic。
func LoadDataPackage(data []byte) (*DataPackage, error) {
	var pkg DataPackage
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&pkg); err != nil {
		return nil, err
	}
	// 过滤 null 条目（{"datas":[null]} 等外部构造的包），剩余条目保持原顺序
	n := 0
	for _, e := range pkg.Entries {
		if e != nil {
			pkg.Entries[n] = e
			n++
		}
	}
	pkg.Entries = pkg.Entries[:n]
	if pkg.MapIndex == nil {
		pkg.MapIndex = map[string]int{}
	}
	// 重建合并索引（宿主经 DataPreparer 修改包时 Add 需合并进既有条目）
	pkg.mergeIndex = make(map[string]int, len(pkg.Entries))
	for i, e := range pkg.Entries {
		if e == nil {
			continue
		}
		k := e.Table + "\x00" + strconv.Itoa(e.Type)
		if _, ok := pkg.mergeIndex[k]; !ok {
			pkg.mergeIndex[k] = i
		}
	}
	return &pkg, nil
}

// Add 追加条目：同表**同类型**数据合并到既有条目；同表不同类型（建表 type=0 与
// 数据 type=1）为独立条目，按添加顺序应用（先建表后写数据）。
// 合并索引按 (表,类型) 复合键——MapIndex 单键在"先建表后写数据"的默认导出路径下
// 恒指向 type=0 条目，数据条目将无法合并（每行独立成条，JSON 膨胀）。
// MapIndex 契约不变：仍指向该表首个条目（兼容 tl-env 格式）。
// 直接构造 DataPackage（未经 LoadDataPackage）时索引为 nil，此处懒初始化兜底
func (p *DataPackage) Add(table string, entry DataEntry) {
	entry.Table = table
	if p.MapIndex == nil {
		p.MapIndex = map[string]int{}
	}
	if p.mergeIndex == nil {
		p.mergeIndex = map[string]int{}
	}
	k := table + "\x00" + strconv.Itoa(entry.Type)
	if i, ok := p.mergeIndex[k]; ok && i < len(p.Entries) {
		p.Entries[i].Data = append(p.Entries[i].Data, entry.Data...)
		return
	}
	p.Entries = append(p.Entries, &entry)
	p.mergeIndex[k] = len(p.Entries) - 1
	if _, ok := p.MapIndex[table]; !ok {
		p.MapIndex[table] = len(p.Entries) - 1
	}
}

// GetTable 取表对应条目
func (p *DataPackage) GetTable(table string) (*DataEntry, bool) {
	if i, ok := p.MapIndex[table]; ok && i < len(p.Entries) {
		return p.Entries[i], true
	}
	return nil, false
}

// DataApplyResult 数据包应用结果
type DataApplyResult struct {
	Stmts         int64    // 执行的语句数（DDL/REPLACE/成对 SQL）
	RollbackSQL   []string // 回滚 SQL（按执行顺序排列，整体回放即恢复到应用前状态）
	SkippedTables []string // 跳过的表（无主键等无法精确回滚的场景，宿主侧告警）
	Unrollback    []string // 执行了但无法生成精确回滚的语句（宿主侧告警；见 tryColumnRollback）
}

// ApplyDataPackage 在单库连接上应用数据包，并生成行级回滚 SQL。
// 回滚语义：基于应用时点的旧值快照，适用于"紧随导入的撤销"，不适用于长期 revert。
// connKey 用于审计钩子归属。无主键表的数据条目跳过并记录在 SkippedTables。
// 事务语义按方言：PG/gaussdb/kingbase 的 DDL 可回滚（真单事务）；MySQL 系与 Oracle
// 的 DDL 隐式提交，事务仅覆盖数据语句（建表失败时已建的表不随事务撤销，需回滚产物兜底）。
func ApplyDataPackage(ctx context.Context, cli *cydb.DBCli, pkg *DataPackage, connKey string) (*DataApplyResult, error) {
	res := &DataApplyResult{}
	// 库名前缀清理清单：包内库名 + 目标连接库名（不再依赖硬编码库名清单）。
	// 外部生成的 SQL 常带源环境 `db.table` / db.table 前缀，导入目标库名可能已变，
	// 前缀不清理在 PG 上直接报错、在 MySQL 上会指错库。
	knownDBs := pkgKnownDBs(cli, pkg)
	dbType, dbSub := cli.DBType(), cli.DBSubType()
	isMySQL := strings.EqualFold(dbType, "mysql")
	// 方言化标识符引用（MySQL `x` / PG "x" / Oracle "X"），回滚语句随执行方言生成
	qt := func(t string) string { return EscapeTable(dbType, dbSub, t) }
	qc := func(c string) string { return EscapeColumn(dbType, dbSub, c) }
	strip := func(sql string) string { return stripSQLDBPrefix(isMySQL, sql, knownDBs...) }
	err := cli.WithTransactionContext(ctx, func(tx cydb.DatabaseClient) error {
		for _, e := range pkg.Entries {
			if err := ctx.Err(); err != nil {
				return NewMsgErr(errCancelled)
			}
			switch e.Type {
			case DataEntryCreateTable:
				exist, err := tx.IsTableExist(e.Table)
				if err != nil {
					return err
				}
				if exist {
					continue // 表已存在：不重复建表，也不生成 DROP 回滚（不能回滚掉既有表）
				}
				exec := strip(e.SQL)
				start := time.Now()
				affected, err := tx.Execute(exec)
				if err != nil {
					fireQueryHook(ctx, connKey, exec, start, -1)
					return err
				}
				res.Stmts++
				if strings.EqualFold(dbType, "oracle") {
					// Oracle 无 DROP TABLE IF EXISTS；回滚场景表由本包刚建、必然存在
					res.RollbackSQL = append(res.RollbackSQL, fmt.Sprintf("DROP TABLE %s;", qt(e.Table)))
				} else {
					res.RollbackSQL = append(res.RollbackSQL, fmt.Sprintf("DROP TABLE IF EXISTS %s;", qt(e.Table)))
				}
				fireQueryHook(ctx, connKey, exec, start, affected)

			case DataEntryUpsertData:
				if len(e.PK) == 0 {
					// 无主键表无法精确定位旧行（B6 策略）：跳过并告警，不做非精确覆盖
					res.SkippedTables = append(res.SkippedTables, e.Table)
					continue
				}
				// 主键去重
				seen := map[string]struct{}{}
				ids := make([][]string, 0, len(e.Data))
				for _, row := range e.Data {
					k := pkKey(row, e.PK)
					if _, ok := seen[k]; ok {
						continue
					}
					seen[k] = struct{}{}
					ids = append(ids, pkValues(row, e.PK))
				}
				if len(ids) == 0 {
					continue
				}
				pkCols := make([]string, len(e.PK))
				for i, c := range e.PK {
					pkCols[i] = qc(c)
				}
				pkField := strings.Join(pkCols, ", ")
				// PK 值来自外部数据包（不可信输入），拼入 IN 前必须转义（quoteInList）
				inClause := quoteInList(dbType, ids)
				// Oracle 单条 IN 上限 1000 行表达式：数据包契约限定中小数据量，
				// 超限请拆分条目（导入/导出侧均不自动分批）
				deleteSQL := fmt.Sprintf("DELETE FROM %s WHERE (%s) IN (%s);", qt(e.Table), pkField, inClause)
				// 回滚第一步：清空本次导入行（回滚产物按收集顺序回放，DELETE 必须先于旧行 REPLACE 收集）
				res.RollbackSQL = append(res.RollbackSQL, deleteSQL)
				// 回滚第二步：读取旧行生成还原 SQL（REPLACE 旧行 / 无旧行则仅靠上面的 DELETE）；
				// 旧行还原经 RowData.GetReplaceSql 按方言生成（MySQL REPLACE / PG·Oracle upsert）
				selectSQL := fmt.Sprintf("SELECT * FROM %s WHERE (%s) IN (%s)", qt(e.Table), pkField, inClause)
				err := tx.ForEachQuery(e.Table, selectSQL, func(row cydb.RowData) error {
					replaceSQL, err := row.GetReplaceSql()
					if err != nil {
						return err
					}
					res.RollbackSQL = append(res.RollbackSQL, replaceSQL)
					return nil
				})
				if err != nil {
					return err
				}
				// 应用：DELETE 后 REPLACE（幂等 upsert）；受影响行数如实传入审计钩子
				start := time.Now()
				affected, err := tx.Execute(deleteSQL[:len(deleteSQL)-1])
				if err != nil {
					fireQueryHook(ctx, connKey, deleteSQL[:len(deleteSQL)-1], start, -1)
					return err
				}
				res.Stmts++
				fireQueryHook(ctx, connKey, deleteSQL[:len(deleteSQL)-1], start, affected)
				for _, row := range e.Data {
					start := time.Now()
					affected, err := tx.Replace(e.Table, row)
					if err != nil {
						fireQueryHook(ctx, connKey, "REPLACE INTO "+qt(e.Table), start, -1)
						return err
					}
					res.Stmts++
					fireQueryHook(ctx, connKey, "REPLACE INTO "+qt(e.Table), start, affected)
				}

			case DataEntryExecSQL:
				// 成对 SQL：契约约定每项 map 仅一个执行语句键（值可为回滚语句字符串或
				// rollback 键）。map 迭代顺序随机，多执行键时按字典序确定执行顺序（可复现）
				for _, v := range e.Data {
					keys := make([]string, 0, len(v))
					for k := range v {
						if k == rollbackJSONKey {
							continue
						}
						keys = append(keys, k)
					}
					sort.Strings(keys)
					for _, sql := range keys {
						exec := strip(sql)
						// 回滚值必须是字符串（非字符串如数字/bool 视为缺失，防止 "0"/"false" 被当 SQL）
						rb, _ := v[sql].(string)
						rb = strings.TrimSpace(rb)
						if rb == "" {
							// 兼容 rollback 键承载回滚值的形态（按注释契约构造的数据；
							// tl-env 实际数据为"键=执行语句, 值=回滚"形态，不走此分支）
							rb, _ = v[rollbackJSONKey].(string)
							rb = strings.TrimSpace(rb)
						}
						if rb == "" {
							// 回滚为空：从方言权威建表 DDL 提取原列定义生成还原语句（ALTER 列变更场景）；
							// 仍无法生成则显式记入 Unrollback 告警（不再静默丢回滚）
							rb = tryColumnRollback(cli, tx, exec)
							if rb == "" {
								res.Unrollback = append(res.Unrollback, exec)
							}
						} else {
							rb = strip(rb)
						}
						start := time.Now()
						affected, err := tx.Execute(exec)
						if err != nil {
							fireQueryHook(ctx, connKey, exec, start, -1)
							return err
						}
						res.Stmts++
						fireQueryHook(ctx, connKey, exec, start, affected)
						if rb != "" {
							res.RollbackSQL = append(res.RollbackSQL, rb)
						}
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		// 失败时仍返回已收集的部分结果（回滚/告警）：MySQL 系与 Oracle 的 DDL 隐式提交，
		// 事务回滚不撤销已建表，调用方可据此兜底写部分回滚产物；PG 系原子回滚时产物无害
		return res, err
	}
	return res, nil
}

// pkValues 提取行主键值（按 PK 顺序转为**原始值字符串**，不做 SQL 转义——
// 转义统一由 quoteInList 在拼入 IN 前完成，两层职责分离避免双重转义）：
// time.Time（DATETIME/TIMESTAMP 主键）按 SQL 时间格式化（Go 默认格式含时区后缀，
// 拼入 IN 后无法匹配旧行）；json.Number/%v 输出原文（雪花 ID 等大整数不失真）；
// []byte 转字符串。主键列缺失时占位 "NULL"（经 quoteInList 后为字符串 'NULL'，
// 与真实 NULL 均不可匹配旧行，行为等价）。
func pkValues(row map[string]interface{}, pk []string) []string {
	vals := make([]string, len(pk))
	for i, k := range pk {
		v, ok := row[k]
		if !ok || v == nil {
			vals[i] = "NULL"
			continue
		}
		switch t := v.(type) {
		case time.Time:
			vals[i] = t.Format("2006-01-02 15:04:05.999999")
		case []byte:
			vals[i] = string(t)
		default:
			vals[i] = fmt.Sprintf("%v", v)
		}
	}
	return vals
}

// sqlQuoteLiteral 将字符串值转为带引号的 SQL 字面量：单引号双写转义防注入；
// MySQL 系另转反斜杠（默认模式反斜杠是转义符，值尾部 \ 会吞掉闭合引号），
// PG/Oracle 反斜杠为字面量不动
func sqlQuoteLiteral(dbType, v string) string {
	if strings.EqualFold(dbType, "mysql") {
		v = strings.ReplaceAll(v, `\`, `\\`)
	}
	return "'" + strings.ReplaceAll(v, "'", "''") + "'"
}

// quoteInList 将多组主键值拼为 IN 列表字面量（('v1','v2'),(...) 形式），
// 逐值经 sqlQuoteLiteral 转义
func quoteInList(dbType string, ids [][]string) string {
	rows := make([]string, 0, len(ids))
	for _, l := range ids {
		cols := make([]string, 0, len(l))
		for _, v := range l {
			cols = append(cols, sqlQuoteLiteral(dbType, v))
		}
		rows = append(rows, "("+strings.Join(cols, ",")+")")
	}
	return strings.Join(rows, ",")
}

// pkgKnownDBs 数据包应用的库名前缀清理清单：包内库名 + 目标连接库名
// （通用实现，替代 tl-env stripDatabasePrefix 的硬编码业务库名清单）
func pkgKnownDBs(cli *cydb.DBCli, pkg *DataPackage) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, db := range []string{pkg.DB, cli.Database()} {
		db = strings.TrimSpace(db)
		if db == "" {
			continue
		}
		if _, ok := seen[db]; ok {
			continue
		}
		seen[db] = struct{}{}
		out = append(out, db)
	}
	return out
}

// stripSQLDBPrefix 清除 SQL 中的源库名前缀（`db`. / db.），使外部生成的 SQL 可在目标库上执行。
// 前缀仅当命中 knownDBs（包内库名/目标库名）时清除，避免误伤别名前缀（t.col）；
// 非 MySQL 方言同时移除全部反引号（MySQL 专有语法，PG/Oracle 上非法）。
// 已知局限：与库名同形的字符串字面量会被误改（外部 SQL 场景可忽略，与 tl-env 原实现一致）。
func stripSQLDBPrefix(isMySQL bool, sql string, knownDBs ...string) string {
	if !isMySQL {
		sql = strings.ReplaceAll(sql, "`", "")
	}
	alt := make([]string, 0, len(knownDBs))
	for _, db := range knownDBs {
		if db != "" {
			alt = append(alt, regexp.QuoteMeta(db))
		}
	}
	if len(alt) == 0 {
		return sql
	}
	// 库名前缀要求"行首/非标识符字符"开头，防止误切 rpa_csx. 这类更长标识符的片段
	re, err := regexp.Compile(`(?i)(^|[^` + "`" + `\w$])([` + "`" + `"']?(?:` + strings.Join(alt, "|") + `)[` + "`" + `"']?)\.(?=[` + "`" + `"\w$])`)
	if err != nil {
		return sql
	}
	return re.ReplaceAllString(sql, "$1")
}

// alterModifyRe 匹配列级变更语句：ALTER TABLE <表> MODIFY [COLUMN] <列> / ALTER [COLUMN] <列>
var alterModifyRe = regexp.MustCompile("(?is)^\\s*ALTER\\s+TABLE\\s+([`\"']?[\\w$]+[`\"']?)\\s+(?:MODIFY(?:\\s+COLUMN)?|ALTER\\s+(?:COLUMN\\s+)?)\\s*([`\"']?[\\w$]+)")

// sqlIdentRe 安全标识符（回滚语句拼装前校验，防注入）
var sqlIdentRe = regexp.MustCompile(`^[\w$]+$`)

// parseAlterColumn 解析 ALTER TABLE <表> MODIFY [COLUMN] <列> / ALTER [COLUMN] <列>，
// 返回校验安全的裸表名与列名（非法返回 ""）
func parseAlterColumn(execSQL string) (table, col string) {
	m := alterModifyRe.FindStringSubmatch(execSQL)
	if m == nil {
		return "", ""
	}
	table, col = trimSQLIdent(m[1]), trimSQLIdent(m[2])
	return table, col
}

// tryColumnRollback 为"回滚为空"的 ALTER 列级变更语句生成精确回滚：执行前读取方言权威
// 建表 DDL（cli.GetDDLSql，复用 cydb 各方言 DDL 生成能力），从中提取目标列的原始定义
// 片段，按方言拼装还原语句（修复 tl-env holder.go 中"MODIFY 已存在列时回滚为空"的缺陷）。
// 会话语义：cli（连接池连接）与 tx（事务连接）不同会话——PG/Oracle 事务内未提交 DDL 对
// 其他会话不可见，读到的正是执行前原定义；MySQL DDL 隐式提交，读到的是当前已生效定义。
// 提取/解析失败返回 "" 由调用方记入 Unrollback 告警（回滚产物只含精确可回放的语句）。
func tryColumnRollback(cli *cydb.DBCli, tx cydb.DatabaseClient, execSQL string) string {
	table, col := parseAlterColumn(execSQL)
	if table == "" || col == "" {
		return ""
	}
	content, err := cli.GetDDLSql(dialect.FuncNameGetCreateTableSql, table)
	if err != nil || content == nil || strings.TrimSpace(content.Content) == "" {
		return ""
	}
	frag, ok := extractColumnDef(content.Content, col, tx.DBType())
	if !ok {
		return ""
	}
	dbType, dbSub := tx.DBType(), tx.DBSubType()
	qt := EscapeTable(dbType, dbSub, table)
	switch {
	case strings.EqualFold(dbType, "mysql"):
		// MySQL：片段即完整列定义（类型/字符集/默认值/自增/生成列/注释），原样 MODIFY 还原
		return fmt.Sprintf("ALTER TABLE %s MODIFY COLUMN %s;", qt, frag)
	case strings.EqualFold(dbType, "oracle"):
		// Oracle：片段含 DEFAULT/NOT NULL，整段拼入 MODIFY 子句
		return fmt.Sprintf("ALTER TABLE %s MODIFY (%s);", qt, frag)
	case strings.EqualFold(dbType, "postgresql"):
		return buildPgColumnRollback(dbType, dbSub, qt, frag)
	}
	return ""
}

// buildPgColumnRollback 从 PG 列定义片段（"col" type [NOT NULL] [DEFAULT expr]）解析并
// 生成按子句拆分的还原 ALTER（PG 语法要求 TYPE/NOT NULL/DEFAULT 分子句）。
// identity/generated 列的序列属性无法精确还原，返回 "" 走 Unrollback 告警。
func buildPgColumnRollback(dbType, dbSub, qtTable, frag string) string {
	colName, rest := splitFirstToken(frag)
	rest = strings.TrimSpace(rest)
	upper := strings.ToUpper(rest)
	if strings.Contains(upper, "GENERATED") || strings.Contains(upper, " IDENTITY") {
		return ""
	}
	notNull := false
	defExpr := ""
	// 先切 DEFAULT（表达式原样保留，可能含空格/括号/函数调用），再于余下部分剥 NOT NULL
	if idx := findKeyword(rest, "DEFAULT"); idx >= 0 {
		defExpr = strings.TrimSpace(rest[idx+len("DEFAULT"):])
		rest = strings.TrimSpace(rest[:idx])
		upper = strings.ToUpper(rest)
	}
	if idx := strings.LastIndex(upper, "NOT NULL"); idx >= 0 && isWordBoundaryAt(upper, idx, len("NOT NULL")) {
		notNull = true
		rest = strings.TrimSpace(rest[:idx])
	}
	typ := rest
	if typ == "" || colName == "" {
		return ""
	}
	qc := EscapeColumn(dbType, dbSub, colName)
	out := fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s TYPE %s;", qtTable, qc, typ)
	if notNull {
		out += fmt.Sprintf("\nALTER TABLE %s ALTER COLUMN %s SET NOT NULL;", qtTable, qc)
	} else {
		out += fmt.Sprintf("\nALTER TABLE %s ALTER COLUMN %s DROP NOT NULL;", qtTable, qc)
	}
	if defExpr != "" {
		// PG 默认值为可执行表达式文本（如 nextval('seq')），原样回写
		out += fmt.Sprintf("\nALTER TABLE %s ALTER COLUMN %s SET DEFAULT %s;", qtTable, qc, defExpr)
	} else {
		out += fmt.Sprintf("\nALTER TABLE %s ALTER COLUMN %s DROP DEFAULT;", qtTable, qc)
	}
	return out
}

// extractColumnDef 从建表 DDL 中提取目标列的定义片段（括号/引号感知切分，排除表级
// 约束/索引行）。列名匹配策略：MySQL 列名大小写不敏感、Oracle 元数据统一大写、
// PG 按书写原文兜底大小写不敏感（同表大小写异形列共存的表几乎不存在）。
func extractColumnDef(ddl, col, dbType string) (string, bool) {
	defs, ok := splitDDLColumnDefs(ddl)
	if !ok {
		return "", false
	}
	for _, def := range defs {
		name, _ := splitFirstToken(def)
		if name == "" {
			continue
		}
		matched := strings.EqualFold(name, col)
		if strings.EqualFold(dbType, "oracle") {
			matched = strings.EqualFold(strings.ToUpper(name), strings.ToUpper(col))
		}
		if matched {
			return def, true
		}
	}
	return "", false
}

// splitDDLColumnDefs 提取 CREATE TABLE 列定义区（表名后第一个 '(' 到配对 ')'，
// 引号/括号感知）并按顶层逗号切分，排除表级约束与索引行；返回列定义片段。
func splitDDLColumnDefs(ddl string) ([]string, bool) {
	start := strings.IndexByte(ddl, '(')
	if start < 0 {
		return nil, false
	}
	end := matchBracket(ddl, start)
	if end < 0 {
		return nil, false
	}
	inner := ddl[start+1 : end]
	parts := splitTopLevel(inner, ',')
	var defs []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		name, _ := splitFirstToken(p)
		if tableLevelKeywords[strings.ToUpper(name)] {
			continue
		}
		defs = append(defs, p)
	}
	return defs, len(defs) > 0
}

// tableLevelKeywords 建表 DDL 中的表级约束/索引行首关键字（排除用）
var tableLevelKeywords = map[string]bool{
	"PRIMARY": true, "UNIQUE": true, "CONSTRAINT": true, "FOREIGN": true, "CHECK": true,
	"KEY": true, "INDEX": true, "EXCLUDE": true, "LIKE": true, "PERIOD": true, "FULLTEXT": true, "SPATIAL": true,
}

// matchBracket 返回与 ddl[start]（'('）配对的 ')' 下标（引号/嵌套括号感知），未配对返回 -1
func matchBracket(s string, start int) int {
	depth := 0
	for i := start; i < len(s); i++ {
		switch c := s[i]; c {
		case '\'', '"', '`':
			i = skipQuoted(s, i) - 1
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// splitTopLevel 按分隔符切分（括号与引号段内的分隔符不切）
func splitTopLevel(s string, sep byte) []string {
	var out []string
	depth, last := 0, 0
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '\'', '"', '`':
			i = skipQuoted(s, i) - 1
		case '(':
			depth++
		case ')':
			depth--
		case sep:
			if depth == 0 {
				out = append(out, s[last:i])
				last = i + 1
			}
		}
	}
	return append(out, s[last:])
}

// splitFirstToken 取片段首 token（剥引号包裹）与剩余部分
func splitFirstToken(frag string) (token, rest string) {
	frag = strings.TrimSpace(frag)
	if frag == "" {
		return "", ""
	}
	if frag[0] == '`' || frag[0] == '"' {
		if end := strings.IndexByte(frag[1:], frag[0]); end >= 0 {
			return frag[1 : 1+end], strings.TrimSpace(frag[2+end:])
		}
		return "", ""
	}
	if i := strings.IndexAny(frag, " \t\n"); i >= 0 {
		return frag[:i], strings.TrimSpace(frag[i+1:])
	}
	return frag, ""
}

// findKeyword 在引号段之外的文本中查找词边界完整的关键字，返回起始下标（-1 不存在）
func findKeyword(s, keyword string) int {
	up := strings.ToUpper(s)
	kw := strings.ToUpper(keyword)
	for i := 0; i+len(kw) <= len(up); i++ {
		if c := up[i]; c == '\'' || c == '"' || c == '`' {
			i = skipQuoted(up, i) - 1
			continue
		}
		if up[i:i+len(kw)] == kw && isWordBoundaryAt(up, i, len(kw)) {
			return i
		}
	}
	return -1
}

// isWordBoundaryAt 校验 s[start:start+length] 两侧均为词边界
func isWordBoundaryAt(s string, start, length int) bool {
	isWord := func(c byte) bool {
		return c == '_' || c == '$' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
	}
	end := start + length
	before := start == 0 || !isWord(s[start-1])
	after := end >= len(s) || !isWord(s[end])
	return before && after
}

// skipQuoted 跳过从 s[i] 开始的引号段（支持 ”/""/“ 双写转义），返回段后第一个下标
func skipQuoted(s string, i int) int {
	q := s[i]
	for j := i + 1; j < len(s); j++ {
		if s[j] != q {
			continue
		}
		if j+1 < len(s) && s[j+1] == q {
			j++
			continue
		}
		return j + 1
	}
	return len(s)
}

// trimSQLIdent 去除标识符包裹符并校验安全字符，非法返回 ""
func trimSQLIdent(s string) string {
	s = strings.Trim(s, "`\"' ")
	if !sqlIdentRe.MatchString(s) {
		return ""
	}
	return s
}
