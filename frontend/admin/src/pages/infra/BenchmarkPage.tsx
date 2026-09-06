import { CheckCircleFilled, CloseCircleFilled, ExportOutlined, ThunderboltOutlined } from '@ant-design/icons'
import { Alert, App, Button, InputNumber, Select, Tag } from 'antd'
import { useMemo, useState } from 'react'
import { HeroFigure } from '../../components/charts/HeroFigure'
import { MetricChart } from '../../components/charts/MetricChart'
import { AsyncBoundary } from '../../components/common/AsyncBoundary'
import { FilterBar, FilterField } from '../../components/common/FilterBar'
import { PageHeader } from '../../components/common/PageHeader'
import { Panel } from '../../components/common/Panel'
import { formatMetric } from '../../config/metrics'
import { useAsyncData } from '../../hooks/useAsyncData'
import { infraApi } from '../../api'
import { passesSlo, sloKneePoint } from '../../lib/benchmark'

const TARGETS = ['qwen3-72b-prod', 'qwen3-32b-prod', 'deepseek-v4-canary']

export default function BenchmarkPage() {
  const { message } = App.useApp()
  const [target, setTarget] = useState(TARGETS[0])
  const [ttftSlo, setTtftSlo] = useState(500)
  const [tpotSlo, setTpotSlo] = useState(50)

  const { data, loading, error, reload } = useAsyncData(() => infraApi.benchmark(target), [target])
  const history = useAsyncData(() => infraApi.benchmarkHistory(), [])

  const slo = useMemo(() => ({ ttftP95Ms: ttftSlo, tpotP95Ms: tpotSlo }), [ttftSlo, tpotSlo])
  const analysis = useMemo(
    () => (data ? sloKneePoint(data.points, slo) : undefined),
    [data, slo],
  )

  return (
    <div>
      <PageHeader
        description="并发爬坡压测。结论不是最大吞吐，而是仍然满足 SLO 的最大并发。"
        actions={
          <Button size="small" type="primary" icon={<ThunderboltOutlined />}>
            新建压测
          </Button>
        }
      />

      <FilterBar>
        <FilterField label="目标">
          <Select
            size="small"
            value={target}
            onChange={setTarget}
            className="w-44"
            options={TARGETS.map((value) => ({ value, label: value }))}
          />
        </FilterField>
        <FilterField label="TTFT P95 ≤">
          <InputNumber
            size="small"
            value={ttftSlo}
            onChange={(v) => v && setTtftSlo(v)}
            addonAfter="ms"
            className="w-28"
          />
        </FilterField>
        <FilterField label="TPOT P95 ≤">
          <InputNumber
            size="small"
            value={tpotSlo}
            onChange={(v) => v && setTpotSlo(v)}
            addonAfter="ms"
            className="w-28"
          />
        </FilterField>
      </FilterBar>

      <AsyncBoundary loading={loading} error={error} onRetry={reload} skeletonRows={10}>
        {data && analysis && (
          <>
            <Panel
              eyebrow="RESULT"
              title={`最近一次结果 · ${data.ranAt}`}
              description={`${data.scenario}场景 · 输入 ${data.inputLength} / 输出 ${data.outputLength} tokens · ${data.dataset}`}
              className="mb-4"
              actions={
                <Button
                  size="small"
                  icon={<ExportOutlined />}
                  onClick={() => message.success('已回写为该模型的容量评估基准')}
                >
                  回写容量评估基准
                </Button>
              }
            >
              {/* 主结论是一个数字，不是一堆曲线。曲线是佐证 */}
              {analysis.knee ? (
                <HeroFigure
                  value={String(analysis.knee.concurrency)}
                  label="最大可用并发"
                  sub={`在 TTFT P95 ≤ ${ttftSlo}ms 且 TPOT P95 ≤ ${tpotSlo}ms 前提下`}
                />
              ) : (
                <Alert
                  type="error"
                  showIcon
                  message="当前 SLO 下没有任何并发档位通过，需放宽 SLO 或扩容"
                />
              )}

              <div className="mt-5 grid gap-4 sm:grid-cols-3">
                <Stat
                  label="有效吞吐"
                  value={analysis.knee ? `${analysis.knee.throughput} tok/s` : '—'}
                  note="满足 SLO 前提下"
                />
                <Stat
                  label="饱和吞吐"
                  value={`${analysis.saturation.throughput} tok/s`}
                  note={`并发 ${analysis.saturation.concurrency}，已违反 SLO`}
                  muted
                />
                <Stat
                  label="吞吐代价"
                  value={
                    analysis.knee
                      ? `+${Math.round((analysis.saturation.throughput / analysis.knee.throughput - 1) * 100)}%`
                      : '—'
                  }
                  note="继续加压能多榨出的吞吐，代价是延迟失控"
                  muted
                />
              </div>
            </Panel>

            {/*
              吞吐(tok/s) 与延迟(ms) 量纲不同，拆成两张上下对齐的图。
              这里是最容易出现双轴图的地方 —— 把它们叠在一张图上会让人以为
              「吞吐涨所以延迟涨」是同一条曲线的两面，而那是坐标轴对齐编出来的。
            */}
            <div className="mb-4 grid gap-4 lg:grid-cols-2">
              <Panel
                eyebrow="THROUGHPUT"
                title="吞吐 vs 并发"
                description="单位 tok/s。随并发上升至饱和"
              >
                <MetricChart
                  form="line"
                  data={data.points.map((point) => ({
                    concurrency: String(point.concurrency),
                    throughput: point.throughput,
                  }))}
                  xKey="concurrency"
                  unit="tps"
                  height={240}
                  series={[{ id: 'throughput', label: '输出吞吐' }]}
                />
                {analysis.knee && (
                  <p className="mt-2 mb-0 text-[10px] text-ink-muted">
                    SLO 拐点在并发 {analysis.knee.concurrency}（{analysis.knee.throughput} tok/s）
                    —— 再往右吞吐还在涨，但延迟已经违约。
                  </p>
                )}
              </Panel>

              <Panel
                eyebrow="LATENCY"
                title="TTFT 分位 vs 并发"
                description="单位 ms。三条分位线同量纲，共轴合法"
              >
                <MetricChart
                  form="line"
                  data={data.points.map((point) => ({
                    concurrency: String(point.concurrency),
                    p50: point.ttftP50,
                    p95: point.ttftP95,
                    p99: point.ttftP99,
                  }))}
                  xKey="concurrency"
                  unit="ms"
                  height={240}
                  series={[
                    { id: 'p50', label: 'TTFT P50', slot: 1 },
                    { id: 'p95', label: 'TTFT P95', slot: 2 },
                    { id: 'p99', label: 'TTFT P99', slot: 8 },
                  ]}
                  threshold={{ value: ttftSlo, label: `SLO ${ttftSlo}ms` }}
                />
              </Panel>
            </div>

            <Panel eyebrow="DETAIL" title="分档明细" bodyClassName="overflow-x-auto" className="mb-4">
              <table className="w-full min-w-[780px] border-collapse text-[11px]">
                <thead>
                  <tr className="border-b border-line bg-plane font-mono text-[9px] text-ink-muted">
                    <th className="px-2 py-2 text-right font-normal">并发</th>
                    <th className="px-2 py-2 text-right font-normal">吞吐 tok/s</th>
                    <th className="px-2 py-2 text-right font-normal">TTFT P95</th>
                    <th className="px-2 py-2 text-right font-normal">TPOT P95</th>
                    <th className="px-2 py-2 text-right font-normal">E2E P95</th>
                    <th className="px-2 py-2 text-right font-normal">错误率</th>
                    <th className="px-2 py-2 text-left font-normal">SLO</th>
                  </tr>
                </thead>
                <tbody>
                  {data.points.map((point) => {
                    const pass = passesSlo(point, slo)
                    const isKnee = analysis.knee?.concurrency === point.concurrency
                    return (
                      <tr
                        key={point.concurrency}
                        className={`border-b border-line-soft last:border-b-0 ${
                          isKnee ? 'bg-brand-soft' : 'hover:bg-plane'
                        }`}
                      >
                        <td className="px-2 py-2 text-right font-mono">
                          {point.concurrency}
                          {isKnee && (
                            <Tag color="purple" className="ml-1.5 m-0 text-[9px]">
                              拐点
                            </Tag>
                          )}
                        </td>
                        <td className="px-2 py-2 text-right font-mono">{point.throughput}</td>
                        <td
                          className={`px-2 py-2 text-right font-mono ${point.ttftP95 > ttftSlo ? 'text-status-critical' : ''}`}
                        >
                          {formatMetric(point.ttftP95, 'ms')}
                        </td>
                        <td
                          className={`px-2 py-2 text-right font-mono ${point.tpotP95 > tpotSlo ? 'text-status-critical' : ''}`}
                        >
                          {point.tpotP95}ms
                        </td>
                        <td className="px-2 py-2 text-right font-mono">
                          {formatMetric(point.e2eP95, 'ms')}
                        </td>
                        <td className="px-2 py-2 text-right font-mono">
                          {point.errorRate.toFixed(1)}%
                        </td>
                        <td className="px-2 py-2">
                          {pass ? (
                            <span className="inline-flex items-center gap-1 text-status-good">
                              <CheckCircleFilled /> 通过
                            </span>
                          ) : (
                            <span className="inline-flex items-center gap-1 text-status-critical">
                              <CloseCircleFilled />
                              {point.ttftP95 > ttftSlo ? ' TTFT 超标' : ' TPOT 超标'}
                            </span>
                          )}
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </Panel>

            <Panel eyebrow="HISTORY" title="历史压测">
              <AsyncBoundary
                loading={history.loading}
                error={history.error}
                onRetry={history.reload}
              >
                {history.data && (
                  <div className="flex flex-col">
                    {history.data.runs.map((run) => (
                      <div
                        key={run.id}
                        className="flex items-center justify-between gap-3 border-b border-line-soft py-2.5 text-[11px] last:border-b-0"
                      >
                        <span className="flex flex-col">
                          <b className="font-mono">{run.ranAt}</b>
                          <small className="text-[9px] text-ink-muted">{run.note}</small>
                        </span>
                        <span className="flex items-center gap-5 font-mono text-[10px]">
                          <span>最大并发 {run.maxConcurrency}</span>
                          <span>{run.goodput} tok/s</span>
                        </span>
                      </div>
                    ))}
                  </div>
                )}
              </AsyncBoundary>
            </Panel>
          </>
        )}
      </AsyncBoundary>
    </div>
  )
}

function Stat({
  label,
  value,
  note,
  muted,
}: {
  label: string
  value: string
  note: string
  muted?: boolean
}) {
  return (
    <div className="border-l-2 border-line pl-3">
      <span className="text-[10px] text-ink-muted">{label}</span>
      <b className={`mt-1 block font-mono text-lg ${muted ? 'text-ink-secondary' : ''}`}>{value}</b>
      <small className="text-[9px] text-ink-muted">{note}</small>
    </div>
  )
}
