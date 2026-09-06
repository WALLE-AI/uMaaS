import { DownOutlined } from '@ant-design/icons'
import { createElement, useEffect, useState } from 'react'
import { NavLink, useLocation } from 'react-router'
import { navigation } from '../config/navigation'

/**
 * 两级折叠侧边栏。
 * §2 明确不做三级导航 —— 超过两级说明模块该拆了。
 */
export function AdminSidebar() {
  const location = useLocation()

  // 当前路径所属的分组默认展开
  const activeGroup = navigation.find((group) =>
    group.path
      ? group.path === location.pathname
      : (group.items ?? []).some((item) => location.pathname.startsWith(item.path)),
  )

  const [expanded, setExpanded] = useState<Set<string>>(
    () => new Set(activeGroup ? [activeGroup.key] : []),
  )

  useEffect(() => {
    if (activeGroup) {
      setExpanded((old) => (old.has(activeGroup.key) ? old : new Set(old).add(activeGroup.key)))
    }
  }, [activeGroup])

  const toggle = (key: string) =>
    setExpanded((old) => {
      const next = new Set(old)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })

  const leafClass = ({ isActive }: { isActive: boolean }) =>
    [
      'flex h-8 items-center gap-2 rounded pl-8 pr-2.5 text-[11px] transition-colors',
      isActive
        ? 'bg-brand-soft font-semibold text-[#6728df]'
        : 'text-ink-secondary hover:bg-plane hover:text-ink',
    ].join(' ')

  return (
    <aside className="sticky top-14 flex h-[calc(100vh-3.5rem)] flex-col border-r border-line bg-surface px-3 py-4">
      <nav aria-label="管理平台导航" className="flex min-h-0 flex-1 flex-col gap-0.5 overflow-y-auto">
        {navigation.map((group) => {
          const isOpen = expanded.has(group.key)

          if (group.path) {
            return (
              <NavLink
                key={group.key}
                to={group.path}
                end
                className={({ isActive }) =>
                  [
                    'flex h-9 items-center gap-2.5 rounded px-2.5 text-xs transition-colors',
                    isActive
                      ? 'bg-brand-soft font-semibold text-[#6728df]'
                      : 'text-ink-secondary hover:bg-plane hover:text-ink',
                  ].join(' ')
                }
              >
                <span className="w-4 text-sm">{createElement(group.icon)}</span>
                {group.label}
              </NavLink>
            )
          }

          return (
            <div key={group.key} className="flex flex-col">
              <button
                type="button"
                onClick={() => toggle(group.key)}
                aria-expanded={isOpen}
                className="flex h-9 cursor-pointer items-center gap-2.5 rounded border-0 bg-transparent px-2.5 text-left text-xs text-ink-secondary transition-colors hover:bg-plane hover:text-ink"
              >
                <span className="w-4 text-sm">{createElement(group.icon)}</span>
                <span className="flex-1">{group.label}</span>
                <DownOutlined
                  className={`text-[8px] transition-transform ${isOpen ? '' : '-rotate-90'}`}
                />
              </button>

              {isOpen && (
                <div className="flex flex-col gap-0.5 pt-0.5 pb-1">
                  {(group.items ?? []).map((item) => (
                    <NavLink key={item.path} to={item.path} end={item.end} className={leafClass}>
                      {item.label}
                    </NavLink>
                  ))}
                </div>
              )}
            </div>
          )
        })}
      </nav>

      <div className="mt-auto flex flex-col gap-0.5 border-t border-line pt-3 pb-1">
        <span className="px-2.5 font-mono text-[8px] text-ink-muted">CLUSTER</span>
        <b className="px-2.5 text-xs">5 节点 · 33 GPU</b>
        <small className="px-2.5 text-[9px] text-ink-muted">显存水位 73% · 8 实例运行中</small>
      </div>
    </aside>
  )
}
