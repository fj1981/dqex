package engine

import (
	"fmt"
	"sort"
	"strings"

	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cydb"
)

// DBTables 一个数据库（Oracle 为 schema）及其表与对象清单
type DBTables struct {
	Name   string   `json:"name"`
	Tables []string `json:"tables"`
	// Objects 库内对象：_views/_functions/_procedures → 对象名（枚举失败的类型不返回）
	Objects map[string][]string `json:"objects,omitempty"`
}

// MySQL 系统库（遍历时排除）
var mysqlSystemDBs = map[string]bool{
	"information_schema": true,
	"mysql":              true,
	"performance_schema": true,
	"sys":                true,
}

// Oracle 系统用户（遍历时排除）
var oracleSystemUsers = map[string]bool{
	"SYS": true, "SYSTEM": true, "OUTLN": true, "DIP": true, "DBSNMP": true,
	"APPQOSYS": true, "WMSYS": true, "EXFSYS": true, "XDB": true, "CTXSYS": true,
	"MDSYS": true, "OLAPSYS": true, "ORDDATA": true, "ORDSYS": true, "AUDSYS": true,
	"DBSFWUSER": true, "GGSYS": true, "GSMADMIN_INTERNAL": true, "SYSBACKUP": true,
	"SYSDG": true, "SYSKM": true, "SYSRAC": true, "SYS$UMF": true, "OJVMSYS": true,
	"LBACSYS": true, "SI_INFORMTN_SCHEMA": true, "XS$NULL": true, "DVF": true,
	"DVSYS": true, "MDDATA": true, "ANONYMOUS": true, "APEX_PUBLIC_USER": true,
	"REMOTE_SCHEDULER_AGENT": true, "FLOWS_FILES": true, "SPATIAL_WFS_ADMIN_USR": true,
	"SPATIAL_CSW_ADMIN_USR": true,
}

// GetTableTree 获取 "库 → 表" 树形结构。
// conn.DBName 非空时只返回该库；为空时遍历所有库（Oracle 遍历 schema）。
func GetTableTree(conn DBConnInfo) ([]DBTables, error) {
	switch strings.ToLower(conn.Type) {
	case "mysql":
		return mysqlTableTree(conn)
	case "postgresql":
		return postgresTableTree(conn)
	case "oracle":
		return oracleTableTree(conn)
	default:
		return nil, NewMsgErr(errMetaType, conn.Type)
	}
}

// mysqlTableTree MySQL：SHOW DATABASES 后逐库查表（复用池化连接）
func mysqlTableTree(conn DBConnInfo) ([]DBTables, error) {
	cli, err := ConnectPooled(conn, conn.DBName)
	if err != nil {
		return nil, err
	}

	if conn.DBName != "" {
		tables, err := cli.GetTables(conn.DBName, nil, nil)
		if err != nil {
			return nil, NewMsgErrf(errMetaListTables, err, conn.DBName)
		}
		d := DBTables{Name: conn.DBName, Tables: excludeViews(cli, conn.DBName, "", tables)}
		attachObjects(cli, conn.DBName, "", &d)
		return []DBTables{d}, nil
	}

	rows, err := cli.DirectQuery("SHOW DATABASES")
	if err != nil {
		return nil, NewMsgErrf(errMetaListDBs, err)
	}
	tree := make([]DBTables, 0, len(rows))
	for _, r := range rows {
		db := firstString(r)
		if db == "" || mysqlSystemDBs[db] {
			continue
		}
		tables, err := cli.GetTables(db, nil, nil)
		if err != nil {
			// 单个库无权限/查询失败不应中断整棵树加载（与 PostgreSQL 行为一致）
			continue
		}
		d := DBTables{Name: db, Tables: excludeViews(cli, db, "", tables)}
		attachObjects(cli, db, "", &d)
		tree = append(tree, d)
	}
	return tree, nil
}

