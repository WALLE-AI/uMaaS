import { ArrowRightOutlined, WarningFilled } from '@ant-design/icons'
import { Alert, Button, Checkbox, Modal, Radio, Tag } from 'antd'
import { useEffect, useMemo, useState } from 'react'
import type { DiffEntry, DiffKind } from '../../api/contracts'
import { compactNumber } from '../../config/metrics'

/**
 * 上游同步的差异审阅。
 *
 * 这个组件存在的全部理由，是让「从上游同步」不可能是一个直接写库的按钮。
 * 上游改价会即刻影响线上计费，上游删模型会让仍在调用的客户端 404 ——
 * 这类变更必须经人眼确认。因此流程被拆成三步：解析 → 审阅 → 选择应用方式，
 * 且默认不勾选高风险项。
 */

const KIND_META: Record<DiffKind, { label: string; tone: string; hint: string }> = {
  added: { label: '新增', tone: 'text-status-good', hint: '上游新发布，需补齐定价与能力标签后才能上架' },
  changed: { label: '变更', tone: 'text-status-warning', hint: '字段被上游修改，价格类变更会直接影响计费' },
  removed: { label: '上游已消失', tone: 'text-status-critical', hint: '上游已移除。下架前请确认没有客户端仍在调用' },
}

export type ApplyMode = 'list' | 'canary' | 'draft'

const APPLY_MODES: Array<{ value: ApplyMode; label: string; description: string }> = [
  { value: 'list', label: '直接上架', description: '立即对全部用户生效' },
  { value: 'canary', label: '先进灰度 10%', description: '先放量给 10% 请求，观察后再全量' },
  { value: 'draft', label: '仅创建草稿', description: '写入目录但不对外可见，稍后手工处理' },
]

