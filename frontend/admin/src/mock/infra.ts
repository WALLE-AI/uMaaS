import type { ClusterNode, GpuCard, GpuIncident, UiStatus as StatusKind } from '../api/contracts'
import { hourRange, mockResponse, seedFrom, seededRandom, trendSeries } from './generators'

import { XID_MEANING } from '../lib/gpu'
export { XID_MEANING }



function makeCard(nodeId: string, index: number, options: Partial<GpuCard> = {}): GpuCard {
  const random = seededRandom(seedFrom(`${nodeId}-${index}`))
  const util = options.util ?? Math.round(random() * 100)
  const memoryTotalGb = options.memoryTotalGb ?? 80
  return {
    index,
    uuid: `GPU-${nodeId}-${String(index).padStart(2, '0')}`,
    model: options.model ?? 'H100-80G',
    memoryTotalGb,
    memoryUsedGb: options.memoryUsedGb ?? Number(((util / 100) * memoryTotalGb * 0.92).toFixed(1)),
    util,
    tempC: options.tempC ?? Math.round(42 + (util / 100) * 34 + random() * 5),
    powerW: options.powerW ?? Math.round(60 + (util / 100) * 580),
    powerCapW: options.powerCapW ?? 700,
    smClockMhz: options.smClockMhz ?? Math.round(1200 + (util / 100) * 555),
    eccSingleBit: options.eccSingleBit ?? 0,
    eccDoubleBit: options.eccDoubleBit ?? 0,
    status: options.status ?? (util > 0 ? 'good' : 'idle'),
    statusLabel: options.statusLabel ?? (util > 0 ? '正常' : '空闲'),
    ...options,
  }
}

export function clusterNodes(): ClusterNode[] {
  return [
    {
      id: 'node-01',
      gpuModel: 'H100-80G',
      interconnect: 'NVLink 全互联',
      cards: [
        ...[0, 1, 2, 3].map((index) =>
          makeCard('node-01', index, { util: 88 + index, allocatedTo: 'qwen3-72b-prod', memoryUsedGb: 64.5 }),
        ),
        ...[4, 5].map((index) =>
          makeCard('node-01', index, { util: 52 + index, allocatedTo: 'qwen3-32b-prod', memoryUsedGb: 38.2 }),
        ),
        ...[6, 7].map((index) => makeCard('node-01', index, { util: 0, memoryUsedGb: 0 })),
      ],
    },
    {
      id: 'node-02',
      gpuModel: 'H100-80G',
      interconnect: 'NVLink 全互联',
      cards: Array.from({ length: 8 }, (_, index) =>
        makeCard('node-02', index, {
          util: 84 + (index % 3),
          allocatedTo: 'deepseek-v4-canary',
          memoryUsedGb: 71.2,
          ...(index === 5
            ? {
                tempC: 87,
                status: 'warning' as StatusKind,
                statusLabel: '高温',
                throttleReason: 'SW Thermal Slowdown',
                powerW: 698,
              }
            : {}),
        }),
      ),
    },
    {
      id: 'node-03',
      gpuModel: 'A100-40G',
      interconnect: 'PCIe',
      cards: Array.from({ length: 8 }, (_, index) =>
        makeCard('node-03', index, {
          model: 'A100-40G',
          memoryTotalGb: 40,
          powerCapW: 400,
          util: index < 4 ? 61 + index : 0,
          allocatedTo: index < 4 ? 'glm-5-9b-prod' : undefined,
        }),
      ),
    },
    {
      id: 'node-04',
      gpuModel: 'A100-40G',
      interconnect: 'PCIe',
      cards: Array.from({ length: 8 }, (_, index) =>
        makeCard('node-04', index, {
          model: 'A100-40G',
          memoryTotalGb: 40,
          powerCapW: 400,
          util: index < 2 ? 74 + index : 0,
          allocatedTo: index < 2 ? 'qwen3-32b-prod' : undefined,
          ...(index === 2
            ? {
                util: 0,
                memoryUsedGb: 0,
                tempC: 41,
                powerW: 62,
                eccSingleBit: 12,
                eccDoubleBit: 3,
                xid: 48,
                status: 'critical' as StatusKind,
                statusLabel: '故障',
                allocatedTo: undefined,
              }
            : {}),
        }),
      ),
    },
    {
      id: 'node-05',
      gpuModel: 'L40S-48G',
      interconnect: 'PCIe',
      cards: Array.from({ length: 8 }, (_, index) =>
        makeCard('node-05', index, {
          model: 'L40S-48G',
          memoryTotalGb: 48,
          powerCapW: 350,
          util: index < 3 ? 44 + index * 6 : 0,
          allocatedTo: index < 3 ? 'embedding-svc' : undefined,
          ...(index >= 6
            ? {
                util: 0,
                memoryUsedGb: 0,
                status: 'idle' as StatusKind,
                statusLabel: '维护中',
                allocatedTo: undefined,
              }
            : {}),
        }),
      ),
    },
  ]
}

