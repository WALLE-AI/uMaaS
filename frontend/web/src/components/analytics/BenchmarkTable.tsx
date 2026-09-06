import type { Model } from '../../data'
import { ModelLogo } from '../models'
import type { BenchmarkRecord } from './types'

const metricKeys = ['quality', 'value', 'speed'] as const

export function BenchmarkTable({ rows, models, onSelect }: { rows: BenchmarkRecord[]; models: Model[]; onSelect: (row: BenchmarkRecord) => void }) {
  if (!rows.length) return <div className="analytics-empty"><b>暂无评测结果</b><span>该分类尚未完成有效评测。</span></div>

  return (
    <div className="benchmark-table">
      <div className="benchmark-table-head"><span>BENCHMARK</span><span>QUALITY</span><span>VALUE</span><span>SPEED</span></div>
      {rows.map((row) => (
        <button className="benchmark-record" key={row.name} onClick={() => onSelect(row)}>
          <span className="benchmark-info">
            <b>{row.name} <em>›</em></b>
            <small>{row.description}</small>
            <i>{row.models} 个模型 · 最后运行于 Sep {row.group === '搜索' ? '18' : '4'}, 2026</i>
          </span>
          {metricKeys.map((metric, index) => (
            <span className="benchmark-result" data-label={['质量', '价值', '速度'][index]} key={metric}>
              <strong>{row[metric]}</strong>
              <small><ModelLogo model={models[(index + row.group.length) % models.length]} />{row.winners[index]}</small>
            </span>
          ))}
        </button>
      ))}
    </div>
  )
}
