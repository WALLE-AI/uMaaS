import type { DownloadTask, LocalModel, ResolvedRepo } from '../api/contracts'
import { mockResponse, seedFrom, seededRandom } from './generators'


export function localModels(): LocalModel[] {
  return [
    { id: 'qwen3-32b', name: 'Qwen3-32B-Instruct', paramsB: 32.5, precision: 'FP16', sizeGb: 64.2, verified: true, source: 'HuggingFace', deployments: 2, pulledAt: '2026-08-21' },
    { id: 'llama-4-70b', name: 'Llama-4-70B', paramsB: 70.6, precision: 'FP16', sizeGb: 141, verified: true, source: 'HuggingFace', deployments: 0, pulledAt: '2026-08-14' },
    { id: 'deepseek-v4-lite', name: 'DeepSeek-V4-Lite', paramsB: 16.4, precision: 'FP8', sizeGb: 16.8, verified: true, source: 'ModelScope', deployments: 1, pulledAt: '2026-09-01' },
    { id: 'glm-5-9b', name: 'GLM-5-9B', paramsB: 9.4, precision: 'FP16', sizeGb: 18.1, verified: false, source: '手工上传', deployments: 0, pulledAt: '2026-09-03' },
  ]
}


export function downloadTasks(): DownloadTask[] {
  return [
    { id: 'dl-qwen3-72b', repo: 'Qwen/Qwen3-72B-Instruct', sizeGb: 145.2, downloadedGb: 98.4, shards: 32, shardsDone: 21, speedMbps: 82, source: 'ModelScope 镜像', status: 'downloading' },
    { id: 'dl-deepseek-v4', repo: 'deepseek-ai/DeepSeek-V4', sizeGb: 312, downloadedGb: 34.2, shards: 68, shardsDone: 7, speedMbps: 45, source: 'HuggingFace', status: 'downloading' },
  ]
}

/**
 * 解析社区仓库。
 *
 * 「先解析后下载」是这个模块的关键设计：
 *  · 拿到 config.json 的层数/KV头数/头维度，直接喂给容量评估器，管理者不用手抄；
 *  · 门控仓库（Llama 系列等需接受协议）在这一步就暴露，而不是下到 90% 才 403；
 *  · 磁盘空间在这一步预检，避免下到一半撑爆盘。
 */

const KNOWN_REPOS: Record<string, Omit<ResolvedRepo, 'repo' | 'disk'>> = {
  'Qwen/Qwen3-72B-Instruct': {
    displayName: 'Qwen3-72B-Instruct',
    paramsB: 72.7,
    license: 'Apache-2.0',
    gated: false,
    format: 'safetensors',
    shards: 32,
    sizeGb: 145.2,
    config: { layers: 80, attnHeads: 64, kvHeads: 8, headDim: 128, maxContext: 131072 },
    revisions: ['main', 'v1.2', 'v1.1'],
  },
  'meta-llama/Llama-4-70B-Instruct': {
    displayName: 'Llama-4-70B-Instruct',
    paramsB: 70.6,
    license: 'Llama 4 Community License',
    gated: true,
    gatedReason: '该仓库为门控仓库，需先在 HuggingFace 上接受许可协议，并提供具备读权限的访问令牌',
    format: 'safetensors',
    shards: 30,
    sizeGb: 141,
    config: { layers: 80, attnHeads: 64, kvHeads: 8, headDim: 128, maxContext: 131072 },
    revisions: ['main'],
  },
  'deepseek-ai/DeepSeek-V4-Lite': {
    displayName: 'DeepSeek-V4-Lite',
    paramsB: 16.4,
    license: 'DeepSeek License',
    gated: false,
    format: 'safetensors',
    shards: 8,
    sizeGb: 32.8,
    config: { layers: 27, attnHeads: 32, kvHeads: 4, headDim: 128, maxContext: 65536 },
    revisions: ['main', 'v0.9'],
  },
}

export const REPO_SUGGESTIONS = Object.keys(KNOWN_REPOS)

export async function resolveRepo(repo: string): Promise<ResolvedRepo> {
  const random = seededRandom(seedFrom(repo))
  await new Promise((resolve) => setTimeout(resolve, 500 + random() * 700))

  const known = KNOWN_REPOS[repo]
  if (!known) {
    throw new Error(`无法解析 ${repo}：仓库不存在，或当前镜像源不可达`)
  }

  const availableGb = 5600
  return {
    ...known,
    repo,
    disk: {
      availableGb,
      requiredGb: known.sizeGb,
      sufficient: availableGb > known.sizeGb * 1.1,
    },
  }
}

export const registryMock = {
  list: () =>
    mockResponse(
      {
        models: localModels(),
        tasks: downloadTasks(),
        disk: { usedTb: 2.4, totalTb: 8 },
      },
      'registry',
    ),
}
