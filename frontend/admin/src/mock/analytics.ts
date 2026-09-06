import type { Dimension, Measure, PlatformAlert, UsageTrend, UiStatus as StatusKind } from '../api/contracts'
import { dateRange, hourRange, mockResponse, seedFrom, seededRandom, trendSeries } from './generators'

/**
 * 实体 → 色槽注册表。
 *
 * §4「色彩跟随实体，绝不跟随排名」的落地点：OpenAI 在任何图、任何分组、
 * 任何筛选下都是槽 1。切换分组维度时幸存系列不会重新着色，读者学到的
 * "OpenAI 是蓝色"才不会被推翻。
 */
export const ENTITY_SLOT: Record<string, number> = {
  openai: 1,
  google: 2,
  anthropic: 3,
  deepseek: 4,
  alibaba: 5,
  zhipu: 6,
  selfhosted: 7,
  others: 8,
  // 模型维度
  'gpt-5.6-sol': 1,
  'gemini-3.8-flash': 2,
  'claude-opus-5': 3,
  'deepseek-v4': 4,
  'qwen3-max': 5,
  // 用户组维度
  enterprise: 1,
  team: 2,
  selfserve: 3,
  free: 4,
  // 应用维度
  'flow-studio': 1,
  'code-pilot': 2,
  'research-desk': 3,
  'agent-forge': 4,
}


/** 各维度下的实体清单。最后一项恒为「其他」，长尾折叠在此 —— 绝不生成第 9 个颜色 */
const DIMENSION_ENTITIES: Record<Dimension, Array<{ id: string; label: string }>> = {
  vendor: [
    { id: 'openai', label: 'OpenAI' },
    { id: 'google', label: 'Google' },
    { id: 'anthropic', label: 'Anthropic' },
    { id: 'deepseek', label: 'DeepSeek' },
    { id: 'selfhosted', label: '自建模型' },
    { id: 'others', label: '其他' },
  ],
  model: [
    { id: 'gpt-5.6-sol', label: 'GPT-5.6 SOL' },
    { id: 'gemini-3.8-flash', label: 'Gemini 3.8 Flash' },
    { id: 'claude-opus-5', label: 'Claude Opus 5' },
    { id: 'deepseek-v4', label: 'DeepSeek V4' },
    { id: 'others', label: '其他' },
  ],
  userGroup: [
    { id: 'enterprise', label: '企业版' },
    { id: 'team', label: '团队版' },
    { id: 'selfserve', label: '自助版' },
    { id: 'free', label: '免费版' },
  ],
  app: [
    { id: 'flow-studio', label: 'Flow Studio' },
    { id: 'code-pilot', label: 'Code Pilot' },
    { id: 'research-desk', label: 'Research Desk' },
    { id: 'others', label: '其他' },
  ],
}

/** 度量之间的量级差异：tokens 远大于请求数，成本又小得多 */
const MEASURE_BASE: Record<Measure, number> = {
  tokens: 9_400_000,
  requests: 32_000,
  cost: 640,
  toolCalls: 128_000,
}


/**
 * 用量时序。度量与分组维度都是入参 —— 页面上切换它们复用同一张图，
 * 而不是并排画两张不同量纲的图（那正是双轴陷阱的温床）。
 */
export function usageSeries(dimension: Dimension, measure: Measure, days: number): UsageTrend {
  const dates = dateRange(days)
  const entities = DIMENSION_ENTITIES[dimension]
  const base = MEASURE_BASE[measure]

  const columns = entities.map((entity, index) => ({
    ...entity,
    slot: ENTITY_SLOT[entity.id] ?? index + 1,
    values: trendSeries({
      seed: `${dimension}-${measure}-${entity.id}`,
      length: days,
      base: base * (0.42 - index * 0.06),
      growth: 0.012 + index * 0.003,
    }),
  }))

  const data = dates.map((date, index) => {
    const row: Record<string, string | number> = { date }
    columns.forEach((column) => {
      row[column.id] = measure === 'cost'
        ? Number(column.values[index].toFixed(2))
        : Math.round(column.values[index])
    })
    return row
  })

  const sumAt = (index: number) =>
    columns.reduce((acc, column) => acc + column.values[index], 0)
  const total = data.reduce(
    (acc, row) => acc + columns.reduce((sum, column) => sum + Number(row[column.id]), 0),
    0,
  )
  const half = Math.floor(days / 2)
  const firstHalf = Array.from({ length: half }, (_, i) => sumAt(i)).reduce((a, b) => a + b, 0)
  const secondHalf = Array.from({ length: days - half }, (_, i) => sumAt(half + i)).reduce(
    (a, b) => a + b,
    0,
  )

  return {
    series: columns.map(({ id, label, slot }) => ({ id, label, slot })),
    data,
    total,
    deltaPercent: firstHalf > 0 ? ((secondHalf - firstHalf) / firstHalf) * 100 : 0,
  }
}

