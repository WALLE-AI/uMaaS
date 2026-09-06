import { useMemo, useState } from 'react'
import { Link, Navigate, Route, Routes, useLocation, useNavigate, useParams } from 'react-router'
import {
  ApiOutlined, AppstoreOutlined, ArrowRightOutlined, BarChartOutlined, BookOutlined,
  CheckCircleFilled, ClockCircleOutlined, CodeOutlined, CopyOutlined,
  DatabaseOutlined, ExperimentOutlined, GlobalOutlined, SearchOutlined,
  SettingOutlined, ThunderboltOutlined, FilterOutlined, PushpinOutlined,
  UnorderedListOutlined, TableOutlined, SwapOutlined, DownOutlined,
  GithubOutlined, GoogleOutlined, LockOutlined, MailOutlined, SafetyCertificateOutlined,
} from '@ant-design/icons'
import { Button, ConfigProvider, Drawer, Input, Segmented, Select, Slider, Switch, Tabs, Tag, message } from 'antd'
import { Area, AreaChart, Bar, BarChart, CartesianGrid, ResponsiveContainer, Tooltip as ChartTooltip, XAxis, YAxis } from 'recharts'
import { models, rankSections, type Model } from './data'
import {
  AnalyticsSectionHeader, AnalyticsStatStrip, AppShell, BenchmarkTable, Brand,
  CopyCode, Kicker, Metric, ModelLogo, ModelRow, PageTitle, RankingInsight,
  RankingLeaderboard, UsageTrendChart, type BenchmarkRecord,
  type RankingInsightData, type UsageDatum,
} from './components'
import { appTheme } from './config/theme'
import { RequireAuth, useAuth } from './auth'
import ConsolePage from './pages/ConsolePage'

const chartData = [
  { n: '00', v: 34 }, { n: '04', v: 45 }, { n: '08', v: 42 }, { n: '12', v: 68 }, { n: '16', v: 61 }, { n: '20', v: 84 }, { n: '24', v: 78 },
]

function HomePage() {
  const navigate = useNavigate()
  const [routeMode, setRouteMode] = useState('自动路由')
  return <>
    <section className="hero">
      <div className="hero-copy">
        <Kicker>UNIFIED MODEL AS A SERVICE</Kicker>
        <h1>一个接口，连接<br/><em>所有智能模型。</em></h1>
        <p>统一接入全球主流模型，按质量、价格与延迟自动路由。用可观测的数据，做清醒的模型选择。</p>
        <div className="hero-actions"><Button type="primary" size="large" onClick={() => navigate('/models')}>浏览模型 <ArrowRightOutlined /></Button><Button size="large" onClick={() => navigate('/docs')}>阅读文档</Button></div>
        <div className="trust-row"><span><CheckCircleFilled /> OpenAI 兼容 API</span><span><CheckCircleFilled /> 99.99% 网关可用性</span></div>
      </div>
      <div className="router-panel">
        <div className="panel-top"><span>ROUTING CONSOLE</span><span className="live"><i /> LIVE</span></div>
        <div className="request-node"><CodeOutlined /><div><small>INCOMING REQUEST</small><b>chat.completions</b></div><span>24ms</span></div>
        <div className="route-line"><i/><i/><i/></div>
        <div className="route-nodes">
          {models.slice(0,3).map((m, i) => <div key={m.id} className={i === 0 ? 'selected' : ''}><ModelLogo model={m} /><span><b>{m.name}</b><small>{i === 0 ? 'QUALITY 96' : `LATENCY ${74+i*31}ms`}</small></span>{i === 0 && <CheckCircleFilled />}</div>)}
        </div>
        <div className="routing-controls"><span>ROUTING POLICY</span><Segmented size="small" value={routeMode} onChange={setRouteMode} options={['自动路由', '最低成本']} /></div>
      </div>
    </section>
    <section className="stat-strip"><div><b>180+</b><span>可用模型</span></div><div><b>32</b><span>模型供应商</span></div><div><b>12.8B</b><span>月度 Tokens</span></div><div><b>41ms</b><span>网关额外延迟</span></div></section>
    <section className="section-wrap">
      <PageTitle eyebrow="OPERATE WITH EVIDENCE" title="模型基础设施，不是模型清单" text="从一次请求到完整生产工作流，路由、评测、监控与成本管理在同一个控制面完成。" />
      <div className="feature-grid">
        <Feature icon={<ApiOutlined />} index="01" title="统一 API" text="保持 OpenAI SDK 调用方式，切换模型只改一个 model 字段。" />
        <Feature icon={<ThunderboltOutlined />} index="02" title="智能路由" text="在质量、吞吐、区域和预算约束中实时选择最佳端点。" />
        <Feature icon={<BarChartOutlined />} index="03" title="持续评测" text="用公开基准与业务数据比较模型，不依赖发布会分数。" />
        <Feature icon={<DatabaseOutlined />} index="04" title="完整可观测" text="逐请求跟踪成本、延迟、缓存命中与供应商健康状态。" />
      </div>
    </section>
    <section className="dark-band"><div><Kicker>START BUILDING</Kicker><h2>让模型成为可替换的基础设施</h2></div><Button type="primary" size="large" onClick={() => navigate('/docs')}>5 分钟快速接入 <ArrowRightOutlined /></Button></section>
  </>
}

function Feature({ icon, index, title, text }: { icon: React.ReactNode, index: string, title: string, text: string }) {
  return <article className="feature"><span className="feature-index">{index}</span><div className="feature-icon">{icon}</div><h3>{title}</h3><p>{text}</p><ArrowRightOutlined /></article>
}

