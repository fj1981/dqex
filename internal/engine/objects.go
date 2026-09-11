package engine

import (
	"strings"

	"github.com/fj1981/infrakit/pkg/cydb"
	"github.com/fj1981/infrakit/pkg/cydb/dialect"
)

// objectKind 数据库对象类型（值与 DetailKindView/Function/Procedure 一致，导出明细回调直接取 string(kind)）
type objectKind string

const (
	objectView      objectKind = "view"
	objectFunction  objectKind = "function"
	objectProcedure objectKind = "procedure"
)

// 说明：触发器不在此处单独导出——底层库三方言的 GetCreateTableSql 已将该表触发器
// 随建表语句一并返回（MySQL SHOW TRIGGERS / PG pg_get_triggerdef / Oracle all_triggers）

// objectKindDirs 对象类型 ↔ zip 包内子目录名（导入导出共用）
var objectKindDirs = map[objectKind]string{
	objectView:      "_views",
	objectFunction:  "_functions",
	objectProcedure: "_procedures",
}

// dirObjectKinds zip 子目录名 → 对象类型（反向索引）
var dirObjectKinds = func() map[string]objectKind {
	m := make(map[string]objectKind, len(objectKindDirs))
	for k, d := range objectKindDirs {
		m[d] = k
	}
	return m
}()

// objectExportOrder 导出顺序（导入执行顺序与此一致）
var objectExportOrder = []objectKind{objectView, objectFunction, objectProcedure}

// countExportObjects 计算符合白名单的对象总数，用于在任务开始时一次性确定总进度单位，
// 避免对象导出阶段动态增加 TotalUnits 导致进度百分比回退。
func countExportObjects(cli *cydb.DBCli, db, schema string, objects []string) int {
	if objects != nil && len(objects) == 0 {
		return 0
	}
	allowed := make(map[string]bool, len(objects))
	for _, o := range objects {
		allowed[strings.TrimSpace(o)] = true
	}
	objs := listDBObjects(cli, db, schema)
	total := 0
	for _, kind := range objectExportOrder {
		names := objs[kind]
		dirName := objectKindDirs[kind]
		if objects != nil {
			filtered := make([]string, 0, len(names))
			for _, name := range names {
				if objectInWhitelist(allowed, db, objectWhitelistID(dirName, name)) {
					filtered = append(filtered, name)
				}
			}
			names = filtered
		}
		total += len(names)
	}
	return total
}

// objectWhitelistID 由对象枚举名构造白名单匹配 id：
// PG 限定名 "schema.对象名" → "schema.目录/对象名"（匹配 "库.schema.目录/对象名" 三级白名单）；
// 裸名 → "目录/对象名"（匹配 "库.目录/对象名" 旧格式与 CLI 裸条目）
func objectWhitelistID(dirName, name string) string {
	if schema, bare, ok := strings.Cut(name, "."); ok && schema != "" && bare != "" {
		return schema + "." + dirName + "/" + bare
	}
	return dirName + "/" + name
}

// objectInWhitelist 判断对象（id 格式 目录/名 或 schema.目录/名）是否命中白名单。
// 白名单条目支持限定形式 "库.schema.目录/名"（PG 分层）/ "库.目录/名" 与裸形式 "目录/名"（匹配任意库，便于 CLI 手输）；
// 库级裸名条目（"库.目录/名"）可命中 PG 的 schema 限定枚举名（"schema.对象名"，兜底剥 schema 后再匹配，
// 与表过滤的"库.表命中任意 schema 同名表"语义一致）
func objectInWhitelist(allowed map[string]bool, db, id string) bool {
	if allowed[id] || allowed[db+"."+id] {
		return true
	}
	if bare := objectBareWhitelistID(id); bare != id {
		return allowed[bare] || allowed[db+"."+bare]
	}
	return false
}

// objectBareWhitelistID 剥离 schema 限定："schema.目录/名" → "目录/名"；裸 id 原样返回
func objectBareWhitelistID(id string) string {
	slash := strings.LastIndex(id, "/")
	if slash < 0 {
		return id
	}
	if i := strings.LastIndex(id[:slash], "."); i >= 0 {
		return id[i+1:]
	}
	return id
}

// objectWhitelistHits 返回对象白名单条目中被库内枚举对象命中的子集（key 为 trim 后条目原文）。
// 复用 objectInWhitelist 做反向命中判断：每个枚举对象尝试匹配所有未命中条目
// （对象/条目数量级均为百以内，嵌套遍历成本可忽略）
func objectWhitelistHits(allowed map[string]bool, objs dbObjects, db string) map[string]bool {
	hit := make(map[string]bool, len(allowed))
	for _, kind := range objectExportOrder {
		dirName := objectKindDirs[kind]
		for _, name := range objs[kind] {
			id := objectWhitelistID(dirName, name)
			for w := range allowed {
				if !hit[w] && objectInWhitelist(map[string]bool{w: true}, db, id) {
					hit[w] = true
					break
				}
			}
		}
	}
	return hit
}

// objectEntryForDB 判断对象白名单条目是否归属指定库（对象导出按库执行，跨库条目不在此库校验）。
// 条目形式："_views/v1"（裸条目=任意库，便于 CLI 手输）、"库._views/v1"、"库.schema._views/v1"；
// 目录段固定 "_" 前缀，据此区分首段是目录还是库名（与 objectInWhitelist 的库前缀语义一致）
func objectEntryForDB(entry, db string) bool {
	head := strings.SplitN(entry, "/", 2)[0]
	parts := strings.Split(head, ".")
	if strings.HasPrefix(parts[0], "_") {
		return true // 裸条目：任意库
	}
	return strings.EqualFold(parts[0], db)
}

