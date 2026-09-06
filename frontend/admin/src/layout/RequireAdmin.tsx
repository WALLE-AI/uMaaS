import { Spin } from 'antd'
import { createContext, use, useEffect, useState, type ReactNode } from 'react'
import { EmptyState } from '../components/common/EmptyState'

/**
 * 管理员角色守卫。
 *
 * 与 web 的 RequireAuth 是同一个模式，但多一层角色校验：登录只说明"是用户"，
 * 进管理平台还要"是管理员"。真实实现走 GET /api/v1/admin/me；mock 模式下
 * 直接放行，便于后端就绪前开发。
 */

export type AdminUser = {
  id: string
  name: string
  role: 'super-admin' | 'operator' | 'viewer'
}

type AdminAuthState = { status: 'loading' | 'authorized' | 'denied'; user?: AdminUser }

const AdminAuthContext = createContext<AdminAuthState>({ status: 'loading' })

export function useAdminAuth() {
  return use(AdminAuthContext)
}

const USE_MOCK = import.meta.env.VITE_USE_MOCK !== 'false'

export function RequireAdmin({ children }: { children: ReactNode }) {
  const [state, setState] = useState<AdminAuthState>({ status: 'loading' })

  useEffect(() => {
    let cancelled = false

    if (USE_MOCK) {
      setState({
        status: 'authorized',
        user: { id: 'mock-admin', name: '平台管理员', role: 'super-admin' },
      })
      return
    }

    fetch(`${import.meta.env.VITE_API_BASE_URL ?? '/api/v1'}/admin/me`, {
      credentials: 'include',
      headers: { Accept: 'application/json' },
    })
      .then((response) => (response.ok ? response.json() : Promise.reject(response.status)))
      .then((payload: { data: AdminUser }) => {
        if (!cancelled) setState({ status: 'authorized', user: payload.data })
      })
      .catch(() => {
        if (!cancelled) setState({ status: 'denied' })
      })

    return () => {
      cancelled = true
    }
  }, [])

  if (state.status === 'loading') {
    return (
      <div className="flex h-screen items-center justify-center">
        <Spin />
      </div>
    )
  }

  if (state.status === 'denied') {
    return (
      <div className="flex h-screen items-center justify-center p-10">
        <EmptyState
          title="无权访问管理平台"
          description="当前账号不具备管理员角色。如需访问，请联系平台超级管理员分配权限。"
        />
      </div>
    )
  }

  return <AdminAuthContext value={state}>{children}</AdminAuthContext>
}