/** 输入/输出/缓存构成。四段，直接标注百分比 */
export function tokenComposition() {
  return [
    { id: 'prompt', label: '输入', slot: 1, value: 618_400_000 },
    { id: 'output', label: '输出', slot: 2, value: 224_100_000 },
    { id: 'cacheRead', label: '缓存读取', slot: 3, value: 184_600_000 },
    { id: 'cacheWrite', label: '缓存写入', slot: 6, value: 41_200_000 },
  ]
}

/** 时段热力：周 × 小时 */
export function hourlyHeat() {
  const weekdays = ['周一', '周二', '周三', '周四', '周五', '周六', '周日']
  const hours = hourRange(24).filter((_, index) => index % 2 === 0)
  return {
    columns: hours,
    rows: weekdays.map((day, dayIndex) => {
      const random = seededRandom(seedFrom(`heat-${day}`))
      return {
        key: day,
        label: day,
        cells: hours.map((_, hourIndex) => {
          const workHour = hourIndex >= 4 && hourIndex <= 9
          const weekend = dayIndex >= 5
          const ratio = Math.min(
            1,
            Math.max(0.02, (workHour ? 0.72 : 0.24) * (weekend ? 0.45 : 1) + random() * 0.22),
          )
          return { ratio, display: `${(ratio * 4.2).toFixed(2)}M tokens` }
        }),
      }
    }),
  }
}

/** 明细表（可下钻） */
export function usageBreakdown(dimension: Dimension) {
  const entities = DIMENSION_ENTITIES[dimension]
  return entities.map((entity, index) => {
    const random = seededRandom(seedFrom(`bd-${dimension}-${entity.id}`))
    const tokens = 842_100_000 * (0.44 - index * 0.07)
    return {
      key: entity.id,
      label: entity.label,
      slot: ENTITY_SLOT[entity.id] ?? index + 1,
      tokens,
      requests: tokens / 434,
      cost: tokens / 700_000,
      latencyP95: 380 + random() * 260,
      errorRate: 0.12 + random() * 0.4,
      share: 0,
    }
  }).map((row, _, all) => {
    const sum = all.reduce((acc, item) => acc + item.tokens, 0)
    return { ...row, share: (row.tokens / sum) * 100 }
  })
}

/* ─────────────── 总览 ─────────────── */


export function overviewAlerts(): PlatformAlert[] {
  return [
    {
      id: 'gpu-ecc',
      kind: 'critical',
      title: 'node-04 GPU2 ECC 双位错误',
      detail: '已自动驱逐其上实例并隔离该卡，等待人工处理',
      href: '/infra/observability',
    },
    {
      id: 'channel-degraded',
      kind: 'warning',
      title: 'Anthropic 渠道错误率 12.4%',
      detail: '5 分钟窗口超过 5% 阈值，已自动降权并切至备用渠道',
      href: '/catalog/channels',
    },
    {
      id: 'balance',
      kind: 'warning',
      title: '用户 acme-ai 余额为负仍在调用',
      detail: '当前余额 -$3.20，建议核对自动充值配置',
      href: '/customers/billing',
    },
  ]
}

export function overviewRequests24h() {
  const hours = hourRange(24)
  const values = trendSeries({ seed: 'req24h', length: 24, base: 168_000, growth: 0.004, weekly: 0.3 })
  return hours.map((hour, index) => ({ hour, requests: Math.round(values[index]) }))
}

export function channelHealth() {
  return [
    { id: 'openai', name: 'OpenAI', kind: 'good' as StatusKind, label: '正常', p95: 412, errorRate: 0.31 },
    { id: 'google', name: 'Google', kind: 'good' as StatusKind, label: '正常', p95: 388, errorRate: 0.19 },
    { id: 'anthropic', name: 'Anthropic', kind: 'serious' as StatusKind, label: '降级', p95: 1240, errorRate: 12.4 },
    { id: 'selfhosted', name: '自建 vLLM', kind: 'good' as StatusKind, label: '正常', p95: 96, errorRate: 0.02 },
  ]
}

