import type { CSSProperties } from 'react'
import type { Model } from '../../data'

export function ModelLogo({ model, large = false }: { model: Model; large?: boolean }) {
  return (
    <span
      className={`model-logo ${large ? 'large' : ''}`}
      style={{ '--logo-color': model.color } as CSSProperties}
    >
      <span>{model.initials}</span>
      <img
        src={model.logo}
        alt={`${model.maker} logo`}
        loading="lazy"
        onError={(event) => { event.currentTarget.style.display = 'none' }}
      />
    </span>
  )
}