function ModelsPage() {
  const [query, setQuery] = useState('')
  const [capability, setCapability] = useState('全部')
  const [modality, setModality] = useState('全部')
  const [sort, setSort] = useState('最新发布')
  const [view, setView] = useState<'列表'|'表格'>('列表')
  const [filtersOpen, setFiltersOpen] = useState(false)
  const [pinnedOnly, setPinnedOnly] = useState(false)
  const [pinned, setPinned] = useState<Set<string>>(new Set())
  const [compare, setCompare] = useState<Set<string>>(new Set())
  const [advancedFilters, setAdvancedFilters] = useState<Set<string>>(new Set())
  const toggle = (setter: React.Dispatch<React.SetStateAction<Set<string>>>, id: string) => setter(old => { const next = new Set(old); next.has(id) ? next.delete(id) : next.add(id); return next })
  const filtered = useMemo(() => models.filter(m => {
    const matchesQuery = (m.name + m.maker + m.tags.join(' ')).toLowerCase().includes(query.toLowerCase())
    const matchesCapability = capability === '全部' || m.tags.includes(capability)
    const matchesModality = modality === '全部' || (modality === '图像' ? m.tags.includes('Vision') : modality === '语音' ? m.tags.some(t => ['Audio','Realtime'].includes(t)) : !m.tags.includes('Audio'))
    const matchesAdvanced = [...advancedFilters].every(filter => filter === '文本' ? !m.tags.includes('Audio') : filter === '图像' ? m.tags.includes('Vision') : filter === '音频' ? m.tags.includes('Audio') : filter === '视频' ? m.tags.includes('Vision') : filter === '≥ 128K' ? m.context !== 'Audio' : filter === '≥ 256K' ? ['256K','400K','500K','1M'].includes(m.context) : filter === '≥ 1M' ? m.context === '1M' : filter === '免费' ? m.input === 0 : filter === '$0–1 / M' ? m.input <= 1 : filter === '$1–5 / M' ? m.input > 1 && m.input <= 5 : m.tags.includes(filter))
    return matchesQuery && matchesCapability && matchesModality && matchesAdvanced && (!pinnedOnly || pinned.has(m.id))
  }).sort((a,b) => sort === '价格最低' ? a.input-b.input : sort === '速度最快' ? b.speed-a.speed : new Date(b.released).getTime()-new Date(a.released).getTime()), [query, capability, modality, sort, pinnedOnly, pinned, advancedFilters])
  const filterGroups = [
    ['输入模态', ['文本','图像','音频','视频']],
    ['上下文长度', ['≥ 128K','≥ 256K','≥ 1M']],
    ['提示词价格', ['免费','$0–1 / M','$1–5 / M']],
    ['模型能力', ['Reasoning','Coding','Tools','Vision']],
  ]
  const filterCount = (item:string) => models.filter(m => item === '文本' ? !m.tags.includes('Audio') : item === '图像' || item === '视频' ? m.tags.includes('Vision') : item === '音频' ? m.tags.includes('Audio') : item === '≥ 128K' ? m.context !== 'Audio' : item === '≥ 256K' ? ['256K','400K','500K','1M'].includes(m.context) : item === '≥ 1M' ? m.context === '1M' : item === '免费' ? m.input === 0 : item === '$0–1 / M' ? m.input <= 1 : item === '$1–5 / M' ? m.input > 1 && m.input <= 5 : m.tags.includes(item)).length
  return <div className="models-browser">
    <aside className="filters-panel">
      <div className="filter-title"><FilterOutlined /> 筛选模型 <button onClick={() => {setCapability('全部');setModality('全部');setPinnedOnly(false);setAdvancedFilters(new Set())}}>重置</button></div>
      {filterGroups.map(([title,items]) => <div className="filter-group" key={title as string}><b>{title as string}<DownOutlined /></b>{(items as string[]).map(item => <label key={item}><input type="checkbox" checked={advancedFilters.has(item)} onChange={() => toggle(setAdvancedFilters,item)} />{item}<span>{filterCount(item)}</span></label>)}</div>)}
      <div className="filter-link">零数据保留 <span>›</span></div><div className="filter-link">区域内路由 <span>›</span></div><div className="filter-link">支持的参数 <span>›</span></div>
    </aside>
    <div className="models-main">
      <div className="models-heading"><div><h1>模型</h1><p>浏览、筛选并比较 uMaaS 上的所有模型与供应商端点。</p></div><div><Button icon={<SwapOutlined />} disabled={compare.size < 2}>比较 {compare.size || ''}</Button><Button type="primary">发现模型</Button></div></div>
      <div className="model-toolbar-v2"><Input prefix={<SearchOutlined />} placeholder="搜索模型..." value={query} onChange={e=>setQuery(e.target.value)} allowClear/><Button className="mobile-filter-trigger" icon={<FilterOutlined/>} onClick={()=>setFiltersOpen(true)}>筛选</Button><Select value={sort} onChange={setSort} options={['最新发布','速度最快','价格最低'].map(value=>({value,label:value}))}/><Select defaultValue="所有供应商" options={['所有供应商',...Array.from(new Set(models.map(m=>m.maker)))].map(value=>({value,label:value}))}/><Button className={pinnedOnly?'is-active':''} icon={<PushpinOutlined/>} onClick={()=>setPinnedOnly(!pinnedOnly)}>已收藏</Button><Segmented value={view} onChange={x=>setView(x as '列表'|'表格')} options={[{value:'列表',icon:<UnorderedListOutlined/>},{value:'表格',icon:<TableOutlined/>}]}/></div>
      <div className="modality-tabs">{[['全部',models.length],['文本',431],['图像',50],['视频',28],['语音',18],['转录',20],['嵌入',37],['重排',7]].map(([name,count])=><button key={name} className={modality===name?'active':''} onClick={()=>setModality(String(name))}>{name} <span>{count}</span></button>)}</div>
      <div className="catalog-meta"><span><b>{filtered.length}</b> 个可用模型</span><span>选择两个模型即可并排比较</span></div>
      <div className={view === '表格' ? 'model-table-view' : 'model-list-v2'}>{filtered.map(model => <ModelRow key={model.id} model={model} compact={view==='表格'} pinned={pinned.has(model.id)} selected={compare.has(model.id)} onPin={()=>toggle(setPinned,model.id)} onCompare={()=>toggle(setCompare,model.id)} />)}{filtered.length===0&&<div className="model-empty"><SearchOutlined/><h3>没有匹配的模型</h3><p>尝试移除部分筛选条件或更换搜索词。</p><Button onClick={()=>{setQuery('');setCapability('全部');setModality('全部');setPinnedOnly(false);setAdvancedFilters(new Set())}}>清除筛选</Button></div>}</div>
      <Drawer title="筛选模型" open={filtersOpen} onClose={()=>setFiltersOpen(false)} width={320}><div className="mobile-filter-sheet"><span>模型能力</span>{['全部','Reasoning','Coding','Tools','Vision','Audio'].map(item=><button className={capability===item?'active':''} onClick={()=>{setCapability(item);setFiltersOpen(false)}} key={item}>{item}<span>{item==='全部'?models.length:models.filter(m=>m.tags.includes(item)).length}</span></button>)}<span>显示范围</span><button className={pinnedOnly?'active':''} onClick={()=>{setPinnedOnly(!pinnedOnly);setFiltersOpen(false)}}><PushpinOutlined/> 仅已收藏</button></div></Drawer>
    </div>
  </div>
}

