import { expect, test } from '@playwright/test';
import { readFile } from 'node:fs/promises';

test('导播台从链接接收口令后持久化并清理地址栏', async ({ page }) => {
  const token = 'qiuqiu-dev-token';
  const html = await readFile('client/assets/live2d/operator.html', 'utf8');
  await page.route('http://operator.test/**', (route) =>
    route.fulfill({ status: 200, contentType: 'text/html', body: html }),
  );
  await page.addInitScript(() => {
    localStorage.removeItem('qiuqiu.operator.token');
  });

  await page.goto(`http://operator.test/operator.html?token=${encodeURIComponent(token)}#traces`);

  await expect
    .poll(() => page.evaluate(() => localStorage.getItem('qiuqiu.operator.token')))
    .toBe(token);
  await expect
    .poll(() => page.evaluate(() => authHeaders().Authorization || ''))
    .toBe(`Bearer ${token}`);
  expect(page.url()).not.toContain('token=');
});
