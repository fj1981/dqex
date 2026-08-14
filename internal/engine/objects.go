package engine

import (
	"fmt"
	"strings"

	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cydb"
	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cydb/dialect"
)

// objectKind 数据库对象类型
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

// objectInWhitelist 判断对象（id 格式 目录/名）是否命中白名单。
// 白名单条目支持限定形式 "库.目录/名"（仅对应库生效）与裸形式 "目录/名"（匹配任意库，便于 CLI 手输）
func objectInWhitelist(allowed map[string]bool, db, id string) bool {
	return allowed[id] || allowed[db+"."+id]
}

// dbObjects 一个库内的各类对象清单
type dbObjects map[objectKind][]string

// listDBObjects 枚举库内的视图/函数/存储过程（触发器随建表语句由底层库一并导出，不单独枚举）。
// 对象列表查询复用底层库 GetObjects 方言能力；单类失败仅跳过，不阻断表数据导出
func listDBObjects(cli *cydb.DBCli, db, schema string) dbObjects {
	objs := dbObjects{}
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
		return "", fmt.Errorf("不支持的对象类型: %s", objType)
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
		return "", fmt.Errorf("不支持的对象类型: %s", kind)
	}
}

func ddlContent(cli *cydb.DBCli, funcName dialect.DDLSqlFuncName, name string) (string, error) {
	content, err := cli.GetDDLSql(funcName, name)
	if err != nil {
		return "", err
	}
	if content == nil || strings.TrimSpace(content.Content) == "" {
		return "", fmt.Errorf("未获取到 %s 的创建语句", name)
	}
	return content.Content, nil
}
