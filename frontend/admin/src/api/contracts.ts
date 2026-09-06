/**
 * 管理端契约类型 —— 全部管理端数据结构的单一来源。
 *
 * mock 层与页面都从这里取类型，切真实后端时只改 services.ts 的实现，
 * 类型不动、页面不动（§9.1.2）。
 *
 * ⚠️ 一个已知的设计债：目前部分结构里混入了 `statusKind` 这类**前端 UI 状态**
 * （good/warning/critical）。真实后端不该关心前端用什么颜色，它应该只返回
 * 业务状态（running/failed/degraded），由前端映射到 UI 状态。本文件底部提供了
 * 映射函数，接后端时应删掉契约里的 statusKind 字段，改为在页面上调用映射。
 * 现在保留是为了不阻塞 mock 阶段，但这是要还的。
 */

/** UI 状态色槽。属于展示层概念，不应出现在后端响应里 */
export type UiStatus = 'good' | 'warning' | 'serious' | 'critical' | 'idle'

/** 统一响应信封。唯一真相源是 frontend/contracts/openapi.yaml，两端共用 */
export type ApiEnvelope<T> = {
  data: T
  meta?: { next_cursor?: string; total?: number; updated_at?: string }
  request_id: string
}

export type ApiErrorBody = {
  error: { code: string; message: string; field?: string }
  request_id: string
}

/* ─────────────── 数据分析 ─────────────── */

export type Dimension = 'vendor' | 'model' | 'userGroup' | 'app'
export type Measure = 'tokens' | 'requests' | 'cost' | 'toolCalls'

export type SeriesRef = { id: string; label: string; slot: number }

export type UsageTrend = {
  series: SeriesRef[]
  data: Array<Record<string, string | number>>
  total: number
  deltaPercent: number
}

export type UsageBreakdownRow = {
  key: string
  label: string
  slot: number
  tokens: number
  requests: number
  cost: number
  latencyP95: number
  errorRate: number
  share: number
}

export type HeatGrid = {
  columns: string[]
  rows: Array<{
    key: string
    label: string
    cells: Array<{ ratio: number | null; display: string; fault?: boolean }>
  }>
}

export type PlatformAlert = {
  id: string
  kind: UiStatus
  title: string
  detail: string
  href: string
}

export type ChannelHealth = {
  id: string
  name: string
  kind: UiStatus
  label: string
  p95: number
  errorRate: number
}

export type OverviewPayload = {
  alerts: PlatformAlert[]
  requests24h: Array<{ hour: string; requests: number }>
  channels: ChannelHealth[]
  cluster: Array<{ label: string; value: number; max: number; text: string }>
}

export type UsagePayload = {
  trend: UsageTrend
  composition: Array<{ id: string; label: string; slot: number; value: number }>
  heat: HeatGrid
  breakdown: UsageBreakdownRow[]
}

export type UsersAnalyticsPayload = {
  growth: Array<{ date: string; joined: number; churned: number }>
  retention: HeatGrid
  tiers: Array<{ id: string; label: string; slot: number; count: number }>
  funnel: Array<{ stage: string; value: number }>
}

export type ModelLeaderboardRow = {
  id: string
  name: string
  vendor: string
  tokens: number
  cost: number
  latency: number
  errorRate: number
  slot: number
  share: number
  costPerMillion: number
}

/* ─────────────── 供给管理 ─────────────── */

export type ModelStatus = 'listed' | 'canary' | 'delisted' | 'draft'

export type CatalogModel = {
  id: string
  name: string
  vendor: string
  hosting: 'cloud' | 'self'
  status: ModelStatus
  canaryPercent?: number
  context: string
  inputPrice: number | null
  outputPrice: number | null
  capabilities: string[]
  callsLast7d: number
}

export type CatalogSummary = {
  total: number
  listed: number
  canary: number
  delisted: number
  draft: number
}

export type DiffKind = 'added' | 'changed' | 'removed'

export type DiffEntry = {
  id: string
  kind: DiffKind
  modelId: string
  name: string
  changes?: Array<{ field: string; before: string; after: string }>
  missing?: string[]
  callsLast7d?: number
  summary: string
}

export type UpstreamDiff = { source: string; fetchedAt: string; entries: DiffEntry[] }

export type Channel = {
  id: string
  name: string
  type: string
  region: string
  priority: number
  weight: number
  status: UiStatus
  statusLabel: string
  degradedReason?: string
  p95: number
  errorRate: number
  costToday: number
  models: number
}

