import { Tag } from 'antd'
import { Meter } from '../../components/charts/Meter'
import { AsyncBoundary } from '../../components/common/AsyncBoundary'
import { PageHeader } from '../../components/common/PageHeader'
import { Panel } from '../../components/common/Panel'
import { StatusBadge } from '../../components/common/StatusBadge'
import { formatMetric } from '../../config/metrics'
import { useAsyncData } from '../../hooks/useAsyncData'
import { customersApi } from '../../api'

export default function WorkspacesPage() {
  const { data, loading, error, reload } = useAsyncData(() => customersApi.workspaces(), [])

  return (
    <div>
      <PageHeader description="团队席位与预算。超出预算的工作空间会在此处标红。" />

      <AsyncBoundary loading={loading} error={error} onRetry={reload} skeletonRows={8}>
        {data && (
          <div className="grid gap-3 lg:grid-cols-2">
            {data.workspaces.map((workspace) => {
              const overBudget = workspace.monthlySpend > workspace.monthlyBudget
              return (
                <Panel key={workspace.id} className="p-4">
                  <div className="flex flex-wrap items-start justify-between gap-3">
                    <div className="min-w-0">
                      <div className="flex flex-wrap items-center gap-2">
                        <b className="text-[13px]">{workspace.name}</b>
                        <Tag className="m-0 text-[9px]">{workspace.plan}</Tag>
                        {overBudget && (
                          <StatusBadge kind="warning" label="超出预算" size="small" />
                        )}
                      </div>
                      <p className="mt-1 mb-0 font-mono text-[10px] text-ink-muted">
                        {workspace.owner} · {workspace.models} 个可用模型
                      </p>
                    </div>
                  </div>

                  <div className="mt-4 flex flex-col gap-3.5">
                    <Meter
                      label="席位"
                      value={workspace.seatsUsed}
                      max={workspace.seatsTotal}
                      valueText={`${workspace.seatsUsed} / ${workspace.seatsTotal}`}
                    />
                    <Meter
                      label="本月预算"
                      value={workspace.monthlySpend}
                      max={workspace.monthlyBudget}
                      valueText={`${formatMetric(workspace.monthlySpend, 'usd')} / ${formatMetric(workspace.monthlyBudget, 'usd')}`}
                      warnAt={0.9}
                    />
                  </div>
                </Panel>
              )
            })}
          </div>
        )}
      </AsyncBoundary>
    </div>
  )
}