const detailSections = ['Providers','Pricing','Performance','Uptime','Benchmarks','Apps','Activity','FAQ','Explore']
function ModelDetailPage() {
  const { '*': path = '' } = useParams()
  const model = models.find(m => m.id === path) || models[0]
  const [section, setSection] = useState('Providers')
  return <div className="detail-layout">
    <aside className="side-nav"><Link to="/models" className="back-link">← 所有模型</Link><span className="side-label">ON THIS PAGE</span>{detailSections.map(s => <button className={section === s ? 'active' : ''} onClick={() => { setSection(s); document.getElementById(s)?.scrollIntoView({behavior:'smooth'}) }} key={s}>{s}</button>)}</aside>
    <div className="detail-content">
      <div className="model-hero"><ModelLogo model={model} large /><div><span className="model-id">{model.id}</span><h1>{model.name}</h1><p>{model.summary}</p><div className="tag-row">{model.tags.map(t => <Tag key={t}>{t}</Tag>)}</div></div><Button type="primary" onClick={() => navigator.clipboard.writeText(model.id)} icon={<CopyOutlined />}>复制模型 ID</Button></div>
      <div className="metric-cards"><Metric label="上下文窗口" value={model.context} note="tokens"/><Metric label="输入价格" value={`$${model.input}`} note="每百万 tokens"/><Metric label="输出价格" value={model.output ? `$${model.output}` : '—'} note="每百万 tokens"/><Metric label="吞吐速度" value={`${model.speed}`} note="tokens / sec"/></div>
      <DetailSection id="Providers" title="Providers" intro="uMaaS 实时检查供应商健康、价格与区域，为每次请求选择合适端点。"><ProviderTable model={model}/></DetailSection>
      <DetailSection id="Pricing" title="Pricing" intro="按实际用量计费，无订阅费。"><div className="price-grid"><Metric label="PROMPT" value={`$${model.input}`} note="/ 1M tokens"/><Metric label="COMPLETION" value={model.output ? `$${model.output}` : '—'} note="/ 1M tokens"/><Metric label="CACHE READ" value={`$${(model.input*.1).toFixed(3)}`} note="/ 1M tokens"/></div></DetailSection>
      <DetailSection id="Performance" title="Performance" intro="过去 24 小时的端到端吞吐。"><div className="chart-box"><ResponsiveContainer width="100%" height={260}><AreaChart data={chartData}><defs><linearGradient id="fill" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stopColor="#8fc92e" stopOpacity={.35}/><stop offset="1" stopColor="#8fc92e" stopOpacity={0}/></linearGradient></defs><CartesianGrid strokeDasharray="3 3" vertical={false}/><XAxis dataKey="n"/><YAxis/><ChartTooltip/><Area dataKey="v" stroke="#6e9e1f" fill="url(#fill)" strokeWidth={2} isAnimationActive={false}/></AreaChart></ResponsiveContainer></div></DetailSection>
      <DetailSection id="Uptime" title="Uptime" intro="过去 30 天端点可用性。"><div className="uptime"><b>99.99%</b><div>{Array.from({length: 45}).map((_,i)=><i key={i} className={i===31?'warn':''}/>)}</div><span>30 days ago <em>Today</em></span></div></DetailSection>
      <DetailSection id="Benchmarks" title="Benchmarks" intro="标准化公开评测结果。"><div className="score-bars">{[['综合推理',model.quality],['编程',model.quality-4],['指令遵循',model.quality+1]].map(([n,v])=><div key={String(n)}><span>{n}<b>{v}</b></span><i><em style={{width:`${v}%`}}/></i></div>)}</div></DetailSection>
      <DetailSection id="Apps" title="Apps" intro="本周使用该模型最多的应用。"><div className="mini-list">{['Flow Studio','Code Pilot','Research Desk'].map((x,i)=><div key={x}><span>{i+1}</span><b>{x}</b><em>{(3.8-i*.7).toFixed(1)}M tokens</em></div>)}</div></DetailSection>
      <DetailSection id="Activity" title="Activity" intro="实时请求活动。"><div className="activity"><i/><span>亚洲区域路由恢复正常</span><time>12 分钟前</time></div><div className="activity"><i/><span>价格数据已同步</span><time>2 小时前</time></div></DetailSection>
      <DetailSection id="FAQ" title="FAQ" intro=""><div className="faq"><b>如何调用该模型？</b><p>使用 uMaaS OpenAI 兼容端点，并将 model 设置为上方模型 ID。</p></div><div className="faq"><b>会自动故障转移吗？</b><p>默认启用。你也可以在请求参数中锁定供应商或区域。</p></div></DetailSection>
      <DetailSection id="Explore" title="Explore" intro="继续比较同类型模型。"><div className="explore-grid">{models.filter(m=>m.id!==model.id).slice(0,3).map(m=><ModelRow model={m} key={m.id}/>)}</div></DetailSection>
    </div>
  </div>
}

