import { InboxOutlined } from '@ant-design/icons'
import type { ReactNode } from 'react'

/**
 * 空状态。
 * §5.1 的原则：没有内容时整块消失优于渲染占位框 —— 空状态不是内容。
 * 只有当"空"本身是需要解释的信息时（筛选无结果、功能未接入）才用这个组件。
 */
export function EmptyState({
  title,
  description,
  icon,
  action,
  compact,
}: {
  title: string
  description?: string
  icon?: ReactNode
  action?: ReactNode
  compact?: boolean
}) {
  return (
    <div
      className={`flex flex-col items-center justify-center rounded border border-dashed border-line text-center ${
        compact ? 'gap-1.5 p-6' : 'gap-2 p-12'
      }`}
    >
      <span className="text-xl text-ink-muted">{icon ?? <InboxOutlined />}</span>
      <h3 className="m-0 text-sm font-medium text-ink">{title}</h3>
      {description && <p className="m-0 max-w-md text-xs text-ink-muted">{description}</p>}
      {action && <div className="mt-2">{action}</div>}
    </div>
  )
}
