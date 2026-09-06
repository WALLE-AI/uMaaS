import { ArrowRightOutlined, PushpinOutlined, SwapOutlined } from '@ant-design/icons'
import { Tag, Tooltip } from 'antd'
import { Link } from 'react-router'
import type { Model } from '../../data'
import { ModelLogo } from './ModelLogo'

type ModelRowProps = {
  model: Model
  compact?: boolean
  pinned?: boolean
  selected?: boolean
  onPin?: () => void
  onCompare?: () => void
}

export function ModelRow({
  model,
  compact = false,
  pinned = false,
  selected = false,
  onPin,
  onCompare,
}: ModelRowProps) {
  return (
    <article className={`model-row ${compact ? 'compact' : ''} ${selected ? 'selected' : ''}`}>
      <div className="model-main">
        <ModelLogo model={model} />
        <div>
          <div className="model-name">
            <h3>{model.name}</h3><span>by {model.maker}</span><em>{model.usage} tokens</em>
          </div>
          <p>{model.summary}</p>
          <div className="model-meta-line">
            <div className="tag-row">{model.tags.map((tag) => <Tag key={tag}>{tag}</Tag>)}</div>
            <span>{model.released}</span>
          </div>
        </div>
      </div>
      <div className="model-metrics">
        <div><small>上下文</small><b>{model.context}</b></div>
        <div><small>输入 / 1M</small><b>${model.input.toFixed(2)}</b></div>
        <div><small>输出 / 1M</small><b>{model.output ? `$${model.output.toFixed(2)}` : '—'}</b></div>
        <div><small>速度</small><b>{model.speed} t/s</b></div>
      </div>
      <div className="model-row-actions">
        {onCompare && <Tooltip title="加入比较"><button className={selected ? 'active' : ''} onClick={onCompare}><SwapOutlined /></button></Tooltip>}
        {onPin && <Tooltip title="收藏"><button className={pinned ? 'active' : ''} onClick={onPin}><PushpinOutlined /></button></Tooltip>}
        <Link to={`/models/${model.id}`} aria-label={`查看 ${model.name}`}><ArrowRightOutlined /></Link>
      </div>
    </article>
  )
}
