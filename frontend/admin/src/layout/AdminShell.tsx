import type { ReactNode } from 'react'
import { AdminSidebar } from './AdminSidebar'
import { AdminTopbar } from './AdminTopbar'
import { useEnvironment } from './environment'

/**
 * 管理平台外壳：顶栏 + 环境色带 + 两级侧边栏 + 内容区。
 *
 * 环境色带（3px）是与开发者门户最直接的视觉区分，也防止在预发环境误以为
 * 自己在动生产数据（§3.4）。
 */
export function AdminShell({ children }: { children: ReactNode }) {
  const { env } = useEnvironment()

  return (
    <div className="min-h-screen bg-plane">
      <AdminTopbar alertCount={3} />

      {/* 环境色带：颜色由 @theme 的 --color-env-* 提供 */}
      <div
        className="sticky top-14 z-40 h-[3px]"
        style={{ background: `var(--color-env-${env})` }}
        role="presentation"
      />

      <div className="mx-auto grid max-w-[1500px] grid-cols-[230px_minmax(0,1fr)]">
        <AdminSidebar />
        <main className="min-w-0 px-6 py-7 pb-20 lg:px-10">{children}</main>
      </div>
    </div>
  )
}
