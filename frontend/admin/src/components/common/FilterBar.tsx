import type { ReactNode } from 'react'

/**
 * 筛选行 —— §4.4 规定筛选控件统一放在图表区上方一行。
 * 左侧放筛选，右侧放动作（导出等）。
 */
export function FilterBar({ children, actions }: { children: ReactNode; actions?: ReactNode }) {
  return (
    <div className="mb-4 flex flex-wrap items-center justify-between gap-3 border-b border-line pb-3">
      <div className="flex flex-wrap items-center gap-2">{children}</div>
      {actions && <div className="flex items-center gap-2">{actions}</div>}
    </div>
  )
}

/** 带标签的筛选控件包装，保证标签样式一致 */
export function FilterField({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="inline-flex items-center gap-1.5">
      <span className="text-[11px] whitespace-nowrap text-ink-muted">{label}</span>
      {children}
    </label>
  )
}