export function clusterSummary(nodes: ClusterNode[]) {
  const cards = nodes.flatMap((node) => node.cards)
  const healthy = cards.filter((card) => card.status === 'good' || card.status === 'idle').length
  const allocated = cards.filter((card) => card.allocatedTo).length
  const memTotal = cards.reduce((acc, card) => acc + card.memoryTotalGb, 0)
  const memUsed = cards.reduce((acc, card) => acc + card.memoryUsedGb, 0)
  const utilAvg = cards.reduce((acc, card) => acc + card.util, 0) / cards.length
  return {
    nodes: nodes.length,
    gpuTotal: cards.length,
    gpuHealthy: healthy,
    gpuFaulty: cards.length - healthy,
    allocated,
    memRatio: (memUsed / memTotal) * 100,
    utilAvg,
    instances: new Set(cards.map((card) => card.allocatedTo).filter(Boolean)).size,
  }
}

/** 利用率与显存占用同为百分比 —— 量纲一致，共轴合法 */
export function gpuUtilizationSeries(hours = 6) {
  const points = hourRange(hours * 2).slice(0, hours * 2)
  const util = trendSeries({ seed: 'gpu-util', length: points.length, base: 62, growth: 0.006, weekly: 0.35, noise: 0.08 })
  const mem = trendSeries({ seed: 'gpu-mem', length: points.length, base: 71, growth: 0.003, weekly: 0.18, noise: 0.04 })
  return points.map((label, index) => ({
    time: label,
    util: Math.min(100, Math.round(util[index])),
    memory: Math.min(100, Math.round(mem[index])),
  }))
}

/** 温度与功耗量纲不同，各自独立成图 */
export function gpuThermalSeries(hours = 6) {
  const points = hourRange(hours * 2).slice(0, hours * 2)
  const temp = trendSeries({ seed: 'gpu-temp', length: points.length, base: 68, growth: 0.004, weekly: 0.12, noise: 0.05 })
  const power = trendSeries({ seed: 'gpu-power', length: points.length, base: 580, growth: 0.003, weekly: 0.1, noise: 0.06 })
  return {
    temperature: points.map((time, index) => ({ time, temperature: Math.round(temp[index]) })),
    power: points.map((time, index) => ({ time, power: Math.round(power[index]) })),
  }
}

/** 全集群 GPU × 时间 利用率热力 */
export function gpuHeatmap(nodes: ClusterNode[], slots = 12) {
  const columns = hourRange(slots).slice(0, slots)
  const rows = nodes.flatMap((node) =>
    node.cards.slice(0, 4).map((card) => {
      const random = seededRandom(seedFrom(`${node.id}-${card.index}-heat`))
      return {
        key: `${node.id}-${card.index}`,
        label: `${node.id.replace('node-', 'n')}·${card.index}`,
        cells: columns.map(() => {
          if (card.status === 'critical') return { ratio: null, display: '故障', fault: true }
          const ratio = Math.min(1, Math.max(0, card.util / 100 + (random() - 0.5) * 0.25))
          return { ratio, display: `${Math.round(ratio * 100)}%` }
        }),
      }
    }),
  )
  return { columns, rows }
}


export function gpuIncidents(): GpuIncident[] {
  return [
    {
      id: 'node-04-gpu2',
      kind: 'critical',
      node: 'node-04',
      gpu: 2,
      xid: 48,
      title: 'ECC 双位错误 ×3',
      detail: '已自动驱逐其上实例并将该卡标记为不可调度',
      since: '2026-09-06 07:12',
      evictedInstance: 'qwen3-32b-prod',
    },
    {
      id: 'node-02-gpu5',
      kind: 'warning',
      node: 'node-02',
      gpu: 5,
      title: '温度 87°C 持续 12 分钟',
      detail: '已触发 SW Thermal Slowdown，SM 时钟下调，吞吐受影响',
      since: '2026-09-06 09:31',
    },
  ]
}

export const infraMock = {
  cluster: () => {
    const nodes = clusterNodes()
    return mockResponse({ nodes, summary: clusterSummary(nodes) }, 'cluster')
  },
  observability: (hours: number) => {
    const nodes = clusterNodes()
    const thermal = gpuThermalSeries(hours)
    return mockResponse(
      {
        nodes,
        summary: clusterSummary(nodes),
        utilization: gpuUtilizationSeries(hours),
        temperature: thermal.temperature,
        power: thermal.power,
        heat: gpuHeatmap(nodes),
        incidents: gpuIncidents(),
      },
      `observability-${hours}`,
    )
  },
}
