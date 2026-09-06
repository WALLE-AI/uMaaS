import { useCallback, useEffect, useState } from 'react'

/**
 * 异步数据加载。
 *
 * mock 层刻意注入 5% 失败率，就是为了逼着每个页面从第一天就把加载态和错误态
 * 写全 —— 而不是等联调时才发现只有 happy path。
 */
export type AsyncState<T> = {
  data: T | undefined
  loading: boolean
  error: Error | undefined
  reload: () => void
}

export function useAsyncData<T>(loader: () => Promise<T>, deps: unknown[]): AsyncState<T> {
  const [data, setData] = useState<T>()
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<Error>()
  const [nonce, setNonce] = useState(0)

  // loader 每次渲染都是新函数，依赖用调用方给的 deps 控制
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const run = useCallback(loader, deps)

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setError(undefined)

    run()
      .then((result) => {
        if (!cancelled) setData(result)
      })
      .catch((cause: unknown) => {
        if (!cancelled) setError(cause instanceof Error ? cause : new Error(String(cause)))
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })

    return () => {
      cancelled = true
    }
  }, [run, nonce])

  const reload = useCallback(() => setNonce((value) => value + 1), [])

  return { data, loading, error, reload }
}
