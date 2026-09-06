import { DownloadOutlined } from '@ant-design/icons'
import { Button, Radio, Segmented, Select } from 'antd'
import { useState } from 'react'
import { Heatmap } from '../../components/charts/Heatmap'
import { HeroFigure } from '../../components/charts/HeroFigure'
import { MetricChart } from '../../components/charts/MetricChart'
import { AsyncBoundary } from '../../components/common/AsyncBoundary'
import { FilterBar, FilterField } from '../../components/common/FilterBar'
import { PageHeader } from '../../components/common/PageHeader'
import { Panel } from '../../components/common/Panel'
import { formatMetric, type MetricUnit } from '../../config/metrics'
import { seriesColor } from '../../config/viz'
import { useAsyncData } from '../../hooks/useAsyncData'
import { DIMENSION_LABEL, MEASURE_LABEL, analyticsApi, type Dimension, type Measure } from '../../api'

/** 度量 → 单位。切换度量时图表的单位随之改变，而不是新开一根 Y 轴 */
const MEASURE_UNIT: Record<Measure, MetricUnit> = {
  tokens: 'tokens',
  requests: 'count',
  cost: 'usd',
  toolCalls: 'count',
}

export default function UsagePage() {
  const [days, setDays] = useState(30)
  const [dimension, setDimension] = useState<Dimension>('vendor')
  const [measure, setMeasure] = useState<Measure>('tokens')
  const [unitMode, setUnitMode] = useState<'absolute' | 'share'>('absolute')

  const { data, loading, error, reload } = useAsyncData(
    () => analyticsApi.usage(dimension, measure, days),
    [dimension, measure, days],
  )

  const unit = MEASURE_UNIT[measure]

  return (
    <div>
      <PageHeader description="tokens、请求、成本与工具调用的多维下钻。度量与分组维度可独立切换。" />

      <FilterBar
        actions={
          <Button size="small" icon={<DownloadOutlined />}>
            导出 CSV
          </Button>
        }
      >
        <FilterField label="时间">
          <Select
            size="small"
            value={days}
            onChange={setDays}
            className="w-28"
            options={[7, 14, 30, 90].map((value) => ({ value, label: `近 ${value} 天` }))}
          />
        </FilterField>
        <FilterField label="分组">
          <Select
            size="small"
            value={dimension}
            onChange={setDimension}
            className="w-32"
            options={(Object.keys(DIMENSION_LABEL) as Dimension[]).map((value) => ({
              value,
              label: DIMENSION_LABEL[value],
            }))}
          />
        </FilterField>
        <FilterField label="模型">
          <Select size="small" defaultValue="all" className="w-28" options={[{ value: 'all', label: '全部模型' }]} />
        </FilterField>
        <FilterField label="用户组">
          <Select size="small" defaultValue="all" className="w-28" options={[{ value: 'all', label: '全部用户组' }]} />
        </FilterField>
      </FilterBar>

      <Panel className="mb-4">
        {/*
          度量用 radio 切换、复用同一张图，是本页最重要的一个设计决定。
          Tokens 与成本量纲不同，若并排画在一张图上就必须引入第二根 Y 轴 ——
          那会凭空制造出数据里不存在的相关性。切换代替并列，从根上避开。
        */}
        <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
          <Radio.Group
            size="small"
            value={measure}
            onChange={(event) => setMeasure(event.target.value as Measure)}
            options={(Object.keys(MEASURE_LABEL) as Measure[]).map((value) => ({
              value,
              label: MEASURE_LABEL[value],
            }))}
            optionType="button"
          />
          <Segmented
            size="small"
            value={unitMode}
            onChange={(value) => setUnitMode(value as 'absolute' | 'share')}
            options={[
              { value: 'absolute', label: '绝对值' },
              { value: 'share', label: '占比' },
            ]}
          />
        </div>

        <AsyncBoundary loading={loading} error={error} onRetry={reload} skeletonRows={6}>
          {data && (
            <>
              <HeroFigure
                value={formatMetric(data.trend.total, unit)}
                label={`${MEASURE_LABEL[measure]} · 近 ${days} 天合计`}
                delta={{
                  value: `${Math.abs(data.trend.deltaPercent).toFixed(1)}%`,
                  direction: data.trend.deltaPercent >= 0 ? 'up' : 'down',
                  good: measure === 'cost' ? data.trend.deltaPercent < 0 : data.trend.deltaPercent > 0,
                }}
                sub={`${DIMENSION_LABEL[dimension]}分组 · 后半段较前半段`}
              />

              <div className="mt-5">
                <MetricChart
                  form={unitMode === 'share' ? 'stacked-bar' : 'area'}
                  data={data.trend.data}
                  xKey="date"
                  unit={unit}
                  height={300}
                  series={data.trend.series}
                />
              </div>
            </>
          )}
        </AsyncBoundary>
      </Panel>

      <div className="mb-4 grid gap-4 lg:grid-cols-2">
        <Panel
          eyebrow="COMPOSITION"
          title="Tokens 构成"
          description="缓存读写单列，否则成本对不上"
        >
          <AsyncBoundary loading={loading} error={error} onRetry={reload}>
            {data && <Composition items={data.composition} />}
          </AsyncBoundary>
        </Panel>

        <Panel eyebrow="HOURLY" title="时段热力" description="周 × 小时，顺序色单一蓝色相">
          <AsyncBoundary loading={loading} error={error} onRetry={reload}>
            {data && (
              <Heatmap
                rows={data.heat.rows}
                columns={data.heat.columns}
                legend={{ low: '低', high: '高' }}
              />
            )}
          </AsyncBoundary>
        </Panel>
      </div>

      <Panel eyebrow="BREAKDOWN" title="明细" description="点击行可下钻到该分组内部">
        <AsyncBoundary loading={loading} error={error} onRetry={reload} skeletonRows={6}>
          {data && <Breakdown rows={data.breakdown} />}
        </AsyncBoundary>
      </Panel>
    </div>
  )
}

