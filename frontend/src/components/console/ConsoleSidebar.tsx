import {
  BarChartOutlined,
  CreditCardOutlined,
  DashboardOutlined,
  KeyOutlined,
  SafetyOutlined,
  SettingOutlined,
  TeamOutlined,
} from '@ant-design/icons'
import type { ReactNode } from 'react'
import { useAuth } from '../../auth'

export type ConsoleSection = 'overview' | 'keys' | 'usage' | 'billing' | 'team' | 'security' | 'settings'

const items: Array<{ key: ConsoleSection; label: string; icon: ReactNode }> = [
  { key: 'overview', label: '概览', icon: <DashboardOutlined /> },
  { key: 'keys', label: 'API Keys', icon: <KeyOutlined /> },
  { key: 'usage', label: '用量与预算', icon: <BarChartOutlined /> },
  { key: 'billing', label: '账单', icon: <CreditCardOutlined /> },
  { key: 'team', label: '团队成员', icon: <TeamOutlined /> },
  { key: 'security', label: '账户安全', icon: <SafetyOutlined /> },
  { key: 'settings', label: '工作空间设置', icon: <SettingOutlined /> },
]

export function ConsoleSidebar({ active, onNavigate }: { active: ConsoleSection; onNavigate: (section: ConsoleSection) => void }) {
  const { user } = useAuth()
  return (
    <aside className="console-sidebar">
      <div className="workspace-switcher">
        <span>WS</span>
        <div><b>{user?.workspace ?? 'Acme AI'}</b><small>Production workspace</small></div>
        <i>⌄</i>
      </div>
      <nav aria-label="控制台导航">
        <span className="console-nav-label">WORKSPACE</span>
        {items.map((item) => (
          <button key={item.key} className={active === item.key ? 'active' : ''} onClick={() => onNavigate(item.key)}>
            {item.icon}<span>{item.label}</span>
          </button>
        ))}
      </nav>
      <div className="console-sidebar-foot"><span>当前套餐</span><b>Developer</b><small>按量付费 · $42.80 余额</small></div>
    </aside>
  )
}
