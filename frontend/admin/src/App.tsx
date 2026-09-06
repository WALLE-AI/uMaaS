import { Route, Routes } from 'react-router'
import { EmptyState } from './components/common/EmptyState'
import { AdminShell } from './layout/AdminShell'
import { RequireAdmin } from './layout/RequireAdmin'
import OverviewPage from './pages/OverviewPage'
import SelfCheckPage from './pages/SelfCheckPage'
import ModelsAnalyticsPage from './pages/analytics/ModelsPage'
import ToolsPage from './pages/analytics/ToolsPage'
import UsagePage from './pages/analytics/UsagePage'
import UsersAnalyticsPage from './pages/analytics/UsersPage'
import ChannelsPage from './pages/catalog/ChannelsPage'
import BillingPage from './pages/customers/BillingPage'
import CustomerUsersPage from './pages/customers/UsersPage'
import WorkspacesPage from './pages/customers/WorkspacesPage'
import CatalogModelsPage from './pages/catalog/ModelsPage'
import RoutingPage from './pages/catalog/RoutingPage'
import BenchmarkPage from './pages/infra/BenchmarkPage'
import CapacityPage from './pages/infra/CapacityPage'
import ClusterPage from './pages/infra/ClusterPage'
import DeploymentsPage from './pages/infra/DeploymentsPage'
import ObservabilityPage from './pages/infra/ObservabilityPage'
import RegistryPage from './pages/infra/RegistryPage'
import AuditPage from './pages/system/AuditPage'
import RbacPage from './pages/system/RbacPage'
import SettingsPage from './pages/system/SettingsPage'

/**
 * 路由表 —— 只做路由，不放页面逻辑。
 * 20 条业务路由与 §2 的信息架构、config/navigation.ts 一一对应。
 * P0 阶段全部指向占位页，后续按 P1–P6 逐个替换为真实页面。
 */
export default function App() {
  return (
    <RequireAdmin>
      <AdminShell>
        <Routes>
          <Route path="/" element={<OverviewPage />} />

          {/* 数据分析 */}
          <Route path="/analytics/usage" element={<UsagePage />} />
          <Route path="/analytics/users" element={<UsersAnalyticsPage />} />
          <Route path="/analytics/models" element={<ModelsAnalyticsPage />} />
          <Route path="/analytics/tools" element={<ToolsPage />} />

          {/* 供给管理 */}
          <Route path="/catalog/models" element={<CatalogModelsPage />} />
          <Route path="/catalog/channels" element={<ChannelsPage />} />
          <Route path="/catalog/routing" element={<RoutingPage />} />

          {/* 客户管理 */}
          <Route path="/customers/users" element={<CustomerUsersPage />} />
          <Route path="/customers/workspaces" element={<WorkspacesPage />} />
          <Route path="/customers/billing" element={<BillingPage />} />

          {/* 私有化部署 */}
          <Route path="/infra" element={<ClusterPage />} />
          <Route path="/infra/capacity" element={<CapacityPage />} />
          <Route path="/infra/registry" element={<RegistryPage />} />
          <Route path="/infra/deployments" element={<DeploymentsPage />} />
          <Route path="/infra/benchmark" element={<BenchmarkPage />} />
          <Route path="/infra/observability" element={<ObservabilityPage />} />

          {/* 系统 */}
          <Route path="/system/settings" element={<SettingsPage />} />
          <Route path="/system/rbac" element={<RbacPage />} />
          <Route path="/system/audit" element={<AuditPage />} />

          {/* 地基自检，不在导航中 */}
          <Route path="/_selfcheck" element={<SelfCheckPage />} />

          <Route
            path="*"
            element={
              <EmptyState
                title="页面不存在"
                description="请从左侧导航进入，或用顶部搜索（⌘K）查找页面。"
              />
            }
          />
        </Routes>
      </AdminShell>
    </RequireAdmin>
  )
}