function Composition({
  items,
}: {
  items: Array<{ id: string; label: string; slot: number; value: number }>
}) {
  const total = items.reduce((acc, item) => acc + item.value, 0)

  return (
    <div>
      {/* 部分-整体用堆叠条，不用饼图；每段直接标注百分比 */}
      <div className="flex h-7 w-full overflow-hidden rounded" style={{ gap: 2 }}>
        {items.map((item) => (
          <div
            key={item.id}
            className="flex items-center justify-center"
            style={{
              width: `${(item.value / total) * 100}%`,
              background: seriesColor(item.slot),
            }}
            title={`${item.label} ${formatMetric(item.value, 'tokens')}`}
          />
        ))}
      </div>
      <div className="mt-4 flex flex-col gap-2">
        {items.map((item) => (
          <div key={item.id} className="flex items-center gap-2 text-[11px]">
            <i
              className="inline-block h-2.5 w-2.5 shrink-0 rounded-sm"
              style={{ background: seriesColor(item.slot) }}
            />
            <span className="flex-1 text-ink-secondary">{item.label}</span>
            <span className="font-mono text-ink">{formatMetric(item.value, 'tokens')}</span>
            <span className="w-12 text-right font-mono text-ink-muted">
              {((item.value / total) * 100).toFixed(1)}%
            </span>
          </div>
        ))}
      </div>
    </div>
  )
}

function Breakdown({
  rows,
}: {
  rows: Array<{
    key: string
    label: string
    slot: number
    tokens: number
    requests: number
    cost: number
    latencyP95: number
    errorRate: number
    share: number
  }>
}) {
  return (
    <div className="overflow-x-auto">
      <table className="w-full min-w-[760px] border-collapse text-[11px]">
        <thead>
          <tr className="border-b border-line bg-plane font-mono text-[9px] text-ink-muted">
            <th className="px-2 py-2 text-left font-normal">分组</th>
            <th className="px-2 py-2 text-right font-normal">Tokens</th>
            <th className="px-2 py-2 text-right font-normal">请求数</th>
            <th className="px-2 py-2 text-right font-normal">成本</th>
            <th className="px-2 py-2 text-right font-normal">P95</th>
            <th className="px-2 py-2 text-right font-normal">错误率</th>
            <th className="px-2 py-2 text-left font-normal">占比</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr key={row.key} className="border-b border-line-soft last:border-b-0 hover:bg-plane">
              <td className="px-2 py-2.5">
                <span className="inline-flex items-center gap-2">
                  <i
                    className="inline-block h-2.5 w-2.5 shrink-0 rounded-sm"
                    style={{ background: seriesColor(row.slot) }}
                  />
                  <b className="font-medium">{row.label}</b>
                </span>
              </td>
              <td className="px-2 py-2.5 text-right font-mono">
                {formatMetric(row.tokens, 'tokens')}
              </td>
              <td className="px-2 py-2.5 text-right font-mono">
                {formatMetric(row.requests, 'count')}
              </td>
              <td className="px-2 py-2.5 text-right font-mono">{formatMetric(row.cost, 'usd')}</td>
              <td className="px-2 py-2.5 text-right font-mono">
                {formatMetric(row.latencyP95, 'ms')}
              </td>
              <td className="px-2 py-2.5 text-right font-mono">{row.errorRate.toFixed(2)}%</td>
              <td className="px-2 py-2.5">
                <span className="flex items-center gap-2">
                  <i className="h-1.5 flex-1 overflow-hidden rounded-full bg-line-soft">
                    <em
                      className="block h-full rounded-full"
                      style={{ width: `${row.share}%`, background: seriesColor(row.slot) }}
                    />
                  </i>
                  <span className="w-11 shrink-0 text-right font-mono text-ink-muted">
                    {row.share.toFixed(1)}%
                  </span>
                </span>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
