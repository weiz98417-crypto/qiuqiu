import { expect, test } from '@playwright/test';
import { createContractMockState, startConsoleStaticServer } from './support/console-server.mjs';

// console-threads.spec.mjs —— 话题台账 + 用户层 E2E（openspec/changes/operator-console 期2）。
//
// 运行方式：真实 Go 后端（QIUQIU_BASE_URL）+ console/dist 构建产物，由
// beforeAll 启动的内联静态服务器伺服（见 support/console-server.mjs 头注）。
// /api 代理到真实后端；/api/console/** 仅在两处走契约回退 mock（响应带
// `x-console-contract-mock: 1`）：后端 404（路由未落地）或 spec 显式强制
// （路由已落地但 eval 后端集合为空、无法驱动写流程，按 SHARED API CONTRACT
// 应答）。后端一旦带真实数据落地，强制清单即为空，规格全量走真后端。
//
// 路由形态为 HashRouter（`/console/#/console/...`），令牌经 addInitScript
// 预置到 localStorage['qiuqiu.console.token']（与 operator-auth.spec.mjs
// 同一惯用法）。「后端状态」断言统一经页面同源代理 GET /api/console/threads
// ——强制模式下即契约状态机，真实模式下即真后端响应。

const token = process.env.APP_TOKEN || 'qiuqiu-dev-token';
const backendURL = process.env.QIUQIU_BASE_URL || 'http://127.0.0.1:18080';

// antd v6 全量 bundle + 真实后端往返比 25s 默认预算重。
test.setTimeout(45_000);
const matchId = 'demo-console-threads-e2e';
const threadUser = 'console-user-1';
const addressedThreadContent = '上半场你预测西班牙 2:0，现在还作数吗？';
const expiredThreadContent = '亚马尔下场还上吗？';

let consoleServer;
let consoleBaseURL;
const mockState = createContractMockState({ appToken: token });
// 三类集合的真实数据可用性：为空则对相应前缀强制契约回退。
let threadsForced = false;
let usersForced = false;
let portraitForced = false;

test.beforeAll(async () => {
  threadsForced = await collectionEmpty('/api/console/threads', 'threads');
  usersForced = await collectionEmpty('/api/console/matches/demo/users', 'users');
  portraitForced = await collectionEmpty(`/api/console/users/${threadUser}/portrait`, 'entries');

  const forceMockPrefixes = [];
  if (threadsForced) forceMockPrefixes.push('/api/console/threads');
  if (usersForced) forceMockPrefixes.push('/api/console/matches/');
  if (portraitForced) forceMockPrefixes.push('/api/console/users/');

  consoleServer = await startConsoleStaticServer({ backendURL, mockState, forceMockPrefixes });
  consoleBaseURL = consoleServer.baseURL;
});

test.afterAll(async () => {
  await consoleServer?.close();
});

test.beforeEach(async ({ request, context }) => {
  await context.addInitScript((value) => {
    localStorage.setItem('qiuqiu.console.token', value);
  }, token);

  // 真实后端路由（比赛层复位 + 阵容，复用 operator-control 惯用法）。
  await apiPost(request, `/api/matches/${matchId}/reset`, {});
  await apiPost(request, `/api/matches/${matchId}/config`, {
    homeTeam: '西班牙',
    awayTeam: '德国',
  });
});

test('用户网格渲染在线状态、话痨档位与开放话题', async ({ page }) => {
  await page.goto(`${consoleBaseURL}/console/#/console/match/${matchId}`);

  const grid = page.locator('.ant-card').filter({ hasText: '用户网格' });
  await expect(grid).toBeVisible();
  await expect(grid.locator('table')).toBeVisible();
  await expect(grid.getByRole('columnheader', { name: '话痨档位' })).toBeVisible();

  // 契约回退模式下网格有种子用户；真实数据模式下由连接注册表驱动行数。
  if (usersForced) {
    const row = grid.getByRole('row').filter({ hasText: threadUser });
    await expect(row).toContainText('在线');
    await expect(row).toContainText('话痨');
    // 列序：用户 / 在线 / 话痨档位 / 开放话题 / 画像更新 / 操作。
    await expect(row.locator('td').nth(3)).toHaveText('2');
  }

  // 设置区（Task 2.7 迁移）：自动化策略 + 数据源与人工接管，导演可见写控件。
  const settings = page.locator('.ant-card').filter({ hasText: '设置' });
  await expect(settings).toBeVisible();
  await expect(settings.getByText('自动化播报策略')).toBeVisible();
  await expect(settings.getByText('数据源与人工接管')).toBeVisible();
  await expect(settings.getByRole('button', { name: '保存策略' })).toBeVisible();
  await expect(settings.getByRole('button', { name: '人工接管' })).toBeVisible();

  if (usersForced) {
    // 钻取链接进入用户层（放在设置断言之后：钻取会离开比赛层）。
    const row = grid.getByRole('row').filter({ hasText: threadUser });
    await row.getByRole('link', { name: threadUser }).click();
    await expect(page).toHaveURL(new RegExp(`/user/${threadUser}`));
    await expect(page.locator('.ant-card').filter({ hasText: '画像' })).toBeVisible();
  }
});

