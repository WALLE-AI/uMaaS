export * from './generators'

/** 后端就绪前保持 true。api/ 层据此在 mock 与真实请求间切换 */
export const USE_MOCK = import.meta.env.VITE_USE_MOCK !== 'false'
