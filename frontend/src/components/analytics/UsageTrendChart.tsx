import {
  Area,
  AreaChart,
  CartesianGrid,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import type { UsageDatum } from './types'

const series = [
  { key: 'openai', label: 'OpenAI', color: '#f05aa8' },
  { key: 'google', label: 'Google', color: '#7c4dff' },
  { key: 'anthropic', label: 'Anthropic', color: '#23b5d3' },
  { key: 'deepseek', label: 'DeepSeek', color: '#8dc63f' },
  { key: 'others', label: '其他', color: '#ff9947' },
] as const

function UsageTooltip({ active, payload, label }: any) {
  if (!active || !payload?.length) return null
  const total = payload.reduce((sum: number, item: { value?: number }) => sum + (item.value || 0), 0)
  return (
    <div className="analytics-tooltip">
      <span>{label} · 周使用量</span>
      {payload.slice().reverse().map((item: { dataKey: string; color: string; value: number }) => {
        const definition = series.find((entry) => entry.key === item.dataKey)
        return <div key={item.dataKey}><i style={{ background: item.color }} /><b>{definition?.label}</b><em>{item.value.toFixed(1)}T</em></div>
      })}
      <div className="tooltip-total"><b>合计</b><em>{total.toFixed(1)}T</em></div>
    </div>
  )
}

export function UsageTrendChart({ data, scale }: { data: UsageDatum[]; scale: '线性' | '对数' }) {
  if (!data.length) return <div className="analytics-empty"><b>暂无趋势数据</b><span>更换时间范围后重试。</span></div>

  return (
    <div role="img" aria-label="模型供应商每周使用量堆叠趋势图">
      <div className="stacked-chart">
        <ResponsiveContainer width="100%" height={390}>
          <AreaChart data={data} margin={{ left: 4, right: 10, top: 20 }}>
            <CartesianGrid vertical={false} stroke="#ececf0" />
            <XAxis dataKey="date" tick={{ fontSize: 11 }} axisLine={false} tickLine={false} />
            <YAxis scale={scale === '对数' ? 'log' : 'auto'} domain={scale === '对数' ? [1, 'auto'] : [0, 'auto']} tick={{ fontSize: 11 }} axisLine={false} tickLine={false} tickFormatter={(value) => `${Math.round(value)}T`} />
            <Tooltip content={<UsageTooltip />} cursor={{ stroke: '#9b78ec', strokeDasharray: '3 3' }} />
            {series.map((item) => <Area key={item.key} stackId="usage" dataKey={item.key} stroke={item.color} fill={item.color} fillOpacity={0.82} strokeWidth={1.5} isAnimationActive={false} />)}
          </AreaChart>
        </ResponsiveContainer>
      </div>
      <div className="chart-legend">{series.map((item) => <span key={item.key}><i style={{ background: item.color }} />{item.label}</span>)}</div>
    </div>
  )
}
