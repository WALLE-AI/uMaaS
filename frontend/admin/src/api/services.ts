import { analyticsMock } from '../mock/analytics'
import { benchmarkMock } from '../mock/benchmark'
import { catalogMock, testChannel as testChannelMock } from '../mock/catalog'
import { customersMock } from '../mock/customers'
import { deploymentsMock, preflight as preflightMock } from '../mock/deployments'
import { infraMock } from '../mock/infra'
import { registryMock, resolveRepo as resolveRepoMock } from '../mock/registry'
import { systemMock } from '../mock/system'
import { apiRequest } from './client'
import type {
  BenchmarkRun,
  BillingPayload,
  CatalogModel,
  CatalogSummary,
  Channel,
  ChannelTestResult,
  ClusterNode,
  ClusterSummary,
  CustomerUser,
  Deployment,
  Dimension,
  DownloadTask,
  LocalModel,
  Measure,
  ModelLeaderboardRow,
  ObservabilityPayload,
  PreflightCheck,
  OverviewPayload,
  ResolvedRepo,
  RoutingPolicy,
  SettingGroup,
  UsagePayload,
  UsersAnalyticsPayload,
  Workspace,
  AdminAccount,
  AuditEntry,
  BenchmarkHistoryRow,
  UpstreamDiff,
} from './contracts'

/**
 * 管理端端点。
 *
 * 每个函数都是「mock 或真实请求」的分流点 —— 页面只认这一层，永远不直接
 * import mock。切后端时把 USE_MOCK 关掉即可，页面一行不用改（§9.1.2）。
 *
 * 端点路径与方案 §9.3 一致。
 */
const USE_MOCK = import.meta.env.VITE_USE_MOCK !== 'false'

/** mock 与真实请求二选一。保持在一处，避免每个函数写一遍三元表达式 */
function route<T>(mock: () => Promise<T>, real: () => Promise<T>): Promise<T> {
  return USE_MOCK ? mock() : real()
}

export const analyticsApi = {
  overview: () =>
    route<OverviewPayload>(
      () => analyticsMock.overview(),
      () => apiRequest('/admin/overview'),
    ),
  usage: (dimension: Dimension, measure: Measure, days: number) =>
    route<UsagePayload>(
      () => analyticsMock.usage(dimension, measure, days),
      () => apiRequest('/admin/analytics/usage', { query: { dimension, measure, days } }),
    ),
  users: () =>
    route<UsersAnalyticsPayload>(
      () => analyticsMock.users(),
      () => apiRequest('/admin/analytics/users'),
    ),
  models: () =>
    route<{ leaderboard: ModelLeaderboardRow[] }>(
      () => analyticsMock.models(),
      () => apiRequest('/admin/analytics/models'),
    ),
}

export const catalogApi = {
  models: () =>
    route<{ models: CatalogModel[]; summary: CatalogSummary }>(
      () => catalogMock.models(),
      () => apiRequest('/admin/catalog/models'),
    ),
  diff: (source: string) =>
    route<UpstreamDiff>(
      () => catalogMock.diff(source),
      () => apiRequest('/admin/catalog/upstream-diff', { query: { source } }),
    ),
  applyDiff: (entryIds: string[], mode: 'list' | 'canary' | 'draft') =>
    route<void>(
      async () => undefined,
      () =>
        apiRequest('/admin/catalog/upstream-diff/apply', {
          method: 'POST',
          body: { entry_ids: entryIds, mode },
        }),
    ),
  delistModel: (modelId: string) =>
    route<void>(
      async () => undefined,
      () => apiRequest(`/admin/catalog/models/${encodeURIComponent(modelId)}/delist`, { method: 'POST' }),
    ),
  channels: () =>
    route<{ channels: Channel[] }>(
      () => catalogMock.channels(),
      () => apiRequest('/admin/catalog/channels'),
    ),
  testChannel: (channelId: string) =>
    route<ChannelTestResult>(
      () => testChannelMock(channelId),
      () => apiRequest(`/admin/catalog/channels/${encodeURIComponent(channelId)}/test`, { method: 'POST' }),
    ),
  routing: () =>
    route<{ policies: RoutingPolicy[]; channels: Channel[] }>(
      () => catalogMock.routing(),
      () => apiRequest('/admin/catalog/routing'),
    ),
}

