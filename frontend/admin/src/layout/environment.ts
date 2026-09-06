import { useCallback, useSyncExternalStore } from 'react'

/**
 * 当前操作的环境。决定顶栏色带颜色，也决定所有数据请求打到哪套集群。
 * §3.4：管理者必须随时知道自己在动哪套数据。
 */
export type EnvName = 'production' | 'staging' | 'local'

export const ENV_LABEL: Record<EnvName, string> = {
  production: '生产',
  staging: '预发',
  local: '本地',
}

const STORAGE_KEY = 'umaas.admin.env'

function readEnv(): EnvName {
  try {
    const stored = window.localStorage.getItem(STORAGE_KEY) as EnvName | null
    if (stored && stored in ENV_LABEL) return stored
  } catch {
    // 存储不可用时回落到构建期配置
  }
  const fromEnv = import.meta.env.VITE_ENV_NAME
  return fromEnv && fromEnv in ENV_LABEL ? fromEnv : 'local'
}

const listeners = new Set<() => void>()
let current: EnvName = typeof window === 'undefined' ? 'local' : readEnv()

function subscribe(listener: () => void) {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

export function useEnvironment() {
  const env = useSyncExternalStore(
    subscribe,
    () => current,
    () => 'local' as EnvName,
  )

  const setEnv = useCallback((next: EnvName) => {
    current = next
    try {
      window.localStorage.setItem(STORAGE_KEY, next)
    } catch {
      // 忽略：内存状态已更新
    }
    listeners.forEach((listener) => listener())
  }, [])

  return { env, setEnv }
}
