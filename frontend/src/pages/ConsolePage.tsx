import {
  ApiOutlined,
  ArrowDownOutlined,
  ArrowUpOutlined,
  CheckCircleFilled,
  ClockCircleOutlined,
  CopyOutlined,
  CreditCardOutlined,
  DeleteOutlined,
  DownloadOutlined,
  KeyOutlined,
  LockOutlined,
  MailOutlined,
  PlusOutlined,
  SafetyCertificateOutlined,
  TeamOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons'
import { Button, Input, Modal, Popconfirm, Progress, Segmented, Select, Slider, Switch, Tag, message } from 'antd'
import { useState } from 'react'
import { useNavigate, useParams } from 'react-router'
import { Area, AreaChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import { ConsolePageHeader, ConsoleSidebar, ConsoleStatGrid, ModelLogo, type ConsoleSection } from '../components'
import { models } from '../data'

const sections: ConsoleSection[] = ['overview', 'keys', 'usage', 'billing', 'team', 'security', 'settings']
const sectionMeta: Record<ConsoleSection, { eyebrow: string; title: string; description: string }> = {
  overview: { eyebrow: 'CONTROL CENTER', title: '工作空间概览', description: '监控模型请求、成本、预算和生产端点状态。' },
  keys: { eyebrow: 'DEVELOPER ACCESS', title: 'API Keys', description: '创建、限制和撤销用于调用 uMaaS API 的密钥。' },
  usage: { eyebrow: 'OBSERVABILITY', title: '用量与预算', description: '按模型、项目和时间范围分析 Tokens、请求量与成本。' },
  billing: { eyebrow: 'BILLING', title: '账单与余额', description: '管理预付余额、付款方式和历史发票。' },
  team: { eyebrow: 'ACCESS CONTROL', title: '团队成员', description: '邀请成员并为工作空间分配最小必要权限。' },
  security: { eyebrow: 'IDENTITY', title: '账户安全', description: '管理登录方式、双重验证和活跃会话。' },
  settings: { eyebrow: 'WORKSPACE', title: '工作空间设置', description: '配置默认路由、数据保留和工作空间身份。' },
}

const usageData = Array.from({ length: 30 }, (_, index) => {
  const day = new Date(Date.UTC(2026, 7, 8 + index))
  return {
    date: `${String(day.getUTCMonth() + 1).padStart(2, '0')}/${String(day.getUTCDate()).padStart(2, '0')}`,
    requests: 14 + index * 2.8 + (index % 3) * 4,
    cost: 3.2 + index * .62 + (index % 2) * 1.1,
  }
})

type ApiKeyRecord = { id: number; name: string; prefix: string; scope: string; created: string; lastUsed: string }
type Member = { id: number; name: string; email: string; role: string; initials: string; status: string }

export default function ConsolePage() {
  const { '*': routePath } = useParams()
  const navigate = useNavigate()
  const path = routePath?.split('/')[0] as ConsoleSection | undefined
  const active: ConsoleSection = path && sections.includes(path) ? path : 'overview'
  const go = (section: ConsoleSection) => navigate(`/console/${section}`)
  const meta = sectionMeta[active]

  return (
    <div className="console-layout">
      <ConsoleSidebar active={active} onNavigate={go} />
      <section className="console-main">
        <ConsolePageHeader eyebrow={meta.eyebrow} title={meta.title} description={meta.description} />
        {active === 'overview' && <OverviewView />}
        {active === 'keys' && <ApiKeysView />}
        {active === 'usage' && <UsageView />}
        {active === 'billing' && <BillingView />}
        {active === 'team' && <TeamView />}
        {active === 'security' && <SecurityView />}
        {active === 'settings' && <SettingsView />}
      </section>
    </div>
  )
}

function UsageChart({ metric = 'requests', data = usageData }: { metric?: 'requests' | 'cost'; data?: typeof usageData }) {
  const color = metric === 'requests' ? '#7c3cff' : '#15945a'
  return <div className="console-chart" role="img" aria-label={metric === 'requests' ? '每日请求量趋势' : '每日成本趋势'}><ResponsiveContainer width="100%" height={260}><AreaChart data={data} margin={{ top: 12, left: 0, right: 8 }}><defs><linearGradient id={`console-${metric}`} x1="0" y1="0" x2="0" y2="1"><stop offset="0" stopColor={color} stopOpacity={.28}/><stop offset="1" stopColor={color} stopOpacity={0}/></linearGradient></defs><CartesianGrid vertical={false} stroke="#ececf1"/><XAxis dataKey="date" axisLine={false} tickLine={false} tick={{fontSize:10}}/><YAxis axisLine={false} tickLine={false} tick={{fontSize:10}} tickFormatter={(value)=>metric==='cost'?`$${value}`:`${value}K`}/><Tooltip formatter={(value)=>[metric==='cost'?`$${Number(value).toFixed(2)}`:`${Number(value).toFixed(1)}K`,metric==='cost'?'成本':'请求']}/><Area type="monotone" dataKey={metric} stroke={color} strokeWidth={2} fill={`url(#console-${metric})`} isAnimationActive={false}/></AreaChart></ResponsiveContainer></div>
}

function OverviewView() {
  const [days,setDays]=useState(30)
  return <><ConsoleStatGrid items={[{label:'本月请求',value:'1.28M',note:'较上月',trend:'+18.4%',icon:<ApiOutlined/>},{label:'已处理 Tokens',value:'842M',note:'输入与输出合计',trend:'+12.1%',icon:<ThunderboltOutlined/>},{label:'本月成本',value:'$127.42',note:'预算 $200',trend:'63.7%',icon:<CreditCardOutlined/>},{label:'网关可用性',value:'99.99%',note:'过去 30 天',trend:'正常',icon:<CheckCircleFilled/>}]}/><div className="console-overview-grid"><section className="console-panel usage-panel"><div className="console-panel-head"><div><span>REQUEST VOLUME</span><h2>请求趋势</h2></div><Segmented size="small" options={[7,14,30].map(value=>({label:`${value} 天`,value}))} value={days} onChange={value=>setDays(Number(value))}/></div><UsageChart data={usageData.slice(-days)}/><div className="chart-caption"><span><i/>成功请求 99.2%</span><span>UTC 日聚合 · 5 分钟前更新</span></div></section><section className="console-panel budget-panel"><div className="console-panel-head"><div><span>MONTHLY LIMIT</span><h2>预算使用</h2></div><b>$127.42 / $200</b></div><Progress percent={64} showInfo={false} strokeColor="#7c3cff"/><div className="budget-forecast"><span><small>预计月末</small><b>$184.20</b></span><span><small>剩余预算</small><b>$72.58</b></span></div><div className="budget-notice"><CheckCircleFilled/><span><b>预算状态正常</b><small>按当前速率不会超出月度限制。</small></span></div><Button block>调整预算</Button></section></div><section className="console-panel"><div className="console-panel-head"><div><span>LIVE TRAFFIC</span><h2>最近请求</h2></div><Button type="link">查看全部</Button></div><div className="console-data-table request-table"><div className="console-table-head"><span>时间</span><span>模型</span><span>状态</span><span>Tokens</span><span>延迟</span><span>费用</span></div>{models.slice(0,5).map((model,index)=><div key={model.id}><span>{`${14-index}:2${index}`}</span><span className="table-model"><ModelLogo model={model}/><b>{model.name}</b></span><span><i className={index===3?'warn':'ok'}/>{index===3?'Fallback':'200 OK'}</span><span>{(2840+index*731).toLocaleString()}</span><span>{42+index*27}ms</span><span>${(.008+index*.006).toFixed(3)}</span></div>)}</div></section></>
}

function ApiKeysView() {
  const [keys,setKeys]=useState<ApiKeyRecord[]>([{id:1,name:'Production gateway',prefix:'um_live_8f2a••••••••',scope:'所有模型',created:'2026-08-12',lastUsed:'2 分钟前'},{id:2,name:'Evaluation runner',prefix:'um_live_31bc••••••••',scope:'只读评测',created:'2026-08-24',lastUsed:'3 小时前'}])
  const [open,setOpen]=useState(false); const [name,setName]=useState(''); const [scope,setScope]=useState('所有模型'); const [secret,setSecret]=useState(''); const [api,holder]=message.useMessage()
  const create=()=>{if(!name.trim())return api.warning('请输入密钥名称');const value=`um_live_${crypto.randomUUID().replaceAll('-','')}`;setKeys(old=>[{id:Date.now(),name:name.trim(),prefix:`${value.slice(0,12)}••••••••`,scope,created:'刚刚',lastUsed:'从未使用'},...old]);setSecret(value)}
  const close=()=>{setOpen(false);setSecret('');setName('')}
  return <>{holder}<div className="console-toolbar"><div><b>{keys.length} 个有效密钥</b><span>密钥权限应按环境和用途隔离。</span></div><Button type="primary" icon={<PlusOutlined/>} onClick={()=>setOpen(true)}>创建 API Key</Button></div><section className="console-panel"><div className="console-data-table key-table"><div className="console-table-head"><span>名称</span><span>密钥</span><span>权限</span><span>创建时间</span><span>最后使用</span><span/></div>{keys.map(key=><div key={key.id}><span><KeyOutlined/><b>{key.name}</b></span><code>{key.prefix}</code><span><Tag>{key.scope}</Tag></span><span>{key.created}</span><span>{key.lastUsed}</span><span><Button type="text" icon={<CopyOutlined/>} onClick={()=>navigator.clipboard.writeText(key.prefix)}/><Popconfirm title="撤销这个密钥？" description="使用该密钥的应用将立即停止工作。" okText="撤销" cancelText="取消" onConfirm={()=>setKeys(old=>old.filter(item=>item.id!==key.id))}><Button danger type="text" icon={<DeleteOutlined/>}/></Popconfirm></span></div>)}</div></section><div className="console-callout"><LockOutlined/><div><b>不要在浏览器或公开仓库中暴露密钥</b><p>服务端调用时从环境变量读取密钥。uMaaS 不会再次显示完整密钥。</p></div></div><Modal open={open} onCancel={close} title={secret?'保存新的 API Key':'创建 API Key'} footer={secret?<Button type="primary" onClick={close}>我已安全保存</Button>:<><Button onClick={close}>取消</Button><Button type="primary" onClick={create}>创建密钥</Button></>}>{secret?<div className="secret-result"><CheckCircleFilled/><h3>密钥创建成功</h3><p>这是唯一一次显示完整密钥。</p><div><code>{secret}</code><Button icon={<CopyOutlined/>} onClick={()=>{navigator.clipboard.writeText(secret);api.success('密钥已复制')}}/></div></div>:<div className="console-form"><label><span>密钥名称</span><Input value={name} onChange={event=>setName(event.target.value)} placeholder="例如 Production gateway"/></label><label><span>权限范围</span><Select value={scope} onChange={setScope} options={['所有模型','指定模型','只读评测'].map(value=>({value,label:value}))}/></label><p>建议为不同项目和环境创建独立密钥。</p></div>}</Modal></>
}

function UsageView(){const [metric,setMetric]=useState<'requests'|'cost'>('requests');const [days,setDays]=useState(30);return <><div className="console-toolbar"><div className="console-filters"><Select value={days} onChange={setDays} options={[7,14,30].map(value=>({value,label:`过去 ${value} 天`}))}/><Select defaultValue="所有项目" options={['所有项目','Production','Evaluation'].map(value=>({value,label:value}))}/></div><Button icon={<DownloadOutlined/>}>导出 CSV</Button></div><ConsoleStatGrid items={[{label:'请求总量',value:'1.28M',note:'99.2% 成功',trend:'+18.4%',icon:<ApiOutlined/>},{label:'输入 Tokens',value:'618M',note:'平均 483 / 请求',icon:<ArrowDownOutlined/>},{label:'输出 Tokens',value:'224M',note:'平均 175 / 请求',icon:<ArrowUpOutlined/>},{label:'实际成本',value:'$127.42',note:'平均 $0.0001 / 请求',icon:<CreditCardOutlined/>}]}/><section className="console-panel"><div className="console-panel-head"><div><span>USAGE OVER TIME</span><h2>{metric==='requests'?'请求量':'成本'}趋势</h2></div><Segmented value={metric} onChange={value=>setMetric(value as 'requests'|'cost')} options={[{label:'请求量',value:'requests'},{label:'成本',value:'cost'}]}/></div><UsageChart metric={metric} data={usageData.slice(-days)}/></section><section className="console-panel"><div className="console-panel-head"><div><span>MODEL BREAKDOWN</span><h2>按模型分布</h2></div></div><div className="model-usage-list">{models.slice(0,5).map((model,index)=><div key={model.id}><ModelLogo model={model}/><span><b>{model.name}</b><small>{model.maker}</small></span><i><em style={{width:`${88-index*13}%`}}/></i><strong>{(38-index*5.2).toFixed(1)}%</strong><small>${(48-index*7.3).toFixed(2)}</small></div>)}</div></section></>}

function BillingView(){const [open,setOpen]=useState(false);const [amount,setAmount]=useState(50);const [api,holder]=message.useMessage();return <>{holder}<div className="billing-hero"><div><span>AVAILABLE BALANCE</span><b>$42.80</b><small>自动充值已开启 · 低于 $10 时充值 $50</small></div><Button type="primary" onClick={()=>setOpen(true)}>充值余额</Button></div><div className="console-two-column"><section className="console-panel plan-panel"><div className="console-panel-head"><div><span>CURRENT PLAN</span><h2>Developer</h2></div><Tag color="purple">按量付费</Tag></div><p>访问全部模型、智能路由和团队协作，无固定月费。</p><ul><li><CheckCircleFilled/> 每分钟 1,000 次请求</li><li><CheckCircleFilled/> 5 个团队席位</li><li><CheckCircleFilled/> 30 天请求日志</li></ul><Button>比较套餐</Button></section><section className="console-panel payment-panel"><div className="console-panel-head"><div><span>PAYMENT METHOD</span><h2>付款方式</h2></div><Button type="link">更换</Button></div><div><CreditCardOutlined/><span><b>Visa •••• 4242</b><small>有效期 09/29</small></span><Tag>默认</Tag></div><p>账单通知发送到 finance@acme.ai</p></section></div><section className="console-panel"><div className="console-panel-head"><div><span>INVOICES</span><h2>历史发票</h2></div></div><div className="console-data-table invoice-table"><div className="console-table-head"><span>日期</span><span>发票编号</span><span>金额</span><span>状态</span><span/></div>{[['2026-09-01','INV-2026-0901','$100.00'],['2026-08-12','INV-2026-0812','$50.00'],['2026-07-08','INV-2026-0708','$50.00']].map(row=><div key={row[1]}><span>{row[0]}</span><code>{row[1]}</code><b>{row[2]}</b><span className="paid"><CheckCircleFilled/> 已支付</span><Button type="text" icon={<DownloadOutlined/>}>PDF</Button></div>)}</div></section><Modal open={open} onCancel={()=>setOpen(false)} title="充值余额" okText={`充值 $${amount}`} cancelText="取消" onOk={()=>{setOpen(false);api.success(`已提交 $${amount} 充值`)}}><div className="console-form"><label><span>充值金额</span><Slider min={10} max={500} step={10} value={amount} onChange={setAmount}/><b className="amount-preview">${amount}</b></label><p>将使用 Visa •••• 4242 完成支付。</p></div></Modal></>}

function TeamView(){const [members,setMembers]=useState<Member[]>([{id:1,name:'林墨',email:'lin@acme.ai',role:'所有者',initials:'LM',status:'你'},{id:2,name:'陈屿',email:'chen@acme.ai',role:'管理员',initials:'CY',status:'活跃'},{id:3,name:'周言',email:'zhou@acme.ai',role:'开发者',initials:'ZY',status:'活跃'}]);const [open,setOpen]=useState(false);const [email,setEmail]=useState('');const [role,setRole]=useState('开发者');const [api,holder]=message.useMessage();const invite=()=>{if(!/^\S+@\S+\.\S+$/.test(email))return api.warning('请输入有效邮箱');setMembers(old=>[...old,{id:Date.now(),name:'等待加入',email,role,initials:'--',status:'已邀请'}]);setOpen(false);setEmail('');api.success('邀请已发送')};return <>{holder}<div className="console-toolbar"><div><b>{members.length} / 5 个席位</b><span>管理员可以管理密钥和账单，开发者只能调用 API。</span></div><Button type="primary" icon={<PlusOutlined/>} onClick={()=>setOpen(true)}>邀请成员</Button></div><section className="console-panel"><div className="member-list">{members.map(member=><div key={member.id}><span className="member-avatar">{member.initials}</span><span><b>{member.name}</b><small>{member.email}</small></span><span className={`member-status ${member.status==='已邀请'?'pending':''}`}><i/>{member.status}</span><Select value={member.role} disabled={member.role==='所有者'} onChange={value=>setMembers(old=>old.map(item=>item.id===member.id?{...item,role:value}:item))} options={['管理员','开发者','查看者'].map(value=>({value,label:value}))}/>{member.role==='所有者'?<span/>:<Popconfirm title="移除该成员？" onConfirm={()=>setMembers(old=>old.filter(item=>item.id!==member.id))}><Button type="text" danger icon={<DeleteOutlined/>}/></Popconfirm>}</div>)}</div></section><Modal open={open} onCancel={()=>setOpen(false)} onOk={invite} title="邀请团队成员" okText="发送邀请" cancelText="取消"><div className="console-form"><label><span>工作邮箱</span><Input prefix={<MailOutlined/>} value={email} onChange={event=>setEmail(event.target.value)} placeholder="name@company.com"/></label><label><span>工作空间角色</span><Select value={role} onChange={setRole} options={['管理员','开发者','查看者'].map(value=>({value,label:value}))}/></label></div></Modal></>}

function SecurityView(){const [twoFactor,setTwoFactor]=useState(false);const [github,setGithub]=useState(true);const [google,setGoogle]=useState(false);const [sessions,setSessions]=useState(['Windows · Shanghai · 当前设备','macOS · Hangzhou · 2 天前']);const [api,holder]=message.useMessage();return <>{holder}<section className="settings-section"><div><h2>登录方式</h2><p>绑定第三方账户，避免单一登录方式失效。</p></div><div className="security-list"><div><span className="security-provider github">GH</span><span><b>GitHub</b><small>{github?'已连接 · linmo':'未连接'}</small></span><Button onClick={()=>setGithub(!github)}>{github?'断开':'连接'}</Button></div><div><span className="security-provider google">G</span><span><b>Google</b><small>{google?'已连接 · lin@acme.ai':'未连接'}</small></span><Button onClick={()=>setGoogle(!google)}>{google?'断开':'连接'}</Button></div></div></section><section className="settings-section"><div><h2>双重验证</h2><p>使用身份验证器保护登录和敏感操作。</p></div><div className="setting-line"><span><SafetyCertificateOutlined/><span><b>Authenticator App</b><small>{twoFactor?'已启用，恢复代码已生成':'尚未配置'}</small></span></span><Switch checked={twoFactor} onChange={value=>{setTwoFactor(value);api.success(value?'双重验证已启用':'双重验证已关闭')}}/></div></section><section className="settings-section"><div><h2>活跃会话</h2><p>检查最近登录设备并撤销未知会话。</p></div><div className="session-list">{sessions.map((session,index)=><div key={session}><ClockCircleOutlined/><span><b>{session.split(' · ')[0]}</b><small>{session.split(' · ').slice(1).join(' · ')}</small></span>{index===0?<Tag color="green">当前</Tag>:<Button danger type="text" onClick={()=>setSessions(old=>old.filter(item=>item!==session))}>撤销</Button>}</div>)}</div></section></>}

function SettingsView(){const [name,setName]=useState('Acme AI');const [region,setRegion]=useState('自动选择');const [retention,setRetention]=useState(true);const [training,setTraining]=useState(false);const [api,holder]=message.useMessage();return <>{holder}<section className="settings-section"><div><h2>工作空间身份</h2><p>显示在团队邀请、账单和控制台导航中。</p></div><div className="console-form"><label><span>工作空间名称</span><Input value={name} onChange={event=>setName(event.target.value)}/></label><label><span>工作空间 Slug</span><Input addonBefore="umaas.dev/" defaultValue="acme-ai"/></label></div></section><section className="settings-section"><div><h2>默认路由</h2><p>这些设置应用于未在请求中明确覆盖的调用。</p></div><div className="console-form"><label><span>默认区域</span><Select value={region} onChange={setRegion} options={['自动选择','亚太地区','美国','欧洲'].map(value=>({value,label:value}))}/></label><div className="setting-line"><span><span><b>自动故障转移</b><small>供应商不可用时切换至健康端点</small></span></span><Switch defaultChecked/></div></div></section><section className="settings-section"><div><h2>数据与隐私</h2><p>控制请求日志和模型供应商的数据使用策略。</p></div><div className="setting-stack"><div className="setting-line"><span><span><b>保留请求元数据 30 天</b><small>不保存提示词和模型输出正文</small></span></span><Switch checked={retention} onChange={setRetention}/></div><div className="setting-line"><span><span><b>允许供应商用于训练</b><small>默认关闭；部分供应商不支持此选项</small></span></span><Switch checked={training} onChange={setTraining}/></div></div></section><div className="settings-save"><span>修改不会自动保存</span><Button type="primary" onClick={()=>api.success('工作空间设置已保存')}>保存更改</Button></div></>}
