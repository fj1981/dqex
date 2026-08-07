import { clsx, type ClassValue } from "clsx"
import { twMerge } from "tailwind-merge"

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

export function formatBytes(size: number): string {
  if (!size || size <= 0) return "0 B"
  const units = ["B", "KB", "MB", "GB", "TB"]
  let i = 0
  let v = size
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v.toFixed(i === 0 ? 0 : 1)} ${units[i]}`
}

export function formatTime(iso: string): string {
  if (!iso) return "-"
  const d = new Date(iso)
  if (isNaN(d.getTime())) return iso
  const pad = (n: number) => String(n).padStart(2, "0")
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

// 压缩消息中的绝对路径为文件名（界面不展示完整服务器路径，日志保留原文）
export function shortPaths(s: string): string {
  return s.replace(/\/[\w./-]+\/[\w.-]+/g, (p) => p.split("/").pop() || p)
}
