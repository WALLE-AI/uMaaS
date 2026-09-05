export type Model = {
  id: string
  name: string
  maker: string
  initials: string
  logo: string
  color: string
  summary: string
  context: string
  input: number
  output: number
  speed: number
  quality: number
  usage: string
  released: string
  tags: string[]
}

export const models: Model[] = [
  { id: 'google/gemini-3.8-flash', name: 'Gemini 3.8 Flash', maker: 'Google', initials: 'G', logo: '/logos/google.svg', color: '#4285f4', summary: '高吞吐多模态模型，面向实时代理、搜索和长上下文工作流。', context: '1M', input: 0.5, output: 2.2, speed: 318, quality: 86, usage: '724B', released: 'Sep 2, 2026', tags: ['Vision', 'Tools', 'Reasoning'] },
  { id: 'openai/gpt-5.6-sol', name: 'GPT-5.6 SOL', maker: 'OpenAI', initials: 'O', logo: '/logos/openai.svg', color: '#111827', summary: '面向复杂工程任务的高可靠推理模型，支持结构化输出与工具调用。', context: '400K', input: 2.5, output: 12, speed: 142, quality: 96, usage: '1.9T', released: 'Aug 28, 2026', tags: ['Reasoning', 'Tools'] },
  { id: 'anthropic/claude-opus-5', name: 'Claude Opus 5', maker: 'Anthropic', initials: 'A', logo: '/logos/anthropic.svg', color: '#d97757', summary: '专注深度分析、代码理解和长周期智能体任务。', context: '500K', input: 4, output: 18, speed: 88, quality: 94, usage: '612B', released: 'Aug 21, 2026', tags: ['Coding', 'Agents'] },
  { id: 'deepseek/deepseek-v4', name: 'DeepSeek V4', maker: 'DeepSeek', initials: 'D', logo: '/logos/deepseek.png', color: '#4f46e5', summary: '兼顾中文、数学和编程能力的高性价比推理模型。', context: '256K', input: 0.28, output: 0.9, speed: 201, quality: 91, usage: '11.9T', released: 'Jul 31, 2026', tags: ['Coding', 'Reasoning'] },
  { id: 'zhipu/glm-5.3-flash', name: 'GLM 5.3 Flash', maker: '智谱', initials: 'Z', logo: '/logos/zhipu.png', color: '#10b981', summary: '为中文业务场景和工具型 Agent 优化的快速模型。', context: '200K', input: 0.16, output: 0.62, speed: 276, quality: 83, usage: '12.3T', released: 'Aug 12, 2026', tags: ['Chinese', 'Tools'] },
  { id: 'alibaba/qwen-3.8-max', name: 'Qwen 3.8 Max', maker: '阿里云', initials: 'Q', logo: '/logos/qwen.png', color: '#7c3aed', summary: '覆盖文本、视觉与代码的通用旗舰模型。', context: '256K', input: 0.9, output: 3.5, speed: 185, quality: 89, usage: '12.7B', released: 'Sep 2, 2026', tags: ['Vision', 'Coding'] },
  { id: 'microsoft/mai-transcribe-2', name: 'MAI Transcribe 2', maker: 'Microsoft', initials: 'M', logo: '/logos/microsoft.svg', color: '#0078d4', summary: '低延迟多语言语音识别，支持时间戳和说话人区分。', context: 'Audio', input: 0.12, output: 0, speed: 340, quality: 84, usage: '96.3M', released: 'Sep 1, 2026', tags: ['Audio', 'Realtime'] },
  { id: 'minimax/minimax-m3', name: 'MiniMax M3', maker: 'MiniMax', initials: 'M', logo: '/logos/minimax.png', color: '#ef4444', summary: '面向消费级应用和内容生产的长上下文模型。', context: '1M', input: 0.22, output: 1.1, speed: 224, quality: 82, usage: '5.48T', released: 'Aug 18, 2026', tags: ['Long context', 'Creative'] },
]

export const rankSections = ['Top Models', 'Leaderboard', 'Top models by task', 'Cost per session', 'Market Share', 'Benchmarks', 'Fastest models', 'Languages', 'Programming', 'Context Length', 'Tool Calls', 'Images', 'Top Apps']

export const benchmarkRows = [
  ['TerminalBench 2.0', '智能体', 'GPT-5.6 SOL', '72.4'],
  ['SWE-bench Verified', '编程', 'Claude Opus 5', '81.7'],
  ['LiveBench', '综合推理', 'GPT-5.6 SOL', '76.9'],
  ['MMLU-Pro', '知识', 'Gemini 3.8 Flash', '89.2'],
  ['AIME 2026', '数学', 'DeepSeek V4', '91.0'],
]