export function clusterWater() {
  return [
    { label: '显存', value: 73, max: 100, text: '73%' },
    { label: '算力', value: 61, max: 100, text: '61%' },
    { label: '磁盘', value: 34, max: 100, text: '34%' },
  ]
}

/* ─────────────── 用户分析 ─────────────── */

export function userGrowth(days = 14) {
  const dates = dateRange(days)
  const joined = trendSeries({ seed: 'joined', length: days, base: 62, growth: 0.02 })
  const churned = trendSeries({ seed: 'churned', length: days, base: 18, growth: 0.008 })
  return dates.map((date, index) => ({
    date,
    joined: Math.round(joined[index]),
    churned: -Math.round(churned[index]),
  }))
}

export function retentionCohorts() {
  const cohorts = ['07-01', '07-08', '07-15', '07-22', '07-29', '08-05']
  const weeks = ['W0', 'W1', 'W2', 'W3', 'W4']
  return {
    columns: weeks,
    rows: cohorts.map((cohort, cohortIndex) => {
      const random = seededRandom(seedFrom(`cohort-${cohort}`))
      return {
        key: cohort,
        label: cohort,
        cells: weeks.map((_, weekIndex) => {
          if (cohortIndex + weekIndex > cohorts.length - 1) {
            return { ratio: null, display: '尚未到达' }
          }
          const value = weekIndex === 0 ? 1 : Math.max(0.28, 0.66 - weekIndex * 0.07 - random() * 0.06)
          return { ratio: value, display: `${(value * 100).toFixed(1)}%` }
        }),
      }
    }),
  }
}

export function userTiers() {
  return [
    { id: 'free', label: '免费版', slot: 4, count: 6218 },
    { id: 'selfserve', label: '自助版', slot: 3, count: 1584 },
    { id: 'team', label: '团队版', slot: 2, count: 482 },
    { id: 'enterprise', label: '企业版', slot: 1, count: 128 },
  ]
}

export function conversionFunnel() {
  return [
    { stage: '注册', value: 8412 },
    { stage: '创建 Key', value: 5108 },
    { stage: '首次调用', value: 3942 },
    { stage: '持续使用', value: 1204 },
    { stage: '付费', value: 318 },
  ]
}

/* ─────────────── 模型分析 ─────────────── */

export function modelLeaderboard() {
  const rows = [
    { id: 'gpt-5.6-sol', name: 'GPT-5.6 SOL', vendor: 'OpenAI', tokens: 612_400_000, cost: 902, latency: 398, errorRate: 0.28 },
    { id: 'gemini-3.8-flash', name: 'Gemini 3.8 Flash', vendor: 'Google', tokens: 501_200_000, cost: 418, latency: 388, errorRate: 0.19 },
    { id: 'claude-opus-5', name: 'Claude Opus 5', vendor: 'Anthropic', tokens: 384_600_000, cost: 1204, latency: 642, errorRate: 0.34 },
    { id: 'deepseek-v4', name: 'DeepSeek V4', vendor: 'DeepSeek', tokens: 246_100_000, cost: 118, latency: 512, errorRate: 0.22 },
    { id: 'qwen3-max', name: 'Qwen3 Max（自建）', vendor: '自建', tokens: 128_400_000, cost: 64, latency: 96, errorRate: 0.02 },
  ]
  const max = Math.max(...rows.map((row) => row.tokens))
  return rows.map((row) => ({
    ...row,
    slot: ENTITY_SLOT[row.id] ?? 8,
    share: (row.tokens / max) * 100,
    costPerMillion: (row.cost / (row.tokens / 1_000_000)) * 1000,
  }))
}

/* ─────────────── 统一的 mock 入口 ─────────────── */

export const analyticsMock = {
  overview: () =>
    mockResponse(
      {
        alerts: overviewAlerts(),
        requests24h: overviewRequests24h(),
        channels: channelHealth(),
        cluster: clusterWater(),
      },
      'overview',
    ),
  usage: (dimension: Dimension, measure: Measure, days: number) =>
    mockResponse(
      {
        trend: usageSeries(dimension, measure, days),
        composition: tokenComposition(),
        heat: hourlyHeat(),
        breakdown: usageBreakdown(dimension),
      },
      `usage-${dimension}-${measure}-${days}`,
    ),
  users: () =>
    mockResponse(
      {
        growth: userGrowth(),
        retention: retentionCohorts(),
        tiers: userTiers(),
        funnel: conversionFunnel(),
      },
      'users',
    ),
  models: () => mockResponse({ leaderboard: modelLeaderboard() }, 'models'),
}
