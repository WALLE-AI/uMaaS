import type { ThemeConfig } from 'antd'

export const appTheme: ThemeConfig = {
  token: {
    colorPrimary: '#7c3cff',
    colorLink: '#6d2ee9',
    borderRadius: 6,
    fontFamily: 'Inter, "PingFang SC", "Microsoft YaHei", sans-serif',
    colorText: '#171719',
    colorBorder: '#dedee5',
  },
  components: {
    Button: { primaryShadow: 'none' },
    Input: { activeShadow: '0 0 0 2px rgba(124,60,255,.12)' },
    Select: { activeOutlineColor: 'rgba(124,60,255,.12)' },
  },
}

