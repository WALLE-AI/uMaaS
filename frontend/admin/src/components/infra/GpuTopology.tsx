import { Tag, Tooltip } from 'antd'
import { sequentialColor, sequentialScale } from '../../config/viz'
import { StatusBadge } from '../common/StatusBadge'
import type { ClusterNode, GpuCard } from '../../api/contracts'
import { XID_MEANING } from '../../lib/gpu'

/**
 * 节点 × GPU 拓扑网格。
 *
 * 两个编码通道严格分开，这是本组件唯一重要的设计约束：
 *   · 格子填充 = 利用率（连续量 → 顺序色，单一蓝色相由浅到深）
 *   · 右上角标 = 健康状态（离散态 → 状态色 + 图标）
 * 混用会让"深蓝"同时可能意味着"很忙"和"有问题"，读者无从分辨。
 *
 * 故障格另加 45° 斜线纹理：色觉障碍与黑白打印下，纹理是唯一可靠的通道。
 */

function GpuCell({ card, nodeId }: { card: GpuCard; nodeId: string }) {
  const faulty = card.status === 'critical'
  const xid = card.xid ? XID_MEANING[card.xid] : undefined

  return (
    <Tooltip
      title={
        <div className="text-[11px]">
          <b>
            {nodeId} · GPU{card.index}
          </b>
          <div className="mt-1 font-mono text-[10px]">
            <div>利用率 {card.util}%</div>
            <div>
              显存 {card.memoryUsedGb.toFixed(1)} / {card.memoryTotalGb} GB
            </div>
            <div>
              {card.tempC}°C · {card.powerW}W / {card.powerCapW}W
            </div>
            {card.allocatedTo && <div className="mt-1">实例：{card.allocatedTo}</div>}
            {card.throttleReason && <div className="mt-1">降频：{card.throttleReason}</div>}
            {xid && (
              <div className="mt-1">
                XID {card.xid}：{xid.title}
              </div>
            )}
          </div>
        </div>
      }
    >
      <div
        className={`relative flex h-14 cursor-default flex-col items-center justify-center rounded border border-line ${
          faulty ? 'viz-hatch' : ''
        }`}
        style={{
          // 填充只承载利用率这一个含义
          background: faulty
            ? 'var(--color-status-critical)'
            : card.util === 0
              ? 'var(--color-line-soft)'
              : sequentialColor(card.util / 100),
          color: faulty || card.util > 55 ? '#fff' : 'var(--color-ink)',
        }}
      >
        <b className="font-mono text-[11px] leading-none">{card.index}</b>
        <small className="mt-1 font-mono text-[9px] opacity-90">
          {faulty ? 'ERR' : card.util === 0 ? '—' : `${card.util}%`}
        </small>

        {/* 角标只承载健康状态。非正常态才出现，避免每格都挂一个绿点 */}
        {card.status !== 'good' && (
          <span
            className="absolute top-0.5 right-0.5 flex h-2 w-2 items-center justify-center rounded-full"
            style={{
              background:
                card.status === 'critical'
                  ? '#fff'
                  : card.status === 'warning'
                    ? 'var(--color-status-warning)'
                    : 'var(--color-ink-muted)',
            }}
            aria-hidden
          />
        )}
      </div>
    </Tooltip>
  )
}

export function GpuTopology({ nodes }: { nodes: ClusterNode[] }) {
  const scale = sequentialScale()

  return (
    <div className="flex flex-col gap-5">
      {nodes.map((node) => {
        const allocations = Array.from(
          new Set(node.cards.map((card) => card.allocatedTo).filter(Boolean)),
        ) as string[]
        const faulty = node.cards.filter((card) => card.status === 'critical')

        return (
          <section key={node.id}>
            <div className="mb-2 flex flex-wrap items-center gap-2">
              <b className="font-mono text-[12px]">{node.id}</b>
              <span className="text-[10px] text-ink-muted">
                {node.cards.length}×{node.gpuModel} · {node.interconnect}
              </span>
              {allocations.map((instance) => (
                <Tag key={instance} className="m-0 text-[9px]">
                  {instance}
                </Tag>
              ))}
            </div>

            <div className="grid gap-1.5" style={{ gridTemplateColumns: 'repeat(8, minmax(0,1fr))' }}>
              {node.cards.map((card) => (
                <GpuCell key={card.uuid} card={card} nodeId={node.id} />
              ))}
            </div>

            {faulty.map((card) => {
              const xid = card.xid ? XID_MEANING[card.xid] : undefined
              return (
                <div
                  key={card.uuid}
                  className="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 rounded bg-plane px-3 py-2"
                >
                  <StatusBadge kind="critical" label={`GPU${card.index} ${card.statusLabel}`} size="small" />
                  {xid && (
                    <span className="text-[10px] text-ink-secondary">
                      <b className="font-mono">XID {card.xid}</b> · {xid.title} —— {xid.action}
                    </span>
                  )}
                </div>
              )
            })}
          </section>
        )
      })}

      {/* 两个通道各自的图例，明确它们不是一回事 */}
      <div className="flex flex-wrap items-center gap-x-6 gap-y-2 border-t border-line pt-3 text-[10px] text-ink-muted">
        <span className="flex items-center gap-2">
          填充 = 利用率
          <span className="flex">
            {scale.map((color) => (
              <i key={color} className="block h-2.5 w-5" style={{ background: color }} />
            ))}
          </span>
          <span>0 → 100%</span>
        </span>
        <span className="flex items-center gap-3">
          角标 = 健康状态
          <StatusBadge kind="warning" label="告警" size="small" />
          <StatusBadge kind="idle" label="空闲/维护" size="small" />
        </span>
        <span className="flex items-center gap-1.5">
          <i
            className="viz-hatch inline-block h-3 w-6 rounded-[2px] border border-line"
            style={{ background: 'var(--color-status-critical)', color: '#fff' }}
          />
          故障（含纹理，灰度可辨）
        </span>
      </div>
    </div>
  )
}
