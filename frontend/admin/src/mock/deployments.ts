import type { Deployment, PreflightCheck } from '../api/contracts'
import { mockResponse } from './generators'



export function deployments(): Deployment[] {
  return [
    {
      id: 'qwen3-72b-prod',
      name: 'qwen3-72b-prod',
      model: 'Qwen3-72B-Instruct',
      engine: 'vLLM 0.9',
      node: 'node-01',
      gpus: [0, 1, 2, 3],
      tensorParallel: 4,
      precision: 'FP16',
      status: 'running',
      statusKind: 'good',
      statusLabel: '健康',
      replicas: { ready: 2, desired: 2 },
      qps: 42,
      p95: 890,
      memUsedGb: 64.5,
      memTotalGb: 80,
      kvHitRate: 71,
    },
    {
      id: 'deepseek-v4-canary',
      name: 'deepseek-v4-canary',
      model: 'DeepSeek-V4',
      engine: 'SGLang 0.5',
      node: 'node-02',
      gpus: [0, 1, 2, 3, 4, 5, 6, 7],
      tensorParallel: 8,
      precision: 'FP8',
      status: 'canary',
      statusKind: 'warning',
      statusLabel: '灰度 10%',
      replicas: { ready: 1, desired: 1 },
      qps: 4,
      p95: 1120,
      memUsedGb: 288,
      memTotalGb: 320,
      kvHitRate: 44,
      canaryPercent: 10,
    },
    {
      id: 'qwen3-32b-prod',
      name: 'qwen3-32b-prod',
      model: 'Qwen3-32B-Instruct',
      engine: 'vLLM 0.9',
      node: 'node-01',
      gpus: [4, 5],
      tensorParallel: 2,
      precision: 'FP16',
      status: 'running',
      statusKind: 'good',
      statusLabel: '健康',
      replicas: { ready: 2, desired: 2 },
      qps: 18,
      p95: 412,
      memUsedGb: 38.2,
      memTotalGb: 80,
      kvHitRate: 66,
    },
    {
      id: 'glm-5-9b-test',
      name: 'glm-5-9b-test',
      model: 'GLM-5-9B',
      engine: 'vLLM 0.9',
      node: 'node-04',
      gpus: [2],
      tensorParallel: 1,
      precision: 'FP16',
      status: 'failed',
      statusKind: 'critical',
      statusLabel: '启动失败',
      replicas: { ready: 0, desired: 1 },
      qps: 0,
      p95: 0,
      memUsedGb: 0,
      memTotalGb: 40,
      failureReason:
        'CUDA out of memory：请求 24.0 GB，设备可用 18.2 GB。该卡已因 ECC 双位错误被隔离，请改选其他 GPU。',
    },
  ]
}

/** 预检项 —— 部署向导的最后一步，不是直接启动 */

export function preflight(model: string, node: string, tp: number): PreflightCheck[] {
  return [
    {
      id: 'weights',
      kind: 'good',
      label: '权重完整性',
      detail: `${model} 全部分片就位，SHA256 校验通过`,
      blocking: false,
    },
    {
      id: 'gpu',
      kind: 'good',
      label: 'GPU 可用',
      detail: `${node} 有 ${tp} 张空闲卡，NVLink 全互联`,
      blocking: false,
    },
    {
      id: 'memory',
      kind: 'good',
      label: '显存充足',
      detail: '预计单卡 47.7 GB / 80 GB（60%），余量 32.3 GB',
      blocking: false,
    },
    {
      id: 'port',
      kind: 'good',
      label: '端口可用',
      detail: '8081 未被占用',
      blocking: false,
    },
    {
      id: 'tp',
      kind: 'warning',
      label: 'TP 约束',
      detail: `TP=${tp} 可整除 KV 头数 8 与注意力头数 64`,
      blocking: false,
    },
    {
      id: 'coldstart',
      kind: 'warning',
      label: '首次启动',
      detail: '权重加载预计 3–5 分钟，期间健康检查会持续失败，属正常现象',
      blocking: false,
    },
  ]
}

/** 部署日志流。加载阶段刻意不给百分比 —— 权重加载没有可靠进度可报 */
export const DEPLOY_LOG_LINES = [
  { level: 'info', text: 'creating deployment qwen3-72b-prod (tp=4, precision=FP16)' },
  { level: 'info', text: 'allocating GPU 0,1,2,3 on node-01' },
  { level: 'info', text: 'pulling container image vllm/vllm-openai:0.9.0' },
  { level: 'info', text: 'image already present, skipping pull' },
  { level: 'info', text: 'starting engine process (pid 48213)' },
  { level: 'info', text: 'INFO 09-06 17:22:01 llm_engine.py:184] Initializing LLM engine' },
  { level: 'info', text: 'INFO 09-06 17:22:03 utils.py:608] Found nccl from library libnccl.so.2' },
  { level: 'info', text: 'INFO 09-06 17:22:05 selector.py:81] Using FlashAttention-2 backend' },
  { level: 'info', text: 'loading safetensors shards ... (no reliable progress available)' },
  { level: 'info', text: 'INFO 09-06 17:23:44 model_runner.py:1024] Loading model weights took 135.4062 GB' },
  { level: 'info', text: 'INFO 09-06 17:24:12 gpu_executor.py:122] # GPU blocks: 14208, # CPU blocks: 2048' },
  { level: 'info', text: 'INFO 09-06 17:24:13 model_runner.py:1329] Capturing CUDA graphs' },
  { level: 'warn', text: 'WARN 09-06 17:24:31 config.py:412] max_model_len 131072 exceeds KV budget, clamped to 32768' },
  { level: 'info', text: 'INFO 09-06 17:24:48 api_server.py:206] Started server at http://0.0.0.0:8081' },
  { level: 'ok', text: 'health check passed, registering into routing table' },
  { level: 'ok', text: 'deployment qwen3-72b-prod is ready (took 3m 12s)' },
]

export const deploymentsMock = {
  list: () => mockResponse({ deployments: deployments() }, 'deployments'),
}