function DetailSection({id,title,intro,children}:{id:string,title:string,intro:string,children:React.ReactNode}) { return <section id={id} className="detail-section"><div className="section-heading"><span>{String(detailSections.indexOf(id)+1).padStart(2,'0')}</span><div><h2>{title}</h2>{intro&&<p>{intro}</p>}</div></div>{children}</section> }
function ProviderTable({model}:{model:Model}) { return <div className="provider-table"><div className="table-head"><span>供应商</span><span>延迟</span><span>吞吐</span><span>价格</span><span>状态</span></div>{['uMaaS Edge','Azure AI','Cloudflare AI'].map((p,i)=><div key={p}><span><b>{p}</b><small>{i===0?'Auto route':'Singapore'}</small></span><span>{42+i*27}ms</span><span>{model.speed-i*31} t/s</span><span>${(model.input+i*.05).toFixed(2)}</span><span className="healthy"><i/> Healthy</span></div>)}</div> }

function BenchmarksPage() {
  const [category,setCategory] = useState('全部')
  const [selected,setSelected] = useState<BenchmarkRecord | null>(null)
  const cats = ['全部','智能体','推理','搜索']
  const rows: BenchmarkRecord[] = [
    {name:'τ²-Bench Airline',group:'智能体',description:'在严格策略约束下执行工具调用的多轮服务智能体。',models:121,quality:'81.5%',value:'$0.020',speed:'70s',winners:['Claude Fable 5','Step 3.7 Flash','GLM 5.3']},
    {name:'TerminalBench 2.0',group:'智能体',description:'在隔离终端环境中完成真实软件工程任务。',models:38,quality:'72.4%',value:'$0.084',speed:'94s',winners:['GPT-5.6 SOL','DeepSeek V4','Gemini 3.8 Flash']},
    {name:'GPQA Diamond',group:'推理',description:'抵抗简单检索、需要严谨推理的研究生级科学问题。',models:128,quality:'94.4%',value:'$0.029',speed:'60s',winners:['Gemini 3.8 Pro','MiniMax M3','Gemini 3.8 Flash']},
    {name:'AIME 2026',group:'推理',description:'高难度数学竞赛问题，验证多步推导与答案可靠性。',models:64,quality:'91.0%',value:'$0.016',speed:'43s',winners:['DeepSeek V4','GLM 5.3 Flash','GPT-5.6 Luna']},
    {name:'BrowseComp',group:'搜索',description:'需要持续多步研究才能定位的实时网页事实。',models:4,quality:'89.0%',value:'$0.99',speed:'1.9m',winners:['Perplexity + Claude Opus 5','Perplexity + Claude Opus 5','Perplexity + Claude Opus 5']},
    {name:'DeepSearchQA',group:'搜索',description:'答案为列表的问题，评估穷尽检索能力并惩罚无关填充。',models:4,quality:'77.0%',value:'$0.10',speed:'1.6m',winners:['Parallel + Claude Opus 5','Perplexity + GPT-5.6 Luna','Perplexity + GPT-5.6 Luna']},
    {name:'WideSearch',group:'搜索',description:'填充完整研究表格，对部分匹配按答案项计分。',models:4,quality:'84.0%',value:'$0.063',speed:'1.9m',winners:['Perplexity + GPT-5.6 SOL','Perplexity + GPT-5.6 Luna','Perplexity + GPT-5.6 SOL']},
  ]
  const visible = rows.filter(r=>category==='全部'||r.group===category)
  return <div className="benchmarks-page"><section className="bench-intro"><div><h1>评测榜</h1><p>对请求中真正可控的变量进行独立、可复现测量：模型、供应商、搜索引擎和工具预算。每个分数都关联其配置、成本和遥测数据。</p><div className="bench-stats"><b>7 个评测</b><span>•</span><b>2,458,279 次任务执行</b><span>•</span><span>最后运行于 2026 年 9 月 4 日</span></div></div><div className="bench-links"><button><CodeOutlined/><span><b>Benchmarks API</b><small>通过 API 获取结果与元数据</small></span><ArrowRightOutlined/></button><button><AppstoreOutlined/><span><b>媒体评测</b><small>比较多媒体模型生成质量</small></span><ArrowRightOutlined/></button></div></section><div className="benchmark-tabs">{cats.map((x,i)=><button className={category===x?'active':''} onClick={()=>setCategory(x)} key={x}><span>{['◎','▣','△','⊕'][i]}</span>{x}</button>)}</div>{['智能体','推理','搜索'].map(group => {const groupRows=visible.filter(r=>r.group===group);if(!groupRows.length)return null;return <section className="benchmark-section" key={group}><h2><span>{group==='智能体'?'▣':group==='推理'?'△':'⊕'}</span>{group}{group==='智能体'?'与工具':''}</h2><BenchmarkTable rows={groupRows} models={models} onSelect={setSelected}/></section>})}<p className="bench-footnote">需要相同模型的使用量视图？前往 <Link to="/rankings">模型排行榜</Link> 或 <Link to="/models">完整模型列表</Link>。</p><Drawer open={!!selected} onClose={()=>setSelected(null)} width={520} title={selected?.name}>{selected&&<div className="benchmark-detail"><p>{selected.description}</p><div><Metric label="最佳质量" value={selected.quality} note={selected.winners[0]}/><Metric label="最佳价值" value={selected.value} note={selected.winners[1]}/><Metric label="最快完成" value={selected.speed} note={selected.winners[2]}/></div><h3>评测配置</h3><pre>{JSON.stringify({dataset:selected.name,models:selected.models,repetitions:3,temperature:0,telemetry:true},null,2)}</pre><Button type="primary" block>查看完整结果与遥测</Button></div>}</Drawer></div>
}

