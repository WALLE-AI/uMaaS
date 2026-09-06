import type { BenchmarkPoint, BenchmarkRun } from '../api/contracts'
import { DEFAULT_SLO } from '../lib/benchmark'
import { mockResponse } from './generators'

/**
 * 压测结果。
 *
 * 关键概念是 **SLO 拐点**：吞吐随并发上升会一直涨到饱和，但延迟在某个并发之后
 * 会急剧恶化。容量规划真正要的不是"最大吞吐"，而是"仍然满足 SLO 的最大并发"
 * —— 前者是把用户体验卖掉换来的数字。
 */



const POINTS: BenchmarkPoint[] = [
  { concurrency: 1, throughput: 98, ttftP50: 62, ttftP95: 88, ttftP99: 104, tpotP95: 10, e2eP95: 420, errorRate: 0 },
  { concurrency: 2, throughput: 188, ttftP50: 68, ttftP95: 96, ttftP99: 118, tpotP95: 12, e2eP95: 460, errorRate: 0 },
  { concurrency: 4, throughput: 356, ttftP50: 79, ttftP95: 118, ttftP99: 142, tpotP95: 15, e2eP95: 540, errorRate: 0 },
  { concurrency: 8, throughput: 648, ttftP50: 104, ttftP95: 156, ttftP99: 198, tpotP95: 20, e2eP95: 760, errorRate: 0 },
  { concurrency: 16, throughput: 1180, ttftP50: 142, ttftP95: 212, ttftP99: 288, tpotP95: 28, e2eP95: 1200, errorRate: 0 },
  { concurrency: 32, throughput: 1840, ttftP50: 226, ttftP95: 341, ttftP99: 452, tpotP95: 39, e2eP95: 2100, errorRate: 0 },
  { concurrency: 48, throughput: 2140, ttftP50: 312, ttftP95: 468, ttftP99: 640, tpotP95: 47, e2eP95: 3000, errorRate: 0 },
  { concurrency: 64, throughput: 2420, ttftP50: 468, ttftP95: 712, ttftP99: 980, tpotP95: 63, e2eP95: 4400, errorRate: 0.1 },
  { concurrency: 96, throughput: 2710, ttftP50: 890, ttftP95: 1320, ttftP99: 1810, tpotP95: 88, e2eP95: 7600, errorRate: 0.6 },
  { concurrency: 128, throughput: 2890, ttftP50: 1240, ttftP95: 1840, ttftP99: 2560, tpotP95: 118, e2eP95: 12100, errorRate: 1.8 },
]



export function latestRun(target: string): BenchmarkRun {
  return {
    id: 'run-20260905-1422',
    target,
    scenario: '对话',
    inputLength: 1024,
    outputLength: 512,
    dataset: 'ShareGPT 采样',
    ranAt: '2026-09-05 14:22',
    slo: DEFAULT_SLO,
    points: POINTS,
  }
}

export function historyRuns() {
  return [
    { id: 'run-20260905-1422', ranAt: '2026-09-05 14:22', maxConcurrency: 48, goodput: 2140, note: '当前' },
    { id: 'run-20260828-0910', ranAt: '2026-08-28 09:10', maxConcurrency: 40, goodput: 1880, note: 'vLLM 0.8' },
    { id: 'run-20260812-1655', ranAt: '2026-08-12 16:55', maxConcurrency: 32, goodput: 1540, note: '首次定标' },
  ]
}

export const benchmarkMock = {
  run: (target: string) => mockResponse(latestRun(target), `bench-${target}`),
  history: () => mockResponse({ runs: historyRuns() }, 'bench-history'),
}
