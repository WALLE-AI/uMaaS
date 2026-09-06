import { BellOutlined, LogoutOutlined, SearchOutlined, UserOutlined } from '@ant-design/icons'
import { Avatar, Badge, Button, Dropdown, Select, Tooltip } from 'antd'
import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router'
import { allRoutes } from '../config/navigation'
import { ENV_LABEL, useEnvironment } from './environment'

/**
 * 顶栏：品牌 · 全局搜索(⌘K) · 环境切换 · 告警 · 账号。
 *
 * 环境切换器放在这里而不是侧边栏底部，是因为它决定了页面上所有数字属于哪套
 * 集群 —— 必须始终可见（§3.4）。
 */
export function AdminTopbar({ alertCount = 0 }: { alertCount?: number }) {
  const navigate = useNavigate()
  const { env, setEnv } = useEnvironment()
  const [searchOpen, setSearchOpen] = useState(false)

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') {
        event.preventDefault()
        setSearchOpen(true)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])

  return (
    <header className="sticky top-0 z-50 flex h-14 items-center gap-4 border-b border-line bg-surface/95 px-5 backdrop-blur">
      <div className="flex items-center gap-2">
        <span className="flex h-6 w-6 items-center justify-center rounded bg-ink font-mono text-[9px] text-white">
          uM
        </span>
        <b className="text-sm">uMaaS</b>
        <span className="rounded border border-line px-1 py-0.5 font-mono text-[8px] text-ink-muted">
          ADMIN
        </span>
      </div>

      <div className="ml-2 min-w-0 flex-1">
        <Select
          showSearch
          open={searchOpen}
          onDropdownVisibleChange={setSearchOpen}
          value={null}
          placeholder="搜索页面、模型、用户、实例…  ⌘K"
          suffixIcon={<SearchOutlined />}
          variant="filled"
          className="w-full max-w-md"
          filterOption={(input, option) =>
            String(option?.label ?? '')
              .toLowerCase()
              .includes(input.toLowerCase())
          }
          onSelect={(path) => {
            if (!path) return
            setSearchOpen(false)
            navigate(path)
          }}
          options={allRoutes.map((route) => ({
            value: route.path,
            label: `${route.group} / ${route.label}`,
          }))}
        />
      </div>

      <Select
        value={env}
        onChange={setEnv}
        variant="borderless"
        className="w-28"
        options={(Object.keys(ENV_LABEL) as Array<keyof typeof ENV_LABEL>).map((key) => ({
          value: key,
          label: (
            <span className="inline-flex items-center gap-1.5">
              <i
                className="inline-block h-2 w-2 rounded-full"
                style={{ background: `var(--color-env-${key})` }}
              />
              {ENV_LABEL[key]}
            </span>
          ),
        }))}
      />

      <Tooltip title={alertCount ? `${alertCount} 条待处理告警` : '暂无告警'}>
        <Badge count={alertCount} size="small" offset={[-2, 2]}>
          <Button type="text" icon={<BellOutlined />} aria-label="告警" />
        </Badge>
      </Tooltip>

      <Dropdown
        placement="bottomRight"
        trigger={['click']}
        menu={{
          items: [
            { key: 'profile', icon: <UserOutlined />, label: '个人资料' },
            { type: 'divider' },
            { key: 'logout', icon: <LogoutOutlined />, label: '退出登录', danger: true },
          ],
        }}
      >
        <button
          type="button"
          className="flex cursor-pointer items-center gap-2 rounded border-0 bg-transparent px-1.5 py-1 hover:bg-plane"
        >
          <Avatar size={26} className="bg-ink font-mono text-[10px]">
            AD
          </Avatar>
          <span className="flex flex-col items-start leading-tight">
            <b className="text-[11px]">平台管理员</b>
            <small className="text-[8px] text-ink-muted">super-admin</small>
          </span>
        </button>
      </Dropdown>
    </header>
  )
}
