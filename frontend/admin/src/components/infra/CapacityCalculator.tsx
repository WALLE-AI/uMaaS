import { CheckCircleFilled, CloseCircleFilled, InfoCircleOutlined } from '@ant-design/icons'
import { Alert, Collapse, InputNumber, Select, Slider, Tooltip } from 'antd'
import { Meter } from '../charts/Meter'
import { seriesColor } from '../../config/viz'
import {
  ACTIVATION_RATIO_DISPLAY,
  KV_BYTES,
  OVERHEAD_DISPLAY,
  PRECISION_BYTES,
  type CapacityBreakdown,
  type CapacityInput,
  type Precision,
} from '../../lib/capacity'

/**
 * 容量评估器的输入面板与结果卡片。
 *
 * §6.2 的硬要求：不做黑盒。「计算依据」折叠面板里给出完整公式与本次代入的
 * 每一个数字，让管理者能自己复核 —— 一个只给结论不给推导的估算器，在
 * 采购决策场景里是没人敢用的。
 */

const BREAKDOWN_SLOTS = { weights: 1, kv: 2, activation: 3, overhead: 6 }

export function CapacityForm({
  value,
  onChange,
  gpuPresets,
  modelPresets,
}: {
  value: CapacityInput
  onChange: (next: CapacityInput) => void
  gpuPresets: Array<{ id: string; label: string; memoryGb: number }>
  modelPresets: Array<CapacityInput['shape']>
}) {
  const patch = (partial: Partial<CapacityInput>) => onChange({ ...value, ...partial })

  return (
    <div className="flex flex-col gap-4">
      <Field label="模型">
        <Select
          size="small"
          value={value.shape.name}
          className="w-full"
          onChange={(name) => {
            const shape = modelPresets.find((item) => item.name === name)
            if (shape) patch({ shape })
          }}
          options={modelPresets.map((item) => ({ value: item.name, label: item.name }))}
        />
      </Field>

      {/* 结构参数由模型仓库解析 config.json 回填，此处允许手工覆盖 */}
      <div className="grid grid-cols-2 gap-3">
        <Field label="参数量 (B)">
          <InputNumber
            size="small"
            className="w-full"
            value={value.shape.paramsB}
            min={0.1}
            step={0.1}
            onChange={(paramsB) => paramsB && patch({ shape: { ...value.shape, paramsB } })}
          />
        </Field>
        <Field label="层数">
          <InputNumber
            size="small"
            className="w-full"
            value={value.shape.layers}
            min={1}
            onChange={(layers) => layers && patch({ shape: { ...value.shape, layers } })}
          />
        </Field>
        <Field
          label="KV 头数"
          hint="GQA/MQA 模型的 KV 头数远小于注意力头数。用错这个值，KV Cache 会估高好几倍。"
        >
          <InputNumber
            size="small"
            className="w-full"
            value={value.shape.kvHeads}
            min={1}
            onChange={(kvHeads) => kvHeads && patch({ shape: { ...value.shape, kvHeads } })}
          />
        </Field>
        <Field label="头维度">
          <InputNumber
            size="small"
            className="w-full"
            value={value.shape.headDim}
            min={1}
            onChange={(headDim) => headDim && patch({ shape: { ...value.shape, headDim } })}
          />
        </Field>
      </div>

      <Field label="精度">
        <Select
          size="small"
          className="w-full"
          value={value.precision}
          onChange={(precision: Precision) => patch({ precision })}
          options={(Object.keys(PRECISION_BYTES) as Precision[]).map((item) => ({
            value: item,
            label: `${item} · ${PRECISION_BYTES[item]} 字节/参数`,
          }))}
        />
      </Field>

      <div className="grid grid-cols-2 gap-3">
        <Field label="最大上下文">
          <Select
            size="small"
            className="w-full"
            value={value.contextLength}
            onChange={(contextLength) => patch({ contextLength })}
            options={[4096, 8192, 16384, 32768, 65536, 131072].map((item) => ({
              value: item,
              label: item >= 1024 ? `${item / 1024}K` : String(item),
            }))}
          />
        </Field>
        <Field label="目标并发">
          <InputNumber
            size="small"
            className="w-full"
            value={value.concurrency}
            min={1}
            max={512}
            onChange={(concurrency) => concurrency && patch({ concurrency })}
          />
        </Field>
      </div>

      <Field
        label={`平均上下文占用率  ${(value.contextUtilization * 100).toFixed(0)}%`}
        hint="按并发请求全部占满上下文算是理论最坏情况。vLLM 的 PagedAttention 按实际 token 分页分配，真实负载通常在 20–40%。这个值直接决定卡数，部署后应按实测平均序列长度回调。"
      >
        <Slider
          min={0.05}
          max={1}
          step={0.05}
          value={value.contextUtilization}
          onChange={(contextUtilization) => patch({ contextUtilization })}
          tooltip={{ formatter: (v) => `${((v ?? 0) * 100).toFixed(0)}%` }}
        />
      </Field>

      <Field label="GPU 型号">
        <Select
          size="small"
          className="w-full"
          value={value.cardMemoryGb}
          onChange={(cardMemoryGb) => patch({ cardMemoryGb })}
          options={gpuPresets.map((item) => ({ value: item.memoryGb, label: item.label }))}
        />
      </Field>

      <Field label={`显存预留余量  ${(value.reserveRatio * 100).toFixed(0)}%`}>
        <Slider
          min={0}
          max={0.3}
          step={0.05}
          value={value.reserveRatio}
          onChange={(reserveRatio) => patch({ reserveRatio })}
          tooltip={{ formatter: (v) => `${((v ?? 0) * 100).toFixed(0)}%` }}
        />
      </Field>
    </div>
  )
}

