import {
  ApiOutlined,
  CheckCircleFilled,
  CreditCardOutlined,
  ReloadOutlined,
  RightOutlined,
  TeamOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons'
import { Button } from 'antd'
import { Link } from 'react-router'
import { MetricChart } from '../components/charts/MetricChart'
import { Meter } from '../components/charts/Meter'
import { StatTileRow } from '../components/charts/StatTile'
import { AsyncBoundary } from '../components/common/AsyncBoundary'
import { PageHeader } from '../components/common/PageHeader'
import { Panel } from '../components/common/Panel'
import { StatusBadge } from '../components/common/StatusBadge'
import { metrics } from '../config/metrics'
import { useAsyncData } from '../hooks/useAsyncData'
import { analyticsApi } from '../api'

/**
 * 平台总览 —— 只回答一个问题：现在健不健康，要不要我立刻处理什么。
 */
export default function OverviewPage() {
  const { data, loading, error, reload } = useAsyncData(() => analyticsApi.overview(), [])

  return (
    <div>
      <PageHeader
        title="平台总览"
        description="全平台健康度、流量、供给状态与集群水位。数据按 UTC 日聚合，5 分钟前更新。"
        actions={
          <Button size="small" icon={<ReloadOutlined />} onClick={reload}>
            刷新
          </Button>
        }
      />

      <AsyncBoundary loading={loading} error={error} onRetry={reload} skeletonRows={8}>
        {data && (
          <>
            {/*
              告警区为空时整块消失 —— 空状态不是内容。
              没有告警的日子不该在首屏留一个"暂无告警"的空框占位。
            */}
            {data.alerts.length > 0 && (
              <section className="mb-4 overflow-hidden rounded border border-line bg-surface">
                <div className="border-b border-line bg-plane px-4 py-2">
                  <span className="font-mono text-[9px] tracking-widest text-brand">
                    需要处理 · {data.alerts.length}
                  </span>
                </div>
                {data.alerts.map((alert) => (
                  <Link
                    key={alert.id}
                    to={alert.href}
                    className="flex items-center gap-3 border-b border-line-soft px-4 py-3 last:border-b-0 hover:bg-plane"
                  >
                    <StatusBadge kind={alert.kind} label={alert.title} />
                    <span className="min-w-0 flex-1 truncate text-[11px] text-ink-muted">
                      {alert.detail}
                    </span>
                    <RightOutlined className="text-[10px] text-ink-muted" />
                  </Link>
                ))}
              </section>
            )}

            <StatTileRow
              items={[
                {
                  label: '今日请求',
                  value: '4.82M',
                  note: '成功率 99.2%',
                  delta: { value: '12.4%', direction: 'up', good: true },
                  icon: <ApiOutlined />,
                  caveat: metrics.requests.caveat,
                },
                {
                  label: '今日 Tokens',
                  value: '1.94B',
                  note: '含缓存读写',
                  delta: { value: '18.1%', direction: 'up', good: true },
                  icon: <ThunderboltOutlined />,
                  caveat: metrics.tokens.caveat,
                },
                {
                  label: '今日成本',
                  value: '$2,418',
                  note: '供应商计费',
                  delta: { value: '9.7%', direction: 'up' },
                  icon: <CreditCardOutlined />,
                  caveat: metrics.cost.caveat,
                },
                {
                  label: '活跃用户',
                  value: '1,203',
                  note: '当日有成功调用',
                  delta: { value: '3.2%', direction: 'up', good: true },
                  icon: <TeamOutlined />,
                  caveat: metrics.activeUsers.caveat,
                },
                {
                  label: 'GPU 水位',
                  value: '73%',
                  note: '24 / 33 卡已分配',
                  delta: { value: '持平', direction: 'flat' },
                  icon: <CheckCircleFilled />,
                  caveat: metrics.gpuMemRatio.caveat,
                },
              ]}
            />

            <div className="mb-4 grid gap-4 lg:grid-cols-[minmax(0,1.55fr)_minmax(280px,0.65fr)]">
              <Panel
                eyebrow="REQUEST VOLUME"
                title="请求量 24 小时"
                description="单系列不出图例，标题已说明它是什么"
              >
                <MetricChart
                  form="area"
                  data={data.requests24h}
                  xKey="hour"
                  unit="count"
                  height={248}
                  series={[{ id: 'requests', label: '请求数' }]}
                />
              </Panel>

              <Panel eyebrow="SUPPLY HEALTH" title="供给健康">
                <div className="flex flex-col">
                  {data.channels.map((channel) => (
                    <div
                      key={channel.id}
                      className="flex items-center justify-between gap-3 border-b border-line-soft py-2.5 last:border-b-0"
                    >
                      <StatusBadge kind={channel.kind} label={channel.name} size="small" />
                      <span className="flex items-center gap-3 font-mono text-[10px] text-ink-muted">
                        <span>P95 {channel.p95}ms</span>
                        <span
                          className={channel.errorRate > 5 ? 'text-status-critical' : undefined}
                        >
                          {channel.errorRate.toFixed(2)}%
                        </span>
                      </span>
                    </div>
                  ))}
                </div>
                <Link
                  to="/catalog/channels"
                  className="mt-3 inline-flex items-center gap-1 text-[11px] text-brand"
                >
                  查看全部渠道 <RightOutlined className="text-[9px]" />
                </Link>
              </Panel>
            </div>

            <div className="grid gap-4 lg:grid-cols-[minmax(0,1.55fr)_minmax(280px,0.65fr)]">
              <Panel
                eyebrow="VENDOR SHARE"
                title="厂商 Token 份额 · 近 14 天"
                description="色彩跟随厂商而非排名，切换筛选后幸存系列不会改色"
              >
                <VendorShare />
              </Panel>

              <Panel eyebrow="CLUSTER" title="集群水位">
                <div className="flex flex-col gap-4">
                  {data.cluster.map((item) => (
                    <Meter
                      key={item.label}
                      label={item.label}
                      value={item.value}
                      max={item.max}
                      valueText={item.text}
                    />
                  ))}
                </div>
                <Link
                  to="/infra"
                  className="mt-4 inline-flex items-center gap-1 text-[11px] text-brand"
                >
                  进入集群总览 <RightOutlined className="text-[9px]" />
                </Link>
              </Panel>
            </div>
          </>
        )}
      </AsyncBoundary>
    </div>
  )
}

function VendorShare() {
  const { data, loading, error, reload } = useAsyncData(
    () => analyticsApi.usage('vendor', 'tokens', 14),
    [],
  )

  return (
    <AsyncBoundary loading={loading} error={error} onRetry={reload}>
      {data && (
        <MetricChart
          form="area"
          data={data.trend.data}
          xKey="date"
          unit="tokens"
          height={248}
          series={data.trend.series}
        />
      )}
    </AsyncBoundary>
  )
}
