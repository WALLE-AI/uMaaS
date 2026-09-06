import { DownloadOutlined, SearchOutlined } from '@ant-design/icons'
import { Button, Input, Switch } from 'antd'
import { useEffect, useMemo, useRef, useState } from 'react'

export type LogLine = { level: 'info' | 'warn' | 'error' | 'ok'; text: string; ts?: string }

const LEVEL_CLASS: Record<LogLine['level'], string> = {
  info: 'text-ink-secondary',
  warn: 'text-status-warning',
  error: 'text-status-critical',
  ok: 'text-status-good',
}

/**
 * 实时日志流。
 *
 * 权重加载阶段刻意**不显示百分比进度条**：safetensors 分片加载没有可靠的
 * 进度可报，硬做一个匀速前进的假进度条会在卡住时误导人以为一切正常。
 * 用不确定进度（跳动的光标 + 持续滚动的日志）诚实地表达"还在动"。
 */
export function LogStream({
  lines,
  streaming,
  title,
  onDownload,
  height = 320,
}: {
  lines: LogLine[]
  streaming?: boolean
  title?: string
  onDownload?: () => void
  height?: number
}) {
  const [query, setQuery] = useState('')
  const [autoScroll, setAutoScroll] = useState(true)
  const bodyRef = useRef<HTMLDivElement>(null)

  const filtered = useMemo(
    () => (query ? lines.filter((line) => line.text.toLowerCase().includes(query.toLowerCase())) : lines),
    [lines, query],
  )

  useEffect(() => {
    if (autoScroll && bodyRef.current) {
      bodyRef.current.scrollTop = bodyRef.current.scrollHeight
    }
  }, [filtered.length, autoScroll])

  const download = () => {
    if (onDownload) return onDownload()
    const blob = new Blob([lines.map((line) => line.text).join('\n')], { type: 'text/plain' })
    const url = URL.createObjectURL(blob)
    const anchor = document.createElement('a')
    anchor.href = url
    anchor.download = `${title ?? 'deployment'}.log`
    anchor.click()
    URL.revokeObjectURL(url)
  }

  return (
    <div className="overflow-hidden rounded border border-line">
      <div className="flex flex-wrap items-center gap-2 border-b border-line bg-plane px-3 py-2">
        <span className="flex items-center gap-2 font-mono text-[10px] text-ink-secondary">
          {streaming && (
            <span className="flex items-center gap-1.5">
              {/* 不确定进度：只表示"还在动"，不谎报完成度 */}
              <i className="inline-block h-1.5 w-1.5 animate-pulse rounded-full bg-status-good" />
              streaming
            </span>
          )}
          {title}
        </span>
        <Input
          size="small"
          prefix={<SearchOutlined />}
          placeholder="过滤日志"
          allowClear
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          className="ml-auto w-44"
        />
        <label className="flex items-center gap-1.5 text-[10px] text-ink-muted">
          自动滚动
          <Switch size="small" checked={autoScroll} onChange={setAutoScroll} />
        </label>
        <Button size="small" type="text" icon={<DownloadOutlined />} onClick={download} />
      </div>

      <div
        ref={bodyRef}
        className="log-stream overflow-auto bg-[#101114] px-3 py-2"
        style={{ height }}
      >
        {filtered.map((line, index) => (
          <div key={`${index}-${line.text.slice(0, 24)}`} className="flex gap-2">
            <span className="shrink-0 text-[#4b4f57] select-none">
              {String(index + 1).padStart(3, ' ')}
            </span>
            <span className={`${LEVEL_CLASS[line.level]} break-all`}>{line.text}</span>
          </div>
        ))}

        {streaming && (
          <div className="flex gap-2">
            <span className="shrink-0 text-[#4b4f57] select-none">
              {String(filtered.length + 1).padStart(3, ' ')}
            </span>
            <span className="animate-pulse text-ink-muted">▊</span>
          </div>
        )}

        {filtered.length === 0 && !streaming && (
          <div className="py-6 text-center text-ink-muted">没有匹配的日志行</div>
        )}
      </div>
    </div>
  )
}
