// react-i18next 类型增强：以 zh 字典为基准，t() 的 key 获得编译期校验
import "i18next"
import type zh from "@/locales/zh"

declare module "i18next" {
  interface CustomTypeOptions {
    defaultNS: "translation"
    resources: {
      translation: typeof zh
    }
  }
}
