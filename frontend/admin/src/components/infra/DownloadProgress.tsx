import { CloseOutlined, PauseOutlined, PlayCircleOutlined } from '@ant-design/icons'
import { Button, Tooltip } from 'antd'
import type { DownloadTask } from '../../api/contracts'

/**
 * 大文件下载进度。
 *
 * 72B 模型 FP16 约 145GB，下载要几十分钟到几小时。这类任务的可观测性不是
 * 加分项：管理者必须随时能回答"还要多久""卡在哪个分片""是不是断了"。
 * 因此进度条之外必须给出：分片进度、瞬时速率、剩余时间、镜像源。
 */
export function DownloadProgress({
  task,
  onPause,
  onResume,
  onCancel,
}: {
  task: DownloadTask
  onPause?: (id: string) => void
  onResume?: (id: string) => void
  onCancel?: (id: string) => void
}) {
  const percent = (task.downloadedGb / task.sizeGb) * 100
  const remainingGb = task.sizeGb - task.downloadedGb
  // speedMbps 是 MB/s
  const etaSeconds = task.speedMbps > 0 ? (remainingGb * 1024) / task.speedMbps : Infinity
  const eta =
    !Number.isFinite(etaSeconds) || task.status !== 'downloading'
      ? '—'
      : etaSeconds > 3600
        ? `约 ${(etaSeconds / 3600).toFixed(1)} 小时`
        : `约 ${Math.ceil(etaSeconds / 60)} 分钟`

  return (
    <div className="border-b border-line-soft py-3 last:border-b-0">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <b className="font-mono text-[11px]">{task.repo}</b>
        <span className="flex items-center gap-1">
          {task.status === 'downloading' ? (
            <Tooltip title="暂停（已下载分片会保留，可续传）">
              <Button
                size="small"
                type="text"
                icon={<PauseOutlined />}
                onClick={() => onPause?.(task.id)}
              />
            </Tooltip>
          ) : (
            <Tooltip title="继续">
              <Button
                size="small"
                type="text"
                icon={<PlayCircleOutlined />}
                onClick={() => onResume?.(task.id)}
              />
            </Tooltip>
          )}
          <Tooltip title="取消并删除已下载分片">
            <Button
              size="small"
              type="text"
              danger
              icon={<CloseOutlined />}
              onClick={() => onCancel?.(task.id)}
            />
          </Tooltip>
        </span>
      </div>

      <div className="mt-2 flex items-center gap-3">
        <i className="h-2 flex-1 overflow-hidden rounded-full bg-line-soft">
          <em
            className="block h-full rounded-full transition-[width]"
            style={{
              width: `${percent}%`,
              background:
                task.status === 'paused' ? 'var(--color-ink-muted)' : 'var(--color-brand)',
            }}
          />
        </i>
        <span className="w-10 shrink-0 text-right font-mono text-[11px]">
          {percent.toFixed(0)}%
        </span>
      </div>

      <div className="mt-1.5 flex flex-wrap items-center gap-x-4 gap-y-1 font-mono text-[10px] text-ink-muted">
        <span>
          {task.downloadedGb.toFixed(1)} / {task.sizeGb} GB
        </span>
        <span>
          分片 {task.shardsDone}/{task.shards}
        </span>
        <span>{task.status === 'paused' ? '已暂停' : `↓ ${task.speedMbps} MB/s`}</span>
        <span>剩余 {eta}</span>
        <span className="ml-auto">{task.source}</span>
      </div>
    </div>
  )
}