export const infraApi = {
  cluster: () =>
    route<{ nodes: ClusterNode[]; summary: ClusterSummary }>(
      () => infraMock.cluster(),
      () => apiRequest('/admin/infra/nodes'),
    ),
  observability: (hours: number) =>
    route<ObservabilityPayload>(
      () => infraMock.observability(hours),
      () => apiRequest('/admin/infra/gpus/metrics', { query: { hours } }),
    ),
  registry: () =>
    route<{ models: LocalModel[]; tasks: DownloadTask[]; disk: { usedTb: number; totalTb: number } }>(
      () => registryMock.list(),
      () => apiRequest('/admin/infra/registry/models'),
    ),
  resolveRepo: (repo: string) =>
    route<ResolvedRepo>(
      () => resolveRepoMock(repo),
      () => apiRequest('/admin/infra/registry/resolve', { method: 'POST', body: { repo } }),
    ),
  pull: (repo: string, options: { mirror: boolean; verify: boolean; token?: string }) =>
    route<{ taskId: string }>(
      async () => ({ taskId: `dl-${Date.now()}` }),
      () => apiRequest('/admin/infra/registry/pull', { method: 'POST', body: { repo, ...options } }),
    ),
  cancelPull: (taskId: string) =>
    route<void>(
      async () => undefined,
      () => apiRequest(`/admin/infra/registry/pull/${taskId}`, { method: 'DELETE' }),
    ),
  deployments: () =>
    route<{ deployments: Deployment[] }>(
      () => deploymentsMock.list(),
      () => apiRequest('/admin/infra/deployments'),
    ),
  preflight: (model: string, node: string, tp: number) =>
    route<PreflightCheck[]>(
      async () => preflightMock(model, node, tp),
      () => apiRequest('/admin/infra/deployments/preflight', { query: { model, node, tp } }),
    ),
  createDeployment: (body: Record<string, unknown>) =>
    route<{ id: string }>(
      async () => ({ id: `dep-${Date.now()}` }),
      () => apiRequest('/admin/infra/deployments', { method: 'POST', body }),
    ),
  deleteDeployment: (id: string) =>
    route<void>(
      async () => undefined,
      () => apiRequest(`/admin/infra/deployments/${encodeURIComponent(id)}`, { method: 'DELETE' }),
    ),
  benchmark: (target: string) =>
    route<BenchmarkRun>(
      () => benchmarkMock.run(target),
      () => apiRequest('/admin/infra/benchmark/runs/latest', { query: { target } }),
    ),
  benchmarkHistory: () =>
    route<{ runs: BenchmarkHistoryRow[] }>(
      () => benchmarkMock.history(),
      () => apiRequest('/admin/infra/benchmark/runs'),
    ),
}

export const customersApi = {
  users: () =>
    route<{ users: CustomerUser[] }>(
      () => customersMock.users(),
      () => apiRequest('/admin/customers/users'),
    ),
  suspendUser: (userId: string) =>
    route<void>(
      async () => undefined,
      () => apiRequest(`/admin/customers/users/${encodeURIComponent(userId)}/suspend`, { method: 'POST' }),
    ),
  workspaces: () =>
    route<{ workspaces: Workspace[] }>(
      () => customersMock.workspaces(),
      () => apiRequest('/admin/customers/workspaces'),
    ),
  billing: () =>
    route<BillingPayload>(
      () => customersMock.billing(),
      () => apiRequest('/admin/customers/billing'),
    ),
  refund: (topupId: string) =>
    route<void>(
      async () => undefined,
      () => apiRequest(`/admin/customers/topups/${encodeURIComponent(topupId)}/refund`, { method: 'POST' }),
    ),
}

export const systemApi = {
  settings: () =>
    route<{ groups: SettingGroup[] }>(
      () => systemMock.settings(),
      () => apiRequest('/admin/system/settings'),
    ),
  saveSettings: (patch: Record<string, unknown>) =>
    route<void>(
      async () => undefined,
      () => apiRequest('/admin/system/settings', { method: 'PATCH', body: patch }),
    ),
  rbac: () =>
    route<{ accounts: AdminAccount[] }>(
      () => systemMock.rbac(),
      () => apiRequest('/admin/system/admins'),
    ),
  removeAdmin: (adminId: string) =>
    route<void>(
      async () => undefined,
      () => apiRequest(`/admin/system/admins/${encodeURIComponent(adminId)}`, { method: 'DELETE' }),
    ),
  audit: () =>
    route<{ entries: AuditEntry[] }>(
      () => systemMock.audit(),
      () => apiRequest('/admin/system/audit'),
    ),
}
