import { ClusterOutlined, DeploymentUnitOutlined, HddOutlined, ReloadOutlined, RightOutlined } from '@ant-design/icons'
import { Button } from 'antd'
import { Link } from 'react-router'
import { Meter } from '../../components/charts/Meter'
import { StatTileRow } from '../../components/charts/StatTile'
import { AsyncBoundary } from '../../components/common/AsyncBoundary'
import { PageHeader } from '../../components/common/PageHeader'
import { Panel } from '../../components/common/Panel'
import { StatusBadge } from '../../components/common/StatusBadge'
import { GpuTopology } from '../../components/infra/GpuTopology'
import { metrics } from '../../config/metrics'
import { useAsyncData } from '../../hooks/useAsyncData'
import { infraApi } from '../../api'

export default function ClusterPage() {
  const { data, loading, error, reload } = useAsyncData(() => infraApi.cluster(), [])

  return (
    <div>
      <PageHeader
        description="节点与 GPU 拓扑、分配情况与故障概览。填充表示利用率，角标表示健康状态。"
        actions={
          <Button size="small" icon={<ReloadOutlined />} onClick={reload}>
            刷新
          </Button>
        }
      />

      <AsyncBoundary loading={loading} error={error} onRetry={reload} skeletonRows={10}>
        {data && (
          <>
            <StatTileRow
              items={[
                { label: '节点', value: String(data.summary.nodes), note: '在线', icon: <ClusterOutlined /> },
                {
                  label: 'GPU',
                  value: `${data.summary.gpuHealthy}/${data.summary.gpuTotal}`,
                  note: data.summary.gpuFaulty > 0 ? `${data.summary.gpuFaulty} 卡故障` : '全部健康',
                  icon: <HddOutlined />,
                },
                {
                  label: '已分配',
                  value: `${data.summary.allocated} 卡`,
                  note: `${((data.summary.allocated / data.summary.gpuTotal) * 100).toFixed(0)}% 被实例占用`,
                  icon: <DeploymentUnitOutlined />,
                  caveat: '分配不等于实际使用，两个指标需分别观察。',
                },
                {
                  label: '显存水位',
                  value: `${data.summary.memRatio.toFixed(0)}%`,
                  note: '全集群已分配 ÷ 总显存',
                  icon: <HddOutlined />,
                  caveat: metrics.gpuMemRatio.caveat,
                },
                {
                  label: '运行实例',
                  value: String(data.summary.instances),
                  note: '跨全部节点',
                  icon: <DeploymentUnitOutlined />,
                },
              ]}
            />

            <div className="mb-4 grid gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(260px,0.35fr)]">
              <Panel
                eyebrow="TOPOLOGY"
                title="节点与 GPU 拓扑"
                description="悬浮任意格子查看显存、温度、功耗与占用实例"
              >
                <GpuTopology nodes={data.nodes} />
              </Panel>

              <div className="flex flex-col gap-4">
                <Panel eyebrow="WATERLINE" title="集群水位">
                  <div className="flex flex-col gap-4">
                    <Meter
                      label="显存"
                      value={data.summary.memRatio}
                      max={100}
                      valueText={`${data.summary.memRatio.toFixed(0)}%`}
                    />
                    <Meter
                      label="平均算力利用率"
                      value={data.summary.utilAvg}
                      max={100}
                      valueText={`${data.summary.utilAvg.toFixed(0)}%`}
                    />
                    <Meter label="模型仓库磁盘" value={2.4} max={8} valueText="2.4 / 8 TB" />
                  </div>
                </Panel>

                <Panel eyebrow="NODES" title="节点明细">
                  <div className="flex flex-col">
                    {data.nodes.map((node) => {
                      const faulty = node.cards.filter((card) => card.status === 'critical').length
                      const busy = node.cards.filter((card) => card.allocatedTo).length
                      return (
                        <div
                          key={node.id}
                          className="flex items-center justify-between gap-2 border-b border-line-soft py-2 text-[11px] last:border-b-0"
                        >
                          <span className="flex flex-col">
                            <b className="font-mono">{node.id}</b>
                            <small className="text-[9px] text-ink-muted">{node.gpuModel}</small>
                          </span>
                          <span className="flex items-center gap-3">
                            <span className="font-mono text-[10px] text-ink-muted">
                              {busy}/{node.cards.length} 占用
                            </span>
                            <StatusBadge
                              kind={faulty > 0 ? 'critical' : 'good'}
                              label={faulty > 0 ? `${faulty} 卡故障` : '正常'}
                              size="small"
                            />
                          </span>
                        </div>
                      )
                    })}
                  </div>
                  <Link
                    to="/infra/observability"
                    className="mt-3 inline-flex items-center gap-1 text-[11px] text-brand"
                  >
                    进入 GPU 监控 <RightOutlined className="text-[9px]" />
                  </Link>
                </Panel>
              </div>
            </div>
          </>
        )}
      </AsyncBoundary>
    </div>
  )
}