function RankingsPage() {
  const [active,setActive] = useState('Top Models')
  const [period,setPeriod] = useState('本周')
  const [modality,setModality] = useState('文本')
  const [scale,setScale] = useState('线性')
  const [unit,setUnit] = useState('绝对值')
  const usageData: UsageDatum[] = ['09/08','10/20','12/01','01/12','02/23','04/06','05/18','06/29','08/10','09/01'].map((date,i)=>({date, openai:8+i*i*1.5, google:5+i*3, anthropic:4+i*2.2, deepseek:3+i*2.8, others:6+i*4}))
  const modalities = ['文本','图像','嵌入','重排','视频','语音','转录','批处理']
  const sectionId = (name:string) => `rank-${name.toLowerCase().replaceAll(' ','-')}`
  const insightSections: RankingInsightData[] = [
    {name:'Top models by task',title:'按任务排名',description:'开发者在不同任务中实际选择的模型',labels:['编程','角色扮演','市场营销','科研'],values:['Claude Opus 5','MiniMax M3','Gemini 3.8 Flash','GPT-5.6 SOL']},
    {name:'Cost per session',title:'单次会话成本',description:'按完整会话计算的中位推理成本',labels:['GPT-5.6 SOL','Claude Opus 5','Gemini 3.8 Flash','DeepSeek V4'],values:['$0.84','$0.72','$0.21','$0.09']},
    {name:'Market Share',title:'模型厂商市场份额',description:'按处理 tokens 统计的厂商份额',labels:['OpenAI','Google','Anthropic','DeepSeek'],values:['31.8%','26.4%','21.2%','12.7%']},
    {name:'Benchmarks',title:'基准能力对照',description:'公开评测中的模型综合表现',labels:['GPT-5.6 SOL','Claude Opus 5','DeepSeek V4','Gemini 3.8 Flash'],values:['96.2','94.8','91.1','89.6']},
    {name:'Fastest models',title:'最快模型',description:'首 token 延迟与持续吞吐综合排名',labels:['MAI Transcribe 2','Gemini 3.8 Flash','GLM 5.3 Flash','MiniMax M3'],values:['340 t/s','318 t/s','276 t/s','224 t/s']},
    {name:'Languages',title:'语言使用分布',description:'匿名提示词语言检测结果',labels:['English','中文','日本語','Español'],values:['61.2%','18.7%','6.4%','4.9%']},
    {name:'Programming',title:'编程语言',description:'代码请求中的主要语言',labels:['TypeScript','Python','Rust','Go'],values:['29.4%','27.1%','11.6%','9.8%']},
    {name:'Context Length',title:'上下文长度',description:'请求输入上下文窗口的使用分布',labels:['< 8K','8K – 32K','32K – 128K','> 128K'],values:['35%','39%','19%','7%']},
    {name:'Tool Calls',title:'工具调用',description:'模型生成工具调用的周使用量',labels:['GPT-5.6 SOL','Claude Opus 5','Gemini 3.8 Flash','DeepSeek V4'],values:['8.9B','7.4B','6.8B','3.1B']},
    {name:'Images',title:'图像输入',description:'多模态请求中处理的图像数量',labels:['Gemini 3.8 Flash','GPT-5.6 SOL','Qwen 3.8 Max','Claude Opus 5'],values:['42.8M','35.2M','22.4M','18.9M']},
    {name:'Top Apps',title:'热门应用',description:'公开归因应用的模型调用量',labels:['Flow Studio','Code Pilot','Research Desk','Agent Forge'],values:['4.2T','3.8T','2.6T','1.9T']},
  ]
  return <div className="rankings-page">
    <div className="rank-modality-bar">{modalities.map((m,i)=><button className={modality===m?'active':''} onClick={()=>setModality(m)} key={m}><span>{['T','▧','⌘','↕','▣','◖','♩','▱'][i]}</span>{m}</button>)}</div>
    <div className="rank-layout">
      <aside className="rank-nav"><span className="side-label">RANKINGS</span>{rankSections.map((s,i)=><button key={s} onClick={()=>{setActive(s);document.getElementById(sectionId(s))?.scrollIntoView({behavior:'smooth',block:'start'})}} className={active===s?'active':''}><span>{String(i+1).padStart(2,'0')}</span>{s}</button>)}</aside>
      <div className="rank-content">
        <section className="ranking-intro"><h1>AI 模型排行榜</h1><p>基于真实调用量的实时模型排名。数据来自 uMaaS API 中匿名聚合的 tokens，不代表模型质量。</p><span>数据更新至 2026 年 9 月 4 日</span></section>
        <section id={sectionId('Top Models')} className="usage-chart-section rank-scroll-section"><AnalyticsSectionHeader title="Top Models" description={`${modality}模型在 uMaaS 上的每周使用量`} actions={<Segmented size="small" value={scale} onChange={setScale} options={['线性','对数']}/>}/><AnalyticsStatStrip items={[{label:'本周总量',value:'281.4T',change:'↑ 18.6%',tone:'positive'},{label:'增长最快',value:'OpenAI',change:'↑ 42.1%',tone:'positive'},{label:'活跃模型',value:'437',change:'+19 本周',tone:'neutral'}]}/><UsageTrendChart data={usageData} scale={scale as '线性'|'对数'}/></section>
        <section id={sectionId('Leaderboard')} className="ranking-board rank-scroll-section"><AnalyticsSectionHeader title="LLM Leaderboard" description={`比较 uMaaS 上最常用的${modality}模型`} actions={<div className="rank-selectors"><Select defaultValue="全部模型" options={[{value:'全部模型',label:'全部模型'},{value:'开源模型',label:'开源模型'}]}/><Select value={period} onChange={setPeriod} options={['今天','本周','本月','增长最快'].map(value=>({value,label:value}))}/><Segmented size="small" value={unit} onChange={setUnit} options={['绝对值','百分比']}/></div>}/><RankingLeaderboard models={models} unit={unit}/><Button block className="show-more">显示更多</Button></section>
        <div className="ranking-insights">{insightSections.map((section,index)=><RankingInsight id={sectionId(section.name)} index={index} data={section} key={section.name}/>)}</div>
        <section className="ranking-note"><h2>如何理解这些排名</h2><p>排名统计提示词与补全 tokens，并按 UTC 日聚合。它反映 uMaaS 网络中的采用度，而不是准确率、推理能力或整个市场份额。私密应用的请求不会进入统计。</p></section>
      </div>
    </div>
  </div>
}

