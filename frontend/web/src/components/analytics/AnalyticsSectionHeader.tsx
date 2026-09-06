import type { ReactNode } from 'react'

type AnalyticsSectionHeaderProps = {
  title: string
  description: string
  actions?: ReactNode
}

export function AnalyticsSectionHeader({ title, description, actions }: AnalyticsSectionHeaderProps) {
  return (
    <div className="rank-section-title">
      <div><h2>{title}</h2><p>{description}</p></div>
      {actions}
    </div>
  )
}
