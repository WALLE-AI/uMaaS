import type { ReactNode } from 'react'

export function ConsolePageHeader({ eyebrow, title, description, actions }: { eyebrow: string; title: string; description: string; actions?: ReactNode }) {
  return (
    <header className="console-page-header">
      <div><span>{eyebrow}</span><h1>{title}</h1><p>{description}</p></div>
      {actions && <div className="console-page-actions">{actions}</div>}
    </header>
  )
}
