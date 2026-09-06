import { SearchOutlined } from '@ant-design/icons'
import { App, Button, Input, Select } from 'antd'
import { useMemo, useState } from 'react'
import { AsyncBoundary } from '../../components/common/AsyncBoundary'
import { DangerConfirm } from '../../components/common/DangerConfirm'
import { FilterBar, FilterField } from '../../components/common/FilterBar'
import { PageHeader } from '../../components/common/PageHeader'
import { Panel } from '../../components/common/Panel'
import { StatusBadge } from '../../components/common/StatusBadge'
import { formatMetric } from '../../config/metrics'
import { useAsyncData } from '../../hooks/useAsyncData'
import { customersApi, type CustomerUser } from '../../api'

export default function CustomerUsersPage() {
  const { message } = App.useApp()
  const { data, loading, error, reload } = useAsyncData(() => customersApi.users(), [])
  const [query, setQuery] = useState('')
  const [plan, setPlan] = useState('all')
  const [banning, setBanning] = useState<CustomerUser>()

  const filtered = useMemo(() => {
    if (!data) return []
    return data.users.filter((user) => {
      const matchQuery =
        !query ||
        `${user.name} ${user.email} ${user.workspace}`.toLowerCase().includes(query.toLowerCase())
      return matchQuery && (plan === 'all' || user.plan === plan)
    })
  }, [data, query, plan])

  return (
    <div>
      <PageHeader description="用户检索、套餐与用量、余额状态。停用会立即中断该用户的全部请求。" />

      <FilterBar>
        <Input
          size="small"
          prefix={<SearchOutlined />}
          placeholder="搜索姓名、邮箱或工作空间"
          allowClear
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          className="w-64"
        />
        <FilterField label="套餐">
          <Select
            size="small"
            value={plan}
            onChange={setPlan}
            className="w-28"
            options={[
              { value: 'all', label: '全部' },
              ...['免费版', '自助版', '团队版', '企业版'].map((v) => ({ value: v, label: v })),
            ]}
          />
        </FilterField>
      </FilterBar>

      <AsyncBoundary loading={loading} error={error} onRetry={reload} skeletonRows={8}>
        {data && (
          <Panel bodyClassName="overflow-x-auto">
            <table className="w-full min-w-[900px] border-collapse text-[11px]">
              <thead>
                <tr className="border-b border-line bg-plane font-mono text-[9px] text-ink-muted">
                  <th className="px-2 py-2 text-left font-normal">用户</th>
                  <th className="px-2 py-2 text-left font-normal">工作空间</th>
                  <th className="px-2 py-2 text-left font-normal">套餐</th>
                  <th className="px-2 py-2 text-right font-normal">30 日 Tokens</th>
                  <th className="px-2 py-2 text-right font-normal">30 日成本</th>
                  <th className="px-2 py-2 text-right font-normal">余额</th>
                  <th className="px-2 py-2 text-left font-normal">状态</th>
                  <th className="px-2 py-2 text-right font-normal">最近活跃</th>
                  <th className="px-2 py-2 text-right font-normal">操作</th>
                </tr>
              </thead>
              <tbody>
                {filtered.map((user) => (
                  <tr key={user.id} className="border-b border-line-soft last:border-b-0 hover:bg-plane">
                    <td className="px-2 py-2.5">
                      <b className="font-medium">{user.name}</b>
                      <small className="ml-2 text-ink-muted">{user.email}</small>
                    </td>
                    <td className="px-2 py-2.5 text-ink-secondary">{user.workspace}</td>
                    <td className="px-2 py-2.5">{user.plan}</td>
                    <td className="px-2 py-2.5 text-right font-mono">
                      {formatMetric(user.tokens30d, 'tokens')}
                    </td>
                    <td className="px-2 py-2.5 text-right font-mono">
                      {formatMetric(user.cost30d, 'usd')}
                    </td>
                    <td
                      className={`px-2 py-2.5 text-right font-mono ${user.balance < 0 ? 'text-status-critical' : ''}`}
                    >
                      {formatMetric(user.balance, 'usd')}
                    </td>
                    <td className="px-2 py-2.5">
                      <StatusBadge kind={user.status} label={user.statusLabel} size="small" />
                    </td>
                    <td className="px-2 py-2.5 text-right text-ink-muted">{user.lastActive}</td>
                    <td className="px-2 py-2.5 text-right">
                      <Button size="small" type="text" danger onClick={() => setBanning(user)}>
                        停用
                      </Button>
                    </td>
                  </tr>
                ))}
                {filtered.length === 0 && (
                  <tr>
                    <td colSpan={9} className="py-10 text-center text-ink-muted">
                      没有匹配的用户
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </Panel>
        )}
      </AsyncBoundary>

      <DangerConfirm
        open={!!banning}
        title="停用用户"
        resourceName={banning?.email ?? ''}
        description="停用后该用户的全部 API Key 立即失效，进行中的请求会被中断。"
        impact={
          banning && banning.tokens30d > 0
            ? `该用户近 30 日消耗 ${formatMetric(banning.tokens30d, 'tokens')} tokens，可能正在生产环境使用。`
            : undefined
        }
        okText="确认停用"
        onCancel={() => setBanning(undefined)}
        onConfirm={() => {
          message.success(`${banning?.email} 已停用`)
          setBanning(undefined)
          reload()
        }}
      />
    </div>
  )
}
