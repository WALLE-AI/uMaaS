import {
  AlertOutlined,
  ApiOutlined,
  AppstoreOutlined,
  AuditOutlined,
  BarChartOutlined,
  BranchesOutlined,
  CloudServerOutlined,
  ClusterOutlined,
  CreditCardOutlined,
  DashboardOutlined,
  DatabaseOutlined,
  DeploymentUnitOutlined,
  ExperimentOutlined,
  FundOutlined,
  SafetyOutlined,
  SettingOutlined,
  TeamOutlined,
  ToolOutlined,
  UserOutlined,
} from '@ant-design/icons'
import type { ComponentType } from 'react'

export type NavLeaf = {
  path: string
  label: string
  /** 精确匹配（仅用于各分组下与父路径同名的首项） */
  end?: boolean
  icon?: ComponentType
}

export type NavGroup = {
  key: string
  label: string
  icon: ComponentType
  /** 分组本身可点（如「总览」）时给 path，否则给 items */
  path?: string
  items?: NavLeaf[]
}

/**
 * §2 信息架构的唯一真相源。
 * 侧边栏、面包屑、全局搜索、路由表全部从这里派生，不各写一份。
 */
export const navigation: NavGroup[] = [
  {
    key: 'overview',
    label: '总览',
    icon: DashboardOutlined,
    path: '/',
  },
  {
    key: 'analytics',
    label: '数据分析',
    icon: BarChartOutlined,
    items: [
      { path: '/analytics/usage', label: '用量分析', icon: FundOutlined },
      { path: '/analytics/users', label: '用户分析', icon: UserOutlined },
      { path: '/analytics/models', label: '模型分析', icon: AppstoreOutlined },
      { path: '/analytics/tools', label: '工具调用', icon: ToolOutlined },
    ],
  },
  {
    key: 'catalog',
    label: '供给管理',
    icon: DatabaseOutlined,
    items: [
      { path: '/catalog/models', label: '模型目录', icon: AppstoreOutlined },
      { path: '/catalog/channels', label: '供应商渠道', icon: ApiOutlined },
      { path: '/catalog/routing', label: '路由策略', icon: BranchesOutlined },
    ],
  },
  {
    key: 'customers',
    label: '客户管理',
    icon: TeamOutlined,
    items: [
      { path: '/customers/users', label: '用户', icon: UserOutlined },
      { path: '/customers/workspaces', label: '工作空间', icon: AppstoreOutlined },
      { path: '/customers/billing', label: '账务', icon: CreditCardOutlined },
    ],
  },
  {
    key: 'infra',
    label: '私有化部署',
    icon: CloudServerOutlined,
    items: [
      { path: '/infra', label: '集群总览', end: true, icon: ClusterOutlined },
      { path: '/infra/capacity', label: '容量评估', icon: ExperimentOutlined },
      { path: '/infra/registry', label: '模型仓库', icon: DatabaseOutlined },
      { path: '/infra/deployments', label: '部署实例', icon: DeploymentUnitOutlined },
      { path: '/infra/benchmark', label: '性能压测', icon: FundOutlined },
      { path: '/infra/observability', label: 'GPU 监控', icon: AlertOutlined },
    ],
  },
  {
    key: 'system',
    label: '系统',
    icon: SettingOutlined,
    items: [
      { path: '/system/settings', label: '平台设置', icon: SettingOutlined },
      { path: '/system/rbac', label: '管理员与角色', icon: SafetyOutlined },
      { path: '/system/audit', label: '审计日志', icon: AuditOutlined },
    ],
  },
]

/** 扁平化的全部叶子路由，路由表与全局搜索用 */
export const allRoutes: Array<{ path: string; label: string; group: string }> = navigation.flatMap(
  (group) =>
    group.path
      ? [{ path: group.path, label: group.label, group: group.label }]
      : (group.items ?? []).map((item) => ({
          path: item.path,
          label: item.label,
          group: group.label,
        })),
)

/** 由当前 pathname 反查面包屑 */
export function breadcrumbFor(pathname: string): { group: string; label: string } | undefined {
  const hit = allRoutes.find((route) => route.path === pathname)
  return hit ? { group: hit.group, label: hit.label } : undefined
}
