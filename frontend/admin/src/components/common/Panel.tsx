import type { ReactNode } from 'react'

/**
 * 卡片容器。所有图表、表格、列表都装在这里，保证边框与内距一致。
 */
export function Panel({
  eyebrow,
  title,
  description,
  actions,
  children,
  className = '',
  bodyClassName = '',
}: {
  eyebrow?: string
  title?: string
  description?: string
  actions?: ReactNode
  children: ReactNode
  className?: string
  bodyClassName?: string
}) {
  const hasHead = eyebrow || title || description || actions

  return (
    <section className={`rounded border border-line bg-surface p-5 ${className}`}>
      {hasHead && (
        <div className="mb-4 flex flex-wrap items-start justify-between gap-4">
          <div className="min-w-0">
            {eyebrow && (
              <span className="font-mono text-[8px] tracking-widest text-brand">{eyebrow}</span>
            )}
            {title && <h2 className="mt-1 mb-0 text-[15px] font-semibold">{title}</h2>}
            {description && <p className="mt-1 mb-0 text-[11px] text-ink-muted">{description}</p>}
          </div>
          {actions && <div className="flex shrink-0 items-center gap-2">{actions}</div>}
        </div>
      )}
      <div className={bodyClassName}>{children}</div>
    </section>
  )
}
