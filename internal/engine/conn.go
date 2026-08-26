package engine

import (
	"fmt"
	"sort"
	"strings"

	"github.com/fj1981/infrakit/pkg/cydb"
	"github.com/fj1981/infrakit/pkg/cydb/dialect"

	// 注册 tidb parser 驱动（DDL 解析必需，否则 parser.New() panic）
	_ "github.com/pingcap/tidb/parser/test_driver"

	// 注册数据库方言（mysql/postgresql/oracle）
	_ "github.com/fj1981/infrakit/pkg/cydb/dialect/mysql"
	_ "github.com/fj1981/infrakit/pkg/cydb/dialect/oracle"
	_ "github.com/fj1981/infrakit/pkg/cydb/dialect/postgresql"
)

// cliPool 为进程级 cli 连接池：相同连接配置（含库名）复用同一 DBCli 实例，
// 底层池化。对比路径使用 GetOrCreateCli 获取并复用，程序退出前不手动 Close
// （由 CloseAllCliPool 统一释放）。导出/导入等短生命周期路径仍走 Connect，由调用方 Close。
var cliPool = &cydb.DBMgr{}

// Connect 根据连接信息建立数据库连接（每次新建，调用方需 Close）
func Connect(info DBConnInfo) (*cydb.DBCli, error) {
	conn := info.DBConnection
	if conn.SubType == "" {
		conn.SubType = info.SubType
	}
	cli, err := cydb.TryConnect(&conn)
	if err != nil {
		return nil, NewMsgErrf(errConnFail, err, conn.Un, conn.Host, conn.Port)
	}
	return cli, nil
}

// pgAnchorCandidates PG 系枚举/健康检测的可连接锚点库候选：
// 标准 PostgreSQL 必有 postgres；Kingbase/GaussDB 等兼容库默认库名不同（如 TEST/security），
// template1 为 PG 系实例必有的模板库，可作通用兜底
var pgAnchorCandidates = []string{"postgres", "template1"}

// ConnectPGWithAnchor 连接 PG 系实例：连接未指定库名时依次尝试候选锚点库，返回首个可连上的连接。
// 每次新建（调用方需 Close），适合健康检测等短生命周期场景；指定了库名则直接连接。
func ConnectPGWithAnchor(info DBConnInfo) (*cydb.DBCli, error) {
	if info.DBName != "" || !strings.EqualFold(info.Type, "postgresql") {
		return Connect(info)
	}
	var lastErr error
	for _, anchor := range pgAnchorCandidates {
		cli, err := ConnectDB(info, anchor)
		if err == nil {
			return cli, nil
		}
		lastErr = err
	}
	return nil, NewMsgErrf(errMetaAnchorDB, lastErr, info.Un, info.Host, info.Port)
}

// ConnectDB 建立到指定库的连接（覆盖连接信息中的库名，每次新建，调用方需 Close）
func ConnectDB(info DBConnInfo, dbName string) (*cydb.DBCli, error) {
	info.DBName = dbName
	return Connect(info)
}

// ConnectPooled 池化获取绑定指定库名的连接，复用同一实例，调用方不要 Close
func ConnectPooled(info DBConnInfo, dbName string) (*cydb.DBCli, error) {
	conn := info.DBConnection
	if conn.SubType == "" {
		conn.SubType = info.SubType
	}
	conn.DBName = dbName
	cli, err := cliPool.GetOrCreateCli(conn)
	if err != nil {
		return nil, NewMsgErrf(errConnFailDB, err, conn.Un, conn.Host, conn.Port, dbName)
	}
	return cli, nil
}

// CloseAllCliPool 释放进程级连接池（程序退出时调用）
func CloseAllCliPool() {
	cliPool.CloseAll()
}

// EnsureDBExists 确保指定数据库存在，不存在则自动创建（复用 cydb 各方言实现：
// MySQL 为 CREATE DATABASE IF NOT EXISTS，PostgreSQL 先查 pg_database 再创建）。
// Oracle 的“库”语义为用户/模式，且连接本身已定位到目标 schema，不做自动创建。
func EnsureDBExists(info DBConnInfo, dbName string) error {
	if dbName == "" || strings.EqualFold(info.Type, "oracle") {
		return nil
	}
	conn := info.DBConnection
	conn.DBName = dbName
	return cydb.EnsureDBExists(&conn)
}

// EscapeTable 按数据库方言转义表名
func EscapeTable(dbType, subType, tableName string) string {
	if md, ok := dialect.GetMigrationDialect(dbType, subType); ok {
		return md.EscapeTableName(tableName)
	}
	return "`" + tableName + "`"
}

// EscapeColumn 按数据库方言转义列名
func EscapeColumn(dbType, subType, columnName string) string {
	if md, ok := dialect.GetMigrationDialect(dbType, subType); ok {
		return md.EscapeColumnName(columnName)
	}
	return "`" + columnName + "`"
}

// splitTableBare 拆分当前表名为 schema + 裸名（首个 . 分隔，无点时 schema 为空）
func splitTableBare(tableName string) (schema, bare string) {
	if i := strings.Index(tableName, "."); i > 0 {
		return tableName[:i], tableName[i+1:]
	}
	return "", tableName
}

