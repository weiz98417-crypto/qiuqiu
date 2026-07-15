import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: './tests/evals',
  timeout: 25_000,
  fullyParallel: false,
  workers: 1,
  reporter: [
    ['list'],
    ['json', { outputFile: 'artifacts/evals/browser.json' }],
  ],
  use: {
    baseURL: process.env.QIUQIU_BASE_URL || 'http://127.0.0.1:18080',
    screenshot: 'only-on-failure',
    trace: 'retain-on-failure',
    launchOptions: process.env.PLAYWRIGHT_EXECUTABLE_PATH
      ? { executablePath: process.env.PLAYWRIGHT_EXECUTABLE_PATH }
      : {},
  },
});
