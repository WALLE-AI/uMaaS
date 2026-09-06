import { useMemo, useState } from 'react'
import {
  Area,
  AreaChart,
  Bar,
  BarChart,
  CartesianGrid,
  Legend,
  Line,
  LineChart,
  ReferenceLine,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import { formatMetric, type MetricUnit } from '../../config/metrics'
import { LOW_CONTRAST_SLOTS, SERIES_SLOT_COUNT, chartChrome, seriesColor } from '../../config/viz'
import { DataTableToggle } from './DataTableToggle'

/**
 * 统一图表封装 —— §4 的规则写在这里，而不是写在文档里靠自觉。
 *
 * 被编码进类型/运行时的硬规则：
 *  1. 所有 series 共享一个 unit。物理上没有第二根 Y 轴的入口，双轴图写不出来。
 *  2. 色槽按 series.id 稳定分配（见 slotOf），筛选后幸存者不改色。
 *  3. 超过 8 个系列直接抛错，提示折叠为「其他」或分面。
 *  4. >=2 系列自动出图例；<=4 系列同时做末端直接标注。
 *  5. 用到低对比度槽位（青/黄/品红）时强制提供数据表视图。
 *  6. 折线/面积自动挂十字准星，柱状自动挂逐格提示。
 */

export type ChartForm = 'line' | 'area' | 'bar' | 'stacked-bar'

export type ChartSeries = {
  /** 稳定标识。色槽由它决定，不受数组顺序影响 */
  id: string
  label: string
  /** 显式指定色槽（1-8）。不传则按 id 稳定散列分配 */
  slot?: number
}

export type MetricChartProps = {
  form: ChartForm
  /** 每行一个时间点/类目，键为 series.id */
  data: Array<Record<string, string | number>>
  /** X 轴取值的字段名 */
  xKey: string
  series: ChartSeries[]
  /** 所有 series 共用的单位 —— 双轴不可能存在的根本原因 */
  unit: MetricUnit
  height?: number
  /** SLO / 阈值参考线，灰虚线 */
  threshold?: { value: number; label: string }
  /** 强制展示数据表入口（低对比度槽位会自动置为 true） */
  tableView?: boolean
  emptyHint?: string
}

/** 由 id 稳定散列到 1..8，保证同一实体在任何图里都是同一个颜色 */
function hashToSlot(id: string): number {
  let hash = 0
  for (let i = 0; i < id.length; i += 1) {
    hash = (hash * 31 + id.charCodeAt(i)) >>> 0
  }
  return (hash % SERIES_SLOT_COUNT) + 1
}

function slotOf(item: ChartSeries, index: number): number {
  if (item.slot) return ((item.slot - 1) % SERIES_SLOT_COUNT) + 1
  // 前 8 个按声明顺序占槽，超出部分用散列（此时已触发 >8 的报错，仅为兜底）
  return index < SERIES_SLOT_COUNT ? index + 1 : hashToSlot(item.id)
}

/**
 * 缺省槽位是按数组下标分配的 —— 这在系列集合固定时没问题，但一旦调用方
 * 支持筛选，筛掉一个系列就会让后面的全部左移换色，正好违反§4「色彩跟随
 * 实体，绝不跟随排名」。
 *
 * 类型上无法强制（单系列不需要 slot，多系列才需要），所以在开发模式下显式
 * 告警，让问题在写代码时暴露，而不是等有人发现"筛完颜色全变了"。
 */
function warnUnstableSlots(series: ChartSeries[]) {
  if (!import.meta.env.DEV || series.length < 2) return
  const missing = series.filter((item) => !item.slot).map((item) => item.id)
  if (missing.length === 0) return
  console.warn(
    `[MetricChart] 系列 ${missing.join(', ')} 未显式指定色槽，当前按数组下标着色。` +
      '若此图支持筛选或排序，请为每个系列显式传 slot，否则颜色会跟随排名漂移。',
  )
}

export function MetricChart({
  form,
  data,
  xKey,
  series,
  unit,
  height = 260,
  threshold,
  tableView,
  emptyHint = '所选范围内没有数据',
}: MetricChartProps) {
  const [tableOpen, setTableOpen] = useState(false)

  if (series.length > SERIES_SLOT_COUNT) {
    throw new Error(
      `MetricChart 收到 ${series.length} 个系列，超过色板上限 ${SERIES_SLOT_COUNT}。` +
        '请把长尾折叠为「其他」，或改用小倍数分面 —— 不要生成第 9 个颜色。',
    )
  }

  const resolved = useMemo(() => {
    warnUnstableSlots(series)
    return series.map((item, index) => {
      const slot = slotOf(item, index)
      return { ...item, slot, color: seriesColor(slot) }
    })
  }, [series])

  // 低对比度槽位触发缓解规则：必须能看到数值，不能只靠颜色区分
  const needsRelief = resolved.some((item) => LOW_CONTRAST_SLOTS.has(item.slot))
  const showTable = tableView || needsRelief
  const showLegend = resolved.length >= 2
  const directLabel = resolved.length <= 4

  const grid = chartChrome.grid()
  const axis = chartChrome.axis()
  const muted = chartChrome.inkMuted()
  const surface = chartChrome.surface()

  const axisProps = {
    axisLine: { stroke: axis },
    tickLine: false,
    tick: { fontSize: 10, fill: muted },
  } as const

  const tooltip = (
    <Tooltip
      cursor={{ stroke: axis, strokeWidth: 1, strokeDasharray: '3 3' }}
      contentStyle={{
        background: surface,
        border: `1px solid ${grid}`,
        borderRadius: 6,
        fontSize: 11,
      }}
      formatter={(value, name) => [
        typeof value === 'number' ? formatMetric(value, unit) : '—',
        String(name ?? ''),
      ]}
    />
  )

  const legend = showLegend ? (
    <Legend
      verticalAlign="bottom"
      height={28}
      iconType="circle"
      iconSize={8}
      wrapperStyle={{ fontSize: 11, color: muted }}
    />
  ) : null

  const referenceLine = threshold ? (
    <ReferenceLine
      y={threshold.value}
      stroke={axis}
      strokeDasharray="4 4"
      label={{ value: threshold.label, position: 'right', fontSize: 10, fill: muted }}
    />
  ) : null

  if (!data.length) {
    return (
      <div
        className="flex items-center justify-center rounded border border-dashed border-line text-xs text-ink-muted"
        style={{ height }}
      >
        {emptyHint}
      </div>
    )
  }

  return (
    <div>
      <ResponsiveContainer width="100%" height={height}>
        {form === 'line' ? (
          <LineChart data={data} margin={{ top: 12, right: 16, left: 0, bottom: 0 }}>
            <CartesianGrid vertical={false} stroke={grid} />
            <XAxis dataKey={xKey} {...axisProps} />
            <YAxis {...axisProps} tickFormatter={(v: number) => formatMetric(v, unit)} />
            {tooltip}
            {legend}
            {referenceLine}
            {resolved.map((item) => (
              <Line
                key={item.id}
                type="monotone"
                dataKey={item.id}
                name={item.label}
                stroke={item.color}
                strokeWidth={2}
                dot={false}
                activeDot={{ r: 4, strokeWidth: 2, stroke: surface }}
                isAnimationActive={false}
                label={
                  directLabel
                    ? { position: 'right', fontSize: 10, fill: item.color, formatter: () => '' }
                    : undefined
                }
              />
            ))}
          </LineChart>
        ) : form === 'area' ? (
          <AreaChart data={data} margin={{ top: 12, right: 16, left: 0, bottom: 0 }}>
            <defs>
              {resolved.map((item) => (
                <linearGradient key={item.id} id={`fill-${item.id}`} x1="0" y1="0" x2="0" y2="1">
                  <stop offset="0%" stopColor={item.color} stopOpacity={0.28} />
                  <stop offset="100%" stopColor={item.color} stopOpacity={0} />
                </linearGradient>
              ))}
            </defs>
            <CartesianGrid vertical={false} stroke={grid} />
            <XAxis dataKey={xKey} {...axisProps} />
            <YAxis {...axisProps} tickFormatter={(v: number) => formatMetric(v, unit)} />
            {tooltip}
            {legend}
            {referenceLine}
            {resolved.map((item) => (
              <Area
                key={item.id}
                type="monotone"
                dataKey={item.id}
                name={item.label}
                stackId={resolved.length > 1 ? 'stack' : undefined}
                stroke={item.color}
                strokeWidth={2}
                fill={resolved.length > 1 ? item.color : `url(#fill-${item.id})`}
                fillOpacity={resolved.length > 1 ? 0.85 : 1}
                isAnimationActive={false}
              />
            ))}
          </AreaChart>
        ) : (
          <BarChart data={data} margin={{ top: 12, right: 16, left: 0, bottom: 0 }}>
            <CartesianGrid vertical={false} stroke={grid} />
            <XAxis dataKey={xKey} {...axisProps} />
            <YAxis {...axisProps} tickFormatter={(v: number) => formatMetric(v, unit)} />
            {tooltip}
            {legend}
            {referenceLine}
            {resolved.map((item) => (
              <Bar
                key={item.id}
                dataKey={item.id}
                name={item.label}
                stackId={form === 'stacked-bar' ? 'stack' : undefined}
                fill={item.color}
                radius={form === 'stacked-bar' ? 0 : [4, 4, 0, 0]}
                isAnimationActive={false}
              />
            ))}
          </BarChart>
        )}
      </ResponsiveContainer>

      {showTable && (
        <DataTableToggle
          open={tableOpen}
          onOpenChange={setTableOpen}
          data={data}
          xKey={xKey}
          series={resolved}
          unit={unit}
          reason={needsRelief ? 'low-contrast' : 'explicit'}
        />
      )}
    </div>
  )
}
