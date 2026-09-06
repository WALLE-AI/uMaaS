import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  build: {
    chunkSizeWarningLimit: 750,
    rollupOptions: {
      output: {
        // Vite 8 起打包器换为 rolldown，manualChunks 只接受函数形式
        manualChunks: (id: string) => {
          if (!id.includes('node_modules')) return
          if (/[\\/]node_modules[\\/](react|react-dom|react-router)[\\/]/.test(id)) return 'react'
          if (/[\\/]node_modules[\\/](antd|@ant-design)[\\/]/.test(id)) return 'antd'
          if (/[\\/]node_modules[\\/]recharts[\\/]/.test(id)) return 'charts'
        },
      },
    },
  },
})
