import { DownloadOutlined, SearchOutlined, WarningFilled } from '@ant-design/icons'
import { Button, Input, Select, Switch, Tag } from 'antd'
import { useMemo, useState } from 'react'
import { AsyncBoundary } from '../../components/common/AsyncBoundary'
import { FilterBar, FilterField } from '../../components/common/FilterBar'
import { PageHeader } from '../../components/common/PageHeader'
import { Panel } from '../../components/common/Panel'
import { useAsyncData } from '../../hooks/useAsyncData'
import { systemApi } from '../../api'

/**
 * 审计日志。
 *
 * 破坏性操作单独标记并可一键筛选 —— 事故复盘时第一个要问的问题永远是
 * "谁动了什么"，而不是把它埋在几百条查询记录里。
 */
export default function AuditPage() {
  const { data, loading, error, reload } = useAsyncData(() => systemApi.audit(), [])
  const [query, setQuery] = useState('')
  const [actor, setActor] = useState('all')
  const [onlyDestructive, setOnlyDestructive] = useState(false)

  const actors = useMemo(
    () => Array.from(new Set(data?.entries.map((entry) => entry.actor) ?? [])),
    [data],
  )

  const filtered = useMemo(() => {
    if (!data) return []
    return data.entries.filter((entry) => {
      const matchQuery =
        !query ||
        `${entry.action} ${entry.target} ${entry.detail}`.toLowerCase().includes(query.toLowerCase())
      const matchActor = actor === 'all' || entry.actor === actor
      const matchDestructive = !onlyDestructive || entry.destructive
      return matchQuery && matchActor && matchDestructive
    })
  }, [data, query, actor, onlyDestructive])

  const destructiveCount = data?.entries.filter((entry) => entry.destructive).length ?? 0

  return (
    <div>
      <PageHeader
        description="管理操作留痕。破坏性操作已单独标记，可一键筛选。"
        actions={
          <Button size="small" icon={<DownloadOutlined />}>
            导出
          </Button>
        }
      />

      <FilterBar>
        <Input
          size="small"
          prefix={<SearchOutlined />}
          placeholder="搜索操作、目标或详情"
          allowClear
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          className="w-64"
        />
        <FilterField label="操作者">
          <Select
            size="small"
            value={actor}
            onChange={setActor}
            className="w-44"
            options={[
              { value: 'all', label: '全部' },
              ...actors.map((item) => ({ value: item, label: item })),
            ]}
          />
        </FilterField>
        <label className="flex items-center gap-2 text-[11px] text-ink-secondary">
          <Switch size="small" checked={onlyDestructive} onChange={setOnlyDestructive} />
          仅看破坏性操作
          <Tag color="red" className="m-0 text-[9px]">
            {destructiveCount}
          </Tag>
        </label>
      </FilterBar>

      <AsyncBoundary loading={loading} error={error} onRetry={reload} skeletonRows={10}>
        {data && (
          <Panel bodyClassName="overflow-x-auto">
            <table className="w-full min-w-[900px] border-collapse text-[11px]">
              <thead>
                <tr className="border-b border-line bg-plane font-mono text-[9px] text-ink-muted">
                  <th className="px-2 py-2 text-left font-normal">时间</th>
                  <th className="px-2 py-2 text-left font-normal">操作者</th>
                  <th className="px-2 py-2 text-left font-normal">操作</th>
                  <th className="px-2 py-2 text-left font-normal">目标</th>
                  <th className="px-2 py-2 text-left font-normal">详情</th>
                  <th className="px-2 py-2 text-right font-normal">来源 IP</th>
                </tr>
              </thead>
              <tbody>
                {filtered.map((entry) => (
                  <tr
                    key={entry.id}
                    className={`border-b border-line-soft last:border-b-0 ${
                      entry.destructive ? 'bg-[#fdf5f5]' : 'hover:bg-plane'
                    }`}
                  >
                    <td className="px-2 py-2.5 font-mono whitespace-nowrap text-ink-muted">
                      {entry.at}
                    </td>
                    <td className="px-2 py-2.5 font-mono">
                      {entry.actor === 'system' ? (
                        <Tag className="m-0 text-[9px]">系统自动</Tag>
                      ) : (
                        entry.actor
                      )}
                    </td>
                    <td className="px-2 py-2.5">
                      <span className="inline-flex items-center gap-1.5">
                        {entry.destructive && (
                          <WarningFilled className="text-status-critical" />
                        )}
                        <b className="font-medium">{entry.action}</b>
                      </span>
                    </td>
                    <td className="px-2 py-2.5 font-mono text-ink-secondary">{entry.target}</td>
                    <td className="px-2 py-2.5 text-ink-muted">{entry.detail}</td>
                    <td className="px-2 py-2.5 text-right font-mono text-ink-muted">{entry.ip}</td>
                  </tr>
                ))}
                {filtered.length === 0 && (
                  <tr>
                    <td colSpan={6} className="py-10 text-center text-ink-muted">
                      没有匹配的记录
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </Panel>
        )}
      </AsyncBoundary>
    </div>
  )
}
