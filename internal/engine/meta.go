package engine

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/fj1981/infrakit/pkg/cydb"
)

// DBSchema 一个库内的 schema（PG 系）及其表与对象清单
// （PG 系对象树按 库 → schema → 分组 → 对象 分层；MySQL/Oracle 不使用）
type DBSchema struct {
	Name    string              `json:"name"`
	Tables  []string            `json:"tables"`
	Objects map[string][]string `json:"objects,omitempty"`
}

// DBTables 一个数据库（Oracle 为 schema）及其表与对象清单
type DBTables struct {
	Name   string   `json:"name"`
	Tables []string `json:"tables"`
	// Schemas 库内 schema 清单（PG 系按 schema 分层时填充；MySQL/Oracle 为空）
	Schemas []DBSchema `json:"schemas,omitempty"`
	// Objects 库内对象：_views/_functions/_procedures → 对象名（枚举失败的类型不返回）
	Objects map[string][]string `json:"objects,omitempty"`
}

// listDatabases 连接默认库并枚举全部库名（MySQL/Oracle 通用；错误包装用指定文案）
func listDatabases(conn DBConnInfo, errMsg string) ([]string, error) {
	cli, err := ConnectPooled(conn, conn.DBName)
	if err != nil {
		return nil, err
	}
	dbs, err := cli.GetDatabases()
	if err != nil {
		return nil, NewMsgErrf(errMsg, err)
	}
	return dbs, nil
}

// GetDatabaseList 仅返回库（Oracle 为 schema）名列表，不做对象枚举（对象树分级加载第一层）。
// 连接配置指定库时只返回该库（名称已知无需连接）；未指定时枚举全部，
// 系统库/用户过滤与库枚举 SQL 由方言 GetDatabases 承担（与 GetTableTree 口径一致）。
func GetDatabaseList(conn DBConnInfo) ([]string, error) {
	switch strings.ToLower(conn.Type) {
	case "postgresql":
		// PG 枚举 pg_database 需锚点库连接（库名未知时 postgres→template1 回退）
		if conn.DBName != "" {
			return []string{conn.DBName}, nil
		}
		cli, err := connectPostgresAnchor(conn)
		if err != nil {
			return nil, err
		}
		dbs, err := cli.GetDatabases()
		if err != nil {
			return nil, NewMsgErrf(errMetaListDBs, err)
		}
		return dbs, nil
	case "oracle":
		// Oracle 以 schema(user) 为树节点：连接配置指定 schema/库时只返回该 schema
		schema := conn.Schema
		if schema == "" {
			schema = conn.DBName
		}
		if schema != "" {
			return []string{strings.ToUpper(schema)}, nil
		}
		return listDatabases(conn, errMetaSchemaList)
	case "mysql":
		if conn.DBName != "" {
			return []string{conn.DBName}, nil
		}
		return listDatabases(conn, errMetaListDBs)
	default:
		return nil, NewMsgErr(errMetaType, conn.Type)
	}
}

// SchemaSummary 库内 schema 概要（对象树分级第二层，PG 系）
type SchemaSummary struct {
	Name       string `json:"name"`
	TableCount int    `json:"tableCount"` // 表数量（视图/函数等不计入，展开后见明细）
}

// GetDbSchemas 返回库的 schema 列表（含表计数，PG 系，一次往返）；
// 非 PG 系返回空列表（前端走 GetDbObjects 加载库对象）。
// 实现下沉方言层 GetSchemaSummaries，系统 schema 黑名单由方言一处维护。
func GetDbSchemas(conn DBConnInfo, db string) ([]SchemaSummary, error) {
	cli, err := ConnectPooled(conn, db)
	if err != nil {
		return nil, err
	}
	sums, err := cli.GetSchemaSummaries(db)
	if err != nil {
		return nil, NewMsgErrf(errMetaSchemaList, err)
	}
	out := make([]SchemaSummary, 0, len(sums))
	for _, s := range sums {
		out = append(out, SchemaSummary{Name: s.Name, TableCount: s.TableCount})
	}
	return out, nil
}

