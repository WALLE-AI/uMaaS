/**
 * §8 指标字典的前端映射。
 *
 * 每个指标在这里登记单位、精度和口径说明。UI 上凡是展示指标的地方都从这里
 * 取 label 与 unit —— 目的是让"口径"跟着指标走，而不是散落在各个页面的
 * 硬编码文案里。带 caveat 的指标必须在界面上提供说明入口（tooltip 或脚注），
 * 因为这些是最容易被误读的数字。
 */

export type MetricUnit =
  | 'count'
  | 'tokens'
  | 'usd'
  | 'ms'
  | 'percent'
  | 'bytes'
  | 'tps'
  | 'celsius'
  | 'watt'

export type MetricDef = {
  key: string
  label: string
  unit: MetricUnit
  /** 口径说明，界面上以 tooltip 呈现 */
  caveat?: string
}

export const metrics: Record<string, MetricDef> = {
  requests: {
    key: 'requests',
    label: '请求数',
    unit: 'count',
    caveat: '含失败请求。需要只看成功请求时使用「成功请求」指标。',
  },
  requestsOk: { key: 'requestsOk', label: '成功请求', unit: 'count' },
  tokens: {
    key: 'tokens',
    label: 'Tokens',
    unit: 'tokens',
    caveat: 'prompt + completion 合计。缓存读写单列，不计入此项，否则成本对不上。',
  },
  tokensPrompt: { key: 'tokensPrompt', label: '输入 Tokens', unit: 'tokens' },
  tokensOutput: { key: 'tokensOutput', label: '输出 Tokens', unit: 'tokens' },
  tokensCacheRead: { key: 'tokensCacheRead', label: '缓存读取 Tokens', unit: 'tokens' },
  tokensCacheWrite: { key: 'tokensCacheWrite', label: '缓存写入 Tokens', unit: 'tokens' },
  tokenQuotaRatio: {
    key: 'tokenQuotaRatio',
    label: 'Tokens 使用率',
    unit: 'percent',
    caveat: '已用 tokens ÷ 配额 tokens。分母为套餐配额，非预算折算值。',
  },
  cost: {
    key: 'cost',
    label: '成本',
    unit: 'usd',
    caveat: '供应商实际计费，与平台收入不同。自建模型按 GPU 折旧与电费摊销。',
  },
  activeUsers: {
    key: 'activeUsers',
    label: '活跃用户',
    unit: 'count',
    caveat: '当日有至少一次成功调用的唯一用户，按 UTC 日聚合，已排除测试账号。',
  },
  toolCalls: {
    key: 'toolCalls',
    label: '工具调用次数',
    unit: 'count',
    caveat: '响应中 tool_calls 数组的元素总数。一次请求可能含多个调用，不等于请求数。',
  },
  toolSuccessRate: {
    key: 'toolSuccessRate',
    label: '工具调用成功率',
    unit: 'percent',
    caveat: '需客户端回传 tool_result。未接入上报的客户端只能统计「已生成调用」。',
  },
  ttft: { key: 'ttft', label: '首 Token 延迟', unit: 'ms', caveat: 'TTFT，与端到端延迟不可混用。' },
  tpot: { key: 'tpot', label: '每输出 Token 时延', unit: 'ms' },
  latencyP95: {
    key: 'latencyP95',
    label: 'P95 延迟',
    unit: 'ms',
    caveat: '端到端，含排队时间。',
  },
  gpuMemRatio: {
    key: 'gpuMemRatio',
    label: 'GPU 显存水位',
    unit: 'percent',
    caveat: '已分配 ÷ 总显存。分配不等于实际使用，两个指标需分别观察。',
  },
  gpuUtil: { key: 'gpuUtil', label: 'GPU 利用率', unit: 'percent' },
  goodput: {
    key: 'goodput',
    label: '有效吞吐',
    unit: 'tps',
    caveat: '满足 SLO 前提下的 tokens/s。与无约束的「饱和吞吐」不同。',
  },
  throughput: { key: 'throughput', label: '饱和吞吐', unit: 'tps' },
}

const UNIT_SUFFIX: Record<MetricUnit, string> = {
  count: '',
  tokens: '',
  usd: '',
  ms: 'ms',
  percent: '%',
  bytes: '',
  tps: ' tok/s',
  celsius: '°C',
  watt: 'W',
}

/** 大数字缩写：1_280_000 -> 1.28M */
export function compactNumber(value: number, digits = 2): string {
  const abs = Math.abs(value)
  const units: Array<[number, string]> = [
    [1e12, 'T'],
    [1e9, 'B'],
    [1e6, 'M'],
    [1e3, 'K'],
  ]
  for (const [size, suffix] of units) {
    if (abs >= size) return `${(value / size).toFixed(digits)}${suffix}`
  }
  return value.toFixed(abs < 10 && !Number.isInteger(value) ? digits : 0)
}

export function formatBytes(bytes: number, digits = 1): string {
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']
  let value = Math.abs(bytes)
  let index = 0
  while (value >= 1024 && index < units.length - 1) {
    value /= 1024
    index += 1
  }
  return `${(bytes < 0 ? -value : value).toFixed(index === 0 ? 0 : digits)} ${units[index]}`
}

/** 按指标定义格式化数值，保证同一指标在全站显示一致 */
export function formatMetric(value: number, unit: MetricUnit): string {
  switch (unit) {
    case 'usd':
      return `$${value >= 1000 ? compactNumber(value) : value.toFixed(2)}`
    case 'percent':
      return `${value.toFixed(1)}%`
    case 'bytes':
      return formatBytes(value)
    case 'ms':
      return value >= 1000 ? `${(value / 1000).toFixed(2)}s` : `${Math.round(value)}ms`
    case 'celsius':
    case 'watt':
      // 温度与功耗都是不该被 K/M 缩写的小量级读数
      return `${Math.round(value)}${UNIT_SUFFIX[unit]}`
    default:
      return `${compactNumber(value)}${UNIT_SUFFIX[unit]}`
  }
}
