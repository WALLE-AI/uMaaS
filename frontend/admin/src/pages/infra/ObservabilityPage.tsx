import { ReloadOutlined } from '@ant-design/icons'
import { Button, Select, Tooltip } from 'antd'
import { useState } from 'react'
import { Heatmap } from '../../components/charts/Heatmap'
import { MetricChart } from '../../components/charts/MetricChart'
import { AsyncBoundary } from '../../components/common/AsyncBoundary'
import { FilterBar, FilterField } from '../../components/common/FilterBar'
import { PageHeader } from '../../components/common/PageHeader'
import { Panel } from '../../components/common/Panel'
import { StatusBadge } from '../../components/common/StatusBadge'
import { useAsyncData } from '../../hooks/useAsyncData'
import { infraApi } from '../../api'
import { XID_MEANING } from '../../lib/gpu'

export default function ObservabilityPage() {
  const [hours, setHours] = useState(6)
  const [node, setNode] = useState('all')
  const { data, loading, error, reload } = useAsyncData(
    () => infraApi.observability(hours),
    [hours],
  )

  const cards = (data?.nodes ?? [])
    .filter((item) => node === 'all' || item.id === node)
    .flatMap((item) => item.cards.map((card) => ({ ...card, node: item.id })))

  const counts = {
    good: cards.filter((card) => card.status === 'good').length,
    warning: cards.filter((card) => card.status === 'warning').length,
    critical: cards.filter((card) => card.status === 'critical').length,
    idle: cards.filter((card) => card.status === 'idle').length,
  }

  return (
    <div>
      <PageHeader
        description="GPU 健康、利用率、温度功耗与故障归因。指标来自 DCGM Exporter。"
        actions={
          <Button size="small" icon={<ReloadOutlined />} onClick={reload}>
            刷新
          </Button>
        }
      />

      <AsyncBoundary loading={loading} error={error} onRetry={reload} skeletonRows={10}>
        {data && (
          <>
            <FilterBar>
              <FilterField label="节点">
                <Select
                  size="small"
                  value={node}
                  onChange={setNode}
                  className="w-28"
                  options={[
                    { value: 'all', label: '全部节点' },
                    ...data.nodes.map((item) => ({ value: item.id, label: item.id })),
                  ]}
                />
              </FilterField>
              <FilterField label="时间">
                <Select
                  size="small"
                  value={hours}
                  onChange={setHours}
                  className="w-28"
                  options={[1, 6, 24].map((value) => ({ value, label: `近 ${value} 小时` }))}
                />
              </FilterField>
            </FilterBar>

            <Panel eyebrow="HEALTH" title="健康概览" className="mb-4">
              <div className="flex flex-wrap items-center gap-6">
                <StatusBadge kind="good" label={`正常 ${counts.good}`} />
                <StatusBadge kind="warning" label={`警告 ${counts.warning}`} />
                <StatusBadge kind="critical" label={`危急 ${counts.critical}`} />
                <StatusBadge kind="idle" label={`空闲/维护 ${counts.idle}`} />
              </div>

              {data.incidents.length > 0 && (
                <div className="mt-4 flex flex-col gap-2">
                  {data.incidents.map((incident) => {
                    const xid = incident.xid ? XID_MEANING[incident.xid] : undefined
                    return (
                      <div key={incident.id} className="rounded border border-line bg-plane p-3">
                        <div className="flex flex-wrap items-center gap-2">
                          <StatusBadge
                            kind={incident.kind}
                            label={`${incident.node} GPU${incident.gpu} · ${incident.title}`}
                            size="small"
                          />
                          {incident.xid !== undefined && (
                            <Tooltip title={xid?.detail}>
                              <code className="cursor-help rounded bg-surface px-1.5 py-0.5 font-mono text-[9px] text-ink-secondary">
                                XID {incident.xid} · {xid?.title}
                              </code>
                            </Tooltip>
                          )}
                          <span className="ml-auto font-mono text-[9px] text-ink-muted">
                            {incident.since}
                          </span>
                        </div>
                        <p className="mt-1.5 mb-0 text-[10px] text-ink-muted">{incident.detail}</p>
                        {/* XID 码必须给出处置建议：有些换驱动就行，有些必须换卡 */}
                        {xid && (
                          <p className="mt-1 mb-0 text-[10px] text-ink-secondary">
                            <b>处置建议：</b>
                            {xid.action}
                          </p>
                        )}
                        {incident.evictedInstance && (
                          <p className="mt-1 mb-0 text-[10px] text-ink-muted">
                            受影响实例 <code className="font-mono">{incident.evictedInstance}</code> 已自动驱逐
                          </p>
                        )}
                      </div>
                    )
                  })}
                </div>
              )}
            </Panel>

            {/*
              利用率与显存占用同为百分比 —— 量纲一致，共轴是正确的。
              下面的温度(°C)与功耗(W)量纲不同，必须各自独立成图。
            */}
            <Panel
              eyebrow="UTILIZATION"
              title="利用率与显存占用"
              description="两者同为百分比，共用一根 Y 轴"
              className="mb-4"
            >
              <MetricChart
                form="line"
                data={data.utilization}
                xKey="time"
                unit="percent"
                height={240}
                series={[
                  { id: 'util', label: 'SM 利用率', slot: 1 },
                  { id: 'memory', label: '显存占用', slot: 2 },
                ]}
              />
            </Panel>

            <div className="mb-4 grid gap-4 lg:grid-cols-2">
              <Panel eyebrow="THERMAL" title="温度" description="单位 °C，独立成图">
                <MetricChart
                  form="line"
                  data={data.temperature}
                  xKey="time"
                  unit="celsius"
                  height={200}
                  series={[{ id: 'temperature', label: '平均温度' }]}
                  threshold={{ value: 85, label: '85°C' }}
                />
              </Panel>
              <Panel eyebrow="POWER" title="功耗" description="单位 W，与温度量纲不同，不并图">
                <MetricChart
                  form="line"
                  data={data.power}
                  xKey="time"
                  unit="watt"
                  height={200}
                  series={[{ id: 'power', label: '平均功耗' }]}
                  threshold={{ value: 700, label: '700W' }}
                />
              </Panel>
            </div>

            <Panel
              eyebrow="CLUSTER HEAT"
              title="GPU × 时间 利用率热力"
              description="顺序色单一蓝色相；故障格叠加斜线纹理"
              className="mb-4"
            >
              <Heatmap
                rows={data.heat.rows}
                columns={data.heat.columns}
                legend={{ low: '0%', high: '100%' }}
              />
            </Panel>

            <Panel eyebrow="DETAIL" title="硬件明细" bodyClassName="overflow-x-auto">
              <table className="w-full min-w-[860px] border-collapse text-[11px]">
                <thead>
                  <tr className="border-b border-line bg-plane font-mono text-[9px] text-ink-muted">
                    <th className="px-2 py-2 text-left font-normal">GPU</th>
                    <th className="px-2 py-2 text-left font-normal">型号</th>
                    <th className="px-2 py-2 text-right font-normal">显存</th>
                    <th className="px-2 py-2 text-right font-normal">利用率</th>
                    <th className="px-2 py-2 text-right font-normal">温度</th>
                    <th className="px-2 py-2 text-right font-normal">功耗</th>
                    <th className="px-2 py-2 text-right font-normal">ECC</th>
                    <th className="px-2 py-2 text-right font-normal">XID</th>
                    <th className="px-2 py-2 text-left font-normal">状态</th>
                  </tr>
                </thead>
                <tbody>
                  {cards.map((card) => (
                    <tr
                      key={card.uuid}
                      className="border-b border-line-soft last:border-b-0 hover:bg-plane"
                    >
                      <td className="px-2 py-2 font-mono">
                        {card.node.replace('node-', 'n')}·{card.index}
                      </td>
                      <td className="px-2 py-2 text-ink-secondary">{card.model}</td>
                      <td className="px-2 py-2 text-right font-mono">
                        {card.memoryUsedGb.toFixed(1)}/{card.memoryTotalGb}G
                      </td>
                      <td className="px-2 py-2 text-right font-mono">{card.util}%</td>
                      <td
                        className={`px-2 py-2 text-right font-mono ${card.tempC >= 85 ? 'text-status-warning' : ''}`}
                      >
                        {card.tempC}°C
                      </td>
                      <td className="px-2 py-2 text-right font-mono">{card.powerW}W</td>
                      <td
                        className={`px-2 py-2 text-right font-mono ${card.eccDoubleBit > 0 ? 'text-status-critical' : ''}`}
                      >
                        {card.eccSingleBit}/{card.eccDoubleBit}
                      </td>
                      <td className="px-2 py-2 text-right font-mono">
                        {card.xid ? (
                          <Tooltip title={`${XID_MEANING[card.xid]?.title} —— ${XID_MEANING[card.xid]?.action}`}>
                            <span className="cursor-help text-status-critical">{card.xid}</span>
                          </Tooltip>
                        ) : (
                          '—'
                        )}
                      </td>
                      <td className="px-2 py-2">
                        <StatusBadge kind={card.status} label={card.statusLabel} size="small" />
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
              <p className="mt-3 mb-0 text-[10px] text-ink-muted">
                ECC 列为「单位错误 / 双位错误」。双位错误不可纠正，出现即为硬件故障。
              </p>
            </Panel>
          </>
        )}
      </AsyncBoundary>
    </div>
  )
}
