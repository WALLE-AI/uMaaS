/**
 * 显存估算与并行度推导。
 *
 * 纯函数，不依赖 React —— 这部分是整个容量评估页的真正内容，必须能独立
 * 推敲和校验。UI 只负责把结果画出来，并把公式和代入值一起展示（§6.2 要求
 * 不做黑盒）。
 *
 * 估算模型：
 *   总显存 ≈ 权重 + KV Cache + 激活 + 框架开销
 *
 *   权重     = 参数量 × 每参数字节数           FP16=2 · FP8=1 · INT4≈0.5
 *   KV Cache = 2 × 层数 × KV头数 × 头维度 × 上下文 × 并发 × KV精度字节数
 *              （2 是 K 与 V 各一份；GQA/MQA 必须用 KV 头数而非注意力头数，
 *                这是最容易估错、且会错出好几倍的一项）
 *   激活     ≈ 权重 × 7%
 *   框架开销 ≈ 1.5 GB/卡（CUDA context + NCCL 通信缓冲）
 */

export type Precision = 'FP16' | 'FP8' | 'INT4'

export const PRECISION_BYTES: Record<Precision, number> = {
  FP16: 2,
  FP8: 1,
  INT4: 0.5,
}

/** KV Cache 的精度通常独立于权重精度，INT4 权重一般仍配 FP8 的 KV */
export const KV_BYTES: Record<Precision, number> = {
  FP16: 2,
  FP8: 1,
  INT4: 1,
}

export const PRECISION_QUALITY_LOSS: Record<Precision, string> = {
  FP16: '基准',
  FP8: '约 0.5%',
  INT4: '约 2–4%',
}

const GIB = 1024 ** 3
const ACTIVATION_RATIO = 0.07
const OVERHEAD_GB_PER_CARD = 1.5

/** 供 UI 在「计算依据」里展示，避免公式说明与实际常量脱节 */
export const ACTIVATION_RATIO_DISPLAY = `${(ACTIVATION_RATIO * 100).toFixed(0)}%`
export const OVERHEAD_DISPLAY = OVERHEAD_GB_PER_CARD

export type ModelShape = {
  name: string
  /** 参数量，单位 B（十亿） */
  paramsB: number
  layers: number
  /** KV 头数。GQA 模型远小于注意力头数 */
  kvHeads: number
  /** 注意力头数，用于校验 TP 约束 */
  attnHeads: number
  headDim: number
}

export type CapacityInput = {
  shape: ModelShape
  precision: Precision
  /** 最大上下文长度 */
  contextLength: number
  /** 目标并发请求数 */
  concurrency: number
  /** 单卡显存 GB */
  cardMemoryGb: number
  /** 预留余量比例，0.1 表示留 10% */
  reserveRatio: number
  /**
   * 平均上下文占用率（0–1）。
   *
   * 这个参数不是可有可无的调节旋钮，而是估算是否可用的关键。按"所有并发
   * 请求都占满 max_model_len"计算是理论最坏情况，而 vLLM 的 PagedAttention
   * 是按实际 token 分页分配的 —— 真实负载里绝大多数请求远用不满上下文。
   *
   * 以 Qwen3-72B / 32K 上下文 / 64 并发为例：占用率 100% 时 KV 需要 640GB，
   * 30% 时只要 192GB，差 3 倍多，直接决定了买 8 张卡还是 24 张卡。
   * 默认 1.0（保守），部署过后应按实测的平均序列长度回调。
   */
  contextUtilization: number
}

export type CapacityBreakdown = {
  weightsGb: number
  kvCacheGb: number
  activationGb: number
  overheadGb: number
  /** 不含框架开销的模型总占用 */
  modelTotalGb: number
  /** 最少需要的卡数（先按容量算） */
  minCards: number
  /** 实际可用的张量并行度，已满足整除约束 */
  tensorParallel: number | null
  /** TP 不可行时的原因 */
  tpConstraintNote?: string
  /** 采用 tensorParallel 后，单卡实际占用 */
  perCardGb: number
  perCardCapacityGb: number
  perCardRatio: number
  feasible: boolean
}

/** 权重显存（GB） */
export function weightsGb(paramsB: number, precision: Precision): number {
  return (paramsB * 1e9 * PRECISION_BYTES[precision]) / GIB
}

/**
 * KV Cache 显存（GB）。
 * 注意这里用 kvHeads —— 对 GQA 模型，用 attnHeads 会高估好几倍。
 */
export function kvCacheGb(
  shape: ModelShape,
  contextLength: number,
  concurrency: number,
  precision: Precision,
): number {
  const bytes =
    2 *
    shape.layers *
    shape.kvHeads *
    shape.headDim *
    contextLength *
    concurrency *
    KV_BYTES[precision]
  return bytes / GIB
}

/** 合法的张量并行度：2 的幂，且必须整除 KV 头数与注意力头数 */
export function validTensorParallel(shape: ModelShape): number[] {
  return [1, 2, 4, 8].filter(
    (tp) => shape.kvHeads % tp === 0 && shape.attnHeads % tp === 0,
  )
}

