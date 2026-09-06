import type { CatalogModel, Channel, DiffEntry, RoutingPolicy } from '../api/contracts'
import { mockResponse, seedFrom, seededRandom } from './generators'

/* ─────────────── 模型目录 ─────────────── */




const MODELS: CatalogModel[] = [
  { id: 'openai/gpt-5.6-sol', name: 'GPT-5.6 SOL', vendor: 'OpenAI', hosting: 'cloud', status: 'listed', context: '400K', inputPrice: 1.25, outputPrice: 10, capabilities: ['Reasoning', 'Tools', 'Vision'], callsLast7d: 1_410_000 },
  { id: 'anthropic/claude-opus-5', name: 'Claude Opus 5', vendor: 'Anthropic', hosting: 'cloud', status: 'listed', context: '1M', inputPrice: 5, outputPrice: 25, capabilities: ['Reasoning', 'Coding', 'Tools'], callsLast7d: 980_000 },
  { id: 'google/gemini-3.8-flash', name: 'Gemini 3.8 Flash', vendor: 'Google', hosting: 'cloud', status: 'listed', context: '2M', inputPrice: 0.35, outputPrice: 1.4, capabilities: ['Vision', 'Tools'], callsLast7d: 1_220_000 },
  { id: 'deepseek/deepseek-v4', name: 'DeepSeek V4', vendor: 'DeepSeek', hosting: 'cloud', status: 'listed', context: '256K', inputPrice: 0.28, outputPrice: 1.1, capabilities: ['Reasoning', 'Coding'], callsLast7d: 486_000 },
  { id: 'selfhosted/qwen3-max', name: 'Qwen3 Max', vendor: '自建', hosting: 'self', status: 'canary', canaryPercent: 10, context: '256K', inputPrice: null, outputPrice: null, capabilities: ['Reasoning', 'Tools'], callsLast7d: 12_400 },
  { id: 'selfhosted/qwen3-32b', name: 'Qwen3 32B Instruct', vendor: '自建', hosting: 'self', status: 'listed', context: '128K', inputPrice: null, outputPrice: null, capabilities: ['Coding'], callsLast7d: 84_200 },
  { id: 'google/gemini-3.8-pro', name: 'Gemini 3.8 Pro', vendor: 'Google', hosting: 'cloud', status: 'delisted', context: '2M', inputPrice: 2.5, outputPrice: 15, capabilities: ['Reasoning', 'Vision'], callsLast7d: 0 },
  { id: 'openai/gpt-4.1-legacy', name: 'GPT-4.1 Legacy', vendor: 'OpenAI', hosting: 'cloud', status: 'listed', context: '128K', inputPrice: 2, outputPrice: 8, capabilities: ['Tools'], callsLast7d: 2_400 },
  { id: 'zhipu/glm-5.3', name: 'GLM 5.3', vendor: '智谱', hosting: 'cloud', status: 'listed', context: '200K', inputPrice: 0.42, outputPrice: 1.68, capabilities: ['Reasoning'], callsLast7d: 164_000 },
  { id: 'minimax/minimax-m3', name: 'MiniMax M3', vendor: 'MiniMax', hosting: 'cloud', status: 'draft', context: '192K', inputPrice: null, outputPrice: null, capabilities: ['Tools'], callsLast7d: 0 },
]

export function catalogModels() {
  return MODELS
}

export function catalogSummary() {
  return {
    total: MODELS.length,
    listed: MODELS.filter((model) => model.status === 'listed').length,
    canary: MODELS.filter((model) => model.status === 'canary').length,
    delisted: MODELS.filter((model) => model.status === 'delisted').length,
    draft: MODELS.filter((model) => model.status === 'draft').length,
  }
}

/* ─────────────── 上游同步的差异 ─────────────── */



