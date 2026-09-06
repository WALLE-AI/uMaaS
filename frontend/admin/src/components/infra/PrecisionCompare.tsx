import { CheckCircleFilled, CloseCircleFilled } from '@ant-design/icons'
import { Tag } from 'antd'
import type { Precision } from '../../lib/capacity'

/**
 * 精度方案对比。
 *
 * 质量损失一列是刻意保留的：只看显存和卡数会让 INT4 显得永远最优，
 * 但 2–4% 的质量下降在很多业务场景里是不可接受的。三列并列才是完整的取舍。
 */
export function PrecisionCompare({
  rows,
  current,
  onPick,
}: {
  rows: Array<{
    precision: Precision
    perCardGb: number
    cards: number | null
    feasible: boolean
    qualityLoss: string
    throughputFactor: number
  }>
  current: Precision
  onPick: (precision: Precision) => void
}) {
  const baseline = rows.find((row) => row.precision === 'FP16')

  return (
    <div className="overflow-x-auto">
      <table className="w-full min-w-[560px] border-collapse text-[11px]">
        <thead>
          <tr className="border-b border-line bg-plane font-mono text-[9px] text-ink-muted">
            <th className="px-2 py-2 text-left font-normal">精度</th>
            <th className="px-2 py-2 text-right font-normal">单卡显存</th>
            <th className="px-2 py-2 text-right font-normal">最少卡数</th>
            <th className="px-2 py-2 text-right font-normal">相对吞吐</th>
            <th className="px-2 py-2 text-right font-normal">质量损失</th>
            <th className="px-2 py-2 text-left font-normal">建议</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => {
            const saving =
              baseline && baseline.cards && row.cards
                ? Math.round((1 - row.cards / baseline.cards) * 100)
                : 0
            return (
              <tr
                key={row.precision}
                onClick={() => onPick(row.precision)}
                className={`cursor-pointer border-b border-line-soft last:border-b-0 ${
                  row.precision === current ? 'bg-brand-soft' : 'hover:bg-plane'
                }`}
              >
                <td className="px-2 py-2.5">
                  <b className="font-mono">{row.precision}</b>
                  {row.precision === current && (
                    <Tag color="purple" className="ml-2 m-0 text-[9px]">
                      当前
                    </Tag>
                  )}
                </td>
                <td className="px-2 py-2.5 text-right font-mono">
                  {row.cards === null ? '—' : `${row.perCardGb.toFixed(1)} GB`}
                </td>
                <td className="px-2 py-2.5 text-right font-mono">
                  {row.cards === null ? '不可行' : row.cards}
                  {saving > 0 && (
                    <small className="ml-1 text-status-good">省 {saving}%</small>
                  )}
                </td>
                <td className="px-2 py-2.5 text-right font-mono">
                  ×{row.throughputFactor.toFixed(2)}
                </td>
                <td className="px-2 py-2.5 text-right">{row.qualityLoss}</td>
                <td className="px-2 py-2.5">
                  {!row.feasible ? (
                    <span className="inline-flex items-center gap-1 text-ink-muted">
                      <CloseCircleFilled /> 当前硬件放不下
                    </span>
                  ) : row.precision === 'FP16' ? (
                    <span className="inline-flex items-center gap-1 text-status-good">
                      <CheckCircleFilled /> 质量优先
                    </span>
                  ) : row.precision === 'FP8' ? (
                    <span className="inline-flex items-center gap-1 text-status-good">
                      <CheckCircleFilled /> 性价比最佳
                    </span>
                  ) : (
                    <span className="text-ink-muted">仅限成本敏感场景</span>
                  )}
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}
