import { CheckCircleFilled, CloudDownloadOutlined, ExperimentOutlined, WarningFilled } from '@ant-design/icons'
import { Alert, App, AutoComplete, Button, Checkbox, Input, Modal, Radio, Select, Tag } from 'antd'
import { useState } from 'react'
import { useNavigate } from 'react-router'
import { Meter } from '../../components/charts/Meter'
import { AsyncBoundary } from '../../components/common/AsyncBoundary'
import { PageHeader } from '../../components/common/PageHeader'
import { Panel } from '../../components/common/Panel'
import { StatusBadge } from '../../components/common/StatusBadge'
import { DownloadProgress } from '../../components/infra/DownloadProgress'
import { useAsyncData } from '../../hooks/useAsyncData'
import { infraApi, type DownloadTask, type ResolvedRepo } from '../../api'
import { REPO_SUGGESTIONS } from '../../mock/registry'

export default function RegistryPage() {
  const { message } = App.useApp()
  const navigate = useNavigate()
  const { data, loading, error, reload } = useAsyncData(() => infraApi.registry(), [])

  const [pullOpen, setPullOpen] = useState(false)
  const [tasks, setTasks] = useState<DownloadTask[] | undefined>()

  const list = tasks ?? data?.tasks ?? []

  const mutate = (id: string, patch: Partial<DownloadTask>) =>
    setTasks((old) => (old ?? data?.tasks ?? []).map((t) => (t.id === id ? { ...t, ...patch } : t)))

  return (
    <div>
      <PageHeader
        description="从开源社区解析并拉取权重。先解析后下载，结构参数会自动回填到容量评估器。"
        actions={
          <Button
            size="small"
            type="primary"
            icon={<CloudDownloadOutlined />}
            onClick={() => setPullOpen(true)}
          >
            从社区拉取
          </Button>
        }
      />

      <AsyncBoundary loading={loading} error={error} onRetry={reload} skeletonRows={8}>
        {data && (
          <>
            <div className="mb-4 max-w-sm">
              <Meter
                label="模型仓库磁盘"
                value={data.disk.usedTb}
                max={data.disk.totalTb}
                valueText={`${data.disk.usedTb} / ${data.disk.totalTb} TB`}
              />
            </div>

            {list.length > 0 && (
              <Panel eyebrow="IN PROGRESS" title={`下载中 · ${list.length}`} className="mb-4">
                {list.map((task) => (
                  <DownloadProgress
                    key={task.id}
                    task={task}
                    onPause={(id) => mutate(id, { status: 'paused' })}
                    onResume={(id) => mutate(id, { status: 'downloading' })}
                    onCancel={(id) => {
                      setTasks((old) => (old ?? data.tasks).filter((t) => t.id !== id))
                      message.info('已取消并清理分片')
                    }}
                  />
                ))}
              </Panel>
            )}

            <Panel eyebrow="LOCAL" title={`本地模型 · ${data.models.length}`} bodyClassName="overflow-x-auto">
              <table className="w-full min-w-[780px] border-collapse text-[11px]">
                <thead>
                  <tr className="border-b border-line bg-plane font-mono text-[9px] text-ink-muted">
                    <th className="px-2 py-2 text-left font-normal">模型</th>
                    <th className="px-2 py-2 text-right font-normal">参数量</th>
                    <th className="px-2 py-2 text-left font-normal">精度</th>
                    <th className="px-2 py-2 text-right font-normal">大小</th>
                    <th className="px-2 py-2 text-left font-normal">校验</th>
                    <th className="px-2 py-2 text-left font-normal">来源</th>
                    <th className="px-2 py-2 text-left font-normal">部署</th>
                    <th className="px-2 py-2 text-right font-normal">操作</th>
                  </tr>
                </thead>
                <tbody>
                  {data.models.map((model) => (
                    <tr key={model.id} className="border-b border-line-soft last:border-b-0 hover:bg-plane">
                      <td className="px-2 py-2.5">
                        <b className="font-medium">{model.name}</b>
                        <small className="ml-2 text-ink-muted">{model.pulledAt}</small>
                      </td>
                      <td className="px-2 py-2.5 text-right font-mono">{model.paramsB}B</td>
                      <td className="px-2 py-2.5 font-mono">{model.precision}</td>
                      <td className="px-2 py-2.5 text-right font-mono">{model.sizeGb} GB</td>
                      <td className="px-2 py-2.5">
                        <StatusBadge
                          kind={model.verified ? 'good' : 'warning'}
                          label={model.verified ? 'SHA256 通过' : '未校验'}
                          size="small"
                        />
                      </td>
                      <td className="px-2 py-2.5 text-ink-muted">{model.source}</td>
                      <td className="px-2 py-2.5">
                        {model.deployments > 0 ? (
                          <Tag color="purple" className="m-0 text-[9px]">
                            {model.deployments} 个实例
                          </Tag>
                        ) : (
                          <span className="text-ink-muted">—</span>
                        )}
                      </td>
                      <td className="px-2 py-2.5 text-right">
                        <Button
                          size="small"
                          type="text"
                          icon={<ExperimentOutlined />}
                          onClick={() =>
                            navigate(`/infra/capacity?model=${encodeURIComponent(model.name)}`)
                          }
                        >
                          评估
                        </Button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </Panel>
          </>
        )}
      </AsyncBoundary>

      <PullModal
        open={pullOpen}
        onClose={() => setPullOpen(false)}
        onStarted={(repo) => {
          setPullOpen(false)
          message.success(`已开始拉取 ${repo}`)
        }}
      />
    </div>
  )
}

function PullModal({
  open,
  onClose,
  onStarted,
}: {
  open: boolean
  onClose: () => void
  onStarted: (repo: string) => void
}) {
  const navigate = useNavigate()
  const [source, setSource] = useState<'HuggingFace' | 'ModelScope' | 'custom'>('HuggingFace')
  const [repo, setRepo] = useState('')
  const [token, setToken] = useState('')
  const [resolving, setResolving] = useState(false)
  const [resolved, setResolved] = useState<ResolvedRepo>()
  const [resolveError, setResolveError] = useState<string>()
  const [mirror, setMirror] = useState(true)
  const [verify, setVerify] = useState(true)

  const resolve = async () => {
    if (!repo.trim()) return
    setResolving(true)
    setResolveError(undefined)
    setResolved(undefined)
    try {
      setResolved(await infraApi.resolveRepo(repo.trim()))
    } catch (cause) {
      setResolveError(cause instanceof Error ? cause.message : String(cause))
    } finally {
      setResolving(false)
    }
  }

  const blockedByGate = resolved?.gated && !token.trim()
  const canStart = !!resolved && resolved.disk.sufficient && !blockedByGate

  const reset = () => {
    setResolved(undefined)
    setResolveError(undefined)
    setRepo('')
    setToken('')
  }

  return (
    <Modal
      open={open}
      width={620}
      title="从开源社区拉取"
      onCancel={() => {
        reset()
        onClose()
      }}
      destroyOnHidden
      footer={
        <div className="flex justify-end gap-2">
          <Button
            onClick={() => {
              reset()
              onClose()
            }}
          >
            取消
          </Button>
          <Button
            type="primary"
            disabled={!canStart}
            onClick={() => {
              if (resolved) {
                onStarted(resolved.repo)
                reset()
              }
            }}
          >
            开始拉取
          </Button>
        </div>
      }
    >
      <div className="flex flex-col gap-4">
        <label className="flex flex-col gap-1.5">
          <span className="text-[11px] text-ink-secondary">来源</span>
          <Radio.Group
            size="small"
            value={source}
            onChange={(event) => setSource(event.target.value)}
            optionType="button"
            options={[
              { value: 'HuggingFace', label: 'HuggingFace' },
              { value: 'ModelScope', label: 'ModelScope' },
              { value: 'custom', label: '自定义 Git' },
            ]}
          />
        </label>

        <label className="flex flex-col gap-1.5">
          <span className="text-[11px] text-ink-secondary">仓库</span>
          <div className="flex gap-2">
            <AutoComplete
              className="flex-1"
              value={repo}
              onChange={setRepo}
              options={REPO_SUGGESTIONS.map((item) => ({ value: item }))}
              filterOption={(input, option) =>
                String(option?.value ?? '').toLowerCase().includes(input.toLowerCase())
              }
            >
              <Input placeholder="例如 Qwen/Qwen3-72B-Instruct" onPressEnter={resolve} />
            </AutoComplete>
            <Button onClick={resolve} loading={resolving} disabled={!repo.trim()}>
              解析
            </Button>
          </div>
        </label>

        {resolveError && <Alert type="error" showIcon message={resolveError} />}

        {resolved && (
          <div className="rounded border border-line bg-plane p-3">
            <div className="flex flex-wrap items-center gap-2">
              <b className="text-[12px]">{resolved.displayName}</b>
              <Tag className="m-0 text-[9px]">{resolved.paramsB}B 参数</Tag>
              <Tag className="m-0 text-[9px]">{resolved.license}</Tag>
            </div>

            <div className="mt-2 grid grid-cols-2 gap-x-4 gap-y-1 font-mono text-[10px] text-ink-secondary">
              <span>格式 {resolved.format}</span>
              <span>
                {resolved.shards} 分片 · {resolved.sizeGb} GB
              </span>
              <span className="col-span-2">
                config.json：{resolved.config.layers} 层 / {resolved.config.attnHeads} 注意力头 /{' '}
                {resolved.config.kvHeads} KV 头 / 头维度 {resolved.config.headDim}
              </span>
            </div>

            <div className="mt-2.5 flex flex-col gap-1.5">
              {/* 磁盘在解析阶段预检，避免下到一半撑爆盘 */}
              <span className="flex items-center gap-1.5 text-[10px]">
                {resolved.disk.sufficient ? (
                  <>
                    <CheckCircleFilled className="text-status-good" />
                    磁盘充足：可用 {(resolved.disk.availableGb / 1024).toFixed(1)} TB，需要{' '}
                    {resolved.disk.requiredGb} GB
                  </>
                ) : (
                  <>
                    <WarningFilled className="text-status-critical" />
                    磁盘不足：可用 {(resolved.disk.availableGb / 1024).toFixed(1)} TB，需要{' '}
                    {resolved.disk.requiredGb} GB
                  </>
                )}
              </span>

              {/* 门控仓库在解析阶段就暴露，而不是下到 90% 才 403 */}
              <span className="flex items-center gap-1.5 text-[10px]">
                {resolved.gated ? (
                  <>
                    <WarningFilled className="text-status-warning" />
                    门控仓库，需要访问令牌
                  </>
                ) : (
                  <>
                    <CheckCircleFilled className="text-status-good" />
                    {resolved.license} · 无需接受额外协议
                  </>
                )}
              </span>
            </div>

            {resolved.gated && (
              <Alert
                className="mt-2.5"
                type="warning"
                showIcon
                message={<span className="text-[10px]">{resolved.gatedReason}</span>}
              />
            )}

            <Button
              size="small"
              className="mt-3"
              icon={<ExperimentOutlined />}
              onClick={() => {
                const query = new URLSearchParams({
                  model: resolved.displayName,
                  params: String(resolved.paramsB),
                  layers: String(resolved.config.layers),
                  kvHeads: String(resolved.config.kvHeads),
                  attnHeads: String(resolved.config.attnHeads),
                  headDim: String(resolved.config.headDim),
                })
                navigate(`/infra/capacity?${query.toString()}`)
              }}
            >
              用这些参数去评估容量
            </Button>
          </div>
        )}

        <div className="flex flex-col gap-2">
          <span className="text-[11px] text-ink-secondary">加速与校验</span>
          <Checkbox checked={mirror} onChange={(e) => setMirror(e.target.checked)}>
            <span className="text-[11px]">使用国内镜像端点</span>
          </Checkbox>
          <Checkbox checked={verify} onChange={(e) => setVerify(e.target.checked)}>
            <span className="text-[11px]">下载后校验 SHA256</span>
          </Checkbox>
          <label className="mt-1 flex items-center gap-2">
            <span className="w-20 shrink-0 text-[11px] text-ink-secondary">并发分片</span>
            <Select
              size="small"
              defaultValue={8}
              className="w-20"
              options={[2, 4, 8, 16].map((v) => ({ value: v, label: String(v) }))}
            />
          </label>
        </div>

        <label className="flex flex-col gap-1.5">
          <span className="text-[11px] text-ink-secondary">
            访问令牌{resolved?.gated ? '（门控仓库必填）' : '（可选）'}
          </span>
          <Input.Password
            value={token}
            onChange={(event) => setToken(event.target.value)}
            placeholder="hf_xxx"
            status={blockedByGate ? 'error' : undefined}
          />
        </label>
      </div>
    </Modal>
  )
}
