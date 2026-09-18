import { expect, test } from '@playwright/test';
import { startConsoleStaticServer } from './support/console-server.mjs';

// console-auth.spec.mjs —— 运营台登录 E2E（ADR-0010 task 2.5）。
//
// 真实 Go 后端（带 QIUQIU_JWT_SECRET，见 scripts/evals/backend.mjs）。
// 覆盖：错误密码 401 → 临时密码首登强制改密 → 进入概览 → 刷新页面后访问
// 令牌经刷新续期恢复 → 退出吊销刷新令牌回登录页 → 新密码可再登录 →
// 「高级」页签保留机令牌通道（evals/脚本通道不被登录页替代）。

const backendURL = process.env.QIUQIU_BASE_URL || 'http://127.0.0.1:18080';
const token = process.env.APP_TOKEN || 'qiuqiu-dev-token';

test.setTimeout(60_000);

let consoleServer;
let consoleBaseURL;
// 本 spec 自建导演账号：create 返回一次性临时密码（首登强制改密），
// 测试流程中改为最终密码；afterAll 吊销恢复 legacy 状态。
const directorName = `e2e导演-${Date.now()}`;
let temporaryPassword = '';
const finalPassword = `e2e-final-pass-${Date.now() % 100000}`;
const createdNames = [];

async function api(path, { method = 'GET', body } = {}) {
  const response = await fetch(`${backendURL}${path}`, {
    method,
    headers: {
      'content-type': 'application/json',
      Authorization: `Bearer ${token}`,
      ...(method !== 'GET' ? { 'Idempotency-Key': `console-auth-${Date.now()}-${Math.random().toString(16).slice(2)}` } : {}),
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const text = await response.text();
  if (!response.ok) throw new Error(`${method} ${path} -> ${response.status}: ${text}`);
  return text ? JSON.parse(text) : {};
}

test.beforeAll(async () => {
  consoleServer = await startConsoleStaticServer({ backendURL });
  consoleBaseURL = consoleServer.baseURL;

  // legacy 模式播种一位导演：create 响应携带一次性临时密码。
  const created = await api('/api/console/operators', {
    method: 'POST',
    body: { name: directorName, role: 'director' },
  });
  createdNames.push(directorName);
  temporaryPassword = String(created.temporaryPassword || "");
  if (!temporaryPassword) throw new Error('create operator did not return temporaryPassword');
});

test.afterAll(async () => {
  // 恢复 legacy：APP_TOKEN 在 operators 模式下已失效，改用导演会话登录后
  // 吊销测试账号（删掉最后一位运营员即恢复 legacy 模式）。
  try {
    const login = await fetch(`${backendURL}/api/console/auth/login`, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ username: directorName, password: finalPassword }),
    });
    if (login.ok) {
      const { accessToken } = await login.json();
      for (const name of createdNames) {
        await fetch(`${backendURL}/api/console/operators/${encodeURIComponent(name)}`, {
          method: 'DELETE',
          headers: { authorization: `Bearer ${accessToken}` },
        }).catch(() => {});
      }
    }
  } catch {
    // 服务器已停或状态已清：无需恢复。
  }
  await consoleServer?.close();
});

test.beforeEach(async () => {
  // 会话清理由 gotoLogin 负责（只清测试开始时一次；init script 会在测试内
  // 的 reload 上重复执行，会把刚存的刷新令牌删掉，不能用）。
});

async function gotoLogin(page) {
  // 先开一次页面把 localStorage 清干净，再进登录页。
  await page.goto(`${consoleBaseURL}/console/`);
  await page.evaluate(() => {
    localStorage.removeItem('qiuqiu.console.token');
    localStorage.removeItem('qiuqiu.console.refresh');
  });
  await page.goto(`${consoleBaseURL}/console/#/console/login`);
  await page.waitForLoadState('networkidle');
}

async function doLogin(page, username, password) {
  await page.getByLabel('用户名').fill(username);
  await page.getByLabel('密码').fill(password);
  await page.getByRole('button', { name: '登录' }).click();
}

test('错误密码提示且不进入管理台', async ({ page }) => {
  await gotoLogin(page);
  await doLogin(page, directorName, 'totally-wrong-pass');
  await expect(page.getByTestId('login-error')).toContainText('用户名或密码不正确');
  await expect(page.getByTestId('login-page')).toBeVisible();
});

test('临时密码首登强制改密，改后进概览，刷新经续期保持，退出吊销，新密码可再登录', async ({ page }) => {
  await gotoLogin(page);
  await doLogin(page, directorName, temporaryPassword);
  // 强制改密弹窗出现且不可关闭。
  await expect(page.getByText('首次登录，请修改临时密码')).toBeVisible();
  await page.getByLabel('当前密码').fill(temporaryPassword);
  await page.getByLabel('新密码', { exact: true }).fill(finalPassword);
  await page.getByLabel('确认新密码').fill(finalPassword);
  await page.getByRole('button', { name: '确认修改' }).click();

  // 改密成功后进入管理台。
  await page.waitForLoadState('networkidle');
  await expect(page.getByTestId('operator-name')).toContainText(directorName);

  // 重载页面：内存访问令牌丢失，数据请求 401 → 刷新令牌无感续期。
  await page.reload();
  await page.waitForLoadState('networkidle');
  await expect(page.locator('.ant-card').filter({ hasText: '活跃比赛' })).toBeVisible();
  await expect(page.getByTestId('operator-name')).toContainText(directorName);

  // 退出：吊销刷新令牌并回到登录页。
  await page.getByRole('button', { name: '退出' }).click();
  await expect(page.getByTestId('login-page')).toBeVisible();

  // 已吊销的刷新令牌无法续期：重载后仍在登录页。
  await page.reload();
  await expect(page.getByTestId('login-page')).toBeVisible();

  // 新密码可再次登录，且不再触发强制改密。
  await doLogin(page, directorName, finalPassword);
  await page.waitForLoadState('networkidle');
  await expect(page.getByTestId('operator-name')).toContainText(directorName);
});

test('高级页签保留机令牌通道', async ({ page }) => {
  await gotoLogin(page);
  await page.getByText('高级：使用运营员个人令牌').click();
  await page.getByLabel('运营员令牌').fill('not-a-valid-token');
  await page.getByRole('button', { name: '保存并验证' }).click();
  await expect(page.getByText('运营员令牌不正确').first()).toBeVisible();
  await expect(page.getByTestId('login-page')).toBeVisible();
});
