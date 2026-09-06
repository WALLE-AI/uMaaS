/**
 * 主视觉大数字 —— 一个页面/区块要传达的那一个结论。
 * §4.3：不要用八种分类色去讲一个数字的故事。
 */
export function HeroFigure({
  value,
  label,
  delta,
  sub,
}: {
  value: string
  label?: string
  delta?: { value: string; direction: 'up' | 'down'; good?: boolean }
  sub?: string
}) {
  const arrow = delta?.direction === 'up' ? '↑' : '↓'
  const tone =
    delta?.good === undefined
      ? 'text-ink-muted'
      : delta.good
        ? 'text-status-good'
        : 'text-status-critical'

  return (
    <div className="flex flex-wrap items-baseline gap-x-4 gap-y-1">
      <b className="font-mono text-[48px] leading-none font-semibold tracking-tight">{value}</b>
      {label && <span className="text-sm text-ink-secondary">{label}</span>}
      {delta && (
        <em className={`font-mono text-xs not-italic ${tone}`}>
          {arrow} {delta.value}
        </em>
      )}
      {sub && <span className="w-full text-[11px] text-ink-muted">{sub}</span>}
    </div>
  )
}