export function estimateCapacity(input: CapacityInput): CapacityBreakdown {
  const {
    shape,
    precision,
    contextLength,
    concurrency,
    cardMemoryGb,
    reserveRatio,
    contextUtilization,
  } = input

  const weights = weightsGb(shape.paramsB, precision)
  const kv = kvCacheGb(
    shape,
    contextLength * Math.min(1, Math.max(0.05, contextUtilization)),
    concurrency,
    precision,
  )
  const activation = weights * ACTIVATION_RATIO
  const modelTotal = weights + kv + activation

  // 每张卡扣掉预留余量和框架开销后，真正能装模型的容量
  const perCardCapacity = cardMemoryGb * (1 - reserveRatio) - OVERHEAD_GB_PER_CARD
  const minCards = perCardCapacity > 0 ? Math.ceil(modelTotal / perCardCapacity) : Infinity

  const candidates = validTensorParallel(shape)
  const tensorParallel = candidates.find((tp) => tp >= minCards) ?? null

  let tpConstraintNote: string | undefined
  if (tensorParallel === null) {
    tpConstraintNote =
      minCards > 8
        ? `按容量需要 ${minCards} 张卡，超过单节点 8 卡上限，需要跨节点流水线并行或改用更低精度`
        : `容量上需要 ${minCards} 张卡，但 KV 头数 ${shape.kvHeads} / 注意力头数 ${shape.attnHeads} ` +
          `只能被 ${candidates.join('、')} 整除，没有可用的张量并行度`
  }

  const cards = tensorParallel ?? minCards
  const perCard = Number.isFinite(cards) ? modelTotal / cards + OVERHEAD_GB_PER_CARD : Infinity

  return {
    weightsGb: weights,
    kvCacheGb: kv,
    activationGb: activation,
    overheadGb: OVERHEAD_GB_PER_CARD,
    modelTotalGb: modelTotal,
    minCards,
    tensorParallel,
    tpConstraintNote,
    perCardGb: perCard,
    perCardCapacityGb: cardMemoryGb,
    perCardRatio: perCard / cardMemoryGb,
    feasible: tensorParallel !== null && perCard <= cardMemoryGb,
  }
}

/** 精度方案对比：同一模型在三种精度下的卡数与吞吐取舍 */
export function comparePrecisions(input: Omit<CapacityInput, 'precision'>) {
  return (Object.keys(PRECISION_BYTES) as Precision[]).map((precision) => {
    const result = estimateCapacity({ ...input, precision })
    // 吞吐随精度降低而上升，但 INT4 的反量化开销会抵消一部分
    const throughputFactor = precision === 'FP16' ? 1 : precision === 'FP8' ? 1.62 : 1.33
    return {
      precision,
      perCardGb: result.perCardGb,
      cards: result.tensorParallel,
      feasible: result.feasible,
      qualityLoss: PRECISION_QUALITY_LOSS[precision],
      throughputFactor,
    }
  })
}

/**
 * 并发 × 上下文 的可行域。
 * 返回每格的显存占用比，用于热力图 —— 让管理者一眼看到"再加并发会不会炸"。
 */
export function feasibilityGrid(
  input: Omit<CapacityInput, 'concurrency' | 'contextLength'>,
  concurrencies: number[],
  contexts: number[],
) {
  return concurrencies.map((concurrency) => ({
    concurrency,
    cells: contexts.map((contextLength) => {
      const result = estimateCapacity({ ...input, concurrency, contextLength })
      const cards = result.tensorParallel ?? result.minCards
      const ratio = Number.isFinite(cards) ? result.perCardGb / input.cardMemoryGb : 2
      return { contextLength, ratio, cards, feasible: result.feasible }
    }),
  }))
}

/** 常见 GPU 型号的单卡显存 */
export const GPU_PRESETS = [
  { id: 'H100-80G', label: 'H100 80GB', memoryGb: 80 },
  { id: 'H200-141G', label: 'H200 141GB', memoryGb: 141 },
  { id: 'A100-80G', label: 'A100 80GB', memoryGb: 80 },
  { id: 'A100-40G', label: 'A100 40GB', memoryGb: 40 },
  { id: 'L40S-48G', label: 'L40S 48GB', memoryGb: 48 },
  { id: 'RTX4090-24G', label: 'RTX 4090 24GB', memoryGb: 24 },
]

/** 常见模型的结构参数。真实场景由模型仓库解析 config.json 后回填 */
export const MODEL_PRESETS: ModelShape[] = [
  { name: 'Qwen3-72B-Instruct', paramsB: 72.7, layers: 80, kvHeads: 8, attnHeads: 64, headDim: 128 },
  { name: 'Qwen3-32B-Instruct', paramsB: 32.5, layers: 64, kvHeads: 8, attnHeads: 40, headDim: 128 },
  { name: 'Llama-4-70B', paramsB: 70.6, layers: 80, kvHeads: 8, attnHeads: 64, headDim: 128 },
  { name: 'DeepSeek-V4-Lite', paramsB: 16.4, layers: 27, kvHeads: 4, attnHeads: 32, headDim: 128 },
  { name: 'GLM-5-9B', paramsB: 9.4, layers: 40, kvHeads: 4, attnHeads: 32, headDim: 128 },
]
