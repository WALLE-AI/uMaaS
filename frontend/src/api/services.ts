import { apiRequest } from './client'
import type {
  AuthSession,
  BenchmarkDetail,
  BenchmarkSummary,
  CatalogSummary,
  DocsNavigationGroup,
  DocsPage,
  HarnessConfigRequest,
  HarnessConfigResponse,
  HarnessDefinition,
  LoginRequest,
  ModelDetail,
  ModelListParams,
  ModelSummary,
  RankingParams,
  RankingResponse,
  RegisterRequest,
  User,
} from './contracts'

function modelPath(modelId: string) {
  const [provider, ...modelParts] = modelId.split('/')
  if (!provider || modelParts.length === 0) throw new Error(`Invalid model ID: ${modelId}`)
  return `${encodeURIComponent(provider)}/${encodeURIComponent(modelParts.join('/'))}`
}

export const catalogApi = {
  summary: () => apiRequest<CatalogSummary>('/catalog/summary'),
}

export const modelsApi = {
  list: (params: ModelListParams = {}) => apiRequest<ModelSummary[]>('/models', { query: params }),
  detail: (modelId: string) => apiRequest<ModelDetail>(`/models/${modelPath(modelId)}`),
  favorite: (modelId: string) => apiRequest<void>(`/me/favorites/${modelPath(modelId)}`, { method: 'PUT' }),
  unfavorite: (modelId: string) => apiRequest<void>(`/me/favorites/${modelPath(modelId)}`, { method: 'DELETE' }),
  favorites: () => apiRequest<ModelSummary[]>('/me/favorites'),
}

export const benchmarksApi = {
  list: (category?: string) => apiRequest<BenchmarkSummary[]>('/benchmarks', { query: { category } }),
  detail: (slug: string) => apiRequest<BenchmarkDetail>(`/benchmarks/${encodeURIComponent(slug)}`),
}

export const rankingsApi = {
  get: (params: RankingParams) => apiRequest<RankingResponse>('/rankings', { query: params }),
}

export const docsApi = {
  navigation: () => apiRequest<DocsNavigationGroup[]>('/docs/navigation'),
  page: (slug: string) => apiRequest<DocsPage>(`/docs/${encodeURIComponent(slug)}`),
  search: (query: string) => apiRequest<Array<{ slug: string; title: string; excerpt: string }>>('/docs/search', { query: { q: query } }),
}

export const harnessApi = {
  list: () => apiRequest<HarnessDefinition[]>('/harnesses'),
  generateConfig: (input: HarnessConfigRequest) => apiRequest<HarnessConfigResponse>('/harnesses/config', { method: 'POST', body: input }),
  joinWaitlist: () => apiRequest<{ joined_at: string }>('/harnesses/waitlist', { method: 'POST' }),
}

export const authApi = {
  login: (input: LoginRequest) => apiRequest<AuthSession>('/auth/login', { method: 'POST', body: input }),
  register: (input: RegisterRequest) => apiRequest<AuthSession>('/auth/register', { method: 'POST', body: input }),
  logout: () => apiRequest<void>('/auth/logout', { method: 'POST' }),
  session: () => apiRequest<AuthSession>('/auth/session'),
  currentUser: () => apiRequest<User>('/me'),
  requestPasswordReset: (email: string) => apiRequest<void>('/auth/password/reset-request', { method: 'POST', body: { email } }),
  oauthUrl: (provider: 'github' | 'google', mode: 'login' | 'signup', returnTo: string) =>
    `${import.meta.env.VITE_API_BASE_URL || '/api/v1'}/auth/oauth/${provider}?mode=${mode}&return_to=${encodeURIComponent(returnTo)}`,
}
