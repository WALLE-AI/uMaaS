/**
 * 确定性造数。
 *
 * 固定种子是刻意的：同一页面每次刷新数据一致，截图评审和视觉回归才有意义。
 * 用 Math.random() 会让"这个数字变了"变得没有信息量。
 */

/** mulberry32：小而稳的伪随机，同种子必得同序列 */
export function seededRandom(seed: number): () => number {
  let state = seed >>> 0
  return () => {
    state = (state + 0x6d2b79f5) >>> 0
    let t = state
    t = Math.imul(t ^ (t >>> 15), t | 1)
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61)
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
}

/** 字符串种子，让每个数据集有稳定但互不相同的序列 */
export function seedFrom(text: string): number {
  let hash = 2166136261
  for (let i = 0; i < text.length; i += 1) {
    hash ^= text.charCodeAt(i)
    hash = Math.imul(hash, 16777619)
  }
  return hash >>> 0
}

export function dateRange(days: number, end = new Date(Date.UTC(2026, 8, 6))): string[] {
  return Array.from({ length: days }, (_, index) => {
    const day = new Date(end)
    day.setUTCDate(end.getUTCDate() - (days - 1 - index))
    return `${String(day.getUTCMonth() + 1).padStart(2, '0')}/${String(day.getUTCDate()).padStart(2, '0')}`
  })
}

export function hourRange(hours: number): string[] {
  return Array.from({ length: hours }, (_, index) => `${String(index % 24).padStart(2, '0')}:00`)
}

/**
 * 带趋势与周期的时间序列：基线 + 增长 + 周内波动 + 噪声。
 * 比纯随机更像真实流量，能暴露图表在波峰波谷下的显示问题。
 */
export function trendSeries(options: {
  seed: string
  length: number
  base: number
  growth?: number
  weekly?: number
  noise?: number
}): number[] {
  const { seed, length, base, growth = 0.02, weekly = 0.12, noise = 0.06 } = options
  const random = seededRandom(seedFrom(seed))
  return Array.from({ length }, (_, index) => {
    const trend = base * (1 + growth * index)
    const cycle = 1 + weekly * Math.sin((index / 7) * Math.PI * 2)
    const jitter = 1 + (random() - 0.5) * 2 * noise
    return Math.max(0, trend * cycle * jitter)
  })
}

/** 模拟网络：延迟 200–600ms，5% 概率失败 —— 强制每个页面写加载态与错误态 */
export async function mockResponse<T>(data: T, key = 'default'): Promise<T> {
  const random = seededRandom(seedFrom(key) + Date.now())
  const delay = 200 + random() * 400
  await new Promise((resolve) => setTimeout(resolve, delay))
  if (random() < 0.05) {
    throw new Error('mock: 上游暂时不可用（这是 mock 层刻意注入的 5% 失败）')
  }
  return data
}
