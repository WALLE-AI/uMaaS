import type { ReactNode } from 'react'

export function Kicker({ children }: { children: ReactNode }) {
  return <div className="kicker"><span />{children}</div>
}

export function PageTitle({
  eyebrow,
  title,
  text,
  action,
}: {
  eyebrow: string
  title: string
  text: string
  action?: ReactNode
}) {
  return (
    <section className="page-title">
      <div><Kicker>{eyebrow}</Kicker><h1>{title}</h1><p>{text}</p></div>
      {action}
    </section>
  )
}

