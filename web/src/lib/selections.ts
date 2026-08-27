import type { DBSelection } from "@/types"

// 组装结构化库→表/对象选择：按“库.”前缀把表/对象分组到各自库下，
// 库与表在同一个结构内成对提交，杜绝扁平字段（databases/tables）间库名残留/错配。
// 库无任何选中表/对象时为整库导出（tables=null，由后端枚举全部；连不上时后端跳过）
export function buildSelections(
  databases: string[] | undefined,
  tables: string[] | undefined,
  objects?: string[] | undefined,
): DBSelection[] {
  const dbs = new Set<string>(databases || [])
  for (const t of tables || []) {
    const db = t.split(".")[0]
    if (db) dbs.add(db)
  }
  for (const o of objects || []) {
    const db = o.split(".")[0]
    if (db) dbs.add(db)
  }
  const sels: DBSelection[] = []
  for (const db of dbs) {
    const dbTables = (tables || []).filter((t) => t.startsWith(db + "."))
    const dbObjects = (objects || []).filter((o) => o.startsWith(db + "."))
    sels.push({
      db,
      tables: dbTables.length > 0 ? dbTables : null,
      objects: dbObjects.length > 0 ? dbObjects : null,
    })
  }
  return sels
}
