import { Button, Tag } from 'antd'
import { MetricChart } from '../components/charts/MetricChart'
import { Meter } from '../components/charts/Meter'
import { StatusBadge } from '../components/common/StatusBadge'
import { dateRange, trendSeries } from '../mock'

/**
 * P0 地基自检页（/_selfcheck）。
 *
 * 不在导航里，但保留为常驻路由：每次改动 token 或样式层级后可以来这里一眼
 * 确认地基还成立。§10.P0 的五条验收标准中，1、2、4 在这里可视化验证。
 */
export default function SelfCheckPage() {
  const dates = dateRange(14)
  const openai = trendSeries({ seed: 'openai', length: 14, base: 820 })
  const google = trendSeries({ seed: 'google', length: 14, base: 540 })
  const anthropic = trendSeries({ seed: 'anthropic', length: 14, base: 410 })

  const stackData = dates.map((date, index) => ({
    date,
    openai: Math.round(openai[index]),
    google: Math.round(google[index]),
    anthropic: Math.round(anthropic[index]),
  }))

  const singleData = dates.map((date, index) => ({
    date,
    requests: Math.round(openai[index] + google[index]),
  }))

  return (
    <div className="pb-10">
      <h1 className="text-2xl font-semibold">P0 地基自检</h1>
      <p className="mt-1 text-xs text-ink-muted">
        本页不在导航中，用于验证 §10.P0 的验收标准。地基改动后回来看一眼。
      </p>

      <section className="mt-8">
        <h2 className="text-sm font-medium text-ink-secondary">
          验收 1 · Tailwind utility 能否覆盖 antd
        </h2>
        <p className="mt-1 text-xs text-ink-muted">
          按钮带 <code className="rounded bg-plane px-1 font-mono">className="bg-status-good"</code>。
          <b className="text-ink">左侧按钮显示为绿底即通过</b>；若仍是 antd 默认白底，说明
          StyleProvider 的 layer 失效，层序被破坏。
        </p>
        <div className="mt-3 flex items-center gap-2">
          <Button className="bg-status-good">已加 utility（应为绿底）</Button>
          <Button>对照组（antd 默认）</Button>
        </div>
      </section>

      <section className="mt-10">
        <h2 className="text-sm font-medium text-ink-secondary">验收 2 · 设计 token 单一来源</h2>
        <p className="mt-1 text-xs text-ink-muted">
          三个色块分别来自 utility 类、CSS 变量、antd token。改 index.css 的{' '}
          <code className="rounded bg-plane px-1 font-mono">--color-brand</code> 后三者必须同时变色。
        </p>
        <div className="mt-3 flex items-end gap-4">
          {[
            { node: <div className="h-12 w-12 rounded bg-brand" />, label: 'utility' },
            {
              node: <div className="h-12 w-12 rounded" style={{ background: 'var(--color-brand)' }} />,
              label: 'CSS 变量',
            },
            { node: <Button type="primary" className="h-12 w-12" />, label: 'antd token' },
          ].map((item) => (
            <div key={item.label} className="flex flex-col items-center gap-1">
              {item.node}
              <span className="text-[10px] text-ink-muted">{item.label}</span>
            </div>
          ))}
        </div>
      </section>

      <section className="mt-10">
        <h2 className="text-sm font-medium text-ink-secondary">分类色板 8 槽（§4.1 固定顺序）</h2>
        <div className="mt-3 flex gap-2">
          {[1, 2, 3, 4, 5, 6, 7, 8].map((slot) => (
            <div key={slot} className="flex flex-col items-center gap-1">
              <div
                className="h-10 w-10 rounded"
                style={{ background: `var(--color-series-${slot})` }}
              />
              <span className="font-mono text-[10px] text-ink-muted">{slot}</span>
            </div>
          ))}
        </div>
        <p className="mt-2 text-[11px] text-ink-muted">
          槽 3/4/5（青、黄、品红）在白底对比度低于 3:1，用到它们的图会自动强制提供数据表入口。
        </p>
      </section>

      <section className="mt-10">
        <h2 className="text-sm font-medium text-ink-secondary">状态色 · 三重编码（§4.2）</h2>
        <div className="mt-3 flex flex-wrap gap-5">
          <StatusBadge kind="good" />
          <StatusBadge kind="warning" label="温度偏高" />
          <StatusBadge kind="serious" label="降级中" />
          <StatusBadge kind="critical" label="ECC 双位错误" />
          <StatusBadge kind="idle" />
        </div>
        <p className="mt-2 text-[11px] text-ink-muted">
          颜色 + 图标 + 文字三通道，去掉颜色仍可读。
        </p>
      </section>

      <section className="mt-10">
        <h2 className="text-sm font-medium text-ink-secondary">
          验收 4 · MetricChart（多系列堆叠，自动图例 + 数据表）
        </h2>
        <div className="mt-3 rounded border border-line bg-surface p-4">
          <MetricChart
            form="area"
            data={stackData}
            xKey="date"
            unit="tokens"
            series={[
              { id: 'openai', label: 'OpenAI', slot: 1 },
              { id: 'google', label: 'Google', slot: 2 },
              { id: 'anthropic', label: 'Anthropic', slot: 3 },
            ]}
          />
        </div>
        <p className="mt-2 text-[11px] text-ink-muted">
          用到槽 3（青）→ 自动出现「查看数据表」入口。这是对比度缓解规则在起作用，不是可选装饰。
        </p>
      </section>

      <section className="mt-10">
        <h2 className="text-sm font-medium text-ink-secondary">单系列 · 品牌紫（白底 5.32:1）</h2>
        <div className="mt-3 rounded border border-line bg-surface p-4">
          <MetricChart
            form="line"
            data={singleData}
            xKey="date"
            unit="count"
            height={200}
            series={[{ id: 'requests', label: '请求数' }]}
            threshold={{ value: 1600, label: 'SLO' }}
          />
        </div>
        <p className="mt-2 text-[11px] text-ink-muted">
          单系列不出图例（标题已说明它是什么），阈值线为灰虚线。
        </p>
      </section>

      <section className="mt-10">
        <h2 className="text-sm font-medium text-ink-secondary">计量条 · 超限叠加纹理</h2>
        <div className="mt-3 grid max-w-md gap-4 rounded border border-line bg-surface p-4">
          <Meter label="显存水位 node-01" value={64.5} max={80} valueText="64.5 / 80 GB" />
          <Meter label="显存水位 node-02（超限）" value={76} max={80} valueText="76 / 80 GB" />
        </div>
        <p className="mt-2 text-[11px] text-ink-muted">
          第二条超过 90% 阈值，叠加斜线纹理 —— 灰度打印下仍可分辨。
        </p>
      </section>

      <section className="mt-10">
        <h2 className="text-sm font-medium text-ink-secondary">验收 5 · 双轴防护</h2>
        <p className="mt-1 text-xs text-ink-muted">
          <Tag color="purple">类型层面</Tag>
          MetricChart 只有一个 <code className="rounded bg-plane px-1 font-mono">unit</code> 属性，
          所有 series 共用；没有第二根 Y 轴的入口，双轴图物理上写不出来。系列数超过 8 会直接抛错。
        </p>
      </section>
    </div>
  )
}
