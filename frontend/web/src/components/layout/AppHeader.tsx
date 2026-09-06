import { AppstoreOutlined, ArrowRightOutlined, BellOutlined, CloseOutlined, LogoutOutlined, MenuOutlined, SearchOutlined, SettingOutlined, UserOutlined } from '@ant-design/icons'
import { Avatar, Button, Drawer, Dropdown, Tooltip } from 'antd'
import { useState } from 'react'
import { NavLink, useLocation, useNavigate } from 'react-router'
import { initials, useAuth } from '../../auth'
import { primaryNavigation } from '../../config/navigation'
import { Brand } from '../common'

export function AppHeader() {
  const [open, setOpen] = useState(false)
  const navigate = useNavigate()
  const location = useLocation()
  const { user, authenticated, logout } = useAuth()
  const consoleMode = location.pathname.startsWith('/console')
  const accountMenu = {
    items: [
      { key: 'console', icon: <AppstoreOutlined />, label: '控制台' },
      { key: 'profile', icon: <UserOutlined />, label: '个人资料' },
      { key: 'settings', icon: <SettingOutlined />, label: '工作空间设置' },
      { type: 'divider' as const },
      { key: 'logout', icon: <LogoutOutlined />, label: '退出登录', danger: true },
    ],
    onClick: ({ key }: { key: string }) => {
      if (key === 'logout') { logout(); return navigate('/') }
      navigate(key === 'console' ? '/console' : key === 'settings' ? '/console/settings' : '/console/security')
    },
  }

  return (
    <>
      <header className={`topbar ${consoleMode ? 'console-topbar' : ''}`}>
        <Brand />
        <nav className="desktop-nav">
          {primaryNavigation.map(({ path, label, end }) => (
            <NavLink
              key={path}
              to={path}
              end={end}
              className={({ isActive }) => isActive || (!end && location.pathname.startsWith(path)) ? 'active' : ''}
            >
              {label}
            </NavLink>
          ))}
        </nav>
        <div className="top-actions">
          <Tooltip title={consoleMode ? '搜索控制台' : '全局搜索'}>
            <Button className="icon-btn desktop-only" icon={<SearchOutlined />} aria-label={consoleMode ? '搜索控制台' : '全局搜索'} />
          </Tooltip>
          {authenticated ? <>
            <Tooltip title="通知"><Button className="icon-btn console-notification desktop-only" icon={<BellOutlined />} aria-label="通知"><i /></Button></Tooltip>
            <Dropdown menu={accountMenu} placement="bottomRight" trigger={['click']}>
              <button className="account-trigger"><Avatar size={28}>{initials(user!.name)}</Avatar><span className="desktop-only"><b>{user!.name}</b><small>{user!.workspace}</small></span><i>⌄</i></button>
            </Dropdown>
          </> : <>
            <Button className="desktop-only" onClick={() => navigate('/login')}>登录</Button>
            <Button type="primary" className="desktop-only" onClick={() => navigate('/signup')}>免费注册</Button>
          </>}
          <Button className="icon-btn mobile-only" icon={<MenuOutlined />} onClick={() => setOpen(true)} aria-label="打开导航" />
        </div>
      </header>
      <Drawer open={open} onClose={() => setOpen(false)} width={320} closeIcon={<CloseOutlined />} title={<Brand />}>
        <nav className="mobile-nav">
          {primaryNavigation.map(({ path, label }) => <NavLink key={path} to={path} onClick={() => setOpen(false)}>{label}<ArrowRightOutlined /></NavLink>)}
          {authenticated && <NavLink to="/console" onClick={() => setOpen(false)}>控制台<ArrowRightOutlined /></NavLink>}
        </nav>
        <div className="mobile-auth-actions">
          {authenticated
            ? <Button block danger onClick={() => { setOpen(false); logout(); navigate('/') }}>退出登录</Button>
            : <>
              <Button block onClick={() => { setOpen(false); navigate('/login') }}>登录</Button>
              <Button type="primary" block onClick={() => { setOpen(false); navigate('/signup') }}>免费注册</Button>
            </>}
        </div>
      </Drawer>
    </>
  )
}