// GetSchemaObjects 返回单 schema 的对象清单（对象树分级第三层，PG 系，一次往返）
func GetSchemaObjects(conn DBConnInfo, db, schema string) (*DBSchema, error) {
	cli, err := ConnectPooled(conn, db)
	if err != nil {
		return nil, err
	}
	tables, views, funcs, procs, err := listSchemaObjects(cli, db, schema)
	if err != nil {
		// 回退：单查表清单（兼容库元数据视图异常时至少能显示表）
		tables, err = cli.GetTables(db, &schema, nil)
		if err != nil {
			return nil, NewMsgErrf(errMetaListTables, err, db)
		}
	}
	sc := &DBSchema{Name: schema, Tables: tables}
	if len(views) > 0 || len(funcs) > 0 || len(procs) > 0 {
		m := make(map[string][]string, 3)
		if len(views) > 0 {
			m[objectKindDirs[objectView]] = views
		}
		if len(funcs) > 0 {
			m[objectKindDirs[objectFunction]] = funcs
		}
		if len(procs) > 0 {
			m[objectKindDirs[objectProcedure]] = procs
		}
		sc.Objects = m
	}
	return sc, nil
}

// GetDbObjects 返回单库对象清单（对象树分级第二层，MySQL/Oracle 无 schema 层）：
// 复用 GetTableTree 单库逻辑（指定库名后返回首元素）
func GetDbObjects(conn DBConnInfo, db string) (*DBTables, error) {
	conn.DBName = db
	tree, err := GetTableTree(conn)
	if err != nil {
		return nil, err
	}
	if len(tree) == 0 {
		return nil, NewMsgErrf(errMetaListTables, fmt.Errorf("empty result"), db)
	}
	return &tree[0], nil
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

	// 库清单走方言 GetDatabases（含系统库过滤）
	dbs, err := cli.GetDatabases()
	if err != nil {
		return nil, NewMsgErrf(errMetaListDBs, err)
	}
	tree := make([]DBTables, 0, len(dbs))
	for _, db := range dbs {
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
	if base.DBName != "" {
		cli, err := ConnectPooled(conn, base.DBName)
		if err != nil {
			return nil, err
		}
		d, err := postgresDbNode(cli, conn, base.DBName)
		if err != nil {
			return nil, err
		}
		return []DBTables{*d}, nil
	}

	// 枚举需要一个可连接的锚点库：标准 PostgreSQL 必有 postgres；
	// Kingbase/GaussDB 等兼容库默认库名不同（如 TEST/security），回退尝试 PG 系通用模板库 template1
	cli, err := connectPostgresAnchor(conn)
	if err != nil {
		return nil, err
	}

	// 库清单走方言 GetDatabases（含 datistemplate 过滤）
	dbs, err := cli.GetDatabases()
	if err != nil {
		return nil, NewMsgErrf(errMetaListDBs, err)
	}
	// 多库并发枚举（信号量限流；单库失败跳过，与串行口径一致），收集后按库名排序
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	var mu sync.Mutex
	tree := make([]*DBTables, 0, len(dbs))
	for _, db := range dbs {
		wg.Add(1)
		go func(dbName string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			dbCli, err := ConnectPooled(conn, dbName)
			if err != nil {
				return // 无权限连接的库跳过
			}
			d, err := postgresDbNode(dbCli, conn, dbName)
			if err != nil {
				return
			}
			mu.Lock()
			tree = append(tree, d)
			mu.Unlock()
		}(db)
	}
	wg.Wait()
	sort.Slice(tree, func(i, j int) bool { return tree[i].Name < tree[j].Name })
	out := make([]DBTables, 0, len(tree))
	for _, d := range tree {
		out = append(out, *d)
	}
	return out, nil
}

// postgresDbNode 构建单个 PG 库的树节点：
// 连接配置指定 schema 时只枚举该 schema；否则枚举全部用户 schema 并按 schema 分层。
// Tables 为各 schema 表合并（裸名去重，兼容 TablePicker/导出白名单的 "库.表" 口径）。
// schema 枚举并发执行（每 schema 一次往返，信号量限流防连接池排队）。
func postgresDbNode(cli *cydb.DBCli, conn DBConnInfo, db string) (*DBTables, error) {
	d := &DBTables{Name: db}
	schemas := []string{conn.Schema}
	if conn.Schema == "" {
		names, err := cli.GetSchemas(db)
		if err != nil {
			return nil, NewMsgErrf(errMetaSchemaList, err)
		}
		schemas = names
	}
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	var mu sync.Mutex
	results := make([]*DBSchema, 0, len(schemas))
	for _, s := range schemas {
		if s == "" {
			continue
		}
		wg.Add(1)
		go func(sch string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			sc := attachSchema(cli, db, sch)
			if sc != nil {
				mu.Lock()
				results = append(results, sc)
				mu.Unlock()
			}
		}(s)
	}
	wg.Wait()
	sort.Slice(results, func(i, j int) bool { return results[i].Name < results[j].Name })
	d.Schemas = make([]DBSchema, 0, len(results))
	for _, sc := range results {
		d.Schemas = append(d.Schemas, *sc)
	}
	seen := make(map[string]bool)
	for _, sc := range results {
		for _, t := range sc.Tables {
			if !seen[t] {
				seen[t] = true
				d.Tables = append(d.Tables, t)
			}
		}
	}
	return d, nil
}

// attachSchema 枚举单个 schema 的表与对象并构建 schema 节点（PG 系专用）：
// 一次往返拿全表/视图/函数/过程（视图从表清单剔除，避免重复枚举）；
// 空 schema 返回 nil 不挂载。
func attachSchema(cli *cydb.DBCli, db, schema string) *DBSchema {
	tables, views, funcs, procs, err := listSchemaObjects(cli, db, schema)
	if err != nil {
		// 回退：单查表清单（兼容库元数据视图异常时树至少能显示表）
		tables, err = cli.GetTables(db, &schema, nil)
		if err != nil {
			return nil
		}
	}
	if len(tables) == 0 {
		return nil
	}
	sc := DBSchema{Name: schema, Tables: tables}
	if len(views) > 0 || len(funcs) > 0 || len(procs) > 0 {
		m := make(map[string][]string, 3)
		if len(views) > 0 {
			m[objectKindDirs[objectView]] = views
		}
		if len(funcs) > 0 {
			m[objectKindDirs[objectFunction]] = funcs
		}
		if len(procs) > 0 {
			m[objectKindDirs[objectProcedure]] = procs
		}
		sc.Objects = m
	}
	return &sc
}

// connectPostgresAnchor 获取 pg_database 枚举的锚点连接：
// 依次尝试候选库名，返回首个可连上的（失败连接不会进池，GetOrCreateCli 仅成功才缓存）；
// 全部失败时返回带操作提示的错误，引导用户为连接填写实例上实际存在的库名
func connectPostgresAnchor(conn DBConnInfo) (*cydb.DBCli, error) {
	var lastErr error
	for _, anchor := range pgAnchorCandidates {
		cli, err := ConnectPooled(conn, anchor)
		if err == nil {
			return cli, nil
		}
		lastErr = err
	}
	return nil, NewMsgErrf(errMetaAnchorDB, lastErr, conn.Un, conn.Host, conn.Port)
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

	// 用户清单走方言 GetDatabases（含系统用户过滤）
	users, err := cli.GetDatabases()
	if err != nil {
		return nil, NewMsgErrf(errMetaSchemaList, err)
	}
	tree := make([]DBTables, 0, len(users))
	for _, user := range users {
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