export type ChannelTestResult = { ok: boolean; latency: number; detail: string }

export type RoutingPolicy = {
  id: string
  model: string
  strategy: '质量优先' | '成本优先' | '延迟优先' | '均衡'
  fallbackChain: string[]
  canary?: { channel: string; percent: number }
  regionPinned: boolean
}

/* ─────────────── 私有化部署 ─────────────── */

export type GpuCard = {
  index: number
  uuid: string
  model: string
  memoryTotalGb: number
  memoryUsedGb: number
  util: number
  tempC: number
  powerW: number
  powerCapW: number
  smClockMhz: number
  eccSingleBit: number
  eccDoubleBit: number
  xid?: number
  status: UiStatus
  statusLabel: string
  allocatedTo?: string
  throttleReason?: string
}

export type ClusterNode = {
  id: string
  gpuModel: string
  interconnect: 'NVLink 全互联' | 'PCIe'
  cards: GpuCard[]
}

export type ClusterSummary = {
  nodes: number
  gpuTotal: number
  gpuHealthy: number
  gpuFaulty: number
  allocated: number
  memRatio: number
  utilAvg: number
  instances: number
}

export type GpuIncident = {
  id: string
  kind: UiStatus
  node: string
  gpu: number
  xid?: number
  title: string
  detail: string
  since: string
  evictedInstance?: string
}

export type ObservabilityPayload = {
  nodes: ClusterNode[]
  summary: ClusterSummary
  utilization: Array<{ time: string; util: number; memory: number }>
  temperature: Array<{ time: string; temperature: number }>
  power: Array<{ time: string; power: number }>
  heat: HeatGrid
  incidents: GpuIncident[]
}

export type LocalModel = {
  id: string
  name: string
  paramsB: number
  precision: string
  sizeGb: number
  verified: boolean
  source: 'HuggingFace' | 'ModelScope' | '手工上传'
  deployments: number
  pulledAt: string
}

export type DownloadTask = {
  id: string
  repo: string
  sizeGb: number
  downloadedGb: number
  shards: number
  shardsDone: number
  speedMbps: number
  source: string
  status: 'downloading' | 'paused' | 'verifying' | 'done' | 'failed'
}

export type ResolvedRepo = {
  repo: string
  displayName: string
  paramsB: number
  license: string
  gated: boolean
  gatedReason?: string
  format: string
  shards: number
  sizeGb: number
  config: { layers: number; attnHeads: number; kvHeads: number; headDim: number; maxContext: number }
  disk: { availableGb: number; requiredGb: number; sufficient: boolean }
  revisions: string[]
}

export type DeploymentStatus = 'running' | 'canary' | 'failed' | 'starting'

export type Deployment = {
  id: string
  name: string
  model: string
  engine: string
  node: string
  gpus: number[]
  tensorParallel: number
  precision: string
  status: DeploymentStatus
  statusKind: UiStatus
  statusLabel: string
  replicas: { ready: number; desired: number }
  qps: number
  p95: number
  memUsedGb: number
  memTotalGb: number
  kvHitRate?: number
  canaryPercent?: number
  failureReason?: string
}

export type PreflightCheck = {
  id: string
  kind: UiStatus
  label: string
  detail: string
  blocking: boolean
}

export type BenchmarkSlo = { ttftP95Ms: number; tpotP95Ms: number }

export type BenchmarkPoint = {
  concurrency: number
  throughput: number
  ttftP50: number
  ttftP95: number
  ttftP99: number
  tpotP95: number
  e2eP95: number
  errorRate: number
}

export type BenchmarkRun = {
  id: string
  target: string
  scenario: string
  inputLength: number
  outputLength: number
  dataset: string
  ranAt: string
  slo: BenchmarkSlo
  points: BenchmarkPoint[]
}

export type BenchmarkHistoryRow = {
  id: string
  ranAt: string
  maxConcurrency: number
  goodput: number
  note: string
}

/* ─────────────── 客户与系统 ─────────────── */

export type CustomerUser = {
  id: string
  name: string
  email: string
  workspace: string
  plan: '免费版' | '自助版' | '团队版' | '企业版'
  tokens30d: number
  cost30d: number
  balance: number
  status: UiStatus
  statusLabel: string
  joinedAt: string
  lastActive: string
}

export type Workspace = {
  id: string
  name: string
  plan: string
  seatsUsed: number
  seatsTotal: number
  monthlyBudget: number
  monthlySpend: number
  owner: string
  models: number
}

