import { ApiOutlined, BranchesOutlined, CheckCircleOutlined, ToolOutlined } from '@ant-design/icons'
import { Alert } from 'antd'
import { StatTileRow } from '../../components/charts/StatTile'
import { EmptyState } from '../../components/common/EmptyState'
import { PageHeader } from '../../components/common/PageHeader'
import { Panel } from '../../components/common/Panel'
import { compactNumber, formatMetric, metrics } from '../../config/metrics'
import { sequentialColor, seriesColor } from '../../config/viz'

/** 工具调用量。单一色相 —— 类别之间没有顺序关系，不做「越大越深」的值阶映射 */
const TOP_TOOLS = [
  { id: 'read_file', calls: 4_820_000, successRate: 98.1, avgMs: 42 },
  { id: 'bash', calls: 3_610_000, successRate: 91.4, avgMs: 1200 },
  { id: 'web_search', calls: 2_440_000, successRate: 96.7, avgMs: 890 },
  { id: 'edit_file', calls: 1_980_000, successRate: 93.2, avgMs: 61 },
  { id: 'grep', calls: 1_520_000, successRate: 99.2, avgMs: 28 },
  { id: 'sql_query', calls: 940_000, successRate: 88.6, avgMs: 320 },
]

/** 调用链长度是有序区间 → 序数色阶，不是分类色 */
const CHAIN_LENGTH = [
  { bucket: '1 步', value: 42.1 },
  { bucket: '2–3 步', value: 31.6 },
  { bucket: '4–7 步', value: 18.4 },
  { bucket: '8+ 步', value: 7.9 },
]

export default function ToolsPage() {
  const maxCalls = Math.max(...TOP_TOOLS.map((tool) => tool.calls))

  return (
    <div>
      <PageHeader description="function calling 与 MCP 维度的调用分析。判断用户在拿模型做什么。" />

      <StatTileRow
        items={[
          {
            label: '工具调用数',
            value: '18.4M',
            note: '近 30 天',
            delta: { value: '22.6%', direction: 'up', good: true },
            icon: <ToolOutlined />,
            caveat: metrics.toolCalls.caveat,
          },
          { label: '平均每会话', value: '3.7', note: '次调用', icon: <BranchesOutlined /> },
          {
            label: '调用成功率',
            value: '94.2%',
            note: '客户端已回传 tool_result',
            icon: <CheckCircleOutlined />,
            caveat: metrics.toolSuccessRate.caveat,
          },
          { label: '含工具的请求', value: '38.1%', note: '占全部请求', icon: <ApiOutlined /> },
        ]}
      />

      <div className="mb-4 grid gap-4 lg:grid-cols-[minmax(0,1.4fr)_minmax(280px,0.6fr)]">
        <Panel
          eyebrow="TOP TOOLS"
          title="调用量 Top 工具"
          description="同一度量用同一色相，不做「越大越深」的值阶映射"
        >
          <div className="flex flex-col gap-3">
            {TOP_TOOLS.map((tool) => (
              <div key={tool.id} className="flex items-center gap-3 text-[11px]">
                <code className="w-28 shrink-0 truncate font-mono text-ink">{tool.id}</code>
                <i className="h-4 flex-1 overflow-hidden rounded bg-line-soft">
                  <em
                    className="block h-full rounded"
                    style={{
                      width: `${(tool.calls / maxCalls) * 100}%`,
                      background: seriesColor(1),
                    }}
                  />
                </i>
                <span className="w-14 shrink-0 text-right font-mono">
                  {compactNumber(tool.calls, 2)}
                </span>
                <span
                  className={`w-12 shrink-0 text-right font-mono ${
                    tool.successRate < 92 ? 'text-status-warning' : 'text-ink-muted'
                  }`}
                >
                  {tool.successRate}%
                </span>
                <span className="w-14 shrink-0 text-right font-mono text-ink-muted">
                  {formatMetric(tool.avgMs, 'ms')}
                </span>
              </div>
            ))}
          </div>
          <p className="mt-3 mb-0 text-[10px] text-ink-muted">
            列依次为：调用量 · 成功率 · 平均耗时。成功率低于 92% 的工具已标黄。
          </p>
        </Panel>

        <Panel
          eyebrow="CHAIN"
          title="调用链长度分布"
          description="有序区间用序数色阶"
        >
          <div className="flex flex-col gap-2.5">
            {CHAIN_LENGTH.map((item, index) => (
              <div key={item.bucket} className="flex items-center gap-3 text-[11px]">
                <span className="w-14 shrink-0 text-ink-secondary">{item.bucket}</span>
                <i className="h-5 flex-1 overflow-hidden rounded bg-line-soft">
                  <em
                    className="flex h-full items-center justify-end rounded pr-2 font-mono text-[10px] text-white"
                    style={{
                      width: `${item.value}%`,
                      background: sequentialColor(1 - index / CHAIN_LENGTH.length),
                    }}
                  >
                    {item.value}%
                  </em>
                </i>
              </div>
            ))}
          </div>
          <p className="mt-3 mb-0 text-[10px] text-ink-muted">
            8 步以上的长链占 7.9%，这部分请求的成本与失败率显著高于均值。
          </p>
        </Panel>
      </div>

      {/*
        这里不编数据。
        网关目前无法区分 Agent 框架（§8 指标字典已标注该维度需要新增埋点），
        与其画一张看起来合理、实则凭空捏造的饼图，不如把缺口明确摆出来 ——
        一个假的分布会被拿去做决策，而空状态不会。
      */}
      <Panel eyebrow="BY FRAMEWORK" title="按 Agent 框架分布">
        <Alert
          className="mb-4"
          type="info"
          showIcon
          message="该维度尚无数据来源"
          description={
            <span className="text-[11px]">
              网关当前不记录调用方的 Agent 框架。要区分 Claude Code / uCodex / OpenCode / Pi
              等客户端，需要在网关侧新增 User-Agent 或自定义请求头埋点，并由各 SDK
              配合上报。在埋点上线之前，本区块保持空白 —— 不用推测数据填充，
              以免被当作真实分布用于决策。
            </span>
          }
        />
        <EmptyState
          title="等待网关埋点接入"
          description="埋点方案确认后，此处将展示各 Agent 框架的调用量、工具使用偏好与成功率对比。"
          compact
        />
      </Panel>
    </div>
  )
}
