import { expect, test } from '@playwright/test';
import { startConsoleStaticServer } from './support/console-server.mjs';

// console-director.spec.mjs —— 新版实战导演页冒烟（ADR-0011 步骤二-四）。
//
// 真实 Go 后端 + console/dist。流程走老页面同款端点与请求形状：
// POST /api/matches/:id/config（阵容）→ 页面读 clock/config → 行为按钮写
// 草稿 → 选人 → 描述 → 确认并发送（POST /events，幂等键）→ 时间线出现
// 事实与「球球主动说：」行；比分更正路径走 correct + 更正原因。

const backendURL = process.env.QIUQIU_BASE_URL || 'http://127.0.0.1:18080';
const token = process.env.APP_TOKEN || 'qiuqiu-dev-token';
const matchId = 'test';

test.setTimeout(60_000);

let consoleServer;
let consoleBaseURL;

async function api(path, { method = 'GET', body } = {}) {
  const response = await fetch(`${backendURL}${path}`, {
    method,
    headers: {
      'content-type': 'application/json',
      Authorization: `Bearer ${token}`,
      ...(method !== 'GET' ? { 'Idempotency-Key': `director-e2e-${Date.now()}-${Math.random().toString(16).slice(2)}` } : {}),
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

  await api(`/api/matches/${matchId}/reset`, { method: 'POST', body: {} });
  await api(`/api/matches/${matchId}/config`, {
    method: 'POST',
    body: {
      homeTeam: '西班牙',
      awayTeam: '德国',
      homePlayers: [
        { number: '10', name: '佩德里', position: 'CM' },
        { number: '8', name: '法比安', position: 'CM' },
        { number: '19', name: '亚马尔', position: 'RW' },
      ],
      awayPlayers: [{ number: '10', name: '穆西亚拉', position: 'AM' }],
    },
  });
  await api(`/api/matches/${matchId}/clock`, {
    method: 'PATCH',
    body: { action: 'set', period: 'first_half', elapsedSeconds: 735 },
  });
});

test.afterAll(async () => {
  await consoleServer?.close();
});

test.beforeEach(async ({ context }) => {
  // 机令牌通道（evals/脚本同款）：导演页属于运营台，令牌来自 localStorage。
  await context.addInitScript((value) => {
    localStorage.setItem('qiuqiu.console.token', value);
  }, token);
});

async function gotoDirector(page) {
  await page.goto(`${consoleBaseURL}/console/#/console/match/${matchId}/director`);
  await page.waitForLoadState('networkidle');
  await expect(page.getByTestId('director-scorebar')).toBeVisible();
}

test('行为按钮写草稿 → 选人 → 描述 → 确认并发送 → 时间线出现事实与主动话术', async ({ page }) => {
  await gotoDirector(page);

  // 行为条：点「进球」预设写入草稿（draft-only，不发布）。
  await page.getByTestId('behavior-goal').click();
  await expect(page.getByTestId('director-draft-card')).toContainText('进球');

  // 名单选人：主队佩德里为主参与人。
  await page.getByTestId('roster-佩德里').click();

  // 球球处理=人工话术，填主动线。（antd 下拉在本主题下宽度异常，
  // 用键盘导航选择。）
  await page.getByTestId('draft-mode-select').click();
  await page.keyboard.press('ArrowDown');
  await page.keyboard.press('ArrowDown');
  await page.keyboard.press('Enter');
  await page.getByLabel('球球主动话术').fill('佩德里进球了，西班牙用远射庆祝。');

  // 描述 + 确认并发送。
  await page.getByLabel('事件描述').fill('佩德里禁区抢点破门。');
  await page.getByTestId('draft-submit').click();

  // 时间线：事实行 + 主动话术行（loadTimeline 同源）。
  await page.waitForLoadState('networkidle');
  await expect(page.getByTestId('director-timeline')).toContainText('佩德里', { timeout: 10000 });
  await expect(page.getByTestId('director-timeline')).toContainText('球球主动说');
  // 后端事实账本里确实有这条事实。
  const events = await api(`/api/matches/${matchId}/events`);
  const goal = (events.events || []).find((event) => event.eventType === 'goal' && event.proactiveText === '佩德里进球了，西班牙用远射庆祝。');
  expect(goal, 'goal event persisted').toBeTruthy();
  expect(goal.proactiveText).toBe('佩德里进球了，西班牙用远射庆祝。');
});

test('比分更正走 correct + 更正原因，时间线记录更正原因', async ({ page }) => {
  // 自包含播种：reset → 阵容 → 时钟 → 进球（更正对象）。
  await api(`/api/matches/${matchId}/reset`, { method: 'POST', body: {} });
  await api(`/api/matches/${matchId}/config`, {
    method: 'POST',
    body: {
      homeTeam: '西班牙',
      awayTeam: '德国',
      homePlayers: [
        { number: '10', name: '佩德里', position: 'CM' },
        { number: '8', name: '法比安', position: 'CM' },
      ],
      awayPlayers: [{ number: '10', name: '穆西亚拉', position: 'AM' }],
    },
  });
  await api(`/api/matches/${matchId}/clock`, {
    method: 'PATCH',
    body: { action: 'set', period: 'first_half', elapsedSeconds: 735 },
  });
  await api(`/api/matches/${matchId}/events`, {
    method: 'POST',
    body: {
      eventType: 'goal', period: 'first_half', clock: '12:15', teamId: 'home', teamName: '西班牙',
      playerName: '佩德里', score: { home: 1, away: 0 }, intensity: 5, confirmed: true,
      description: '佩德里推射得分。', visibility: 'public',
    },
  });

  await gotoDirector(page);
  await page.getByRole('button', { name: '刷新比赛状态' }).click();

  // 行为条选「比分更正」，填目标比分与原因后确认发布。
  await page.getByTestId('behavior-score_correction').click();
  await page.getByLabel('更正后主队比分').fill('2');
  await page.getByLabel('更正后客队比分').fill('0');
  await page.getByLabel('事件描述').fill('人工核对后更正当前比分。');
  await page.getByLabel('更正或采用原因').fill('记分牌核对后确认比分为 2-0');
  await page.getByTestId('draft-submit').click();

  await page.waitForLoadState('networkidle');
  const snapshot = await api(`/api/matches/${matchId}/state`);
  expect(Number(snapshot.snapshot.score.home)).toBe(2);
  const events = await api(`/api/matches/${matchId}/events`);
  const correction = (events.events || []).find((event) => event.eventType === 'score_correction');
  expect(correction, 'score correction persisted').toBeTruthy();
  await expect(page.getByTestId(`timeline-${correction.id}`)).toContainText('记分牌核对后确认比分为 2-0', { timeout: 10000 });
});

test('暂存为候选走 pending 事实状态，时间线给确认/撤销动作', async ({ page }) => {
  await gotoDirector(page);
  await page.getByTestId('behavior-shot').click();
  await page.getByTestId('roster-法比安').click();
  await page.getByLabel('事件描述').fill('法比安禁区外试射。');
  await page.getByTestId('draft-candidate').click();

  await page.waitForLoadState('networkidle');
  const events = await api(`/api/matches/${matchId}/events`);
  const shot = (events.events || []).find((event) => event.eventType === 'shot');
  expect(shot, 'shot candidate persisted').toBeTruthy();
  await expect(page.getByTestId(`timeline-${shot.id}`)).toContainText('候选', { timeout: 10000 });
  await expect(page.getByTestId(`timeline-${shot.id}`)).toContainText('确认');
});

test('时钟 409 版本冲突后自动重读恢复，后续操作继续可用', async ({ page }) => {
  // 自包含播种：reset → 阵容 → 时钟 set（v1）。
  await api(`/api/matches/${matchId}/reset`, { method: 'POST', body: {} });
  await api(`/api/matches/${matchId}/config`, {
    method: 'POST',
    body: { homeTeam: '西班牙', awayTeam: '德国', homePlayers: [{ number: '10', name: '佩德里', position: 'CM' }], awayPlayers: [] },
  });
  await api(`/api/matches/${matchId}/clock`, {
    method: 'PATCH',
    body: { action: 'set', period: 'first_half', elapsedSeconds: 735 },
  });

  await gotoDirector(page);

  // 另一端（绕过页面）把时钟 +10：服务端 v2，页面还停在 v1。
  await api(`/api/matches/${matchId}/clock`, {
    method: 'PATCH',
    body: { action: 'adjust', expectedVersion: 1, deltaSeconds: 10 },
  });

  // 页面点 +10秒 → expectedVersion=1 → 409 → 自动重读 + toast。
  await page.getByRole('button', { name: '+10秒' }).click();
  await expect(page.getByText('时钟已被另一端校准，已自动同步')).toBeVisible({ timeout: 10000 });

  // 恢复后页面的下一次时钟操作直接成功（v2 的 +10 → 服务端 v3、755 秒）。
  await page.getByRole('button', { name: '+10秒' }).click();
  await page.waitForLoadState('networkidle');
  const clock = await api(`/api/matches/${matchId}/clock`);
  expect(clock.clock.version).toBe(3);
  expect(clock.clock.elapsedSeconds).toBe(755);
});
