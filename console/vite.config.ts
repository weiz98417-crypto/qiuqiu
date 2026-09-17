import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// QiuQiu 运营管理台。开发时代理 /api 与 /ws 到本地 Go 后端，
// 构建产物 dist/ 由 Go 服务挂载在 /console。
export default defineConfig({
  plugins: [react()],
  base: '/console/',
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:18080',
        changeOrigin: true,
      },
      '/ws': {
        target: 'ws://127.0.0.1:18080',
        ws: true,
      },
    },
  },
  build: {
    outDir: 'dist',
    sourcemap: false,
  },
});