// postgresTableTree PostgreSQL：pg_database 枚举后逐库连接查表（复用池化连接）
func postgresTableTree(conn DBConnInfo) ([]DBTables, error) {
	base := conn
	if base.DBName == "" {
		base.DBName = "postgres" // 枚举需要一个可连接的库
	}
	cli, err := ConnectPooled(conn, base.DBName)
	if err != nil {
		return nil, err
	}

	if conn.DBName != "" {
		tables, err := cli.GetTables(conn.DBName, nil, nil)
		if err != nil {
			return nil, NewMsgErrf(errMetaListTables, err, conn.DBName)
		}
		d := DBTables{Name: conn.DBName, Tables: excludeViews(cli, conn.DBName, conn.Schema, tables)}
		attachObjects(cli, conn.DBName, conn.Schema, &d)
		return []DBTables{d}, nil
	}

	rows, err := cli.DirectQuery("SELECT datname FROM pg_database WHERE datistemplate = false ORDER BY datname")
	if err != nil {
		return nil, NewMsgErrf(errMetaListDBs, err)
	}
	tree := make([]DBTables, 0, len(rows))
	for _, r := range rows {
		db := firstString(r)
		if db == "" {
			continue
		}
		dbCli, err := ConnectPooled(conn, db)
		if err != nil {
			continue // 无权限连接的库跳过
		}
		tables, err := dbCli.GetTables(db, nil, nil)
		if err != nil {
			continue
		}
		d := DBTables{Name: db, Tables: excludeViews(dbCli, db, conn.Schema, tables)}
		attachObjects(dbCli, db, conn.Schema, &d)
		tree = append(tree, d)
	}
	return tree, nil
}

// oracleTableTree Oracle：以 schema(user) 为树节点（复用池化连接）
func oracleTableTree(conn DBConnInfo) ([]DBTables, error) {
	cli, err := ConnectPooled(conn, conn.DBName)
	if err != nil {
		return nil, err
	}

	// 指定了 schema 时只返回该 schema
	schema := conn.Schema
	if schema == "" {
		schema = conn.DBName
	}
	if schema != "" {
		tables, err := cli.GetTables("", &schema, nil)
		if err != nil {
			return nil, NewMsgErrf(errMetaListSchemas, err, schema)
		}
		d := DBTables{Name: strings.ToUpper(schema), Tables: excludeViews(cli, schema, "", tables)}
		attachObjects(cli, schema, "", &d)
		return []DBTables{d}, nil
	}

	rows, err := cli.DirectQuery("SELECT username FROM all_users ORDER BY username")
	if err != nil {
		return nil, NewMsgErrf(errMetaSchemaList, err)
	}
	tree := make([]DBTables, 0, len(rows))
	for _, r := range rows {
		user := firstString(r)
		if user == "" || oracleSystemUsers[user] || strings.HasPrefix(user, "APEX_") {
			continue
		}
		tables, err := cli.GetTables("", &user, nil)
		if err != nil || len(tables) == 0 {
			continue
		}
		d := DBTables{Name: user, Tables: excludeViews(cli, user, "", tables)}
		attachObjects(cli, user, "", &d)
		tree = append(tree, d)
	}
	sort.Slice(tree, func(i, j int) bool { return tree[i].Name < tree[j].Name })
	return tree, nil
}

// attachObjects 为树节点补充库内对象清单（视图/函数/存储过程），枚举失败不影响表树展示
func attachObjects(cli *cydb.DBCli, db, schema string, d *DBTables) {
	objs := listDBObjects(cli, db, schema)
	if len(objs) == 0 {
		return
	}
	m := make(map[string][]string, len(objs))
	for kind, names := range objs {
		if len(names) > 0 {
			m[objectKindDirs[kind]] = names
		}
	}
	if len(m) > 0 {
		d.Objects = m
	}
}