const codeSamples = {
  curl: `curl https://api.umaas.dev/v1/chat/completions \\\n+  -H "Authorization: Bearer $UMAAS_API_KEY" \\\n+  -H "Content-Type: application/json" \\\n+  -d '{"model":"google/gemini-3.8-flash","messages":[{"role":"user","content":"Hello"}]}'`,
  python: `from openai import OpenAI\n\nclient = OpenAI(\n    base_url="https://api.umaas.dev/v1",\n    api_key="YOUR_UMAAS_API_KEY",\n)\n\nresponse = client.chat.completions.create(\n    model="google/gemini-3.8-flash",\n    messages=[{"role": "user", "content": "Hello"}],\n)`,
  node: `import OpenAI from "openai";\n\nconst client = new OpenAI({\n  baseURL: "https://api.umaas.dev/v1",\n  apiKey: process.env.UMAAS_API_KEY,\n});\n\nconst result = await client.chat.completions.create({\n  model: "google/gemini-3.8-flash",\n  messages: [{ role: "user", content: "Hello" }],\n});`,
}
function DocsPage() {
  const [lang,setLang]=useState<keyof typeof codeSamples>('curl')
  const [doc,setDoc]=useState('快速开始')
  const groups = [
    ['概览',['快速开始','批处理 Beta','设计原则','模型','MCP','Terraform Provider']],
    ['模型与路由',['模型回退','供应商选择','Auto Router','私有模型','模型变体','路由器']],
    ['工具调用',['客户端工具','服务端工具','插件']],
    ['功能',['工作空间','预算','响应缓存','结构化输出','零数据保留']],
  ]
  return <div className="docs-page">
    <div className="docs-product-nav"><button className="active"><BookOutlined/> 文档</button><button><CodeOutlined/> API Reference</button><button><DatabaseOutlined/> Client SDKs</button><button><SettingOutlined/> Agent SDK</button><button>Cookbook</button></div>
    <div className="docs-layout"><aside className="docs-side"><Input prefix={<SearchOutlined/>} placeholder="搜索文档"/>{groups.map(([name,items])=><div className="docs-group" key={name as string}><span>{name as string}</span>{(items as string[]).map(x=><button className={doc===x?'active':''} onClick={()=>setDoc(x)} key={x}>{x}{x.includes('Beta')&&<em>Beta</em>}</button>)}</div>)}</aside>
      <article className="docs-article"><div className="doc-breadcrumb">概览</div><div className="doc-title-row"><div><h1>{doc}</h1><p className="lead">快速开始使用 uMaaS</p></div><Button icon={<CopyOutlined/>}>复制页面</Button></div><p>uMaaS 通过单一 API 端点连接数百个 AI 模型，自动处理供应商回退，并为每次请求选择价格和可用性更合适的端点。</p><p>根据你需要的控制程度，可以选择三种接入方式：</p><table className="approach-table"><thead><tr><th>接入方式</th><th>适用场景</th></tr></thead><tbody><tr><td>API</td><td>完整控制、任意语言、零依赖</td></tr><tr><td>Client SDK</td><td>类型安全的模型调用与最小开销</td></tr><tr><td>Agent SDK</td><td>构建包含工具、循环和状态的智能体</td></tr></tbody></table><div className="doc-callout info"><span>i</span><code>基础地址：https://api.umaas.dev/v1</code><CopyCode code="https://api.umaas.dev/v1"/></div><div className="doc-callout success"><ThunderboltOutlined/><span>正在寻找免费模型和速率限制？请查看 FAQ。</span></div><h2 id="api-key">1. 创建 API Key</h2><p>在控制台创建密钥。密钥仅展示一次，请保存在安全的环境变量中。</p><pre><code>UMAAS_API_KEY=um_live_xxxxxxxxxxxx</code><CopyCode code="UMAAS_API_KEY=um_live_xxxxxxxxxxxx"/></pre><h2 id="request">2. 发起第一次请求</h2><Tabs activeKey={lang} onChange={x=>setLang(x as keyof typeof codeSamples)} items={Object.keys(codeSamples).map(x=>({key:x,label:x==='node'?'Node.js':x[0].toUpperCase()+x.slice(1)}))}/><pre className="code-block"><code>{codeSamples[lang]}</code><CopyCode code={codeSamples[lang]}/></pre><h2 id="response">3. 读取响应</h2><p>响应与 OpenAI Chat Completions 格式一致，响应头还包含路由供应商、延迟和费用。</p><div className="docs-ask"><span>询问有关文档的问题...</span><Button type="primary" shape="circle" icon={<ArrowRightOutlined/>}/></div></article>
      <aside className="toc"><span className="side-label">本页目录</span><a className="active" href="#api-key">使用 uMaaS API</a><a href="#api-key">创建 API Key</a><a href="#request">发起请求</a><a href="#response">读取响应</a></aside></div>
  </div>
}

