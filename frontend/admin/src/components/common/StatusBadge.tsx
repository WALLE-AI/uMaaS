import {
  CheckCircleFilled,
  CloseCircleFilled,
  ExclamationCircleFilled,
  MinusCircleFilled,
  WarningFilled,
} from '@ant-design/icons'
import type { StatusTone } from '../../config/viz'

/**
 * 状态徽章 —— 三重编码：颜色 + 图标 + 文字。
 *
 * §4.2 的硬规则：状态色永不单独承载语义。白色面板上 warning 与 serious 的
 * 对比度低于 3:1（1.79 / 2.57），图标与文字是它们唯一可靠的识别通道；此外
 * 状态色与分类色在色觉模拟下存在近似对（如 critical ≈ 系列红），仅凭颜色
 * 无法区分"这是故障"还是"这是第 8 个系列"。
 */

export type StatusKind = StatusTone | 'idle'

const PRESET: Record<StatusKind, { icon: typeof CheckCircleFilled; defaultLabel: string }> = {
  good: { icon: CheckCircleFilled, defaultLabel: '正常' },
  warning: { icon: WarningFilled, defaultLabel: '警告' },
  serious: { icon: ExclamationCircleFilled, defaultLabel: '严重' },
  critical: { icon: CloseCircleFilled, defaultLabel: '危急' },
  idle: { icon: MinusCircleFilled, defaultLabel: '空闲' },
}

const TONE_CLASS: Record<StatusKind, string> = {
  good: 'text-status-good',
  warning: 'text-status-warning',
  serious: 'text-status-serious',
  critical: 'text-status-critical',
  idle: 'text-ink-muted',
}

export function StatusBadge({
  kind,
  label,
  size = 'default',
}: {
  kind: StatusKind
  label?: string
  size?: 'default' | 'small'
}) {
  const preset = PRESET[kind]
  const Icon = preset.icon
  const text = label ?? preset.defaultLabel

  return (
    <span
      className={`inline-flex items-center gap-1.5 whitespace-nowrap ${TONE_CLASS[kind]} ${
        size === 'small' ? 'text-[10px]' : 'text-xs'
      }`}
    >
      <Icon aria-hidden />
      <span className={kind === 'idle' ? '' : 'text-ink'}>{text}</span>
    </span>
  )
}
