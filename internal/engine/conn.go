package engine

import (
	"fmt"
	"strings"

	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cydb"
	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cydb/dialect"

	// 注册 tidb parser 驱动（DDL 解析必需，否则 parser.New() panic）
	_ "github.com/pingcap/tidb/parser/test_driver"

	// 注册数据库方言（mysql/postgresql/oracle）
	_ "gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cydb/dialect/mysql"
	_ "gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cydb/dialect/oracle"
	_ "gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cydb/dialect/postgresql"
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
		return nil, fmt.Errorf("连接数据库失败(%s@%s:%d): %w", conn.Un, conn.Host, conn.Port, err)
	}
	return cli, nil
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
		return nil, fmt.Errorf("连接数据库失败(%s@%s:%d/%s): %w", conn.Un, conn.Host, conn.Port, dbName, err)
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

// findCondition 查找指定表的过滤条件。
// TableName 支持限定形式 "库.表"（仅匹配对应库）与裸表名（匹配任意库，便于 CLI 手输），限定形式优先
func findCondition(conditions []TableCondition, db, tableName string) *TableCondition {
	var bare *TableCondition
	for i := range conditions {
		name := conditions[i].TableName
		if d, t, ok := splitQualifiedName(name); ok {
			if strings.EqualFold(d, db) && t == tableName {
				return &conditions[i]
			}
			continue
		}
		if name == tableName && bare == nil {
			bare = &conditions[i]
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