// excludeViews 从表清单中剔除视图（底层 GetTables 会把视图一并枚举为表，
// 而视图走对象导出/迁移通道 _views，不应当作表处理，否则建表语句为空导致任务失败）。
// 视图枚举失败时保留原清单，不阻断主流程
func excludeViews(cli *cydb.DBCli, db, schema string, tables []string) []string {
	var schemaPtr *string
	if schema != "" {
		schemaPtr = &schema
	}
	if strings.EqualFold(cli.DBType(), "oracle") {
		// Oracle 无多库概念，db 即 schema(owner)
		schemaPtr = &db
	}
	views, err := cli.GetObjects(db, schemaPtr, cydb.ObjectTypeView)
	if err != nil || len(views) == 0 {
		return tables
	}
	viewSet := make(map[string]bool, len(views))
	for _, v := range views {
		viewSet[strings.ToLower(v)] = true
	}
	out := make([]string, 0, len(tables))
	for _, t := range tables {
		if !viewSet[strings.ToLower(t)] {
			out = append(out, t)
		}
	}
	return out
}

// TableColumnInfo 表列信息（JSON 返回给前端用于条件编辑辅助）
type TableColumnInfo struct {
	Name          string `json:"name"`
	DataType      string `json:"dataType"`             // 原始数据类型（如 varchar(255), int, timestamp）
	Nullable      bool   `json:"nullable"`             // 是否允许 NULL
	PrimaryKey    bool   `json:"primaryKey,omitempty"` // 是否主键
	Default       string `json:"default,omitempty"`    // 默认值
	AutoIncrement bool   `json:"autoIncrement,omitempty"`
	Unique        bool   `json:"unique,omitempty"`  // 是否唯一约束（非主键）
	Indexed       bool   `json:"indexed,omitempty"` // 是否有普通索引（非主键/非唯一）
	Comment       string `json:"comment,omitempty"` // 列注释（可能为空）
}

// TableMeta 表级元数据（表注释 + 列信息，供 AI 上下文等场景一次取全）。
type TableMeta struct {
	Comment string            // 表注释（可能为空）
	Columns []TableColumnInfo // 列信息（含列注释）
}

// GetTableMeta 获取指定表的元数据（表注释 + 列信息含注释，复用池化连接）。
func GetTableMeta(conn DBConnInfo, tableName string) (TableMeta, error) {
	cli, err := ConnectPooled(conn, conn.DBName)
	if err != nil {
		return TableMeta{}, err
	}
	info, err := cli.GetTableInfo(tableName)
	if err != nil {
		return TableMeta{}, NewMsgErrf(errMetaTableInfo, err, tableName)
	}
	if info == nil {
		return TableMeta{}, NewMsgErr(errMetaTableEmpty, tableName)
	}
	cols := info.GetColumns()
	result := make([]TableColumnInfo, 0, len(cols))
	for _, col := range cols {
		isPK := col.IsPrimaryKey()
		isUnique := col.IsUnique()
		c := TableColumnInfo{
			Name:          col.GetName(),
			DataType:      col.GetOrginalDataType(),
			Nullable:      !col.IsNotNull(),
			PrimaryKey:    isPK,
			AutoIncrement: col.IsAutoIncrement(),
			// 唯一约束/普通索引均排除主键（主键自带唯一 + 索引，避免重复标识）
			Unique:  !isPK && isUnique,
			Indexed: !isPK && !isUnique && col.IsIndex(),
			Comment: col.GetComment(),
		}
		if d := col.GetDefault(); d != nil {
			c.Default = *d
		}
		result = append(result, c)
	}
	return TableMeta{Comment: info.GetComment(), Columns: result}, nil
}

// GetTableColumns 获取指定表的列信息（名称/类型/可空/主键/默认值/注释，复用池化连接）。
func GetTableColumns(conn DBConnInfo, tableName string) ([]TableColumnInfo, error) {
	meta, err := GetTableMeta(conn, tableName)
	if err != nil {
		return nil, err
	}
	return meta.Columns, nil
}

// firstString 取查询结果行的第一个非空字符串值
func firstString(r map[string]any) string {
	for _, v := range r {
		if v != nil {
			return fmt.Sprint(v)
		}
	}
	return ""
}
