// 数据库操作错误友好化：把驱动/连接层原始错误（dial tcp ...: connect: connection refused 等）
// 转为用户可读的标题、原因与排查建议（文案按 UI 语言翻译）；无法识别的模式返回 null（调用方原样展示）。
// 仅在文本明确匹配时生效，绝不把未知错误硬翻译，避免误导排查方向。

import i18n from "@/lib/i18n"

export interface FriendlyDBError {
  title: string // 简短标题，如「无法连接数据库」
  reason: string // 一句话原因（含目标地址）
  advice: string[] // 排查建议列表
}

// 从错误文本中提取目标地址 host:port（形如 "dial tcp 10.20.16.170:3317" 或 "(root@10.20.16.170:3317/..."）
function extractAddr(msg: string): string {
  const m = msg.match(/dial tcp ([^\s:]+):(\d+)/i) || msg.match(/\([^)]*@?([^\s:()]+):(\d+)/)
  return m ? `${m[1]}:${m[2]}` : ""
}

export function friendlyDBError(raw: string): FriendlyDBError | null {
  const msg = raw.trim()
  if (!msg) return null

  const addr = extractAddr(msg)
  const host = addr ? `（${addr}）` : ""

  // 连接被拒绝：服务未启动 / 端口错误 / 防火墙拦截
  if (/connection refused/i.test(msg)) {
    return {
      title: i18n.t("dbError.refusedTitle"),
      reason: i18n.t("dbError.refusedReason", { host }),
      advice: [
        i18n.t("dbError.refusedAdvice1"),
        i18n.t("dbError.refusedAdvice2"),
        i18n.t("dbError.refusedAdvice3"),
      ],
    }
  }
  // 连接超时：网络延迟 / 防火墙丢包 / 服务繁忙
  if (/connection timed out|i\/o timeout|dial tcp.*timed out/i.test(msg)) {
    return {
      title: i18n.t("dbError.timeoutTitle"),
      reason: i18n.t("dbError.timeoutReason", { host }),
      advice: [
        i18n.t("dbError.timeoutAdvice1"),
        i18n.t("dbError.timeoutAdvice2"),
        i18n.t("dbError.timeoutAdvice3"),
      ],
    }
  }
  // 认证失败：用户名或密码错误 / 用户被拒绝
  if (/access denied|authentication failed/i.test(msg)) {
    return {
      title: i18n.t("dbError.authTitle"),
      reason: i18n.t("dbError.authReason"),
      advice: [i18n.t("dbError.authAdvice1"), i18n.t("dbError.authAdvice2")],
    }
  }
  // 库不存在：拼写错误 / 未创建
  if (/unknown database/i.test(msg)) {
    return {
      title: i18n.t("dbError.noDBTitle"),
      reason: i18n.t("dbError.noDBReason"),
      advice: [i18n.t("dbError.noDBAdvice1"), i18n.t("dbError.noDBAdvice2")],
    }
  }
  // DNS 解析失败
  if (/no such host|lookup .* failed|server not found/i.test(msg)) {
    return {
      title: i18n.t("dbError.dnsTitle"),
      reason: i18n.t("dbError.dnsReason"),
      advice: [i18n.t("dbError.dnsAdvice1"), i18n.t("dbError.dnsAdvice2")],
    }
  }
  // 连接中断：服务重启 / 网络抖动 / 连接超时回收
  if (/broken pipe|connection reset|server closed|unexpected EOF|connection aborted/i.test(msg)) {
    return {
      title: i18n.t("dbError.brokenTitle"),
      reason: i18n.t("dbError.brokenReason"),
      advice: [i18n.t("dbError.brokenAdvice1"), i18n.t("dbError.brokenAdvice2")],
    }
  }
  return null
}