function Field({
  label,
  hint,
  children,
}: {
  label: string
  hint?: string
  children: React.ReactNode
}) {
  return (
    <label className="flex flex-col gap-1.5">
      <span className="flex items-center gap-1.5 text-[11px] text-ink-secondary">
        {label}
        {hint && (
          <Tooltip title={hint}>
            <InfoCircleOutlined className="cursor-help text-[10px] text-ink-muted" />
          </Tooltip>
        )}
      </span>
      {children}
    </label>
  )
}

export function CapacityResult({
  input,
  result,
}: {
  input: CapacityInput
  result: CapacityBreakdown
}) {
  const rows = [
    { key: 'weights', label: '权重', value: result.weightsGb, slot: BREAKDOWN_SLOTS.weights },
    { key: 'kv', label: 'KV Cache', value: result.kvCacheGb, slot: BREAKDOWN_SLOTS.kv },
    { key: 'activation', label: '激活/临时', value: result.activationGb, slot: BREAKDOWN_SLOTS.activation },
    { key: 'overhead', label: '框架开销', value: result.overheadGb, slot: BREAKDOWN_SLOTS.overhead },
  ]
  const max = Math.max(...rows.map((row) => row.value))
  const cards = result.tensorParallel

  return (
    <div>
      {cards === null ? (
        <Alert
          type="error"
          showIcon
          icon={<CloseCircleFilled />}
          message="当前配置无法部署"
          description={<span className="text-[11px]">{result.tpConstraintNote}</span>}
        />
      ) : (
        <div className="flex flex-wrap items-baseline gap-x-3">
          <b className="font-mono text-[40px] leading-none font-semibold">
            {cards} × {input.cardMemoryGb}G
          </b>
          <span className="text-sm text-ink-secondary">张量并行 TP={cards}</span>
        </div>
      )}

      <div className="mt-6">
        <span className="font-mono text-[8px] tracking-widest text-ink-muted">
          单卡显存拆解
        </span>
        <div className="mt-2 flex flex-col gap-2">
          {rows.map((row) => {
            const perCard = row.key === 'overhead' ? row.value : row.value / (cards ?? 1)
            return (
              <div key={row.key} className="flex items-center gap-3 text-[11px]">
                <span className="w-20 shrink-0 text-ink-secondary">{row.label}</span>
                <i className="h-3 flex-1 overflow-hidden rounded bg-line-soft">
                  <em
                    className="block h-full rounded"
                    style={{
                      width: `${(row.value / max) * 100}%`,
                      background: seriesColor(row.slot),
                    }}
                  />
                </i>
                <span className="w-20 shrink-0 text-right font-mono">
                  {perCard.toFixed(1)} GB
                </span>
              </div>
            )
          })}
        </div>
      </div>

      <div className="mt-5">
        <Meter
          label="单卡占用"
          value={result.perCardGb}
          max={input.cardMemoryGb}
          valueText={`${result.perCardGb.toFixed(1)} / ${input.cardMemoryGb} GB`}
          warnAt={0.92}
        />
      </div>

      {result.feasible && (
        <p className="mt-4 mb-0 flex items-center gap-2 text-[11px] text-status-good">
          <CheckCircleFilled />
          可部署，单卡余量 {(input.cardMemoryGb - result.perCardGb).toFixed(1)} GB
        </p>
      )}

      {/* 估算不确定性必须写明。给数字不给误差范围是不负责任的 */}
      <Alert
        className="mt-4"
        type="info"
        showIcon
        message={
          <span className="text-[11px]">
            这是<b>估算值</b>，实际占用受推理框架、PagedAttention 分页策略、CUDA Graph
            与算子实现影响，典型偏差 ±10–15%。部署后请以压测实测校准本页基准。
          </span>
        }
      />

      <Collapse
        className="mt-4"
        size="small"
        ghost
        items={[
          {
            key: 'formula',
            label: <span className="text-[11px]">计算依据（公式与本次代入值）</span>,
            children: <FormulaDetail input={input} result={result} />,
          },
        ]}
      />
    </div>
  )
}

