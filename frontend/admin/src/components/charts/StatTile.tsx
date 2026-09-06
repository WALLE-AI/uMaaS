import { InfoCircleOutlined } from '@ant-design/icons'
import { Tooltip } from 'antd'
import type { ReactNode } from 'react'
import { Area, AreaChart, ResponsiveContainer } from 'recharts'
import { chartChrome } from '../../config/viz'

/**
 * 统计磁贴 —— 单个当前值（可带趋势）的正确形态。
 * §4.3：单个数字用磁贴，不用只有一根柱子的柱状图。
 *
 * delta 用文字前缀（↑/↓）而不是只用颜色表达方向，色觉障碍下同样可读。
 */
export type StatTileProps = {
  label: string
  value: string
  note?: string
  delta?: { value: string; direction: 'up' | 'down' | 'flat'; good?: boolean }
  icon?: ReactNode
  /** 迷你走势，只画形状不画坐标轴 */
  sparkline?: number[]
  /** 指标口径说明 */
  caveat?: string
}

export function StatTile({ label, value, note, delta, icon, sparkline, caveat }: StatTileProps) {
  const arrow = delta?.direction === 'up' ? '↑' : delta?.direction === 'down' ? '↓' : '→'
  const deltaTone =
    delta === undefined
      ? ''
      : delta.good === undefined
        ? 'text-ink-muted'
        : delta.good
          ? 'text-status-good'
          : 'text-status-critical'

  return (
    <div className="relative flex min-w-0 flex-col border-r border-line bg-surface p-4 last:border-r-0">
      <span className="flex items-center gap-1.5 text-[10px] text-ink-secondary">
        {icon && <span className="text-brand">{icon}</span>}
        {label}
        {caveat && (
          <Tooltip title={caveat}>
            <InfoCircleOutlined className="cursor-help text-[10px] text-ink-muted" />
          </Tooltip>
        )}
      </span>

      <b className="mt-3 font-mono text-[22px] leading-none font-semibold">{value}</b>

      {note && <small className="mt-1.5 text-[10px] text-ink-muted">{note}</small>}

      {delta && (
        <em className={`mt-2 font-mono text-[10px] not-italic ${deltaTone}`}>
          {arrow} {delta.value}
        </em>
      )}

      {sparkline && sparkline.length > 1 && (
        <div className="mt-2 h-8">
          <ResponsiveContainer width="100%" height="100%">
            <AreaChart data={sparkline.map((v, i) => ({ i, v }))}>
              <Area
                type="monotone"
                dataKey="v"
                stroke={chartChrome.brand()}
                strokeWidth={1.5}
                fill={chartChrome.brand()}
                fillOpacity={0.12}
                isAnimationActive={false}
              />
            </AreaChart>
          </ResponsiveContainer>
        </div>
      )}
    </div>
  )
}

/** KPI 行：多个磁贴并排，等宽 */
export function StatTileRow({ items }: { items: StatTileProps[] }) {
  return (
    <section
      className="mb-4 grid overflow-hidden rounded border border-line"
      style={{ gridTemplateColumns: `repeat(${items.length}, minmax(0, 1fr))` }}
    >
      {items.map((item) => (
        <StatTile key={item.label} {...item} />
      ))}
    </section>
  )
}
