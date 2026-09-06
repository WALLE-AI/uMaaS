import { sequentialColor } from '../../config/viz'

/**
 * 计量条 —— 单个比率对上限的正确形态（显存水位、预算使用、磁盘占用）。
 * §4.3：不要用只有两片的饼图表达一个比率。
 *
 * 超过阈值时叠加斜线纹理，不只靠颜色表达「超限」。
 */
export function Meter({
  value,
  max,
  label,
  valueText,
  /** 超过该比例视为告警，默认 0.9 */
  warnAt = 0.9,
  showPercent = true,
}: {
  value: number
  max: number
  label?: string
  valueText?: string
  warnAt?: number
  showPercent?: boolean
}) {
  const ratio = max > 0 ? Math.min(1, Math.max(0, value / max)) : 0
  const over = ratio >= warnAt
  const percent = (ratio * 100).toFixed(ratio >= 0.1 ? 0 : 1)

  return (
    <div className="min-w-0">
      {(label || valueText || showPercent) && (
        <div className="mb-1.5 flex items-baseline justify-between gap-3">
          {label && <span className="truncate text-[11px] text-ink-secondary">{label}</span>}
          <span className="shrink-0 font-mono text-[11px] text-ink">
            {valueText ?? `${percent}%`}
          </span>
        </div>
      )}
      <div
        className="h-2 w-full overflow-hidden rounded-full bg-line-soft"
        role="meter"
        aria-valuenow={value}
        aria-valuemin={0}
        aria-valuemax={max}
        aria-label={label}
      >
        <div
          className={`h-full rounded-full transition-[width] ${over ? 'viz-hatch' : ''}`}
          style={{
            width: `${ratio * 100}%`,
            background: over ? 'var(--color-status-warning)' : sequentialColor(ratio),
            color: '#000',
          }}
        />
      </div>
    </div>
  )
}