export function DiffReview({
  open,
  loading,
  source,
  fetchedAt,
  entries,
  onCancel,
  onApply,
}: {
  open: boolean
  loading?: boolean
  source: string
  fetchedAt: string
  entries: DiffEntry[]
  onCancel: () => void
  onApply: (selected: DiffEntry[], mode: ApplyMode) => void
}) {
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [mode, setMode] = useState<ApplyMode>('list')

  /**
   * 默认勾选策略：新增与变更默认勾上（低风险、通常就是要的）；
   * 「上游已消失」默认不勾 —— 误下架一个仍在被调用的模型，代价远高于漏下架。
   */
  useEffect(() => {
    if (!open) return
    setSelected(new Set(entries.filter((entry) => entry.kind !== 'removed').map((entry) => entry.id)))
    setMode('list')
  }, [open, entries])

  const grouped = useMemo(() => {
    const groups: Record<DiffKind, DiffEntry[]> = { added: [], changed: [], removed: [] }
    entries.forEach((entry) => groups[entry.kind].push(entry))
    return groups
  }, [entries])

  const toggle = (id: string) =>
    setSelected((old) => {
      const next = new Set(old)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })

  const selectedEntries = entries.filter((entry) => selected.has(entry.id))
  const riskyCount = selectedEntries.filter(
    (entry) => entry.kind === 'removed' && (entry.callsLast7d ?? 0) > 0,
  ).length

  return (
    <Modal
      open={open}
      width={720}
      title="上游同步 · 差异审阅"
      onCancel={onCancel}
      destroyOnHidden
      footer={
        <div className="flex items-center justify-between gap-3">
          <span className="text-[11px] text-ink-muted">
            已选 {selectedEntries.length} / {entries.length} 项
          </span>
          <span className="flex gap-2">
            <Button onClick={onCancel}>取消</Button>
            <Button
              type="primary"
              danger={riskyCount > 0}
              disabled={selectedEntries.length === 0}
              loading={loading}
              onClick={() => onApply(selectedEntries, mode)}
            >
              应用选中的 {selectedEntries.length} 项变更
            </Button>
          </span>
        </div>
      }
    >
      <p className="mb-4 text-[11px] text-ink-muted">
        来源：<code className="rounded bg-plane px-1 font-mono">{source}</code> · 抓取于 {fetchedAt}
      </p>

      {entries.length === 0 && (
        <Alert type="success" showIcon message="上游与本地目录一致，没有需要处理的差异。" />
      )}

      {(Object.keys(KIND_META) as DiffKind[]).map((kind) => {
        const list = grouped[kind]
        if (list.length === 0) return null
        const meta = KIND_META[kind]

        return (
          <section key={kind} className="mb-4 overflow-hidden rounded border border-line">
            <div className="flex items-center justify-between gap-3 border-b border-line bg-plane px-3 py-2">
              <b className={`text-[11px] ${meta.tone}`}>
                {meta.label} · {list.length}
              </b>
              <span className="text-[10px] text-ink-muted">{meta.hint}</span>
            </div>

            {list.map((entry) => (
              <label
                key={entry.id}
                className="flex cursor-pointer items-start gap-2.5 border-b border-line-soft px-3 py-2.5 last:border-b-0 hover:bg-plane"
              >
                <Checkbox
                  checked={selected.has(entry.id)}
                  onChange={() => toggle(entry.id)}
                  className="mt-0.5"
                />
                <span className="min-w-0 flex-1">
                  <span className="flex flex-wrap items-center gap-2">
                    <b className="text-[12px]">{entry.name}</b>
                    <code className="rounded bg-plane px-1 font-mono text-[9px] text-ink-muted">
                      {entry.modelId}
                    </code>
                  </span>

                  <p className="mt-1 mb-0 text-[11px] text-ink-muted">{entry.summary}</p>

                  {entry.changes?.map((change) => (
                    <span
                      key={change.field}
                      className="mt-1.5 flex items-center gap-2 font-mono text-[11px]"
                    >
                      <span className="text-ink-secondary">{change.field}</span>
                      <span className="text-ink-muted line-through">{change.before}</span>
                      <ArrowRightOutlined className="text-[8px] text-ink-muted" />
                      <b className="text-ink">{change.after}</b>
                    </span>
                  ))}

                  {entry.missing && entry.missing.length > 0 && (
                    <span className="mt-1.5 flex flex-wrap items-center gap-1">
                      <span className="text-[10px] text-ink-muted">需补充：</span>
                      {entry.missing.map((field) => (
                        <Tag key={field} className="m-0 text-[9px]">
                          {field}
                        </Tag>
                      ))}
                    </span>
                  )}

                  {/*
                    防误下架的关键信息。没有这一行，管理者看到「上游已消失」
                    的第一反应就是勾上删掉，而线上可能还有客户端在调它。
                  */}
                  {entry.kind === 'removed' && (entry.callsLast7d ?? 0) > 0 && (
                    <span className="mt-2 flex items-center gap-1.5 rounded bg-plane px-2 py-1.5 text-[10px] text-status-serious">
                      <WarningFilled />
                      近 7 日仍有 {compactNumber(entry.callsLast7d ?? 0, 1)} 次调用，建议先发公告再下架
                    </span>
                  )}
                </span>
              </label>
            ))}
          </section>
        )
      })}

      {entries.length > 0 && (
        <div className="mt-5 border-t border-line pt-4">
          <span className="text-[11px] font-medium text-ink-secondary">应用方式</span>
          <Radio.Group
            className="mt-2 flex flex-col gap-2"
            value={mode}
            onChange={(event) => setMode(event.target.value as ApplyMode)}
          >
            {APPLY_MODES.map((item) => (
              <Radio key={item.value} value={item.value}>
                <span className="text-[11px]">
                  {item.label}
                  <small className="ml-2 text-ink-muted">{item.description}</small>
                </span>
              </Radio>
            ))}
          </Radio.Group>

          {riskyCount > 0 && (
            <Alert
              className="mt-3"
              type="error"
              showIcon
              message={`已选中 ${riskyCount} 个仍有调用量的下架项，应用后相关客户端会立即收到 404。`}
            />
          )}
        </div>
      )}
    </Modal>
  )
}