function HarnessPage() {
  const [model,setModel]=useState(models[0].id)
  const [harness,setHarness]=useState('Claude Code')
  const [budget,setBudget]=useState(12)
  const [fallback,setFallback]=useState(true)
  const command = `npx @umaas/harness init --agent ${harness.toLowerCase().replace(' ','-')} --model ${model}`
  return <div className="harness-page-v2"><div className="ori-window-bar"><span><i/><i/><i/></span><b>uMaaS / ori</b><em>connected</em></div><section className="ori-hero"><div className="ascii-logo" aria-label="ORI OPEN"><span>uMAAS</span><b>ORI OPEN</b></div><h1>Your favorite harness with every model</h1><p>在 Claude Code、Codex、OpenCode、Cline、Kilo Code 等工具中使用 uMaaS 的全部模型。</p><div className="install-command"><span>›</span><code>curl -fsSL https://umaas.dev/ori/install.sh | bash</code><CopyCode code="curl -fsSL https://umaas.dev/ori/install.sh | bash"/></div><button className="text-link">阅读文档 →</button><div className="ori-terminal"><div className="terminal-top"><span><i/><i/><i/></span><b>{harness} · uMaaS</b></div><div className="terminal-screen"><div><small>Welcome back!</small><strong>▟██▙</strong><span>model · {models.find(m=>m.id===model)?.name}</span></div><div><b>Tips for getting started</b><span>Ask your agent to create a new app</span><span>Routing policy: balanced</span><span>Fallback: {fallback?'enabled':'disabled'}</span></div></div><div className="terminal-prompt"><span>›</span> Build a production-ready model gateway_<em>high · /effort</em></div></div></section>
    <section className="harness-builder"><div className="builder-heading"><span>CONFIGURE</span><h2>生成你的运行配置</h2><p>选择工具和模型，即时生成命令与路由策略。</p></div><div className="harness-shell"><section className="harness-config"><label>选择 Harness</label><div className="harness-options">{['Claude Code','Codex CLI','OpenCode','Aider'].map((x,i)=><button className={harness===x?'active':''} onClick={()=>setHarness(x)} key={x}><span>{[<CodeOutlined/>,<SettingOutlined/>,<GlobalOutlined/>,<AppstoreOutlined/>][i]}</span><b>{x}</b></button>)}</div><label>默认模型</label><Select size="large" value={model} onChange={setModel} options={models.map(m=>({value:m.id,label:`${m.name} · ${m.maker}`}))}/><label>每日预算 <b>${budget}</b></label><Slider min={1} max={50} value={budget} onChange={setBudget}/><div className="switch-line"><span><b>自动故障转移</b><small>端点不可用时切换供应商</small></span><Switch checked={fallback} onChange={setFallback}/></div></section><section className="terminal-card"><div className="terminal-top"><span><i/><i/><i/></span><b>INSTALL</b><CopyCode code={command}/></div><pre><code><em>$</em> {command}</code></pre><div className="terminal-output"><span>✓ Detecting {harness}</span><span>✓ Writing model configuration</span><span>✓ Testing uMaaS gateway</span><b>Ready. Start your next session.</b></div><div className="config-preview"><span>umaas.config.json</span><pre>{JSON.stringify({model,routing:{fallback},limits:{daily_usd:budget}},null,2)}</pre></div></section></div></section><div className="ori-waitlist"><span>›</span><b>登录并加入 uMaaS Deploy 候名单</b><ArrowRightOutlined/></div></div>
}

