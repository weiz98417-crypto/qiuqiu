import { expect, test } from '@playwright/test';
import { createContractMockState, startConsoleStaticServer } from './support/console-server.mjs';

// console-identity.spec.mjs —— 运营台身份 E2E（对应 openspec/changes/operator-console 期1）。
//
// 运行方式：真实 Go 后端（QIUQIU_BASE_URL，默认 127.0.0.1:18080）+ console/dist
// 构建产物。构建产物由 spec 的 beforeAll 启动的内联 node 服务器伺服（端口随机），
// 该服务器把 /api 代理到真实后端；仅当后端对 /api/console/** 回 404/被 spec
// 显式强制时才按共享契约 mock（见 support/console-server.mjs）。本文件不强制
// 任何前缀 —— operators/whoami 路由已真实落地，全程走真后端。
//
// 身份双模式（backend/cmd/server/operator_auth.go）：运营员表非空时只认表内
// 个人令牌（共享 APP_TOKEN 立即失效）；空表时 APP_TOKEN 以 director scope
// 工作。为两种模式都成立：
//   1. beforeAll 探测运营员列表 —— 空表则先经 API 播种一位导演
//      （`值班导演`）并把工作令牌切换为该导演的个人令牌；
//   2. 全部用例使用该工作令牌；
//   3. afterAll 吊销测试创建的所有运营员（legacy 模式下连播种导演一起删，
//      且最后删），把后端身份状态恢复原样，保证后续 spec（operator-control
//      等）不受影响。
//
// 路由形态：console 用 HashRouter（`/console/#/console/...`）。令牌惯用法
// 与 operator-auth.spec.mjs 一致：localStorage 键 `qiuqiu.console.token`，
// 经 context.addInitScript 预置。

const backendURL = process.env.QIUQIU_BASE_URL || 'http://127.0.0.1:18080';
const token = process.env.APP_TOKEN || 'qiuqiu-dev-token';

// antd v6 全量 bundle + 真实后端往返比 25s 默认预算重。
test.setTimeout(45_000);

let consoleServer;
let consoleBaseURL;
const mockState = createContractMockState({ appToken: token });
let specToken = token; // 工作导演令牌（operators 模式=APP_TOKEN 本尊；legacy=播种导演令牌）
let expectedOperatorName = '运营员';
let seededDirectorName = '';
const createdOperatorNames = [];

test.beforeAll(async () => {
  consoleServer = await startConsoleStaticServer({ backendURL, mockState });
  consoleBaseURL = consoleServer.baseURL;

  const listResponse = await fetch(`${backendURL}/api/console/operators`, {
    headers: { authorization: `Bearer ${token}` },
  });
  if (listResponse.ok) {
    const list = await listResponse.json();
    if ((list.operators ?? []).length === 0) {
      // legacy 共享令牌模式：播种导演并把工作令牌切到个人令牌。
      const seeded = await fetch(`${backendURL}/api/console/operators`, {
        method: 'POST',
        headers: { authorization: `Bearer ${token}`, 'content-type': 'application/json' },
        body: JSON.stringify({ name: '值班导演', role: 'director' }),
      });
      if (seeded.ok) {
        const body = await seeded.json();
        specToken = body.token;
        seededDirectorName = '值班导演';
      }
    }
  }
  const who = await fetch(`${backendURL}/api/console/whoami`, {
    headers: { authorization: `Bearer ${specToken}` },
  });
  if (who.ok) {
    expectedOperatorName = (await who.json()).name;
  } else if (listResponse.ok) {
    throw new Error(
      `console-identity 前置失败：运营员表非空（operators 模式）但 APP_TOKEN 不是表内导演令牌` +
        `（whoami=${who.status}）。请用 QIUQIU_BOOTSTRAP_OPERATOR=name:APP_TOKEN 启动后端，或清空运营员表。`,
    );
  }
});

