import { StyleProvider } from '@ant-design/cssinjs'
import { App as AntdApp, ConfigProvider } from 'antd'
import zhCN from 'antd/locale/zh_CN'
import React from 'react'
import ReactDOM from 'react-dom/client'
import { BrowserRouter } from 'react-router'
import App from './App'
import { adminTheme } from './config/theme'
import './index.css'

/**
 * <StyleProvider layer> 是 Tailwind 能覆盖 antd 的前提。
 * 它把 antd 的 CSS-in-JS 样式放进名为 antd 的 layer；index.css 首行声明的
 * 层序 `theme, base, antd, components, utilities` 让 utilities 排在其后，
 * utility 类才会赢。去掉这个 prop，className="mt-4" 之类会立刻失效。
 */
ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <StyleProvider layer>
      <ConfigProvider theme={adminTheme} locale={zhCN}>
        {/* antd 的 App 提供 message/modal/notification 的上下文，
            没有它 App.useApp() 会拿不到实例 */}
        <AntdApp>
          <BrowserRouter basename="/admin">
            <App />
          </BrowserRouter>
        </AntdApp>
      </ConfigProvider>
    </StyleProvider>
  </React.StrictMode>,
)
