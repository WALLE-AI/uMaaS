/**
 * §4 图表规范的 JS 侧读取。
 *
 * 色值的真相源是 index.css 的 @theme 块 —— 这里只负责在运行时把 CSS 变量
 * 读成 JS 字符串喂给 Recharts（SVG 的 fill/stroke 不能直接吃 utility 类）。
 * 不在这里重复写十六进制，否则就有了第二个真相源。
 */

function cssVar(name: string, fallback: string): string {
  if (typeof window === 'undefined') return fallback
  const value = getComputedStyle(document.documentElement).getPropertyValue(name).trim()
  return value || fallback
}

/** 分类色板：固定 8 槽，按 series.id 稳定分配，永不循环、永不按排名重排 */
const SERIES_FALLBACK = [
  '#2a78d6', '#eb6834', '#1baf7a', '#eda100',
  '#e87ba4', '#008300', '#4a3aa7', '#e34948',
] as const

export const SERIES_SLOT_COUNT = SERIES_FALLBACK.length

/**
 * 低对比度槽位（青/黄/品红）。
 * 白色绘图面上对比度低于 3:1，实测：aqua 2.82、yellow 2.17、magenta 2.69。
 * 用到这几槽时必须提供直接标注或数据表视图（§4.1 缓解规则）。
 */
export const LOW_CONTRAST_SLOTS = new Set([3, 4, 5])

/** 散点/气泡/地图等「任意两色同屏相邻」的形态，色板上限为前 3 槽（§4.1） */
export const ALL_PAIRS_SERIES_CAP = 3

export function seriesColor(slot: number): string {
  const index = ((slot - 1) % SERIES_SLOT_COUNT + SERIES_SLOT_COUNT) % SERIES_SLOT_COUNT
  return cssVar(`--color-series-${index + 1}`, SERIES_FALLBACK[index])
}

/** 顺序色阶：单一蓝色相，浅→深。用于热力图等连续量级，禁止彩虹 */
const SEQ_STEPS = [100, 200, 300, 400, 500, 600, 700] as const
const SEQ_FALLBACK = ['#cde2fb', '#9ec5f4', '#6da7ec', '#3987e5', '#256abf', '#184f95', '#0d366b']

export function sequentialScale(): string[] {
  return SEQ_STEPS.map((step, i) => cssVar(`--color-seq-${step}`, SEQ_FALLBACK[i]))
}

/** 把 0–1 的归一化值映射到顺序色阶的某一档 */
export function sequentialColor(ratio: number): string {
  const scale = sequentialScale()
  const clamped = Number.isFinite(ratio) ? Math.min(1, Math.max(0, ratio)) : 0
  return scale[Math.min(scale.length - 1, Math.round(clamped * (scale.length - 1)))]
}

/** 状态色：保留槽位，永不用作系列色；必须搭配图标与文字 */
export type StatusTone = 'good' | 'warning' | 'serious' | 'critical'

const STATUS_FALLBACK: Record<StatusTone, string> = {
  good: '#0ca30c',
  warning: '#fab219',
  serious: '#ec835a',
  critical: '#d03b3b',
}

export function statusColor(tone: StatusTone): string {
  return cssVar(`--color-status-${tone}`, STATUS_FALLBACK[tone])
}

/** 图表 chrome */
export const chartChrome = {
  grid: () => cssVar('--color-grid', '#e1e0d9'),
  axis: () => cssVar('--color-axis', '#c3c2b7'),
  ink: () => cssVar('--color-ink', '#171719'),
  inkMuted: () => cssVar('--color-ink-muted', '#898781'),
  brand: () => cssVar('--color-brand', '#7c3cff'),
  surface: () => cssVar('--color-surface', '#ffffff'),
}

/** 单系列图表用品牌紫（白底对比度实测 5.32:1，安全） */
export const SINGLE_SERIES_COLOR = () => chartChrome.brand()
