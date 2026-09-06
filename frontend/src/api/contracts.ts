export type Id = string
export type ISODateTime = string
export type ModelModality = 'text' | 'image' | 'video' | 'audio' | 'embedding' | 'rerank'
export type SortDirection = 'asc' | 'desc'

export type ApiEnvelope<T> = {
  data: T
  request_id: string
  meta?: PageMeta
}

export type PageMeta = {
  next_cursor: string | null
  total?: number
}

export type ApiErrorBody = {
  error: {
    code: string
    message: string
    field?: string
    details?: Record<string, unknown>
  }
  request_id: string
}

export type ProviderSummary = {
  id: Id
  name: string
  slug: string
  logo_url: string
  model_count: number
}

export type ModelSummary = {
  id: Id
  slug: string
  name: string
  provider: ProviderSummary
  description: string
  logo_url: string
  modalities: ModelModality[]
  capabilities: string[]
  context_length: number | null
  pricing: {
    currency: 'USD'
    input_per_million: number | null
    output_per_million: number | null
  }
  performance: {
    throughput_tokens_per_second: number | null
    quality_score: number | null
  }
  usage_tokens_30d: number
  released_at: ISODateTime
}

export type ModelEndpoint = {
  id: Id
  provider: ProviderSummary
  region: string
  latency_ms_p50: number
  latency_ms_p95: number
  throughput_tokens_per_second: number
  input_per_million: number | null
  output_per_million: number | null
  uptime_30d: number
  status: 'healthy' | 'degraded' | 'offline'
  supports_zero_data_retention: boolean
}

export type TimeSeriesPoint = {
  timestamp: ISODateTime
  value: number
}

export type ModelDetail = ModelSummary & {
  architecture: string | null
  input_formats: string[]
  output_formats: string[]
  supported_parameters: string[]
  endpoints: ModelEndpoint[]
  uptime: TimeSeriesPoint[]
  benchmark_scores: BenchmarkScore[]
  top_apps: AppUsage[]
  recent_activity: ActivityItem[]
  faq: FaqItem[]
  related_models: ModelSummary[]
}

export type ModelListParams = {
  q?: string
  provider?: string
  modality?: ModelModality[]
  capability?: string[]
  min_context_length?: number
  max_input_price?: number
  zero_data_retention?: boolean
  sort?: 'released_at' | 'price' | 'throughput' | 'usage'
  direction?: SortDirection
  cursor?: string
  limit?: number
}

export type CatalogSummary = {
  model_count: number
  provider_count: number
  monthly_tokens: number
  gateway_latency_ms_p50: number
  featured_models: ModelSummary[]
  updated_at: ISODateTime
}

export type BenchmarkMetricWinner = {
  model: ModelSummary
  value: number
  display_value: string
}

export type BenchmarkSummary = {
  id: Id
  slug: string
  name: string
  category: 'agent' | 'reasoning' | 'search' | 'coding' | 'multimodal'
  description: string
  evaluated_model_count: number
  run_count: number
  last_run_at: ISODateTime
  winners: {
    quality: BenchmarkMetricWinner
    value: BenchmarkMetricWinner
    speed: BenchmarkMetricWinner
  }
}

export type BenchmarkScore = {
  benchmark_id: Id
  benchmark_name: string
  score: number
  percentile?: number
}

export type BenchmarkDetail = BenchmarkSummary & {
  methodology: string
  configuration: Record<string, unknown>
  results: Array<{
    rank: number
    model: ModelSummary
    score: number
    cost_usd: number
    duration_ms: number
    sample_count: number
  }>
}

export type RankingDimension =
  | 'top-models'
  | 'leaderboard'
  | 'task'
  | 'session-cost'
  | 'market-share'
  | 'benchmarks'
  | 'speed'
  | 'languages'
  | 'programming'
  | 'context-length'
  | 'tool-calls'
  | 'images'
  | 'apps'

export type RankingParams = {
  dimension: RankingDimension
  modality?: ModelModality
  period?: 'day' | 'week' | 'month'
  unit?: 'absolute' | 'percentage'
  scope?: 'all' | 'open-source'
}

export type RankingEntry = {
  rank: number
  key: string
  label: string
  value: number
  display_value: string
  change_percent: number | null
  model?: ModelSummary
}

export type RankingResponse = {
  dimension: RankingDimension
  title: string
  description: string
  entries: RankingEntry[]
  series?: Array<{ key: string; label: string; points: TimeSeriesPoint[] }>
  updated_at: ISODateTime
}

export type AppUsage = {
  id: Id
  name: string
  logo_url?: string
  usage_tokens: number
}

export type ActivityItem = {
  id: Id
  type: 'status' | 'pricing' | 'release' | 'routing'
  title: string
  occurred_at: ISODateTime
}

export type FaqItem = { question: string; answer: string }

export type DocsNavigationGroup = {
  title: string
  items: Array<{ slug: string; title: string; badge?: string }>
}

export type DocsPage = {
  slug: string
  title: string
  description: string
  body_markdown: string
  table_of_contents: Array<{ id: string; title: string; level: number }>
  updated_at: ISODateTime
}

export type HarnessDefinition = {
  id: string
  name: string
  description: string
  install_template: string
  supports_fallback: boolean
}

export type HarnessConfigRequest = {
  harness_id: string
  model_id: string
  daily_budget_usd: number
  fallback_enabled: boolean
}

export type HarnessConfigResponse = {
  install_command: string
  filename: string
  configuration: Record<string, unknown>
}

export type User = {
  id: Id
  name: string
  email: string
  avatar_url?: string
  workspace_id: Id
}

export type AuthSession = { user: User; expires_at: ISODateTime }
export type LoginRequest = { email: string; password: string; remember: boolean }
export type RegisterRequest = { name: string; email: string; password: string; accepted_terms: true }
