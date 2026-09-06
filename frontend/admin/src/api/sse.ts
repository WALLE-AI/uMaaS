/**
 * SSE 封装 —— 用于下载进度与部署日志。
 *
 * 选 SSE 而非轮询或 WebSocket：这两个场景都是长时、单向推送，浏览器原生支持
 * 且自带断线重连，比 WebSocket 轻得多（§9.3）。
 */
export type SseHandlers<T> = {
  onMessage: (payload: T) => void
  onError?: (error: Event) => void
  onDone?: () => void
}

const API_BASE_URL = import.meta.env.VITE_API_BASE_URL || '/api/v1'

export function subscribe<T>(path: string, handlers: SseHandlers<T>): () => void {
  const source = new EventSource(
    `${API_BASE_URL}${path.startsWith('/') ? path : `/${path}`}`,
    { withCredentials: true },
  )

  source.onmessage = (event) => {
    if (event.data === '[DONE]') {
      source.close()
      handlers.onDone?.()
      return
    }
    try {
      handlers.onMessage(JSON.parse(event.data) as T)
    } catch {
      // 非 JSON 行按原始文本传出（部署日志就是纯文本行）
      handlers.onMessage(event.data as unknown as T)
    }
  }

  source.onerror = (event) => {
    handlers.onError?.(event)
    // EventSource 会自动重连，只有明确关闭才终止
    if (source.readyState === EventSource.CLOSED) handlers.onDone?.()
  }

  return () => source.close()
}
