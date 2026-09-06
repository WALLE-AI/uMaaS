import type { BenchmarkPoint, BenchmarkSlo } from '../api/contracts'

/** 压测判定逻辑。纯函数，与数据来源无关。 */
export const DEFAULT_SLO: BenchmarkSlo = { ttftP95Ms: 500, tpotP95Ms: 50 }

export function passesSlo(point: BenchmarkPoint, slo: BenchmarkSlo): boolean {
  return point.ttftP95 <= slo.ttftP95Ms && point.tpotP95 <= slo.tpotP95Ms && point.errorRate < 1
}

/** SLO 拐点：仍然满足 SLO 的最大并发。这是本页的主结论 */
export function sloKneePoint(points: BenchmarkPoint[], slo: BenchmarkSlo) {
  const passing = points.filter((point) => passesSlo(point, slo))
  const knee = passing[passing.length - 1]
  const saturation = points.reduce((best, point) =>
    point.throughput > best.throughput ? point : best,
  )
  return { knee, saturation }
}