test.afterAll(async () => {
  // 身份状态复位：先删测试创建的，再删播种导演（删它之后工作令牌才失效）。
  for (const name of [...createdOperatorNames, seededDirectorName]) {
    if (!name) continue;
    await fetch(`${backendURL}/api/console/operators/${encodeURIComponent(name)}`, {
      method: 'DELETE',
      headers: { authorization: `Bearer ${specToken}` },
    }).catch(() => {});
  }
  await consoleServer?.close();
});

async function gotoConsole(page, hashPath = '/console') {
  await page.goto(`${consoleBaseURL}/console/#${hashPath}`);
}

function useConsoleToken(context) {
  return context.addInitScript((value) => {
    localStorage.setItem('qiuqiu.console.token', value);
  }, specToken);
}

test('令牌录入一次即持久化，随后概览五格渲染', async ({ page }) => {
  await page.addInitScript(() => localStorage.removeItem('qiuqiu.console.token'));
  await gotoConsole(page);

  await expect(page.getByTestId('token-gate')).toBeVisible();
  await page.getByLabel('运营员令牌').fill(specToken);
  await page.getByRole('button', { name: '保存并验证' }).click();

  await expect.poll(() => page.evaluate(() => localStorage.getItem('qiuqiu.console.token'))).toBe(specToken);

  // 五格：活跃比赛 / 在线会话 / 记忆健康 / 话题老化 / 最近主动引用。
  for (const cell of ['matches', 'sessions', 'memory', 'aging', 'proactive']) {
    await expect(page.locator(`[data-cell="${cell}"]`)).toBeVisible();
  }
  await expect(page.locator('[data-cell="matches"] table')).toBeVisible();
  await expect(page.locator('[data-cell="memory"]')).toContainText('积压深度');
  await expect(page.locator('[data-cell="aging"]')).toContainText('3 天以上');

  // 头部姓名来自 GET /api/console/whoami。
  await expect(page.getByTestId('operator-name')).toHaveText(expectedOperatorName);

  // 五格钻取：概览 → 引用审计页。
  await page.getByRole('link', { name: '进入引用审计' }).first().click();
  await expect(page.locator('.ant-card').filter({ hasText: '跨比赛最近主动引用' })).toBeVisible();
  await expect(page).toHaveURL(new RegExp('#/console/citations'));

  // 退出登录：令牌清除并回到录入页。
  await page.getByRole('button', { name: '退出' }).click();
  await expect(page.getByTestId('token-gate')).toBeVisible();
  await expect.poll(() => page.evaluate(() => localStorage.getItem('qiuqiu.console.token'))).toBeNull();
});

test('错误令牌不落库并保留录入入口', async ({ page }) => {
  await page.addInitScript(() => localStorage.removeItem('qiuqiu.console.token'));
  await gotoConsole(page);

  await page.getByLabel('运营员令牌').fill('definitely-wrong-token');
  await page.getByRole('button', { name: '保存并验证' }).click();

  await expect(page.getByTestId('token-error')).toContainText('运营员令牌不正确');
  await expect(page.getByTestId('token-gate')).toBeVisible();
  await expect.poll(() => page.evaluate(() => localStorage.getItem('qiuqiu.console.token'))).toBeNull();
});

test('失效令牌在 401 后被清除并回到录入页', async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem('qiuqiu.console.token', 'revoked-or-forged-token');
  });
  await gotoConsole(page);

  await expect(page.getByTestId('token-gate')).toBeVisible();
  await expect(page.getByTestId('token-error')).toContainText('令牌无效或已被吊销');
  await expect.poll(() => page.evaluate(() => localStorage.getItem('qiuqiu.console.token'))).toBeNull();
});