function AuthPage({ mode }: { mode: 'login' | 'signup' }) {
  const isSignup = mode === 'signup'
  const navigate = useNavigate()
  const location = useLocation()
  const { login, authenticated } = useAuth()
  const redirectTo = (location.state as { from?: string } | null)?.from || '/console'
  const [api, contextHolder] = message.useMessage()
  const [name, setName] = useState('')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [remember, setRemember] = useState(true)
  const [agree, setAgree] = useState(false)
  const [loading, setLoading] = useState<string | null>(null)
  const [error, setError] = useState('')
  const passwordScore = [password.length >= 8, /[A-Z]/.test(password), /\d/.test(password)].filter(Boolean).length
  const oauth = (provider: 'GitHub' | 'Google') => {
    const configuredUrl = provider === 'GitHub' ? import.meta.env.VITE_GITHUB_OAUTH_URL : import.meta.env.VITE_GOOGLE_OAUTH_URL
    if (configuredUrl) {
      const target = new URL(configuredUrl, window.location.origin)
      target.searchParams.set('return_to', `${window.location.origin}${redirectTo}`)
      target.searchParams.set('mode', mode)
      window.location.assign(target.toString())
      return
    }
    setLoading(provider)
    window.setTimeout(() => {
      setLoading(null)
      api.info(`${provider} OAuth 已准备好，请在后端配置 Client ID 与回调地址`)
    }, 700)
  }
  const submit = (event: React.FormEvent) => {
    event.preventDefault()
    setError('')
    if (isSignup && name.trim().length < 2) return setError('请输入至少 2 个字符的姓名')
    if (!/^\S+@\S+\.\S+$/.test(email)) return setError('请输入有效的邮箱地址')
    if (password.length < 8) return setError('密码至少需要 8 个字符')
    if (isSignup && !agree) return setError('请先同意服务条款和隐私政策')
    setLoading('email')
    window.setTimeout(() => {
      setLoading(null)
      api.success(isSignup ? '账户信息验证通过' : '登录信息验证通过')
      login({ name: isSignup ? name.trim() : email.split('@')[0], email, workspace: 'Acme AI' }, isSignup || remember)
      navigate(redirectTo, { replace: true })
    }, 850)
  }
  if (authenticated) return <Navigate to={redirectTo} replace />
  return <div className="auth-page">{contextHolder}<section className="auth-context"><Brand/><div><span className="auth-kicker">uMaaS IDENTITY</span><h1>{isSignup ? '连接每一个模型，\n只需要一个账户。' : '欢迎回到\nuMaaS。'}</h1><p>{isSignup ? '创建工作空间，统一管理模型、供应商、密钥、预算与团队权限。' : '继续管理你的模型路由、评测数据和生产工作流。'}</p><ul><li><CheckCircleFilled/> 一个 API Key 调用全部模型</li><li><CheckCircleFilled/> 团队预算与细粒度权限</li><li><CheckCircleFilled/> 请求数据默认不用于训练</li></ul></div><div className="auth-trust"><SafetyCertificateOutlined/><span><b>企业级安全</b><small>传输加密 · 密钥隔离 · 审计日志</small></span></div></section><section className="auth-form-wrap"><div className="auth-form-head"><span>{isSignup ? '已有账户？' : '还没有账户？'}</span><Link to={isSignup ? '/login' : '/signup'}>{isSignup ? '登录' : '免费注册'} <ArrowRightOutlined/></Link></div><form className="auth-form" onSubmit={submit} noValidate><div className="auth-title"><span>{isSignup ? 'CREATE ACCOUNT' : 'WELCOME BACK'}</span><h2>{isSignup ? '创建 uMaaS 账户' : '登录你的账户'}</h2><p>{isSignup ? '免费开始，无需信用卡。' : '输入账户信息，或使用第三方账户继续。'}</p></div><div className="oauth-grid"><Button size="large" icon={<GithubOutlined/>} loading={loading==='GitHub'} onClick={()=>oauth('GitHub')}>使用 GitHub {isSignup?'注册':'登录'}</Button><Button size="large" icon={<GoogleOutlined/>} loading={loading==='Google'} onClick={()=>oauth('Google')}>使用 Google {isSignup?'注册':'登录'}</Button></div><div className="auth-divider"><span>或使用邮箱</span></div>{isSignup&&<label className="auth-field"><span>姓名</span><Input size="large" value={name} onChange={e=>setName(e.target.value)} placeholder="你的姓名" autoComplete="name"/></label>}<label className="auth-field"><span>工作邮箱</span><Input size="large" prefix={<MailOutlined/>} value={email} onChange={e=>setEmail(e.target.value)} placeholder="name@company.com" autoComplete="email"/></label><label className="auth-field"><span>密码 {!isSignup&&<button type="button" onClick={()=>api.info('密码重置链接将发送到注册邮箱')}>忘记密码？</button>}</span><Input.Password size="large" prefix={<LockOutlined/>} value={password} onChange={e=>setPassword(e.target.value)} placeholder={isSignup?'至少 8 个字符':'输入密码'} autoComplete={isSignup?'new-password':'current-password'}/>{isSignup&&<div className="password-strength"><i className={passwordScore>0?'active':''}/><i className={passwordScore>1?'active':''}/><i className={passwordScore>2?'active':''}/><span>{passwordScore<2?'密码强度较弱':passwordScore===2?'密码强度良好':'密码强度很强'}</span></div>}</label>{isSignup?<label className="auth-check"><input type="checkbox" checked={agree} onChange={e=>setAgree(e.target.checked)}/><span>我同意 <a href="#terms">服务条款</a> 和 <a href="#privacy">隐私政策</a></span></label>:<label className="auth-check"><input type="checkbox" checked={remember} onChange={e=>setRemember(e.target.checked)}/><span>在此设备上保持登录</span></label>}{error&&<div className="auth-error" role="alert">{error}</div>}<Button type="primary" htmlType="submit" size="large" block loading={loading==='email'}>{isSignup?'创建账户':'登录'} <ArrowRightOutlined/></Button><p className="auth-note">继续即表示你理解第三方登录将共享基础账户信息。</p></form></section></div>
}

function NotFound() { return <div className="not-found"><span>404</span><h1>页面不存在</h1><Link to="/">返回首页</Link></div> }

export default function App() {
  return <ConfigProvider theme={appTheme}><AppShell><Routes><Route path="/" element={<HomePage/>}/><Route path="/models" element={<ModelsPage/>}/><Route path="/models/*" element={<ModelDetailPage/>}/><Route path="/benchmarks" element={<BenchmarksPage/>}/><Route path="/rankings" element={<RankingsPage/>}/><Route path="/docs/*" element={<DocsPage/>}/><Route path="/harness" element={<HarnessPage/>}/><Route path="/console/*" element={<RequireAuth><ConsolePage/></RequireAuth>}/><Route path="/login" element={<AuthPage mode="login"/>}/><Route path="/signup" element={<AuthPage mode="signup"/>}/><Route path="*" element={<NotFound/>}/></Routes></AppShell></ConfigProvider>
}
