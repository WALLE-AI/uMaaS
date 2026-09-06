import type { ReactNode } from 'react'
import { useLocation } from 'react-router'
import { breadcrumbFor } from '../../config/navigation'

/**
 * 页面标题区。eyebrow 取自导航分组，不手写 —— 保证与信息架构一致。
 */
export function PageHeader({
  title,
  description,
  actions,
}: {
  title?: string
  description: string
  actions?: ReactNode
}) {
  const location = useLocation()
  const crumb = breadcrumbFor(location.pathname)

  return (
    <div className="mb-6 flex flex-wrap items-end justify-between gap-4 border-b border-line pb-5">
      <div className="min-w-0">
        <span className="font-mono text-[9px] tracking-widest text-brand">
          {(crumb?.group ?? 'ADMIN').toUpperCase()}
        </span>
        <h1 className="mt-1.5 mb-1 text-2xl font-semibold tracking-tight">
          {title ?? crumb?.label ?? '页面'}
        </h1>
        <p className="m-0 text-xs text-ink-muted">{description}</p>
      </div>
      {actions && <div className="flex shrink-0 items-center gap-2">{actions}</div>}
    </div>
  )
}
