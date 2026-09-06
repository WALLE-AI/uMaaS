import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from 'react'

export type AuthUser = { name: string; email: string; workspace: string }

const STORAGE_KEY = 'umaas.auth.user'

function readStoredUser(): AuthUser | null {
  for (const store of [window.localStorage, window.sessionStorage]) {
    try {
      const raw = store.getItem(STORAGE_KEY)
      if (raw) return JSON.parse(raw) as AuthUser
    } catch {
      // 存储被禁用或数据损坏时按未登录处理
    }
  }
  return null
}

function writeStoredUser(user: AuthUser, remember: boolean) {
  try {
    const target = remember ? window.localStorage : window.sessionStorage
    const other = remember ? window.sessionStorage : window.localStorage
    other.removeItem(STORAGE_KEY)
    target.setItem(STORAGE_KEY, JSON.stringify(user))
  } catch {
    // 存储不可用时仅保留内存中的登录态
  }
}

function clearStoredUser() {
  try {
    window.localStorage.removeItem(STORAGE_KEY)
    window.sessionStorage.removeItem(STORAGE_KEY)
  } catch {
    // 忽略：内存状态已在 setUser 中清除
  }
}

export function initials(name: string) {
  const trimmed = name.trim()
  if (!trimmed) return '--'
  const parts = trimmed.split(/\s+/)
  if (parts.length > 1) return parts.slice(0, 2).map(part => part[0].toUpperCase()).join('')
  return trimmed.slice(0, 2).toUpperCase()
}

type AuthContextValue = {
  user: AuthUser | null
  authenticated: boolean
  login: (user: AuthUser, remember?: boolean) => void
  logout: () => void
}

const AuthContext = createContext<AuthContextValue | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<AuthUser | null>(readStoredUser)

  const login = useCallback((next: AuthUser, remember = true) => {
    writeStoredUser(next, remember)
    setUser(next)
  }, [])

  const logout = useCallback(() => {
    clearStoredUser()
    setUser(null)
  }, [])

  const value = useMemo(() => ({ user, authenticated: !!user, login, logout }), [user, login, logout])
  return <AuthContext value={value}>{children}</AuthContext>
}

export function useAuth() {
  const value = useContext(AuthContext)
  if (!value) throw new Error('useAuth 必须在 AuthProvider 内部使用')
  return value
}
