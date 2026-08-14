import * as monaco from "monaco-editor"
// worker 入口：monaco-editor 的 exports 字段已把 "/*" 映射到 "./esm/vs/*"，
// 因此子路径必须去掉 esm/vs 前缀（带前缀会被重复拼接而解析失败），并带 .js 后缀。
import EditorWorker from "monaco-editor/editor/editor.worker.js?worker"
import { loader } from "@monaco-editor/react"
import { registerSQLCompletion } from "@/lib/sqlCompletion"

// monaco-editor 的 ESM 主入口已 import 全部语言定义（含 SQL），并导出 KeyCode/KeyMod/
// editor/languages 等。但 SQL 基础语言只做语法高亮，不含自动补全，需自行注册 provider。

// 配置 worker：SQL 场景仅需基础 editor worker。构建期打包为本地独立文件，零 CDN。
globalThis.MonacoEnvironment = {
  getWorker: () => new EditorWorker(),
}

// 将本地 monaco 实例注入 @monaco-editor/react 的 loader，使其完全本地化、不从 CDN 加载。
loader.config({ monaco })

// 注册 SQL 自动补全：关键字 + 库名 + 表名（元数据复用对象树 store）
registerSQLCompletion(monaco)

export { monaco }