test('比赛层设置：保存自动化策略并经后端确认', async ({ page, request }) => {
  await page.goto(`${consoleBaseURL}/console/#/console/match/${matchId}`);
  const settings = page.locator('.ant-card').filter({ hasText: '设置' });
  await expect(settings).toBeVisible();

  // 模式切到暂停、冷却改 12 秒后保存（antd Radio.Button 的 input 视觉隐藏，点外层 wrapper）。
  await settings.locator('.ant-radio-button-wrapper').filter({ hasText: '暂停' }).click();
  await settings.getByRole('spinbutton').fill('12');
  await settings.getByRole('button', { name: '保存策略' }).click();
  await expect(page.locator('.ant-message')).toContainText('自动化策略已保存');

  // 后端状态确认：直接读真实 /automation 路由。
  await expect
    .poll(async () => {
      const response = await request.get(`/api/matches/${matchId}/automation`, {
        headers: { authorization: `Bearer ${token}` },
      });
      if (!response.ok()) return `http-${response.status()}`;
      return (await response.json()).policy?.mode;
    }, { timeout: 10_000 })
    .toBe('paused');
});

test('话题列表 → 标记已答 → 后端状态一致', async ({ page, request }) => {
  test.skip(!threadsForced, '真实后端已有种子话题时的写流程由期2 E2E 覆盖，此处仅验证契约回退流程');
  await page.goto(`${consoleBaseURL}/console/#/console/threads`);

  const row = page.getByRole('row').filter({ hasText: addressedThreadContent });
  await expect(row).toBeVisible();
  await expect(row.locator('.ant-tag')).toContainText('待答');

  await row.getByRole('button', { name: '标记已答' }).click();

  // UI 反馈：状态徽标翻转为已答（.ant-tag 仅匹配状态徽标，不匹配按钮文案）。
  await expect(row.locator('.ant-tag')).toContainText('已答');

  // 后端状态断言：经页面同源代理（强制模式 = 契约状态机；真实模式 = 真后端）。
  await expect
    .poll(async () => threadStateViaProxy(addressedThreadContent, threadUser), { timeout: 10_000 })
    .toBe('addressed');
});

test('话题过期路径：确认后状态翻转为过期', async ({ page }) => {
  test.skip(!threadsForced, '真实后端已有种子话题时的过期流程由期2 E2E 覆盖');
  await page.goto(`${consoleBaseURL}/console/#/console/threads`);

  const row = page.getByRole('row').filter({ hasText: expiredThreadContent });
  await expect(row).toBeVisible();

  await row.getByRole('button', { name: '过期' }).click();
  // antd v6 的 Popconfirm 浮层根类名是 .ant-popconfirm。
  await page.locator('.ant-popconfirm').getByRole('button', { name: '过期' }).click();

  await expect(row.locator('.ant-tag')).toContainText('过期');
  await expect
    .poll(async () => threadStateViaProxy(expiredThreadContent, threadUser), { timeout: 10_000 })
    .toBe('expired');
});

test('用户层页面：画像只读、话题操作与交互历史', async ({ page }) => {
  await page.goto(`${consoleBaseURL}/console/#/console/match/${matchId}/user/${threadUser}`);

  // 画像（只读 + 代客删除，内容不可导出）。
  const portraitCard = page.locator('.ant-card').filter({ hasText: '画像' });
  await expect(portraitCard).toBeVisible();
  if (portraitForced) {
    await expect(portraitCard).toContainText('偏爱西班牙');
    await expect(portraitCard).toContainText('删除');
  }

  // 话题台账（用户层）。
  const threadCard = page.locator('.ant-card').filter({ hasText: '话题台账' });
  await expect(threadCard).toBeVisible();

  // 交互历史。
  const historyCard = page.locator('.ant-card').filter({ hasText: '交互历史' });
  await expect(historyCard).toBeVisible();
});

// ---- helpers ----

// 探测真实后端集合是否为空：空 → spec 对相应前缀启用契约回退。
async function collectionEmpty(path, listKey) {
  try {
    const response = await fetch(`${backendURL}${path}`, {
      headers: { Authorization: `Bearer ${token}` },
    });
    if (!response.ok) return true; // 路由未落地
    const body = await response.json();
    return (body[listKey] ?? []).length === 0;
  } catch {
    return true;
  }
}

async function threadStateViaProxy(content, userId) {
  const response = await fetch(
    `${consoleBaseURL}/api/console/threads?userId=${encodeURIComponent(userId)}`,
    { headers: { Authorization: `Bearer ${token}` } },
  );
  if (!response.ok) return `http-${response.status}`;
  const body = await response.json();
  const thread = (body.threads ?? []).find((candidate) => candidate.content === content);
  return thread ? thread.state : 'missing';
}

async function apiPost(request, path, body) {
  const response = await request.post(path, {
    data: body,
    headers: {
      Authorization: `Bearer ${token}`,
      'Idempotency-Key': `console-e2e-${Date.now()}-${Math.random().toString(16).slice(2)}`,
    },
  });
  if (!response.ok()) {
    throw new Error(`POST ${path} failed: ${response.status()} ${await response.text()}`);
  }
  return response.json();
}
