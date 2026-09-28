import { defineConfig } from 'vitest/config';

// 运营台组件单测：jsdom 环境，仅收 src 下的 .test.tsx（与 vite build 的
// tsconfig.include=src 保持同界，不参与构建产物）。
export default defineConfig({
  test: {
    environment: 'jsdom',
    include: ['src/**/*.test.tsx'],
  },
});
