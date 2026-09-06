import { Segmented } from 'antd'
import { useState } from 'react'
import { AnalyticsSectionHeader } from './AnalyticsSectionHeader'
import type { RankingInsightData } from './types'

const colors = ['#7c3cff', '#f05aa8', '#23b5d3', '#8dc63f']

export function RankingInsight({ id, index, data }: { id: string; index: number; data: RankingInsightData }) {
  const [mode, setMode] = useState('绝对值')
  const weights = data.labels.map((_, itemIndex) => Math.max(8, 92 - itemIndex * 17))
  const total = weights.reduce((sum, value) => sum + value, 0)

  return (
    <section id={id} className="rank-insight rank-scroll-section">
      <AnalyticsSectionHeader
        title={data.title}
        description={data.description}
        actions={<Segmented size="small" value={mode} onChange={setMode} options={['绝对值', '占比']} />}
      />
      <div className="insight-body">
        <div className="insight-bars">
          {data.labels.map((label, itemIndex) => {
            const percentage = weights[itemIndex] / total * 100
            return (
              <div key={label}>
                <span><b>{label}</b><em>{mode === '绝对值' ? data.values[itemIndex] : `${percentage.toFixed(1)}%`}</em></span>
                <i><u style={{ width: `${weights[itemIndex]}%`, background: colors[itemIndex % colors.length] }} /></i>
              </div>
            )
          })}
        </div>
        <div className="insight-summary">
          <span>{String(index + 3).padStart(2, '0')}</span>
          <b>{mode === '绝对值' ? data.values[0] : `${(weights[0] / total * 100).toFixed(1)}%`}</b>
          <small>当前领先</small>
          <p>基于过去 7 个完整 UTC 日的匿名聚合数据。</p>
        </div>
      </div>
    </section>
  )
}
