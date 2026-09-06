import { CheckCircleFilled, FileTextOutlined, PlusOutlined, WarningFilled } from '@ant-design/icons'
import { Alert, App, Button, Drawer, InputNumber, Modal, Select, Steps, Tag } from 'antd'
import { useEffect, useRef, useState } from 'react'
import { useSearchParams } from 'react-router'
import { Meter } from '../../components/charts/Meter'
import { AsyncBoundary } from '../../components/common/AsyncBoundary'
import { DangerConfirm } from '../../components/common/DangerConfirm'
import { PageHeader } from '../../components/common/PageHeader'
import { Panel } from '../../components/common/Panel'
import { StatusBadge } from '../../components/common/StatusBadge'
import { LogStream, type LogLine } from '../../components/infra/LogStream'
import { useAsyncData } from '../../hooks/useAsyncData'
import { infraApi, type Deployment } from '../../api'
import { DEPLOY_LOG_LINES } from '../../mock/deployments'

export default function DeploymentsPage() {
  const { message } = App.useApp()
  const [params] = useSearchParams()
  const { data, loading, error, reload } = useAsyncData(() => infraApi.deployments(), [])

  const [wizardOpen, setWizardOpen] = useState(false)
  const [logsFor, setLogsFor] = useState<Deployment>()
  const [deleting, setDeleting] = useState<Deployment>()

  // 从容量评估器带参数跳转过来时，直接打开向导
  useEffect(() => {
    if (params.get('model')) setWizardOpen(true)
  }, [params])

  return (
    <div>
      <PageHeader
        description="部署实例的发布、扩缩与回滚。新建部署的最后一步是预检清单，不是直接启动。"
        actions={
          <Button size="small" type="primary" icon={<PlusOutlined />} onClick={() => setWizardOpen(true)}>
            新建部署
          </Button>
        }
      />

      <AsyncBoundary loading={loading} error={error} onRetry={reload} skeletonRows={8}>
        {data && (
          <div className="flex flex-col gap-3">
            {data.deployments.map((item) => (
              <Panel key={item.id} className="p-4">
                <div className="flex flex-wrap items-start justify-between gap-4">
                  <div className="min-w-0">
                    <div className="flex flex-wrap items-center gap-2">
                      <StatusBadge kind={item.statusKind} label={item.name} />
                      <Tag className="m-0 text-[9px]">{item.engine}</Tag>
                      <Tag className="m-0 text-[9px]">{item.precision}</Tag>
                      <Tag className="m-0 text-[9px]">TP={item.tensorParallel}</Tag>
                      {item.canaryPercent && (
                        <Tag color="orange" className="m-0 text-[9px]">
                          灰度 {item.canaryPercent}%
                        </Tag>
                      )}
                    </div>
                    <p className="mt-1.5 mb-0 font-mono text-[10px] text-ink-muted">
                      {item.model} · {item.node} GPU{item.gpus.join(',')} · 副本{' '}
                      {item.replicas.ready}/{item.replicas.desired}
                    </p>
                  </div>

                  <div className="flex items-center gap-5 font-mono text-[10px]">
                    <span className="flex flex-col items-end">
                      <small className="text-ink-muted">QPS</small>
                      <b>{item.qps}</b>
                    </span>
                    <span className="flex flex-col items-end">
                      <small className="text-ink-muted">P95</small>
                      <b>{item.p95 ? `${item.p95}ms` : '—'}</b>
                    </span>
                    {item.kvHitRate !== undefined && (
                      <span className="flex flex-col items-end">
                        <small className="text-ink-muted">KV 命中</small>
                        <b>{item.kvHitRate}%</b>
                      </span>
                    )}
                    <Button
                      size="small"
                      icon={<FileTextOutlined />}
                      onClick={() => setLogsFor(item)}
                    >
                      日志
                    </Button>
                    <Button size="small" danger type="text" onClick={() => setDeleting(item)}>
                      删除
                    </Button>
                  </div>
                </div>

                {item.status !== 'failed' && (
                  <div className="mt-3 max-w-sm">
                    <Meter
                      label="显存占用"
                      value={item.memUsedGb}
                      max={item.memTotalGb}
                      valueText={`${item.memUsedGb} / ${item.memTotalGb} GB`}
                    />
                  </div>
                )}

                {/* 失败原因必须具体到可操作，而不是一句「启动失败」 */}
                {item.failureReason && (
                  <Alert
                    className="mt-3"
                    type="error"
                    showIcon
                    message={<span className="text-[11px]">{item.failureReason}</span>}
                    action={
                      <Button size="small" onClick={() => setLogsFor(item)}>
                        查看日志
                      </Button>
                    }
                  />
                )}
              </Panel>
            ))}
          </div>
        )}
      </AsyncBoundary>

      <DeployWizard
        open={wizardOpen}
        presetModel={params.get('model') ?? undefined}
        presetTp={Number(params.get('tp')) || undefined}
        presetPrecision={params.get('precision') ?? undefined}
        onClose={() => setWizardOpen(false)}
        onDeployed={(name) => {
          setWizardOpen(false)
          message.success(`${name} 已开始部署`)
          reload()
        }}
      />

      <Drawer
        open={!!logsFor}
        width={760}
        title={`日志 · ${logsFor?.name ?? ''}`}
        onClose={() => setLogsFor(undefined)}
        destroyOnHidden
      >
        {logsFor && (
          <LogStream
            title={logsFor.name}
            lines={
              logsFor.status === 'failed'
                ? [
                    ...(DEPLOY_LOG_LINES.slice(0, 9) as LogLine[]),
                    { level: 'error', text: 'ERROR torch.cuda.OutOfMemoryError: CUDA out of memory.' },
                    { level: 'error', text: 'ERROR Tried to allocate 24.00 GiB. GPU 0 has 18.20 GiB free.' },
                    { level: 'error', text: 'deployment failed after 42s' },
                  ]
                : (DEPLOY_LOG_LINES as LogLine[])
            }
            height={520}
          />
        )}
      </Drawer>

      <DangerConfirm
        open={!!deleting}
        title="删除部署实例"
        resourceName={deleting?.name ?? ''}
        description="删除后该实例立即停止服务，占用的 GPU 会被释放。正在处理中的请求会被中断。"
        impact={
          deleting && deleting.qps > 0
            ? `该实例当前承载 ${deleting.qps} QPS，删除会造成请求失败，建议先摘除路由权重。`
            : undefined
        }
        okText="确认删除"
        onCancel={() => setDeleting(undefined)}
        onConfirm={() => {
          message.success(`${deleting?.name} 已删除`)
          setDeleting(undefined)
          reload()
        }}
      />
    </div>
  )
}

