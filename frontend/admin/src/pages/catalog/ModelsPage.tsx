import { CloudDownloadOutlined, PlusOutlined, SearchOutlined } from '@ant-design/icons'
import { App, Button, Input, Select, Tag } from 'antd'
import { useMemo, useState } from 'react'
import { AsyncBoundary } from '../../components/common/AsyncBoundary'
import { DangerConfirm } from '../../components/common/DangerConfirm'
import { DiffReview, type ApplyMode } from '../../components/common/DiffReview'
import { FilterBar, FilterField } from '../../components/common/FilterBar'
import { PageHeader } from '../../components/common/PageHeader'
import { Panel } from '../../components/common/Panel'
import { StatusBadge } from '../../components/common/StatusBadge'
import { compactNumber } from '../../config/metrics'
import { useAsyncData } from '../../hooks/useAsyncData'
import { MODEL_STATUS_KIND, MODEL_STATUS_LABEL, catalogApi, type CatalogModel, type DiffEntry, type ModelStatus } from '../../api'

const UPSTREAM_SOURCES = [
  { value: 'OpenAI /v1/models', label: 'OpenAI' },
  { value: 'Anthropic /v1/models', label: 'Anthropic' },
  { value: 'Google /v1beta/models', label: 'Google' },
]

export default function CatalogModelsPage() {
  const { message } = App.useApp()
  const { data, loading, error, reload } = useAsyncData(() => catalogApi.models(), [])

  const [query, setQuery] = useState('')
  const [vendor, setVendor] = useState('all')
  const [status, setStatus] = useState<ModelStatus | 'all'>('all')
  const [hosting, setHosting] = useState<'all' | 'cloud' | 'self'>('all')

  const [syncSource, setSyncSource] = useState(UPSTREAM_SOURCES[0].value)
  const [syncOpen, setSyncOpen] = useState(false)
  const [applying, setApplying] = useState(false)
  const [delisting, setDelisting] = useState<CatalogModel | undefined>()

  const diff = useAsyncData(
    () => (syncOpen ? catalogApi.diff(syncSource) : Promise.resolve(undefined)),
    [syncOpen, syncSource],
  )

  const filtered = useMemo(() => {
    if (!data) return []
    return data.models.filter((model) => {
      const matchQuery =
        !query ||
        `${model.name} ${model.id} ${model.vendor}`.toLowerCase().includes(query.toLowerCase())
      const matchVendor = vendor === 'all' || model.vendor === vendor
      const matchStatus = status === 'all' || model.status === status
      const matchHosting = hosting === 'all' || model.hosting === hosting
      return matchQuery && matchVendor && matchStatus && matchHosting
    })
  }, [data, query, vendor, status, hosting])

  const vendors = useMemo(
    () => Array.from(new Set(data?.models.map((model) => model.vendor) ?? [])),
    [data],
  )

  const applyDiff = (selected: DiffEntry[], mode: ApplyMode) => {
    setApplying(true)
    window.setTimeout(() => {
      setApplying(false)
      setSyncOpen(false)
      const modeLabel = mode === 'list' ? '已上架' : mode === 'canary' ? '已进入 10% 灰度' : '已存为草稿'
      message.success(`${selected.length} 项变更${modeLabel}`)
      reload()
    }, 900)
  }

  return (
    <div>
      <PageHeader
        description="模型的上下架、能力标签与定价。上游变更需经差异审阅后才会生效。"
        actions={
          <>
            <Select
              size="small"
              value={syncSource}
              onChange={setSyncSource}
              className="w-40"
              options={UPSTREAM_SOURCES}
            />
            <Button size="small" icon={<CloudDownloadOutlined />} onClick={() => setSyncOpen(true)}>
              从上游同步
            </Button>
            <Button size="small" type="primary" icon={<PlusOutlined />}>
              手工添加
            </Button>
          </>
        }
      />

      <AsyncBoundary loading={loading} error={error} onRetry={reload} skeletonRows={8}>
        {data && (
          <>
            <div className="mb-4 flex flex-wrap items-center gap-x-5 gap-y-2 text-[11px]">
              <span className="text-ink-secondary">
                共 <b className="font-mono text-ink">{data.summary.total}</b> 个模型
              </span>
              {(['listed', 'canary', 'delisted', 'draft'] as ModelStatus[]).map((key) => (
                <button
                  key={key}
                  type="button"
                  onClick={() => setStatus(status === key ? 'all' : key)}
                  className={`cursor-pointer rounded border px-2 py-0.5 transition-colors ${
                    status === key
                      ? 'border-brand bg-brand-soft text-[#6728df]'
                      : 'border-line bg-surface text-ink-secondary hover:bg-plane'
                  }`}
                >
                  {MODEL_STATUS_LABEL[key]} {data.summary[key]}
                </button>
              ))}
            </div>

            <FilterBar>
              <Input
                size="small"
                prefix={<SearchOutlined />}
                placeholder="搜索模型名或 ID"
                allowClear
                value={query}
                onChange={(event) => setQuery(event.target.value)}
                className="w-56"
              />
              <FilterField label="厂商">
                <Select
                  size="small"
                  value={vendor}
                  onChange={setVendor}
                  className="w-28"
                  options={[
                    { value: 'all', label: '全部' },
                    ...vendors.map((item) => ({ value: item, label: item })),
                  ]}
                />
              </FilterField>
              <FilterField label="类型">
                <Select
                  size="small"
                  value={hosting}
                  onChange={setHosting}
                  className="w-28"
                  options={[
                    { value: 'all', label: '全部' },
                    { value: 'cloud', label: '云端代理' },
                    { value: 'self', label: '自建部署' },
                  ]}
                />
              </FilterField>
            </FilterBar>

            <Panel bodyClassName="overflow-x-auto">
              <table className="w-full min-w-[900px] border-collapse text-[11px]">
                <thead>
                  <tr className="border-b border-line bg-plane font-mono text-[9px] text-ink-muted">
                    <th className="px-2 py-2 text-left font-normal">模型</th>
                    <th className="px-2 py-2 text-left font-normal">厂商</th>
                    <th className="px-2 py-2 text-left font-normal">状态</th>
                    <th className="px-2 py-2 text-right font-normal">上下文</th>
                    <th className="px-2 py-2 text-right font-normal">输入价</th>
                    <th className="px-2 py-2 text-right font-normal">输出价</th>
                    <th className="px-2 py-2 text-right font-normal">近 7 日调用</th>
                    <th className="px-2 py-2 text-right font-normal">操作</th>
                  </tr>
                </thead>
                <tbody>
                  {filtered.map((model) => (
                    <tr
                      key={model.id}
                      className="border-b border-line-soft last:border-b-0 hover:bg-plane"
                    >
                      <td className="px-2 py-2.5">
                        <b className="font-medium">{model.name}</b>
                        <code className="ml-2 font-mono text-[9px] text-ink-muted">{model.id}</code>
                        <span className="mt-1 flex flex-wrap gap-1">
                          {model.capabilities.map((capability) => (
                            <Tag key={capability} className="m-0 text-[9px]">
                              {capability}
                            </Tag>
                          ))}
                        </span>
                      </td>
                      <td className="px-2 py-2.5 text-ink-secondary">
                        {model.vendor}
                        {model.hosting === 'self' && (
                          <Tag color="purple" className="ml-1.5 m-0 text-[9px]">
                            自建
                          </Tag>
                        )}
                      </td>
                      <td className="px-2 py-2.5">
                        <StatusBadge
                          kind={MODEL_STATUS_KIND[model.status]}
                          label={
                            model.status === 'canary'
                              ? `灰度 ${model.canaryPercent}%`
                              : MODEL_STATUS_LABEL[model.status]
                          }
                          size="small"
                        />
                      </td>
                      <td className="px-2 py-2.5 text-right font-mono">{model.context}</td>
                      <td className="px-2 py-2.5 text-right font-mono">
                        {model.inputPrice === null ? '自建' : `$${model.inputPrice.toFixed(2)}`}
                      </td>
                      <td className="px-2 py-2.5 text-right font-mono">
                        {model.outputPrice === null ? '自建' : `$${model.outputPrice.toFixed(2)}`}
                      </td>
                      <td className="px-2 py-2.5 text-right font-mono">
                        {model.callsLast7d === 0 ? '—' : compactNumber(model.callsLast7d, 2)}
                      </td>
                      <td className="px-2 py-2.5 text-right">
                        {model.status !== 'delisted' && (
                          <Button
                            size="small"
                            type="text"
                            danger
                            onClick={() => setDelisting(model)}
                          >
                            下架
                          </Button>
                        )}
                      </td>
                    </tr>
                  ))}
                  {filtered.length === 0 && (
                    <tr>
                      <td colSpan={8} className="py-10 text-center text-ink-muted">
                        没有匹配的模型，试试放宽筛选条件
                      </td>
                    </tr>
                  )}
                </tbody>
              </table>
            </Panel>
          </>
        )}
      </AsyncBoundary>

      <DiffReview
        open={syncOpen}
        loading={applying || diff.loading}
        source={syncSource}
        fetchedAt={diff.data?.fetchedAt ?? '—'}
        entries={diff.data?.entries ?? []}
        onCancel={() => setSyncOpen(false)}
        onApply={applyDiff}
      />

      {/* 下架是破坏性操作：必须键入模型 ID，并在影响面里给出仍有多少调用 */}
      <DangerConfirm
        open={!!delisting}
        title="下架模型"
        resourceName={delisting?.id ?? ''}
        description="下架后该模型将从所有用户的可用列表中移除，正在使用它的请求会立即收到 404。"
        impact={
          delisting && delisting.callsLast7d > 0
            ? `该模型近 7 日仍有 ${compactNumber(delisting.callsLast7d, 1)} 次调用，建议先发公告并给出迁移期。`
            : undefined
        }
        okText="确认下架"
        onCancel={() => setDelisting(undefined)}
        onConfirm={() => {
          message.success(`${delisting?.name} 已下架`)
          setDelisting(undefined)
          reload()
        }}
      />
    </div>
  )
}
