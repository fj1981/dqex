// 语言注册表（单一来源）：新增语言 = 新建 <code>.ts 字典 + 在此注册，业务代码零改动
import zh from "./zh"
import en from "./en"

export const SUPPORTED_LANGS: { code: string; label: string }[] = [
  { code: "zh", label: zh.lang.zh },
  { code: "en", label: en.lang.en },
]

// i18next 资源聚合映射（key = 语言代码）
export const resources = {
  zh: { translation: zh },
  en: { translation: en },
} as const

// 默认语言（缺翻译/无法识别时的回退）
export const FALLBACK_LANG = "zh"

// 根据浏览器/存储的完整语言码（如 zh-CN / ja-JP）解析为注册表中的主语言码
export function resolveLang(raw: string | undefined | null): string {
  if (!raw) return FALLBACK_LANG
  const code = raw.toLowerCase()
  for (const { code: c } of SUPPORTED_LANGS) {
    if (code === c || code.startsWith(`${c}-`) || code.startsWith(`${c}_`)) return c
  }
  return FALLBACK_LANG
}
