import { Tooltip } from 'antd'
import { MetricChart } from '../../components/charts/MetricChart'
import { AsyncBoundary } from '../../components/common/AsyncBoundary'
import { PageHeader } from '../../components/common/PageHeader'
import { Panel } from '../../components/common/Panel'
import { formatMetric } from '../../config/metrics'
import { seriesColor } from '../../config/viz'
import { useAsyncData } from '../../hooks/useAsyncData'
import { analyticsApi } from '../../api'

/**
 * 模型分析 —— 热度与单位经济性。
 *
 * 「调用量」和「每百万 token 成本」是两个不同量纲的指标，这里刻意拆成
 * 两张上下对齐的图，而不是一张双轴图。双轴会让读者以为"调用量高所以成本高"，
 * 而那个相关性是坐标轴对齐方式编出来的。
 */
export default function ModelsPage() {
  const { data, loading, error, reload } = useAsyncData(() => analyticsApi.models(), [])

  return (
    <div>
      <PageHeader description="模型调用热度与单位经济性对比。两个指标量纲不同，分列两图。" />

      <AsyncBoundary loading={loading} error={error} onRetry={reload} skeletonRows={8}>
        {data && (
          <>
            <Panel
              eyebrow="ADOPTION"
              title="调用量排行"
              description="单一色相表示同一个度量，不做「越大越深」的值阶映射"
              className="mb-4"
            >
              <div className="flex flex-col gap-3">
                {data.leaderboard.map((model, index) => (
                  <div key={model.id} className="flex items-center gap-3 text-[11px]">
                    <span className="w-5 shrink-0 text-right font-mono text-ink-muted">
                      {index + 1}
                    </span>
                    <span className="w-40 shrink-0 truncate">
                      <b className="font-medium">{model.name}</b>
                      <small className="ml-1.5 text-ink-muted">{model.vendor}</small>
                    </span>
                    <i className="h-4 flex-1 overflow-hidden rounded bg-line-soft">
                      <em
                        className="block h-full rounded"
                        style={{ width: `${model.share}%`, background: seriesColor(model.slot) }}
                      />
                    </i>
                    <span className="w-16 shrink-0 text-right font-mono">
                      {formatMetric(model.tokens, 'tokens')}
                    </span>
                  </div>
                ))}
              </div>
            </Panel>

            <div className="mb-4 grid gap-4 lg:grid-cols-2">
              <Panel
                eyebrow="UNIT ECONOMICS"
                title="单位成本"
                description="每百万 Tokens 的供应商计费，越低越省"
              >
                <MetricChart
                  form="bar"
                  data={data.leaderboard.map((model) => ({
                    name: model.name,
                    cost: Number(model.costPerMillion.toFixed(2)),
                  }))}
                  xKey="name"
                  unit="usd"
                  height={240}
                  series={[{ id: 'cost', label: '每百万 Tokens 成本', slot: 1 }]}
                />
              </Panel>

              <Panel
                eyebrow="LATENCY"
                title="P95 延迟"
                description="与成本量纲不同，独立成图"
              >
                <MetricChart
                  form="bar"
                  data={data.leaderboard.map((model) => ({
                    name: model.name,
                    latency: Math.round(model.latency),
                  }))}
                  xKey="name"
                  unit="ms"
                  height={240}
                  series={[{ id: 'latency', label: 'P95 延迟', slot: 1 }]}
                />
              </Panel>
            </div>

            <Panel eyebrow="DETAIL" title="模型明细">
              <div className="overflow-x-auto">
                <table className="w-full min-w-[720px] border-collapse text-[11px]">
                  <thead>
                    <tr className="border-b border-line bg-plane font-mono text-[9px] text-ink-muted">
                      <th className="px-2 py-2 text-left font-normal">模型</th>
                      <th className="px-2 py-2 text-left font-normal">供应商</th>
                      <th className="px-2 py-2 text-right font-normal">Tokens</th>
                      <th className="px-2 py-2 text-right font-normal">成本</th>
                      <th className="px-2 py-2 text-right font-normal">$/1M</th>
                      <th className="px-2 py-2 text-right font-normal">P95</th>
                      <th className="px-2 py-2 text-right font-normal">错误率</th>
                    </tr>
                  </thead>
                  <tbody>
                    {data.leaderboard.map((model) => (
                      <tr
                        key={model.id}
                        className="border-b border-line-soft last:border-b-0 hover:bg-plane"
                      >
                        <td className="px-2 py-2.5">
                          <span className="inline-flex items-center gap-2">
                            <i
                              className="inline-block h-2.5 w-2.5 shrink-0 rounded-sm"
                              style={{ background: seriesColor(model.slot) }}
                            />
                            <b className="font-medium">{model.name}</b>
                          </span>
                        </td>
                        <td className="px-2 py-2.5 text-ink-muted">{model.vendor}</td>
                        <td className="px-2 py-2.5 text-right font-mono">
                          {formatMetric(model.tokens, 'tokens')}
                        </td>
                        <td className="px-2 py-2.5 text-right font-mono">
                          {formatMetric(model.cost, 'usd')}
                        </td>
                        <td className="px-2 py-2.5 text-right font-mono">
                          <Tooltip title="每百万 Tokens 的供应商计费">
                            ${model.costPerMillion.toFixed(2)}
                          </Tooltip>
                        </td>
                        <td className="px-2 py-2.5 text-right font-mono">
                          {formatMetric(model.latency, 'ms')}
                        </td>
                        <td className="px-2 py-2.5 text-right font-mono">
                          {model.errorRate.toFixed(2)}%
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </Panel>
          </>
        )}
      </AsyncBoundary>
    </div>
  )
}
