import { Tooltip } from 'antd'
import { sequentialColor, sequentialScale } from '../../config/viz'

/**
 * 热力图 —— 网格状量级对比（GPU × 时间、周 × 小时）。
 * §4.3：顺序色单一色相，浅→深；禁止彩虹。
 *
 * 故障/无效格用斜线纹理 + 危急色双重标注，不只靠颜色 —— 这是色觉障碍与
 * 黑白打印下唯一可靠的通道（§4.2）。
 */

export type HeatCell = {
  /** 归一化到 0–1 的量级；null 表示无数据 */
  ratio: number | null
  /** 悬浮显示的精确值 */
  display: string
  /** 故障态：叠加纹理并改用危急色 */
  fault?: boolean
}

export type HeatmapProps = {
  rows: Array<{ key: string; label: string; cells: HeatCell[] }>
  columns: string[]
  /** 刻度图例的两端文案 */
  legend?: { low: string; high: string }
  cellHeight?: number
}

export function Heatmap({ rows, columns, legend, cellHeight = 22 }: HeatmapProps) {
  const scale = sequentialScale()

  return (
    <div className="min-w-0">
      <div className="overflow-x-auto">
        <table className="w-full border-separate" style={{ borderSpacing: '2px' }}>
          <thead>
            <tr>
              <th className="w-24" />
              {columns.map((column) => (
                <th
                  key={column}
                  className="pb-1 text-center font-mono text-[9px] font-normal text-ink-muted"
                >
                  {column}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.map((row) => (
              <tr key={row.key}>
                <td className="pr-2 text-right font-mono text-[10px] whitespace-nowrap text-ink-secondary">
                  {row.label}
                </td>
                {row.cells.map((cell, index) => (
                  <td key={`${row.key}-${columns[index] ?? index}`} style={{ height: cellHeight }}>
                    <Tooltip
                      title={`${row.label} · ${columns[index] ?? ''} · ${cell.display}`}
                      mouseEnterDelay={0.1}
                    >
                      <div
                        className={`h-full w-full rounded-[2px] ${cell.fault ? 'viz-hatch' : ''}`}
                        style={{
                          height: cellHeight,
                          background: cell.fault
                            ? 'var(--color-status-critical)'
                            : cell.ratio === null
                              ? 'var(--color-line-soft)'
                              : sequentialColor(cell.ratio),
                          color: '#fff',
                        }}
                      />
                    </Tooltip>
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <div className="mt-3 flex items-center gap-2 text-[10px] text-ink-muted">
        <span>{legend?.low ?? '低'}</span>
        <div className="flex">
          {scale.map((color) => (
            <i key={color} className="block h-2.5 w-5" style={{ background: color }} />
          ))}
        </div>
        <span>{legend?.high ?? '高'}</span>
        <span className="ml-4 inline-flex items-center gap-1.5">
          <i
            className="viz-hatch inline-block h-2.5 w-5 rounded-[2px]"
            style={{ background: 'var(--color-status-critical)', color: '#fff' }}
          />
          故障
        </span>
      </div>
    </div>
  )
}
