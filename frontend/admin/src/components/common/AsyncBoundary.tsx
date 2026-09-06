import { ReloadOutlined, WarningFilled } from '@ant-design/icons'
import { Button, Skeleton } from 'antd'
import type { ReactNode } from 'react'

/**
 * 加载 / 错误 / 内容 三态的统一包装。
 * 错误态必须给出重试入口和原始信息，不能只显示一句"出错了"。
 */
export function AsyncBoundary({
  loading,
  error,
  onRetry,
  children,
  skeletonRows = 4,
}: {
  loading: boolean
  error?: Error
  onRetry?: () => void
  children: ReactNode
  skeletonRows?: number
}) {
  if (loading) {
    return <Skeleton active paragraph={{ rows: skeletonRows }} title={false} />
  }

  if (error) {
    return (
      <div className="flex flex-col items-center gap-2.5 rounded border border-dashed border-line p-8 text-center">
        <WarningFilled className="text-xl text-status-critical" />
        <h3 className="m-0 text-sm font-medium">数据加载失败</h3>
        <p className="m-0 max-w-md font-mono text-[11px] text-ink-muted">{error.message}</p>
        {onRetry && (
          <Button size="small" icon={<ReloadOutlined />} onClick={onRetry} className="mt-1">
            重试
          </Button>
        )}
      </div>
    )
  }

  return <>{children}</>
}
