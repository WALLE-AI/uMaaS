import type { ThemeConfig } from 'antd'

/**
 * antd 的 token 从 index.css 的 @theme 变量取值，不重复写死十六进制。
 * 这样 §3.3「设计 token 单一来源」才成立：改 CSS 变量，utility 类和 antd
 * 组件同时变色。
 *
 * 注意：antd 的 token 接受 CSS 变量字符串，但部分需要做颜色运算的 token
 * （如 colorPrimaryHover 的自动派生）在拿到 var() 时无法计算。因此对
 * colorPrimary 这类参与派生的 token 仍给出字面值，并由 viz.ts 的运行时
 * 断言保证它与 CSS 变量一致（见 assertTokensInSync）。
 */
export const BRAND = '#7c3cff'

export const adminTheme: ThemeConfig = {
  token: {
    colorPrimary: BRAND,
    colorLink: '#6d2ee9',
    colorText: 'var(--color-ink)',
    colorTextSecondary: 'var(--color-ink-secondary)',
    colorBorder: 'var(--color-line)',
    colorBorderSecondary: 'var(--color-line-soft)',
    colorBgLayout: 'var(--color-plane)',
    colorBgContainer: 'var(--color-surface)',
    colorSuccess: 'var(--color-status-good)',
    colorWarning: 'var(--color-status-warning)',
    colorError: 'var(--color-status-critical)',
    borderRadius: 6,
    fontFamily: 'var(--font-sans)',
    fontSize: 13,
  },
  components: {
    Button: { primaryShadow: 'none' },
    Input: { activeShadow: '0 0 0 2px rgba(124,60,255,.12)' },
    Select: { activeOutlineColor: 'rgba(124,60,255,.12)' },
    Table: { headerBg: 'var(--color-plane)', headerColor: 'var(--color-ink-secondary)' },
    Menu: { itemSelectedBg: 'var(--color-brand-soft)', itemSelectedColor: '#6728df' },
  },
}