test('运营员创建后令牌仅显示一次，吊销立即失效', async ({ page }) => {
  await useConsoleToken(page.context());
  await gotoConsole(page, '/console/operators');

  const table = page.locator('.ant-table');
  await expect(table).toBeVisible();
  await expect(page.getByRole('row').filter({ hasText: '值班导演' })).toBeVisible();

  await page.getByLabel('姓名').fill('测试运营');
  await page.getByRole('button', { name: '创建运营员' }).click();

  // 令牌只显示一次：Modal 中 code 节点即令牌本体。
  const modal = page.locator('.ant-modal');
  await expect(modal).toContainText('令牌仅显示一次');
  const issuedToken = (await modal.locator('code').first().textContent())?.trim() ?? '';
  expect(issuedToken).not.toBe('');
  createdOperatorNames.push('测试运营');
  await modal.getByRole('button', { name: '我已保存' }).click();

  // 新令牌已可在真后端通过 whoami 鉴权。
  const issuedWho = await fetch(`${backendURL}/api/console/whoami`, {
    headers: { authorization: `Bearer ${issuedToken}` },
  });
  expect(issuedWho.status).toBe(200);
  expect((await issuedWho.json()).name).toBe('测试运营');

  const row = page.getByRole('row').filter({ hasText: '测试运营' });
  await expect(row).toBeVisible();
  await expect(row).toContainText('审计');

  // 吊销 = 删行，立即生效。
  await row.getByRole('button', { name: '吊销' }).click();
  // antd v6 的 Popconfirm 浮层根类名是 .ant-popconfirm。
  await page.locator('.ant-popconfirm').getByRole('button', { name: '吊销' }).click();
  await expect(page.getByRole('row').filter({ hasText: '测试运营' })).toHaveCount(0);

  // 被吊销的令牌立即 401（经页面同源的 /api 代理验证）。
  const revoked = await page.evaluate(async ({ probe, issued }) => {
    const response = await fetch(`${probe}/api/console/whoami`, {
      headers: { Authorization: `Bearer ${issued}` },
    });
    return response.status;
  }, { probe: consoleBaseURL, issued: issuedToken });
  expect(revoked).toBe(401);
});

test('审计（auditor）令牌只读：运营员页与话题台账隐藏写操作', async ({ page }) => {
  await useConsoleToken(page.context());
  await gotoConsole(page, '/console/operators');
  await page.getByLabel('姓名').fill('只读审计员');
  await page.getByRole('button', { name: '创建运营员' }).click();
  const modal = page.locator('.ant-modal');
  await expect(modal).toContainText('令牌仅显示一次');
  const auditorToken = (await modal.locator('code').first().textContent())?.trim() ?? '';
  expect(auditorToken).not.toBe('');
  createdOperatorNames.push('只读审计员');
  await modal.getByRole('button', { name: '我已保存' }).click();

  // 用 auditor 令牌重开页面：运营员页只读（真后端对列表 403）。
  const auditorPage = await page.context().newPage();
  await auditorPage.addInitScript((value) => {
    localStorage.setItem('qiuqiu.console.token', value);
  }, auditorToken);
  await auditorPage.goto(`${consoleBaseURL}/console/#/console/operators`);

  await expect(auditorPage.getByText('仅导演（director）角色可管理运营员')).toBeVisible();
  await expect(auditorPage.getByRole('button', { name: '创建运营员' })).toHaveCount(0);
  await expect(auditorPage.getByTestId('operator-name')).toHaveText('只读审计员');

  // 比赛层设置：写控件对 auditor 隐藏，出现只读标记。
  await auditorPage.goto(`${consoleBaseURL}/console/#/console/match/demo-console-e2e`);
  const settingsCard = auditorPage.locator('.ant-card').filter({ hasText: '设置' });
  await expect(settingsCard).toBeVisible();
  await expect(auditorPage.getByText('审计只读')).toBeVisible();
  await expect(auditorPage.getByRole('button', { name: '保存策略' })).toHaveCount(0);
  await expect(auditorPage.getByRole('button', { name: '人工接管' })).toHaveCount(0);

  // 话题台账：写按钮对 auditor 隐藏（空表时同样成立）。
  await auditorPage.goto(`${consoleBaseURL}/console/#/console/threads`);
  await expect(auditorPage.getByRole('button', { name: '标记已答' })).toHaveCount(0);
  await auditorPage.close();
});
