import { CheckOutlined, MinusOutlined, PlusOutlined } from '@ant-design/icons'
import { App, Button, Select, Tag } from 'antd'
import { useState } from 'react'
import { AsyncBoundary } from '../../components/common/AsyncBoundary'
import { DangerConfirm } from '../../components/common/DangerConfirm'
import { PageHeader } from '../../components/common/PageHeader'
import { Panel } from '../../components/common/Panel'
import { StatusBadge } from '../../components/common/StatusBadge'
import { useAsyncData } from '../../hooks/useAsyncData'
import { ROLE_LABEL, ROLE_MATRIX, systemApi, type AdminAccount } from '../../api'

const ROLES: AdminAccount['role'][] = ['super-admin', 'operator', 'viewer']

export default function RbacPage() {
  const { message } = App.useApp()
  const { data, loading, error, reload } = useAsyncData(() => systemApi.rbac(), [])
  const [removing, setRemoving] = useState<AdminAccount>()

  return (
    <div>
      <PageHeader
        description="管理员账号与角色。权限矩阵明确列出每个角色能做什么，不靠猜。"
        actions={
          <Button size="small" type="primary" icon={<PlusOutlined />}>
            邀请管理员
          </Button>
        }
      />

      <AsyncBoundary loading={loading} error={error} onRetry={reload} skeletonRows={8}>
        {data && (
          <>
            <Panel eyebrow="ACCOUNTS" title="管理员账号" className="mb-4" bodyClassName="overflow-x-auto">
              <table className="w-full min-w-[720px] border-collapse text-[11px]">
                <thead>
                  <tr className="border-b border-line bg-plane font-mono text-[9px] text-ink-muted">
                    <th className="px-2 py-2 text-left font-normal">账号</th>
                    <th className="px-2 py-2 text-left font-normal">角色</th>
                    <th className="px-2 py-2 text-left font-normal">双重验证</th>
                    <th className="px-2 py-2 text-right font-normal">最近活跃</th>
                    <th className="px-2 py-2 text-right font-normal">操作</th>
                  </tr>
                </thead>
                <tbody>
                  {data.accounts.map((account) => (
                    <tr
                      key={account.id}
                      className="border-b border-line-soft last:border-b-0 hover:bg-plane"
                    >
                      <td className="px-2 py-2.5">
                        <b className="font-medium">{account.name}</b>
                        <small className="ml-2 text-ink-muted">{account.email}</small>
                      </td>
                      <td className="px-2 py-2.5">
                        <Select
                          size="small"
                          defaultValue={account.role}
                          disabled={account.role === 'super-admin'}
                          className="w-32"
                          options={ROLES.map((role) => ({ value: role, label: ROLE_LABEL[role] }))}
                        />
                      </td>
                      <td className="px-2 py-2.5">
                        <StatusBadge
                          kind={account.twoFactor ? 'good' : 'warning'}
                          label={account.twoFactor ? '已启用' : '未启用'}
                          size="small"
                        />
                      </td>
                      <td className="px-2 py-2.5 text-right font-mono text-ink-muted">
                        {account.lastActive}
                      </td>
                      <td className="px-2 py-2.5 text-right">
                        {account.role === 'super-admin' ? (
                          <Tag className="m-0 text-[9px]">不可移除</Tag>
                        ) : (
                          <Button size="small" type="text" danger onClick={() => setRemoving(account)}>
                            移除
                          </Button>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </Panel>

            {/* 把「运维到底能干什么」写清楚，而不是留给人猜 */}
            <Panel
              eyebrow="MATRIX"
              title="角色权限矩阵"
              description="破坏性操作与账务权限只授予超级管理员"
              bodyClassName="overflow-x-auto"
            >
              <table className="w-full min-w-[560px] border-collapse text-[11px]">
                <thead>
                  <tr className="border-b border-line bg-plane font-mono text-[9px] text-ink-muted">
                    <th className="px-2 py-2 text-left font-normal">能力</th>
                    {ROLES.map((role) => (
                      <th key={role} className="px-2 py-2 text-center font-normal">
                        {ROLE_LABEL[role]}
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {ROLE_MATRIX.map((row) => (
                    <tr key={row.capability} className="border-b border-line-soft last:border-b-0">
                      <td className="px-2 py-2.5 text-ink-secondary">{row.capability}</td>
                      {ROLES.map((role) => (
                        <td key={role} className="px-2 py-2.5 text-center">
                          {row.roles[role] ? (
                            <CheckOutlined className="text-status-good" />
                          ) : (
                            <MinusOutlined className="text-ink-muted" />
                          )}
                        </td>
                      ))}
                    </tr>
                  ))}
                </tbody>
              </table>
            </Panel>
          </>
        )}
      </AsyncBoundary>

      <DangerConfirm
        open={!!removing}
        title="移除管理员"
        resourceName={removing?.email ?? ''}
        description="移除后该账号将立即失去管理平台的全部访问权限。"
        okText="确认移除"
        onCancel={() => setRemoving(undefined)}
        onConfirm={() => {
          message.success(`${removing?.email} 已移除`)
          setRemoving(undefined)
          reload()
        }}
      />
    </div>
  )
}
