import type { AdminAccount, AuditEntry, SettingGroup } from '../api/contracts'
import { mockResponse } from './generators'



export function adminAccounts(): AdminAccount[] {
  return [
    { id: 'a1', name: '平台管理员', email: 'admin@umaas.dev', role: 'super-admin', twoFactor: true, lastActive: '当前在线', status: 'good' },
    { id: 'a2', name: '运维-张岭', email: 'zhang@umaas.dev', role: 'operator', twoFactor: true, lastActive: '2 小时前', status: 'good' },
    { id: 'a3', name: '运维-李川', email: 'li@umaas.dev', role: 'operator', twoFactor: false, lastActive: '1 天前', status: 'warning' },
    { id: 'a4', name: '财务-王荷', email: 'wang@umaas.dev', role: 'viewer', twoFactor: true, lastActive: '3 天前', status: 'good' },
  ]
}


export function auditLog(): AuditEntry[] {
  return [
    { id: 'au-1042', at: '2026-09-06 09:31', actor: 'admin@umaas.dev', action: '应用上游变更', target: 'openai/gpt-5.6-sol', destructive: false, detail: '输入价格 $1.25 → $1.10（来源 OpenAI /v1/models）', ip: '10.2.4.18' },
    { id: 'au-1041', at: '2026-09-06 09:12', actor: 'system', action: '自动驱逐实例', target: 'qwen3-32b-prod @ node-04', destructive: true, detail: 'GPU2 触发 XID 48 ECC 双位错误，实例迁移至 node-01', ip: '—' },
    { id: 'au-1040', at: '2026-09-06 08:47', actor: 'zhang@umaas.dev', action: '渠道降权', target: 'anthropic-main', destructive: false, detail: '权重 100 → 40，原因：错误率 12.4%', ip: '10.2.4.31' },
    { id: 'au-1039', at: '2026-09-05 17:20', actor: 'admin@umaas.dev', action: '删除部署实例', target: 'glm-5-9b-test', destructive: true, detail: '实例启动失败后手工清理，释放 node-04 GPU2', ip: '10.2.4.18' },
    { id: 'au-1038', at: '2026-09-05 14:22', actor: 'zhang@umaas.dev', action: '发起压测', target: 'qwen3-72b-prod', destructive: false, detail: '并发爬坡 1→128，SLO TTFT P95 ≤ 500ms', ip: '10.2.4.31' },
    { id: 'au-1037', at: '2026-09-04 11:03', actor: 'admin@umaas.dev', action: '模型下架', target: 'google/gemini-3.8-pro', destructive: true, detail: '下架前近 7 日调用 0 次，已发布公告', ip: '10.2.4.18' },
    { id: 'au-1036', at: '2026-09-03 16:38', actor: 'li@umaas.dev', action: '拉取模型权重', target: 'Qwen/Qwen3-72B-Instruct', destructive: false, detail: '145.2 GB，来源 ModelScope 镜像，SHA256 校验通过', ip: '10.2.4.44' },
  ]
}


export function settings(): SettingGroup[] {
  return [
    {
      key: 'routing',
      title: '默认路由',
      description: '应用于未在模型上单独配置路由策略的请求。',
      items: [
        { key: 'strategy', label: '默认策略', hint: '新模型上架时的初始路由策略', type: 'select', value: '均衡', options: ['质量优先', '成本优先', '延迟优先', '均衡'] },
        { key: 'fallback', label: '自动故障转移', hint: '端点不可用时自动切换到回退链的下一个渠道', type: 'switch', value: true },
        { key: 'degradeThreshold', label: '自动降权阈值', hint: '5 分钟窗口错误率超过该值即自动降权', type: 'number', value: 5, unit: '%' },
      ],
    },
    {
      key: 'quota',
      title: '配额与限流',
      description: '平台级兜底限制，工作空间可在其下自行收紧。',
      items: [
        { key: 'rpm', label: '默认每分钟请求上限', hint: '新工作空间的初始 RPM', type: 'number', value: 1000, unit: 'RPM' },
        { key: 'negativeBalance', label: '允许余额为负继续调用', hint: '关闭后余额耗尽将立即拒绝请求。开启便于避免误伤，但会产生坏账', type: 'switch', value: true },
        { key: 'negativeLimit', label: '负余额上限', hint: '超过该额度后强制停止服务', type: 'number', value: 50, unit: 'USD' },
      ],
    },
    {
      key: 'data',
      title: '数据与隐私',
      description: '影响全平台用户，修改前需评估合规影响。',
      items: [
        { key: 'retention', label: '请求元数据保留', hint: '不含提示词与模型输出正文', type: 'number', value: 30, unit: '天' },
        { key: 'training', label: '允许供应商用于训练', hint: '默认关闭。开启会改变对全体用户的数据承诺', type: 'switch', value: false },
        { key: 'zdr', label: '零数据保留模式', hint: '仅对支持 ZDR 的供应商生效', type: 'switch', value: true },
      ],
    },
    {
      key: 'infra',
      title: '私有化部署',
      description: '自建集群的默认参数。',
      items: [
        { key: 'engine', label: '默认推理引擎', hint: '新建部署时的默认选择', type: 'select', value: 'vLLM 0.9', options: ['vLLM 0.9', 'SGLang 0.5'] },
        { key: 'gpuUtil', label: '默认显存利用率', hint: '传给引擎的 gpu_memory_utilization', type: 'number', value: 90, unit: '%' },
        { key: 'mirror', label: '模型下载镜像', hint: '优先使用的权重下载源', type: 'select', value: 'ModelScope 镜像', options: ['HuggingFace 官方', 'ModelScope 镜像', '内网缓存'] },
      ],
    },
  ]
}

export const systemMock = {
  settings: () => mockResponse({ groups: settings() }, 'sys-settings'),
  rbac: () => mockResponse({ accounts: adminAccounts() }, 'sys-rbac'),
  audit: () => mockResponse({ entries: auditLog() }, 'sys-audit'),
}
