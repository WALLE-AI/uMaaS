import type { ReactNode } from 'react'
import { useLocation } from 'react-router'
import { Brand } from '../common'
import { AppHeader } from './AppHeader'

export function AppShell({ children }: { children: ReactNode }) {
  const location = useLocation()
  const docsMode = location.pathname.startsWith('/docs')
  const authMode = ['/login', '/signup'].includes(location.pathname)
  const consoleMode = location.pathname.startsWith('/console')

  return (
    <div className={`app ${docsMode ? 'docs-mode' : ''} ${authMode ? 'auth-mode' : ''} ${consoleMode ? 'console-mode' : ''}`}>
      <AppHeader />
      <main>{children}</main>
      {!docsMode && !authMode && !consoleMode && (
        <footer>
          <Brand />
          <span>统一接入、路由与评测模型能力</span>
          <span>© 2026 uMaaS</span>
        </footer>
      )}
    </div>
  )
}
