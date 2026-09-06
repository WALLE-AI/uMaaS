import type { CustomerUser, Invoice, Topup, Workspace, UiStatus as StatusKind } from '../api/contracts'
import { mockResponse, seedFrom, seededRandom } from './generators'


const NAMES = [
  ['林墨', 'lin@acme.ai', 'Acme AI', '企业版'],
  ['陈屿', 'chen@northwind.io', 'Northwind', '团队版'],
  ['周言', 'zhou@flowstudio.dev', 'Flow Studio', '团队版'],
  ['苏和', 'su@codepilot.app', 'Code Pilot', '自助版'],
  ['吴桥', 'wu@research-desk.org', 'Research Desk', '自助版'],
  ['何川', 'he@agentforge.io', 'Agent Forge', '免费版'],
  ['郑禾', 'zheng@pinelabs.cn', 'Pine Labs', '免费版'],
] as const

export function customerUsers(): CustomerUser[] {
  return NAMES.map(([name, email, workspace, plan], index) => {
    const random = seededRandom(seedFrom(email))
    const tokens = Math.round((8_400_000 * (1 - index * 0.12) + random() * 2_000_000) * 10) / 10
    const balance = index === 3 ? -3.2 : Math.round(random() * 480 * 100) / 100
    return {
      id: email.split('@')[0],
      name,
      email,
      workspace,
      plan: plan as CustomerUser['plan'],
      tokens30d: tokens,
      cost30d: Math.round((tokens / 700_000) * 100) / 100,
      balance,
      status: balance < 0 ? ('warning' as StatusKind) : index === 6 ? ('idle' as StatusKind) : ('good' as StatusKind),
      statusLabel: balance < 0 ? '余额为负' : index === 6 ? '已停用' : '正常',
      joinedAt: `2026-0${(index % 8) + 1}-1${index}`,
      lastActive: index === 6 ? '23 天前' : `${index + 1} 小时前`,
    }
  })
}


export function workspaces(): Workspace[] {
  return [
    { id: 'acme-ai', name: 'Acme AI', plan: '企业版', seatsUsed: 42, seatsTotal: 50, monthlyBudget: 4000, monthlySpend: 3184, owner: 'lin@acme.ai', models: 18 },
    { id: 'northwind', name: 'Northwind', plan: '团队版', seatsUsed: 8, seatsTotal: 10, monthlyBudget: 800, monthlySpend: 742, owner: 'chen@northwind.io', models: 9 },
    { id: 'flow-studio', name: 'Flow Studio', plan: '团队版', seatsUsed: 5, seatsTotal: 10, monthlyBudget: 600, monthlySpend: 218, owner: 'zhou@flowstudio.dev', models: 6 },
    { id: 'code-pilot', name: 'Code Pilot', plan: '自助版', seatsUsed: 3, seatsTotal: 5, monthlyBudget: 200, monthlySpend: 206, owner: 'su@codepilot.app', models: 4 },
  ]
}


export function invoices(): Invoice[] {
  return [
    { id: 'INV-2026-0901', workspace: 'Acme AI', period: '2026-08', amount: 3184.2, status: 'paid', issuedAt: '2026-09-01' },
    { id: 'INV-2026-0902', workspace: 'Northwind', period: '2026-08', amount: 742.5, status: 'paid', issuedAt: '2026-09-01' },
    { id: 'INV-2026-0903', workspace: 'Flow Studio', period: '2026-08', amount: 218.0, status: 'pending', issuedAt: '2026-09-01' },
    { id: 'INV-2026-0904', workspace: 'Code Pilot', period: '2026-08', amount: 206.4, status: 'overdue', issuedAt: '2026-08-01' },
  ]
}


export function topups(): Topup[] {
  return [
    { id: 'tp-8821', workspace: 'Acme AI', amount: 2000, method: 'Visa •••• 4242', at: '2026-09-04 10:22', status: 'success' },
    { id: 'tp-8814', workspace: 'Northwind', amount: 500, method: '对公转账', at: '2026-09-02 15:41', status: 'success' },
    { id: 'tp-8802', workspace: 'Flow Studio', amount: 200, method: 'Visa •••• 9931', at: '2026-08-29 09:08', status: 'refunded' },
  ]
}

export const customersMock = {
  users: () => mockResponse({ users: customerUsers() }, 'cust-users'),
  workspaces: () => mockResponse({ workspaces: workspaces() }, 'cust-workspaces'),
  billing: () =>
    mockResponse(
      {
        invoices: invoices(),
        topups: topups(),
        summary: { mrr: 4351.1, outstanding: 424.4, refunded: 200, arpu: 42.6 },
      },
      'cust-billing',
    ),
}
