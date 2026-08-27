// 任务配置恢复的连接绑定规则：库/表/对象/条件选择绑定配置的 sourceConn（这些选择来自源连接的对象树）。
// 自动恢复（getLastTask）时页面已选连接与配置不一致（跨环境/已切换连接）→ 配置的库表选择视为无效：
// 连接保持当前选择，仅恢复通用选项（输出目录/压缩/批处理等）；
// 页面未选连接或连接一致 → 完整恢复（配置的连接与库表作为整体恢复）。
// 显式应用（手动选择配置 / URL 跳转）不走此绑定校验，按用户意图整体切换。

const CONN_BOUND_KEYS = ["databases", "tables", "objects", "conditions", "selections"] as const

export function bindTaskOptions<T extends object>(
  taskOpts: T,
  currentSourceConn: string | undefined,
): Partial<T> {
  const src = taskOpts as Record<string, unknown>
  if (!currentSourceConn || src.sourceConn === currentSourceConn) return taskOpts
  const base = { ...taskOpts } as Record<string, unknown>
  delete base.sourceConn
  for (const k of CONN_BOUND_KEYS) delete base[k]
  return base as Partial<T>
}
