import { WarningFilled } from '@ant-design/icons'
import { Alert, Input, Modal } from 'antd'
import { useEffect, useState } from 'react'

/**
 * 破坏性操作确认 —— 必须键入资源名才能提交。
 *
 * §1：管理者的误操作会让全平台模型下架、或杀掉正在服务的 GPU 实例，
 * 后果与用户端"删掉自己的 key"完全不同。一次点击的确认框在这里不够。
 */
export function DangerConfirm({
  open,
  title,
  /** 必须被完整键入的资源名 */
  resourceName,
  description,
  impact,
  okText = '确认执行',
  onConfirm,
  onCancel,
  loading,
}: {
  open: boolean
  title: string
  resourceName: string
  description: string
  /** 影响面提示，例如「近 7 日仍有 2.4K 次调用」 */
  impact?: string
  okText?: string
  onConfirm: () => void
  onCancel: () => void
  loading?: boolean
}) {
  const [typed, setTyped] = useState('')
  const matched = typed.trim() === resourceName

  useEffect(() => {
    if (open) setTyped('')
  }, [open])

  return (
    <Modal
      open={open}
      title={
        <span className="inline-flex items-center gap-2">
          <WarningFilled className="text-status-critical" />
          {title}
        </span>
      }
      okText={okText}
      cancelText="取消"
      okButtonProps={{ danger: true, disabled: !matched, loading }}
      onOk={onConfirm}
      onCancel={onCancel}
      destroyOnHidden
    >
      <p className="text-xs text-ink-secondary">{description}</p>

      {impact && (
        <Alert className="mt-3" type="warning" showIcon message={impact} banner={false} />
      )}

      <label className="mt-4 block">
        <span className="text-[11px] text-ink-secondary">
          请键入 <code className="rounded bg-plane px-1 font-mono text-ink">{resourceName}</code> 以确认
        </span>
        <Input
          className="mt-1.5"
          value={typed}
          onChange={(event) => setTyped(event.target.value)}
          placeholder={resourceName}
          autoComplete="off"
          status={typed && !matched ? 'error' : undefined}
        />
      </label>
    </Modal>
  )
}