export type Invoice = {
  id: string
  workspace: string
  period: string
  amount: number
  status: 'paid' | 'pending' | 'overdue'
  issuedAt: string
}

export type Topup = {
  id: string
  workspace: string
  amount: number
  method: string
  at: string
  status: 'success' | 'refunded'
}

export type BillingPayload = {
  invoices: Invoice[]
  topups: Topup[]
  summary: { mrr: number; outstanding: number; refunded: number; arpu: number }
}

export type AdminRole = 'super-admin' | 'operator' | 'viewer'

export type AdminAccount = {
  id: string
  name: string
  email: string
  role: AdminRole
  twoFactor: boolean
  lastActive: string
  status: UiStatus
}

export type AuditEntry = {
  id: string
  at: string
  actor: string
  action: string
  target: string
  destructive: boolean
  detail: string
  ip: string
}

export type SettingGroup = {
  key: string
  title: string
  description: string
  items: Array<{
    key: string
    label: string
    hint: string
    type: 'switch' | 'number' | 'select' | 'text'
    value: string | number | boolean
    options?: string[]
    unit?: string
  }>
}

/* ─────────────── 业务状态 → UI 状态的映射 ───────────────
   接真实后端后，契约里的 statusKind / kind 字段应当删除，改由这里映射。
   放在契约层是因为"哪种业务状态算危急"是产品定义，不是某个页面的私事。 */

export function deploymentUiStatus(status: DeploymentStatus): UiStatus {
  switch (status) {
    case 'running':
      return 'good'
    case 'canary':
      return 'warning'
    case 'failed':
      return 'critical'
    case 'starting':
      return 'idle'
  }
}

export function modelUiStatus(status: ModelStatus): UiStatus {
  switch (status) {
    case 'listed':
      return 'good'
    case 'canary':
      return 'warning'
    default:
      return 'idle'
  }
}

export function invoiceUiStatus(status: Invoice['status']): UiStatus {
  return status === 'paid' ? 'good' : status === 'pending' ? 'warning' : 'critical'
}

/** GPU 由硬件计数派生 UI 状态：ECC 双位错误即硬件故障，高温为告警 */
export function gpuUiStatus(card: Pick<GpuCard, 'eccDoubleBit' | 'tempC' | 'util'>): UiStatus {
  if (card.eccDoubleBit > 0) return 'critical'
  if (card.tempC >= 85) return 'warning'
  return card.util > 0 ? 'good' : 'idle'
}

/* ─────────────── 产品定义（label / 权限矩阵） ─────────────── */

export const DIMENSION_LABEL: Record<Dimension, string> = {
  vendor: '按厂商',
  model: '按模型',
  userGroup: '按用户组',
  app: '按应用',
}

export const MEASURE_LABEL: Record<Measure, string> = {
  tokens: 'Tokens',
  requests: '请求数',
  cost: '成本',
  toolCalls: '工具调用',
}

export const MODEL_STATUS_LABEL: Record<ModelStatus, string> = {
  listed: '上架',
  canary: '灰度',
  delisted: '下架',
  draft: '草稿',
}

export const MODEL_STATUS_KIND: Record<ModelStatus, UiStatus> = {
  listed: 'good',
  canary: 'warning',
  delisted: 'idle',
  draft: 'idle',
}

export const ROLE_LABEL: Record<AdminRole, string> = {
  'super-admin': '超级管理员',
  operator: '运维',
  viewer: '只读',
}

/** 角色能力矩阵。写清楚而不是让人猜「运维」到底能干什么 */
export const ROLE_MATRIX: Array<{ capability: string; roles: Record<AdminRole, boolean> }> = [
  { capability: '查看数据分析', roles: { 'super-admin': true, operator: true, viewer: true } },
  { capability: '查看 GPU 监控', roles: { 'super-admin': true, operator: true, viewer: true } },
  { capability: '模型上下架 / 应用上游变更', roles: { 'super-admin': true, operator: true, viewer: false } },
  { capability: '渠道配置与路由权重', roles: { 'super-admin': true, operator: true, viewer: false } },
  { capability: '创建 / 删除部署实例', roles: { 'super-admin': true, operator: true, viewer: false } },
  { capability: '用户封禁与配额调整', roles: { 'super-admin': true, operator: false, viewer: false } },
  { capability: '账务、退款与发票', roles: { 'super-admin': true, operator: false, viewer: false } },
  { capability: '管理员账号与角色', roles: { 'super-admin': true, operator: false, viewer: false } },
]
