import i18n from "i18next"
import { initReactI18next } from "react-i18next"
import { FALLBACK_LANG, SUPPORTED_LANGS, resolveLang, resources } from "@/locales"

// localStorage 持久化键（与 next-themes 的 dbimpex-theme 风格一致）
export const LANG_STORAGE_KEY = "dbimpex-lang"

// 语言解析：手动选择（localStorage）> 浏览器语言（注册表精确/前缀匹配）> 默认
function detectLang(): string {
  try {
    const saved = localStorage.getItem(LANG_STORAGE_KEY)
    if (saved) return resolveLang(saved)
  } catch {
    // localStorage 不可用（隐私模式等）时忽略，走浏览器语言
  }
  if (typeof navigator !== "undefined") {
    const l = navigator.language || navigator.languages?.[0]
    return resolveLang(l)
  }
  return FALLBACK_LANG
}

const lng = detectLang()

i18n.use(initReactI18next).init({
  resources,
  lng,
  fallbackLng: FALLBACK_LANG,
  supportedLngs: SUPPORTED_LANGS.map((l) => l.code),
  interpolation: { escapeValue: false }, // React 已做 XSS 转义
})

// 切换语言并持久化（顶栏 / 设置页共用）
export function changeUILang(code: string) {
  const lang = resolveLang(code)
  i18n.changeLanguage(lang)
  try {
    localStorage.setItem(LANG_STORAGE_KEY, lang)
  } catch {
    // 忽略持久化失败
  }
  // 同步更新 <html lang> 属性，利于辅助技术
  document.documentElement.lang = lang
}

// 动态 key 翻译（key 来自常量映射表，编译期为 string）：
// t() 的 key 有编译期校验（CustomTypeOptions），动态 key 无法静态推断，
// 用此包装绕过联合类型限制；key 本身仍是注册表中的合法路径。
// 组件内需同时调用 useTranslation() 以获得语言切换重渲染。
export const tKey = (key: string): string => i18n.t(key as never)

document.documentElement.lang = lng

export default i18n
