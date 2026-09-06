import type { ReactNode } from 'react'

export type ConsoleStat = {
  label: string
  value: string
  note: string
  trend?: string
  icon?: ReactNode
}

export function ConsoleStatGrid({ items }: { items: ConsoleStat[] }) {
  return (
    <section className="console-stat-grid">
      {items.map((item) => <div key={item.label}><span>{item.icon}{item.label}</span><b>{item.value}</b><small>{item.note}</small>{item.trend && <em>{item.trend}</em>}</div>)}
    </section>
  )
}
