import { Link } from 'react-router'
import type { Model } from '../../data'
import { ModelLogo } from '../models'

export function RankingLeaderboard({ models, unit }: { models: Model[]; unit: string }) {
  if (!models.length) return <div className="analytics-empty"><b>暂无榜单数据</b><span>当前筛选条件没有可展示的模型。</span></div>

  return (
    <div className="leader-columns">
      {models.map((model, index) => {
        const change = index === 3 ? -4 : Math.max(3, 273 - index * 31)
        return (
          <Link to={`/models/${model.id}`} key={model.id} className="rank-entry">
            <strong>{index + 1}.</strong>
            <ModelLogo model={model} />
            <span><b>{model.name}</b><small>by {model.maker}</small></span>
            <em>
              {unit === '绝对值' ? `${(13.6 - index * 1.31).toFixed(1)}T tokens` : `${(24 - index * 2.1).toFixed(1)}%`}
              <small className={change < 0 ? 'down' : ''}>{change < 0 ? '↓' : '↑'} {Math.abs(change)}%</small>
            </em>
          </Link>
        )
      })}
    </div>
  )
}
