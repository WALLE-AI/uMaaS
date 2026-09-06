export type AnalyticsStat = {
  label: string
  value: string
  change?: string
  tone?: 'positive' | 'negative' | 'neutral'
}

export function AnalyticsStatStrip({ items }: { items: AnalyticsStat[] }) {
  return (
    <div className="analytics-stat-strip">
      {items.map((item) => (
        <div key={item.label}>
          <span>{item.label}</span>
          <b>{item.value}</b>
          {item.change && <em className={item.tone || 'neutral'}>{item.change}</em>}
        </div>
      ))}
    </div>
  )
}
