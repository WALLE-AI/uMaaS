import { ArrowRightOutlined, EnvironmentOutlined } from '@ant-design/icons'
import { Select, Tag } from 'antd'
import { Meter } from '../../components/charts/Meter'
import { AsyncBoundary } from '../../components/common/AsyncBoundary'
import { PageHeader } from '../../components/common/PageHeader'
import { Panel } from '../../components/common/Panel'
import { useAsyncData } from '../../hooks/useAsyncData'
import { catalogApi } from '../../api'

const STRATEGY_HINT: Record<string, string> = {
  质量优先: 'always route to the highest-quality endpoint, ignore cost',
  成本优先: '在满足延迟约束的前提下选最便宜的端点',
  延迟优先: '选 P95 最低的健康端点，区域内优先',
  均衡: '按权重在质量、成本、延迟之间取平衡',
}

export default function RoutingPage() {
  const { data, loading, error, reload } = useAsyncData(() => catalogApi.routing(), [])

  return (
    <div>
      <PageHeader description="每个模型的路由策略、回退链与灰度配置。回退链自左向右依次尝试。" />

      <AsyncBoundary loading={loading} error={error} onRetry={reload} skeletonRows={8}>
        {data && (
          <div className="flex flex-col gap-3">
            {data.policies.map((policy) => (
              <Panel key={policy.id} className="p-4">
                <div className="flex flex-wrap items-start justify-between gap-4">
                  <div className="min-w-0">
                    <div className="flex flex-wrap items-center gap-2">
                      <b className="text-[13px]">{policy.model}</b>
                      {policy.regionPinned && (
                        <Tag className="m-0 text-[9px]" icon={<EnvironmentOutlined />}>
                          区域内路由
                        </Tag>
                      )}
                      {policy.canary && (
                        <Tag color="orange" className="m-0 text-[9px]">
                          灰度 {policy.canary.percent}% → {policy.canary.channel}
                        </Tag>
                      )}
                    </div>
                    <p className="mt-1 mb-0 text-[10px] text-ink-muted">
                      {STRATEGY_HINT[policy.strategy]}
                    </p>
                  </div>

                  <Select
                    size="small"
                    defaultValue={policy.strategy}
                    className="w-28"
                    options={['质量优先', '成本优先', '延迟优先', '均衡'].map((value) => ({
                      value,
                      label: value,
                    }))}
                  />
                </div>

                <div className="mt-3">
                  <span className="font-mono text-[8px] tracking-widest text-ink-muted">
                    FALLBACK CHAIN
                  </span>
                  <div className="mt-1.5 flex flex-wrap items-center gap-2">
                    {policy.fallbackChain.map((channel, index) => (
                      <span key={channel} className="flex items-center gap-2">
                        {index > 0 && <ArrowRightOutlined className="text-[8px] text-ink-muted" />}
                        <span className="rounded border border-line bg-plane px-2 py-1 text-[10px]">
                          {channel}
                        </span>
                      </span>
                    ))}
                  </div>
                </div>

                {policy.canary && (
                  <div className="mt-3 max-w-xs">
                    <Meter
                      label={`灰度放量 · ${policy.canary.channel}`}
                      value={policy.canary.percent}
                      max={100}
                      valueText={`${policy.canary.percent}%`}
                      warnAt={2}
                    />
                  </div>
                )}
              </Panel>
            ))}
          </div>
        )}
      </AsyncBoundary>
    </div>
  )
}