const STEPS = ['模型', '资源', '运行参数', '服务接入', '预检']

function DeployWizard({
  open,
  presetModel,
  presetTp,
  presetPrecision,
  onClose,
  onDeployed,
}: {
  open: boolean
  presetModel?: string
  presetTp?: number
  presetPrecision?: string
  onClose: () => void
  onDeployed: (name: string) => void
}) {
  const [step, setStep] = useState(0)
  const [model, setModel] = useState(presetModel ?? 'Qwen3-72B-Instruct')
  const [node, setNode] = useState('node-01')
  const [tp, setTp] = useState(presetTp ?? 4)
  const [precision, setPrecision] = useState(presetPrecision ?? 'FP16')
  const [maxLen, setMaxLen] = useState(32768)
  const [port, setPort] = useState(8081)
  const [deploying, setDeploying] = useState(false)
  const [logs, setLogs] = useState<LogLine[]>([])
  const timer = useRef<number>(undefined)

  useEffect(() => {
    if (!open) {
      setStep(0)
      setDeploying(false)
      setLogs([])
      window.clearInterval(timer.current)
    }
  }, [open])

  useEffect(() => {
    if (presetModel) setModel(presetModel)
    if (presetTp) setTp(presetTp)
    if (presetPrecision) setPrecision(presetPrecision)
  }, [presetModel, presetTp, presetPrecision])

  const preflightState = useAsyncData(
    () => (open && step === STEPS.length - 1 ? infraApi.preflight(model, node, tp) : Promise.resolve([])),
    [open, step, model, node, tp],
  )
  const checks = preflightState.data ?? []
  const name = `${model.toLowerCase().replace(/[^a-z0-9]+/g, '-')}-prod`

  const startDeploy = () => {
    setDeploying(true)
    setLogs([])
    let index = 0
    timer.current = window.setInterval(() => {
      if (index >= DEPLOY_LOG_LINES.length) {
        window.clearInterval(timer.current)
        onDeployed(name)
        return
      }
      setLogs((old) => [...old, DEPLOY_LOG_LINES[index] as LogLine])
      index += 1
    }, 420)
  }

  return (
    <Modal
      open={open}
      width={720}
      title="新建部署"
      onCancel={onClose}
      destroyOnHidden
      maskClosable={!deploying}
      footer={
        deploying ? (
          <span className="text-[11px] text-ink-muted">部署进行中，请勿关闭…</span>
        ) : (
          <div className="flex justify-between gap-2">
            <Button disabled={step === 0} onClick={() => setStep((s) => s - 1)}>
              上一步
            </Button>
            <span className="flex gap-2">
              <Button onClick={onClose}>取消</Button>
              {step < STEPS.length - 1 ? (
                <Button type="primary" onClick={() => setStep((s) => s + 1)}>
                  下一步
                </Button>
              ) : (
                <Button type="primary" onClick={startDeploy}>
                  确认部署
                </Button>
              )}
            </span>
          </div>
        )
      }
    >
      <Steps
        size="small"
        current={step}
        className="mb-5"
        items={STEPS.map((title) => ({ title }))}
      />

      {deploying ? (
        <LogStream title={name} lines={logs} streaming height={340} />
      ) : (
        <div className="flex flex-col gap-4">
          {step === 0 && (
            <>
              <Field label="模型">
                <Select
                  className="w-full"
                  value={model}
                  onChange={setModel}
                  options={[
                    'Qwen3-72B-Instruct',
                    'Qwen3-32B-Instruct',
                    'Llama-4-70B',
                    'DeepSeek-V4-Lite',
                    'GLM-5-9B',
                  ].map((v) => ({ value: v, label: v }))}
                />
              </Field>
              <Field label="推理引擎">
                <Select
                  className="w-full"
                  defaultValue="vLLM 0.9"
                  options={['vLLM 0.9', 'SGLang 0.5'].map((v) => ({ value: v, label: v }))}
                />
              </Field>
              {presetModel && (
                <Alert
                  type="success"
                  showIcon
                  message={`已从容量评估器带入配置：TP=${presetTp} · ${presetPrecision}`}
                />
              )}
            </>
          )}

          {step === 1 && (
            <>
              <Field label="节点">
                <Select
                  className="w-full"
                  value={node}
                  onChange={setNode}
                  options={['node-01', 'node-02', 'node-03', 'node-05'].map((v) => ({
                    value: v,
                    label: v,
                  }))}
                />
              </Field>
              <Field label="张量并行度 TP">
                <Select
                  className="w-full"
                  value={tp}
                  onChange={setTp}
                  options={[1, 2, 4, 8].map((v) => ({ value: v, label: String(v) }))}
                />
              </Field>
              <Field label="精度">
                <Select
                  className="w-full"
                  value={precision}
                  onChange={setPrecision}
                  options={['FP16', 'FP8', 'INT4'].map((v) => ({ value: v, label: v }))}
                />
              </Field>
            </>
          )}

          {step === 2 && (
            <>
              <Field label="最大上下文 max_model_len">
                <Select
                  className="w-full"
                  value={maxLen}
                  onChange={setMaxLen}
                  options={[8192, 16384, 32768, 65536, 131072].map((v) => ({
                    value: v,
                    label: `${v / 1024}K`,
                  }))}
                />
              </Field>
              <Field label="GPU 显存利用率">
                <Select
                  className="w-full"
                  defaultValue={0.9}
                  options={[0.8, 0.85, 0.9, 0.95].map((v) => ({
                    value: v,
                    label: `${v * 100}%`,
                  }))}
                />
              </Field>
              <Field label="副本数">
                <InputNumber className="w-full" defaultValue={2} min={1} max={8} />
              </Field>
            </>
          )}

          {step === 3 && (
            <>
              <Field label="实例名">
                <Select className="w-full" value={name} options={[{ value: name, label: name }]} />
              </Field>
              <Field label="端口">
                <InputNumber
                  className="w-full"
                  value={port}
                  onChange={(v) => v && setPort(v)}
                  min={1024}
                  max={65535}
                />
              </Field>
              <Field label="上架方式">
                <Select
                  className="w-full"
                  defaultValue="canary"
                  options={[
                    { value: 'canary', label: '先进灰度 10%' },
                    { value: 'full', label: '直接全量' },
                    { value: 'none', label: '仅部署，暂不接入路由' },
                  ]}
                />
              </Field>
            </>
          )}

          {/*
            最后一步是预检清单而不是「启动」按钮。
            让阻断性问题在花掉 3-5 分钟加载权重之前暴露出来。
          */}
          {step === 4 && (
            <div className="flex flex-col gap-2">
              {checks.map((check) => (
                <div
                  key={check.id}
                  className="flex items-start gap-2.5 rounded border border-line bg-plane px-3 py-2.5"
                >
                  {check.kind === 'good' ? (
                    <CheckCircleFilled className="mt-0.5 text-status-good" />
                  ) : (
                    <WarningFilled className="mt-0.5 text-status-warning" />
                  )}
                  <span className="min-w-0">
                    <b className="text-[11px]">{check.label}</b>
                    <p className="mt-0.5 mb-0 text-[10px] text-ink-muted">{check.detail}</p>
                  </span>
                </div>
              ))}
              <p className="mt-2 mb-0 text-[10px] text-ink-muted">
                将部署 <code className="font-mono">{name}</code> 到 {node}，TP={tp} · {precision} ·
                max_model_len {maxLen / 1024}K · 端口 {port}
              </p>
            </div>
          )}
        </div>
      )}
    </Modal>
  )
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label className="flex flex-col gap-1.5">
      <span className="text-[11px] text-ink-secondary">{label}</span>
      {children}
    </label>
  )
}