function FormulaDetail({ input, result }: { input: CapacityInput; result: CapacityBreakdown }) {
  const { shape, precision, contextLength, concurrency, contextUtilization } = input
  const effectiveContext = Math.round(contextLength * contextUtilization)

  return (
    <div className="log-stream rounded border border-line bg-plane p-3 text-ink-secondary">
      <pre className="m-0 whitespace-pre-wrap">
{`总显存 = 权重 + KV Cache + 激活 + 框架开销

权重 = 参数量 × 每参数字节数
     = ${shape.paramsB}B × ${PRECISION_BYTES[precision]} (${precision})
     = ${result.weightsGb.toFixed(1)} GB

KV Cache = 2 × 层数 × KV头数 × 头维度 × 有效上下文 × 并发 × KV精度字节数
         = 2 × ${shape.layers} × ${shape.kvHeads} × ${shape.headDim} × ${effectiveContext} × ${concurrency} × ${KV_BYTES[precision]}
         = ${result.kvCacheGb.toFixed(1)} GB
  注：有效上下文 = ${contextLength} × ${(contextUtilization * 100).toFixed(0)}% = ${effectiveContext}
      KV 头数用的是 ${shape.kvHeads}（GQA），不是注意力头数 ${shape.attnHeads}

激活 = 权重 × ${ACTIVATION_RATIO_DISPLAY} = ${result.activationGb.toFixed(1)} GB
框架开销 = ${OVERHEAD_DISPLAY} GB/卡（CUDA context + NCCL 缓冲）

模型合计 = ${result.modelTotalGb.toFixed(1)} GB

单卡可用 = ${input.cardMemoryGb} × (1 - ${(input.reserveRatio * 100).toFixed(0)}%) - ${OVERHEAD_DISPLAY}
         = ${(input.cardMemoryGb * (1 - input.reserveRatio) - result.overheadGb).toFixed(1)} GB
最少卡数 = ceil(${result.modelTotalGb.toFixed(1)} / ${(input.cardMemoryGb * (1 - input.reserveRatio) - result.overheadGb).toFixed(1)}) = ${result.minCards}

TP 约束：需整除 KV头数 ${shape.kvHeads} 与注意力头数 ${shape.attnHeads}
        可选 TP ∈ {${[1, 2, 4, 8].filter((tp) => shape.kvHeads % tp === 0 && shape.attnHeads % tp === 0).join(', ')}}
        取 ≥ ${result.minCards} 的最小值 → TP = ${result.tensorParallel ?? '无可用值'}`}
      </pre>
    </div>
  )
}