export function upstreamDiff(source: string): { source: string; fetchedAt: string; entries: DiffEntry[] } {
  return {
    source,
    fetchedAt: '2 分钟前',
    entries: [
      {
        id: 'add-gpt-5.7',
        kind: 'added',
        modelId: 'openai/gpt-5.7-preview',
        name: 'GPT-5.7 Preview',
        missing: ['输入价格', '输出价格', '能力标签'],
        summary: '上下文 400K · 上游新发布',
      },
      {
        id: 'add-o5-mini',
        kind: 'added',
        modelId: 'openai/o5-mini',
        name: 'o5-mini',
        missing: ['输入价格', '输出价格'],
        summary: '上下文 200K · 上游新发布',
      },
      {
        id: 'chg-gpt-5.6-price',
        kind: 'changed',
        modelId: 'openai/gpt-5.6-sol',
        name: 'GPT-5.6 SOL',
        changes: [{ field: '输入价格', before: '$1.25', after: '$1.10' }],
        summary: '降价 12%，将直接影响线上计费',
      },
      {
        id: 'chg-opus-context',
        kind: 'changed',
        modelId: 'anthropic/claude-opus-5',
        name: 'Claude Opus 5',
        changes: [{ field: '上下文窗口', before: '1M', after: '2M' }],
        summary: '上下文翻倍',
      },
      {
        id: 'rm-gpt-4.1',
        kind: 'removed',
        modelId: 'openai/gpt-4.1-legacy',
        name: 'GPT-4.1 Legacy',
        callsLast7d: 2_400,
        summary: '上游已移除该模型',
      },
    ],
  }
}

/* ─────────────── 渠道 ─────────────── */


export function channels(): Channel[] {
  return [
    { id: 'openai-main', name: 'OpenAI 主', type: '官方', region: '全球', priority: 1, weight: 60, status: 'good', statusLabel: '正常', p95: 412, errorRate: 0.31, costToday: 1204, models: 18 },
    { id: 'openai-azure', name: 'OpenAI 备', type: 'Azure', region: '东亚', priority: 2, weight: 40, status: 'good', statusLabel: '正常', p95: 468, errorRate: 0.42, costToday: 318, models: 16 },
    { id: 'anthropic-main', name: 'Anthropic 主', type: '官方', region: '全球', priority: 1, weight: 100, status: 'serious', statusLabel: '降级', degradedReason: '5 分钟错误率 12.4% 超过 5% 阈值，已自动降权并切至备用渠道', p95: 1240, errorRate: 12.4, costToday: 902, models: 6 },
    { id: 'google-main', name: 'Google 主', type: '官方', region: '全球', priority: 1, weight: 100, status: 'good', statusLabel: '正常', p95: 388, errorRate: 0.19, costToday: 418, models: 9 },
    { id: 'self-vllm', name: '自建 vLLM', type: '私有', region: '本地机房', priority: 1, weight: 100, status: 'good', statusLabel: '正常', p95: 96, errorRate: 0.02, costToday: 0, models: 3 },
  ]
}

/* ─────────────── 路由策略 ─────────────── */


export function routingPolicies(): RoutingPolicy[] {
  return [
    { id: 'gpt-5.6-sol', model: 'GPT-5.6 SOL', strategy: '均衡', fallbackChain: ['OpenAI 主', 'OpenAI 备'], regionPinned: false },
    { id: 'claude-opus-5', model: 'Claude Opus 5', strategy: '质量优先', fallbackChain: ['Anthropic 主'], regionPinned: false },
    { id: 'gemini-3.8-flash', model: 'Gemini 3.8 Flash', strategy: '延迟优先', fallbackChain: ['Google 主', 'OpenAI 备'], regionPinned: true },
    { id: 'qwen3-max', model: 'Qwen3 Max（自建）', strategy: '成本优先', fallbackChain: ['自建 vLLM', 'DeepSeek 主'], canary: { channel: '自建 vLLM', percent: 10 }, regionPinned: true },
  ]
}

/** 渠道连通性测试 —— 发一条真实请求，返回延迟与原始响应 */
export async function testChannel(channelId: string) {
  const random = seededRandom(seedFrom(channelId) + Date.now())
  await new Promise((resolve) => setTimeout(resolve, 600 + random() * 900))
  const failed = random() < 0.2
  if (failed) {
    return {
      ok: false as const,
      latency: Math.round(900 + random() * 2000),
      detail: 'upstream returned 429: rate limit exceeded for organization',
    }
  }
  return {
    ok: true as const,
    latency: Math.round(80 + random() * 380),
    detail: '{"id":"chatcmpl-…","object":"chat.completion","choices":[{"finish_reason":"stop"}]}',
  }
}

export const catalogMock = {
  models: () => mockResponse({ models: catalogModels(), summary: catalogSummary() }, 'catalog-models'),
  diff: (source: string) => mockResponse(upstreamDiff(source), `diff-${source}`),
  channels: () => mockResponse({ channels: channels() }, 'channels'),
  routing: () => mockResponse({ policies: routingPolicies(), channels: channels() }, 'routing'),
}