// schemaPtrValue 解引用 schema 指针（nil 返回空串）
func schemaPtrValue(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// listSchemaObjects 枚举 schema 内全部对象（PG 系专用）：
// 实现下沉方言层 GetSchemaObjects（一次往返，pg_tables + information_schema 的 views/routines UNION ALL），
// 返回按类型分组的裸名清单（函数/过程重载名不去重，与方言 GetObjects 口径一致）。
// 非 PG 系不适用（返回 err，调用方应回退方言逐类枚举）。
func listSchemaObjects(cli *cydb.DBCli, db, schema string) (tables, views, funcs, procs []string, err error) {
	if !strings.EqualFold(cli.DBType(), "postgresql") {
		return nil, nil, nil, nil, fmt.Errorf("listSchemaObjects: only for postgresql dialect")
	}
	objs, err := cli.GetSchemaObjects(db, schema)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return objs.Tables, objs.Views, objs.Functions, objs.Procedures, nil
}

// firstStringOf 取查询结果行中指定列（小写列名）的字符串值
func firstStringOf(r map[string]any, key string) string {
	if v, ok := r[key]; ok && v != nil {
		return fmt.Sprint(v)
	}
	return ""
}

// listSchemaTables 枚举库内表清单并剔除视图，返回可执行表名：
// PG 系按 schema 分层（连接指定 schema 时仅枚举该 schema；否则枚举全部用户 schema），
// 返回限定名 "schema.table"（方言层 DDL/元数据/构建器均支持限定名）；
// MySQL/Oracle 返回裸名（schemaPtr 透传方言）。
func listSchemaTables(cli *cydb.DBCli, db string, schemaPtr *string) ([]string, error) {
	if !strings.EqualFold(cli.DBType(), "postgresql") {
		all, err := cli.GetTables(db, schemaPtr, nil)
		if err != nil {
			return nil, err
		}
		return excludeViews(cli, db, schemaPtrValue(schemaPtr), all), nil
	}
	schemas := make([]string, 0, 1)
	if schemaPtr != nil && *schemaPtr != "" {
		schemas = append(schemas, *schemaPtr)
	} else {
		names, err := cli.GetSchemas(db)
		if err != nil {
			return nil, err
		}
		schemas = append(schemas, names...)
	}
	var out []string
	for _, s := range schemas {
		if s == "" {
			continue
		}
		ts, _, _, _, err := listSchemaObjects(cli, db, s)
		if err != nil {
			continue // 单 schema 枚举失败不阻断（与对象树枚举口径一致）
		}
		for _, t := range ts {
			out = append(out, s+"."+t)
		}
	}
	sort.Strings(out)
	return out, nil
}

// findCondition 查找指定表的过滤条件。
// 条件表名支持限定形式 "库.schema.表"（PG 分层）/"库.表" 与裸表名（便于 CLI 手输），
// 限定形式优先；tableName 可能为 "schema.table"（PG 分层枚举）或裸名（MySQL/Oracle）
func findCondition(conditions []TableCondition, db, tableName string) *TableCondition {
	curSchema, curBare := splitTableBare(tableName)
	var bare *TableCondition
	for i := range conditions {
		name := strings.TrimSpace(conditions[i].TableName)
		parts := strings.Split(name, ".")
		switch len(parts) {
		case 1:
			// 裸名：匹配任意库/schema 的同名表
			if name != "" && strings.EqualFold(name, curBare) && bare == nil {
				bare = &conditions[i]
			}
		case 2:
			if strings.EqualFold(parts[0], db) && strings.EqualFold(parts[1], curBare) {
				return &conditions[i]
			}
		case 3:
			if strings.EqualFold(parts[0], db) && strings.EqualFold(parts[1], curSchema) && strings.EqualFold(parts[2], curBare) {
				return &conditions[i]
			}
		}
	}
	return bare
}

// splitQualifiedName 拆分限定名 "库.名"（首个 . 分隔）；不含有效分隔时 ok=false 按裸名处理
func splitQualifiedName(s string) (db, name string, ok bool) {
	i := strings.Index(s, ".")
	if i <= 0 || i == len(s)-1 {
		return "", s, false
	}
	return s[:i], s[i+1:], true
}

// conditionQuery 归一化表条件为数据取数的完整 SELECT：
// Query 优先（完整 SQL）；旧版配置的 Where/Columns 拼装为 SELECT 兼容。
// cond 为 nil 或无任何条件时返回 ""（调用方回退全表 SELECT）
func conditionQuery(dbType, subType, table string, cond *TableCondition) string {
	if cond == nil {
		return ""
	}
	if q := strings.TrimSpace(cond.Query); q != "" {
		return q
	}
	// 旧版配置兼容：由 Where/Columns 拼装
	cols := "*"
	where := ""
	if len(cond.Columns) > 0 {
		escaped := make([]string, len(cond.Columns))
		for i, c := range cond.Columns {
			escaped[i] = EscapeColumn(dbType, subType, c)
		}
		cols = strings.Join(escaped, ",")
	}
	if w := strings.TrimSpace(cond.Where); w != "" {
		where = " WHERE " + w
	}
	if cols == "*" && where == "" {
		return ""
	}
	return fmt.Sprintf("SELECT %s FROM %s%s", cols, EscapeTable(dbType, subType, table), where)
}
