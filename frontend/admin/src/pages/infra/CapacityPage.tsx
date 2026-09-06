import { ArrowRightOutlined } from '@ant-design/icons'
import { Alert, Button } from 'antd'
import { useMemo, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router'
import { Heatmap } from '../../components/charts/Heatmap'
import { PageHeader } from '../../components/common/PageHeader'
import { Panel } from '../../components/common/Panel'
import { CapacityForm, CapacityResult } from '../../components/infra/CapacityCalculator'
import { PrecisionCompare } from '../../components/infra/PrecisionCompare'
import {
  GPU_PRESETS,
  MODEL_PRESETS,
  comparePrecisions,
  estimateCapacity,
  feasibilityGrid,
  type CapacityInput,
  type Precision,
} from '../../lib/capacity'

const CONCURRENCIES = [128, 64, 32, 16, 8]
const CONTEXTS = [4096, 8192, 16384, 32768, 65536, 131072]

export default function CapacityPage() {
  const navigate = useNavigate()
  const [params] = useSearchParams()

  /**
   * 模型仓库解析 config.json 后会带着结构参数跳到这里 —— 管理者不必手抄
   * 层数 / KV 头数 / 头维度这些最容易抄错的字段（§7 流程 A 的闭环）。
   */
  const initial = useMemo<CapacityInput>(() => {
    const fromQuery = params.get('model')
    const preset = MODEL_PRESETS.find((item) => item.name === fromQuery)
    const shape = preset ?? {
      name: fromQuery ?? MODEL_PRESETS[0].name,
      paramsB: Number(params.get('params')) || MODEL_PRESETS[0].paramsB,
      layers: Number(params.get('layers')) || MODEL_PRESETS[0].layers,
      kvHeads: Number(params.get('kvHeads')) || MODEL_PRESETS[0].kvHeads,
      attnHeads: Number(params.get('attnHeads')) || MODEL_PRESETS[0].attnHeads,
      headDim: Number(params.get('headDim')) || MODEL_PRESETS[0].headDim,
    }
    return {
      shape,
      precision: 'FP16',
      contextLength: 32768,
      concurrency: 64,
      cardMemoryGb: 80,
      reserveRatio: 0.1,
      contextUtilization: 0.3,
    }
  }, [params])

  const [input, setInput] = useState<CapacityInput>(initial)

  const result = useMemo(() => estimateCapacity(input), [input])
  const precisions = useMemo(() => comparePrecisions(input), [input])
  const grid = useMemo(
    () => feasibilityGrid(input, CONCURRENCIES, CONTEXTS),
    [input],
  )

  const fromRegistry = params.get('model') && !MODEL_PRESETS.some((m) => m.name === params.get('model'))

  return (
    <div>
      <PageHeader description="给定模型算需要多少卡，或调整并发与上下文看可行域。所有公式可展开复核。" />

      {fromRegistry && (
        <Alert
          className="mb-4"
          type="success"
          showIcon
          message={`已从模型仓库回填 ${input.shape.name} 的结构参数（层数 ${input.shape.layers} · KV 头数 ${input.shape.kvHeads} · 头维度 ${input.shape.headDim}）`}
        />
      )}

      <div className="mb-4 grid gap-4 lg:grid-cols-[minmax(280px,0.42fr)_minmax(0,0.58fr)]">
        <Panel eyebrow="INPUT" title="配置">
          <CapacityForm
            value={input}
            onChange={setInput}
            gpuPresets={GPU_PRESETS}
            modelPresets={MODEL_PRESETS}
          />
        </Panel>

        <Panel
          eyebrow="ESTIMATE"
          title="估算结果"
          actions={
            result.feasible && (
              <Button
                size="small"
                type="primary"
                onClick={() =>
                  navigate(
                    `/infra/deployments?model=${encodeURIComponent(input.shape.name)}&tp=${result.tensorParallel}&precision=${input.precision}`,
                  )
                }
              >
                用此配置去部署 <ArrowRightOutlined />
              </Button>
            )
          }
        >
          <CapacityResult input={input} result={result} />
        </Panel>
      </div>

      <Panel
        eyebrow="TRADE-OFF"
        title="精度方案对比"
        description="点击任意行切换到该精度。质量损失一列不可忽略 —— 只看显存会让 INT4 显得永远最优。"
        className="mb-4"
      >
        <PrecisionCompare
          rows={precisions}
          current={input.precision}
          onPick={(precision: Precision) => setInput({ ...input, precision })}
        />
      </Panel>

      <Panel
        eyebrow="FEASIBILITY"
        title="并发 × 上下文 可行域"
        description="填充为单卡显存占用比，深色接近上限；超出容量的格子叠加纹理"
      >
        <Heatmap
          rows={grid.map((row) => ({
            key: String(row.concurrency),
            label: `并发 ${row.concurrency}`,
            cells: row.cells.map((cell) => ({
              ratio: cell.feasible ? Math.min(1, cell.ratio) : null,
              display: cell.feasible
                ? `${(cell.ratio * 100).toFixed(0)}% · ${cell.cards} 卡`
                : '超出容量',
              fault: !cell.feasible,
            })),
          }))}
          columns={CONTEXTS.map((item) => (item >= 1024 ? `${item / 1024}K` : String(item)))}
          legend={{ low: '空闲', high: '接近上限' }}
          cellHeight={26}
        />
        <p className="mt-3 mb-0 text-[10px] text-ink-muted">
          横轴为最大上下文，纵轴为目标并发。当前配置按平均上下文占用率{' '}
          {(input.contextUtilization * 100).toFixed(0)}% 折算。
        </p>
      </Panel>
    </div>
  )
}