// missingWhitelistObjects 返回归属当前库的对象白名单条目中未在库内命中的条目
// （缺失告警用，保持原顺序去重；跨库条目不在此库校验，避免误报）。
// 对齐原 ValidateDbObjects 的缺失对象校验标准：rule 定义了视图/函数/存储过程 → 逐一确认真存在
func missingWhitelistObjects(objects []string, allowed map[string]bool, objs dbObjects, db string) []string {
	hit := objectWhitelistHits(allowed, objs, db)
	seen := make(map[string]bool, len(objects))
	missing := make([]string, 0, len(objects))
	for _, o := range objects {
		w := strings.TrimSpace(o)
		if w == "" || !objectEntryForDB(w, db) || hit[w] || seen[w] {
			continue
		}
		seen[w] = true
		missing = append(missing, w)
	}
	return missing
}

// dbObjects 一个库内的各类对象清单
type dbObjects map[objectKind][]string

// listDBObjectsInSchema 枚举单个 schema 内的对象清单（裸名），复用底层库 GetObjects 方言能力；
// PG 系走 listSchemaObjects 一次往返拿全（避免与表枚举重复查视图）
func listDBObjectsInSchema(cli *cydb.DBCli, db, schema string) dbObjects {
	objs := dbObjects{}
	if strings.EqualFold(cli.DBType(), "postgresql") {
		_, views, funcs, procs, err := listSchemaObjects(cli, db, schema)
		if err == nil {
			if len(views) > 0 {
				objs[objectView] = views
			}
			if len(funcs) > 0 {
				objs[objectFunction] = funcs
			}
			if len(procs) > 0 {
				objs[objectProcedure] = procs
			}
			return objs
		}
	}
	var schemaPtr *string
	if schema != "" {
		schemaPtr = &schema
	}
	if strings.EqualFold(cli.DBType(), "oracle") {
		// Oracle 无多库概念，导出时的 db 即 schema(owner)
		schemaPtr = &db
	}
	for _, kind := range objectExportOrder {
		names, err := cli.GetObjects(db, schemaPtr, kindObjectType[kind])
		if err == nil {
			objs[kind] = names
		}
	}
	return objs
}

// listDBObjects 枚举库内的视图/函数/存储过程（触发器随建表语句由底层库一并导出，不单独枚举）。
// PG 系按 schema 分层（schema 为空时遍历全部用户 schema），返回限定名 "schema.对象名"；
// MySQL/Oracle 返回裸名（schema 透传方言）。单类失败仅跳过，不阻断主流程
func listDBObjects(cli *cydb.DBCli, db, schema string) dbObjects {
	objs := dbObjects{}
	if !strings.EqualFold(cli.DBType(), "postgresql") {
		return listDBObjectsInSchema(cli, db, schema)
	}
	schemas := make([]string, 0, 1)
	if schema != "" {
		schemas = append(schemas, schema)
	} else {
		names, err := cli.GetSchemas(db)
		if err != nil {
			return objs
		}
		schemas = append(schemas, names...)
	}
	for _, sch := range schemas {
		sub := listDBObjectsInSchema(cli, db, sch)
		for _, kind := range objectExportOrder {
			for _, n := range sub[kind] {
				objs[kind] = append(objs[kind], sch+"."+n)
			}
		}
	}
	return objs
}

// kindObjectType 应用层对象类型 → 底层库对象类型
var kindObjectType = map[objectKind]dialect.DatabaseObjectType{
	objectView:      cydb.ObjectTypeView,
	objectFunction:  cydb.ObjectTypeFunction,
	objectProcedure: cydb.ObjectTypeProcedure,
}

// ObjectDDLType 可供查询创建语句的对象类型（对应用户可见的 DDL 对象）
type ObjectDDLType string

const (
	ObjectDDLTable     ObjectDDLType = "table"
	ObjectDDLView      ObjectDDLType = "view"
	ObjectDDLFunction  ObjectDDLType = "function"
	ObjectDDLProcedure ObjectDDLType = "procedure"
)

// GetObjectDDL 获取指定对象（表/视图/函数/存储过程）的创建语句。
// 复用底层库方言 DDL 能力；表 DDL 已包含该表触发器。
func GetObjectDDL(cli *cydb.DBCli, objType ObjectDDLType, name string) (string, error) {
	switch objType {
	case ObjectDDLTable:
		return ddlContent(cli, dialect.FuncNameGetCreateTableSql, name)
	case ObjectDDLView:
		return ddlContent(cli, dialect.FuncNameGetCreateViewSql, name)
	case ObjectDDLFunction:
		return ddlContent(cli, dialect.FuncNameGetCreateFunctionSql, name)
	case ObjectDDLProcedure:
		return ddlContent(cli, dialect.FuncNameGetCreateProcedureSql, name)
	default:
		return "", NewMsgErr(errObjType, objType)
	}
}

// objectDDL 获取单个对象的创建语句（复用底层库方言 DDL 能力）
func objectDDL(cli *cydb.DBCli, kind objectKind, name string) (string, error) {
	switch kind {
	case objectView:
		return ddlContent(cli, dialect.FuncNameGetCreateViewSql, name)
	case objectFunction:
		return ddlContent(cli, dialect.FuncNameGetCreateFunctionSql, name)
	case objectProcedure:
		return ddlContent(cli, dialect.FuncNameGetCreateProcedureSql, name)
	default:
		return "", NewMsgErr(errObjType, kind)
	}
}

func ddlContent(cli *cydb.DBCli, funcName dialect.DDLSqlFuncName, name string) (string, error) {
	content, err := cli.GetDDLSql(funcName, name)
	if err != nil {
		return "", err
	}
	if content == nil || strings.TrimSpace(content.Content) == "" {
		return "", NewMsgErr(errObjDDL, name)
	}
	return content.Content, nil
}
