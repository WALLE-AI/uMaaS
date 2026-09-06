import { ApiOutlined, PlusOutlined, ThunderboltOutlined } from '@ant-design/icons'
import { App, Button, Tag } from 'antd'
import { useState } from 'react'
import { Meter } from '../../components/charts/Meter'
import { AsyncBoundary } from '../../components/common/AsyncBoundary'
import { PageHeader } from '../../components/common/PageHeader'
import { Panel } from '../../components/common/Panel'
import { StatusBadge } from '../../components/common/StatusBadge'
import { formatMetric } from '../../config/metrics'
import { useAsyncData } from '../../hooks/useAsyncData'
import { catalogApi, type Channel } from '../../api'

type TestResult = { ok: boolean; latency: number; detail: string }

export default function ChannelsPage() {
  const { message } = App.useApp()
  const { data, loading, error, reload } = useAsyncData(() => catalogApi.channels(), [])
  const [testing, setTesting] = useState<Set<string>>(new Set())
  const [results, setResults] = useState<Record<string, TestResult>>({})

  const runTest = async (channel: Channel) => {
    setTesting((old) => new Set(old).add(channel.id))
    try {
      const result = await catalogApi.testChannel(channel.id)
      setResults((old) => ({ ...old, [channel.id]: result }))
      if (result.ok) message.success(`${channel.name} 连通正常 · ${result.latency}ms`)
      else message.error(`${channel.name} 连通失败`)
    } finally {
      setTesting((old) => {
        const next = new Set(old)
        next.delete(channel.id)
        return next
      })
    }
  }

  const testAll = async (channels: Channel[]) => {
    for (const channel of channels) {
      await runTest(channel)
    }
  }

  return (
    <div>
      <PageHeader
        description="供应商端点、权重、限流与健康状态。连通性测试会发出一条真实请求。"
        actions={
          <>
            <Button
              size="small"
              icon={<ThunderboltOutlined />}
              onClick={() => data && testAll(data.channels)}
              disabled={!data || testing.size > 0}
            >
              测试全部
            </Button>
            <Button size="small" type="primary" icon={<PlusOutlined />}>
              新建渠道
            </Button>
          </>
        }
      />

      <AsyncBoundary loading={loading} error={error} onRetry={reload} skeletonRows={8}>
        {data && (
          <div className="flex flex-col gap-3">
            {data.channels.map((channel) => {
              const result = results[channel.id]
              return (
                <Panel key={channel.id} className="p-4">
                  <div className="flex flex-wrap items-start justify-between gap-4">
                    <div className="min-w-0">
                      <div className="flex flex-wrap items-center gap-2">
                        <ApiOutlined className="text-brand" />
                        <b className="text-[13px]">{channel.name}</b>
                        <Tag className="m-0 text-[9px]">{channel.type}</Tag>
                        <Tag className="m-0 text-[9px]">{channel.region}</Tag>
                        <StatusBadge
                          kind={channel.status}
                          label={channel.statusLabel}
                          size="small"
                        />
                      </div>
                      <p className="mt-1.5 mb-0 text-[10px] text-ink-muted">
                        优先级 {channel.priority} · 承载 {channel.models} 个模型
                      </p>
                    </div>

                    <div className="flex items-center gap-5 font-mono text-[10px]">
                      <span className="flex flex-col items-end">
                        <small className="text-ink-muted">P95</small>
                        <b>{formatMetric(channel.p95, 'ms')}</b>
                      </span>
                      <span className="flex flex-col items-end">
                        <small className="text-ink-muted">错误率</small>
                        <b className={channel.errorRate > 5 ? 'text-status-critical' : undefined}>
                          {channel.errorRate.toFixed(2)}%
                        </b>
                      </span>
                      <span className="flex flex-col items-end">
                        <small className="text-ink-muted">今日成本</small>
                        <b>{formatMetric(channel.costToday, 'usd')}</b>
                      </span>
                      <Button
                        size="small"
                        loading={testing.has(channel.id)}
                        onClick={() => runTest(channel)}
                      >
                        连通性测试
                      </Button>
                    </div>
                  </div>

                  <div className="mt-3 max-w-xs">
                    <Meter
                      label="路由权重"
                      value={channel.weight}
                      max={100}
                      valueText={`${channel.weight}%`}
                      warnAt={2}
                    />
                  </div>

                  {/* 自动降权的原因必须写出来，否则管理者只看到「降级」不知道为什么 */}
                  {channel.degradedReason && (
                    <div className="mt-3 rounded bg-plane px-3 py-2 text-[10px] text-status-serious">
                      {channel.degradedReason}
                    </div>
                  )}

                  {result && (
                    <div className="mt-3 rounded border border-line bg-plane px-3 py-2">
                      <div className="flex items-center gap-2 text-[10px]">
                        <StatusBadge
                          kind={result.ok ? 'good' : 'critical'}
                          label={result.ok ? `连通正常 · ${result.latency}ms` : '连通失败'}
                          size="small"
                        />
                      </div>
                      <pre className="log-stream mt-1.5 mb-0 max-h-24 overflow-auto whitespace-pre-wrap text-ink-muted">
                        {result.detail}
                      </pre>
                    </div>
                  )}
                </Panel>
              )
            })}
          </div>
        )}
      </AsyncBoundary>
    </div>
  )
}
