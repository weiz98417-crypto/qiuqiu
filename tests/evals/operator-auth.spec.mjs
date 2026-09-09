import { expect, test } from '@playwright/test';
import { readFile } from 'node:fs/promises';

async function serveOperatorPage(page, onAPIRequest = async () => {}) {
  const html = await readFile('client/assets/live2d/operator.html', 'utf8');
  const liveState = await readFile('client/assets/live2d/operator-live-state.js', 'utf8');
  await page.route('http://operator.test/**', async (route) => {
    const url = new URL(route.request().url());
    if (url.pathname.endsWith('/operator-live-state.js')) {
      return route.fulfill({ status: 200, contentType: 'text/javascript', body: liveState });
    }
    if (url.pathname.startsWith('/api/')) {
      const override = await onAPIRequest(route.request());
      if (override) return route.fulfill(override);
      const body = url.pathname.endsWith('/clock')
        ? { clock: { period: 'pre_match', elapsedSeconds: 0, running: false, version: 0 }, snapshot: { score: { home: 0, away: 0 } } }
        : url.pathname.endsWith('/events')
          ? { events: [], conflicts: [] }
          : url.pathname.endsWith('/config')
            ? { config: {}, snapshot: { score: { home: 0, away: 0 } } }
            : { revisions: [] };
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(body) });
    }
    return route.fulfill({ status: 200, contentType: 'text/html', body: html });
  });
}

test('导播台从链接接收口令后持久化并清理地址栏', async ({ page }) => {
  const token = 'qiuqiu-dev-token';
  await serveOperatorPage(page);
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

test('缺少导播口令时页面提供可填写和验证的恢复入口', async ({ page }) => {
  const token = 'new-operator-token';
  let authorization = '';
  await serveOperatorPage(page, async (request) => {
    if (request.url().includes('/facts/__auth_check__/revisions')) {
      authorization = request.headers().authorization || '';
    }
  });
  await page.addInitScript(() => {
    localStorage.removeItem('qiuqiu.operator.token');
  });

  await page.goto('http://operator.test/operator.html#live');

  await expect(page.locator('#operatorAuthPanel')).toBeVisible();
  await expect(page.getByLabel('导播口令')).toBeVisible();
  await page.getByLabel('导播口令').fill(token);
  await page.getByRole('button', { name: '保存并验证' }).click();

  await expect.poll(() => authorization).toBe(`Bearer ${token}`);
  await expect.poll(() => page.evaluate(() => localStorage.getItem('qiuqiu.operator.token'))).toBe(token);
  await expect(page.locator('#authState')).toContainText('导播权限已验证');
  await expect(page.locator('#operatorAuthPanel')).toBeHidden();
});

test('错误导播口令不会保存且保留恢复入口', async ({ page }) => {
  await serveOperatorPage(page, async (request) => {
    if (request.url().includes('/facts/__auth_check__/revisions')) {
      return { status: 401, contentType: 'text/plain', body: 'unauthorized' };
    }
    return undefined;
  });
  await page.addInitScript(() => {
    localStorage.removeItem('qiuqiu.operator.token');
  });

  await page.goto('http://operator.test/operator.html#live');
  await page.getByLabel('导播口令').fill('wrong-token');
  await page.getByRole('button', { name: '保存并验证' }).click();

  await expect(page.locator('#operatorAuthPanel')).toBeVisible();
  await expect(page.locator('#operatorAuthMessage')).toContainText('导播口令不正确');
  await expect(page.locator('#authState')).toContainText('缺少导播口令');
  await expect.poll(() => page.evaluate(() => localStorage.getItem('qiuqiu.operator.token'))).toBeNull();
});

test('实时比分栏提供显眼的审计式人工校准入口', async ({ page }) => {
  await serveOperatorPage(page);
  await page.addInitScript(() => {
    localStorage.setItem('qiuqiu.operator.token', 'qiuqiu-dev-token');
  });

  await page.goto('http://operator.test/operator.html#live');

  const correctionButton = page.getByRole('button', { name: '人工校准比分' });
  await expect(correctionButton).toBeVisible();
  await correctionButton.click();
  await expect(page.locator('#scoreCorrectionFields')).toBeVisible();
  await expect(page.locator('#correctionReasonField')).toBeVisible();
  await expect(page.locator('#draftSummary')).toContainText('比分更正');
});
