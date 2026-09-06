import { CreditCardOutlined, FileTextOutlined, RollbackOutlined, WalletOutlined } from '@ant-design/icons'
import { App, Button, Tag } from 'antd'
import { useState } from 'react'
import { StatTileRow } from '../../components/charts/StatTile'
import { AsyncBoundary } from '../../components/common/AsyncBoundary'
import { DangerConfirm } from '../../components/common/DangerConfirm'
import { PageHeader } from '../../components/common/PageHeader'
import { Panel } from '../../components/common/Panel'
import { StatusBadge } from '../../components/common/StatusBadge'
import { formatMetric } from '../../config/metrics'
import { useAsyncData } from '../../hooks/useAsyncData'
import { customersApi, type Topup } from '../../api'

const INVOICE_STATUS = {
  paid: { kind: 'good' as const, label: '已支付' },
  pending: { kind: 'warning' as const, label: '待支付' },
  overdue: { kind: 'critical' as const, label: '已逾期' },
}

export default function BillingPage() {
  const { message } = App.useApp()
  const { data, loading, error, reload } = useAsyncData(() => customersApi.billing(), [])
  const [refunding, setRefunding] = useState<Topup>()

  return (
    <div>
      <PageHeader description="充值、退款、发票与对账。退款不可撤销，需键入订单号确认。" />

      <AsyncBoundary loading={loading} error={error} onRetry={reload} skeletonRows={8}>
        {data && (
          <>
            <StatTileRow
              items={[
                { label: '本月收入', value: formatMetric(data.summary.mrr, 'usd'), note: '已确认', icon: <WalletOutlined /> },
                { label: '待收款', value: formatMetric(data.summary.outstanding, 'usd'), note: '含逾期', icon: <FileTextOutlined /> },
                { label: '本月退款', value: formatMetric(data.summary.refunded, 'usd'), note: '1 笔', icon: <RollbackOutlined /> },
                { label: 'ARPU', value: formatMetric(data.summary.arpu, 'usd'), note: '按付费用户', icon: <CreditCardOutlined /> },
              ]}
            />

            <Panel eyebrow="INVOICES" title="发票" className="mb-4" bodyClassName="overflow-x-auto">
              <table className="w-full min-w-[680px] border-collapse text-[11px]">
                <thead>
                  <tr className="border-b border-line bg-plane font-mono text-[9px] text-ink-muted">
                    <th className="px-2 py-2 text-left font-normal">发票号</th>
                    <th className="px-2 py-2 text-left font-normal">工作空间</th>
                    <th className="px-2 py-2 text-left font-normal">账期</th>
                    <th className="px-2 py-2 text-right font-normal">金额</th>
                    <th className="px-2 py-2 text-left font-normal">状态</th>
                    <th className="px-2 py-2 text-right font-normal">开具日期</th>
                  </tr>
                </thead>
                <tbody>
                  {data.invoices.map((invoice) => (
                    <tr key={invoice.id} className="border-b border-line-soft last:border-b-0 hover:bg-plane">
                      <td className="px-2 py-2.5 font-mono">{invoice.id}</td>
                      <td className="px-2 py-2.5">{invoice.workspace}</td>
                      <td className="px-2 py-2.5 font-mono text-ink-muted">{invoice.period}</td>
                      <td className="px-2 py-2.5 text-right font-mono">
                        {formatMetric(invoice.amount, 'usd')}
                      </td>
                      <td className="px-2 py-2.5">
                        <StatusBadge
                          kind={INVOICE_STATUS[invoice.status].kind}
                          label={INVOICE_STATUS[invoice.status].label}
                          size="small"
                        />
                      </td>
                      <td className="px-2 py-2.5 text-right font-mono text-ink-muted">
                        {invoice.issuedAt}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </Panel>

            <Panel eyebrow="TOPUPS" title="充值记录" bodyClassName="overflow-x-auto">
              <table className="w-full min-w-[680px] border-collapse text-[11px]">
                <thead>
                  <tr className="border-b border-line bg-plane font-mono text-[9px] text-ink-muted">
                    <th className="px-2 py-2 text-left font-normal">订单号</th>
                    <th className="px-2 py-2 text-left font-normal">工作空间</th>
                    <th className="px-2 py-2 text-right font-normal">金额</th>
                    <th className="px-2 py-2 text-left font-normal">支付方式</th>
                    <th className="px-2 py-2 text-right font-normal">时间</th>
                    <th className="px-2 py-2 text-right font-normal">操作</th>
                  </tr>
                </thead>
                <tbody>
                  {data.topups.map((topup) => (
                    <tr key={topup.id} className="border-b border-line-soft last:border-b-0 hover:bg-plane">
                      <td className="px-2 py-2.5 font-mono">{topup.id}</td>
                      <td className="px-2 py-2.5">{topup.workspace}</td>
                      <td className="px-2 py-2.5 text-right font-mono">
                        {formatMetric(topup.amount, 'usd')}
                      </td>
                      <td className="px-2 py-2.5 text-ink-muted">{topup.method}</td>
                      <td className="px-2 py-2.5 text-right font-mono text-ink-muted">{topup.at}</td>
                      <td className="px-2 py-2.5 text-right">
                        {topup.status === 'refunded' ? (
                          <Tag className="m-0 text-[9px]">已退款</Tag>
                        ) : (
                          <Button size="small" type="text" danger onClick={() => setRefunding(topup)}>
                            退款
                          </Button>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </Panel>
          </>
        )}
      </AsyncBoundary>

      <DangerConfirm
        open={!!refunding}
        title="发起退款"
        resourceName={refunding?.id ?? ''}
        description="退款将原路退回支付渠道，操作不可撤销，且会同步扣减该工作空间的余额。"
        impact={
          refunding
            ? `退款金额 ${formatMetric(refunding.amount, 'usd')}，若余额不足将导致该工作空间余额为负。`
            : undefined
        }
        okText="确认退款"
        onCancel={() => setRefunding(undefined)}
        onConfirm={() => {
          message.success(`${refunding?.id} 退款已提交`)
          setRefunding(undefined)
          reload()
        }}
      />
    </div>
  )
}
