import { RiseOutlined, TeamOutlined, UserAddOutlined, WalletOutlined } from '@ant-design/icons'
import { Heatmap } from '../../components/charts/Heatmap'
import { MetricChart } from '../../components/charts/MetricChart'
import { StatTileRow } from '../../components/charts/StatTile'
import { AsyncBoundary } from '../../components/common/AsyncBoundary'
import { PageHeader } from '../../components/common/PageHeader'
import { Panel } from '../../components/common/Panel'
import { compactNumber } from '../../config/metrics'
import { sequentialColor, seriesColor } from '../../config/viz'
import { useAsyncData } from '../../hooks/useAsyncData'
import { analyticsApi } from '../../api'

export default function UsersPage() {
  const { data, loading, error, reload } = useAsyncData(() => analyticsApi.users(), [])

  return (
    <div>
      <PageHeader description="用户规模、增长与流失、留存队列、分层结构与转化漏斗。" />

      <StatTileRow
        items={[
          { label: '总注册', value: '8,412', note: '累计', icon: <TeamOutlined /> },
          {
            label: '本月新增',
            value: '642',
            note: '较上月',
            delta: { value: '14.2%', direction: 'up', good: true },
            icon: <UserAddOutlined />,
          },
          {
            label: '日活',
            value: '1,203',
            note: '当日有成功调用',
            delta: { value: '3.2%', direction: 'up', good: true },
            icon: <RiseOutlined />,
          },
          { label: '付费用户', value: '318', note: '转化率 3.8%', icon: <WalletOutlined /> },
          { label: 'ARPU', value: '$42.6', note: '按付费用户计', icon: <WalletOutlined /> },
        ]}
      />

      <AsyncBoundary loading={loading} error={error} onRetry={reload} skeletonRows={8}>
        {data && (
          <>
            <div className="mb-4 grid gap-4 lg:grid-cols-2">
              <Panel
                eyebrow="GROWTH"
                title="新增与流失"
                description="向上为新增、向下为流失，中性灰基线"
              >
                {/*
                  发散形态：新增与流失同为「人数」，量纲一致，可以共轴。
                  流失值为负，天然以 0 为中性基线向两侧发散。
                */}
                <MetricChart
                  form="bar"
                  data={data.growth}
                  xKey="date"
                  unit="count"
                  height={260}
                  series={[
                    { id: 'joined', label: '新增', slot: 1 },
                    { id: 'churned', label: '流失', slot: 8 },
                  ]}
                />
              </Panel>

              <Panel
                eyebrow="RETENTION"
                title="留存队列"
                description="每行一个注册周，列为注册后第 N 周"
              >
                <Heatmap
                  rows={data.retention.rows}
                  columns={data.retention.columns}
                  legend={{ low: '0%', high: '100%' }}
                  cellHeight={26}
                />
              </Panel>
            </div>

            <div className="grid gap-4 lg:grid-cols-2">
              <Panel eyebrow="TIERS" title="用户分层" description="按套餐划分，每段直接标注人数">
                <div className="flex flex-col gap-3">
                  {data.tiers.map((tier) => {
                    const total = data.tiers.reduce((acc, item) => acc + item.count, 0)
                    const share = (tier.count / total) * 100
                    return (
                      <div key={tier.id} className="flex items-center gap-3 text-[11px]">
                        <span className="w-16 shrink-0 text-ink-secondary">{tier.label}</span>
                        <i className="h-4 flex-1 overflow-hidden rounded bg-line-soft">
                          <em
                            className="block h-full rounded"
                            style={{ width: `${share}%`, background: seriesColor(tier.slot) }}
                          />
                        </i>
                        <span className="w-16 shrink-0 text-right font-mono">
                          {tier.count.toLocaleString()}
                        </span>
                        <span className="w-12 shrink-0 text-right font-mono text-ink-muted">
                          {share.toFixed(1)}%
                        </span>
                      </div>
                    )
                  })}
                </div>
              </Panel>

              <Panel
                eyebrow="FUNNEL"
                title="转化漏斗"
                description="有序阶段用序数色阶，不是分类色"
              >
                <div className="flex flex-col gap-2">
                  {data.funnel.map((stage, index) => {
                    const top = data.funnel[0].value
                    const share = (stage.value / top) * 100
                    const prev = index === 0 ? undefined : data.funnel[index - 1].value
                    const stepRate = prev ? (stage.value / prev) * 100 : undefined
                    return (
                      <div key={stage.stage} className="flex items-center gap-3 text-[11px]">
                        <span className="w-20 shrink-0 text-ink-secondary">{stage.stage}</span>
                        <i className="h-6 flex-1 overflow-hidden rounded bg-line-soft">
                          <em
                            className="flex h-full items-center justify-end rounded pr-2 font-mono text-[10px] text-white"
                            style={{
                              width: `${Math.max(share, 8)}%`,
                              // 有序阶段 → 序数色阶（同一色相由深到浅），不是 8 个分类色
                              background: sequentialColor(1 - index / data.funnel.length),
                            }}
                          >
                            {compactNumber(stage.value, 0)}
                          </em>
                        </i>
                        <span className="w-24 shrink-0 text-right font-mono text-ink-muted">
                          {stepRate === undefined ? '—' : `↓ ${stepRate.toFixed(1)}%`}
                        </span>
                      </div>
                    )
                  })}
                </div>
              </Panel>
            </div>
          </>
        )}
      </AsyncBoundary>
    </div>
  )
}
