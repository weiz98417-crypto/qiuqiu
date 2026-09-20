import { expect, test } from '@playwright/test';
import { openTextMode } from './support/open-text-mode.mjs';
import { startConsoleStaticServer } from './support/console-server.mjs';

const token = process.env.APP_TOKEN || 'qiuqiu-dev-token';
const matchId = 'test';

let consoleServer;
let consoleBaseURL;

test.beforeAll(async () => {
  consoleServer = await startConsoleStaticServer({ backendURL: process.env.QIUQIU_BASE_URL || 'http://127.0.0.1:18080' });
  consoleBaseURL = consoleServer.baseURL;
});

test.afterAll(async () => {
  await consoleServer?.close();
});

// 轨迹审计已收敛到新台引用审计页（ADR-0013）：机令牌 + URL 预填，清空前缀看全量。
async function openConsoleCitationAudit(context) {
  const audit = await context.newPage();
  await audit.addInitScript((value) => {
    localStorage.setItem('qiuqiu.console.token', value);
  }, token);
  await audit.goto(`${consoleBaseURL}/console/#/console/citations?matchId=${encodeURIComponent(matchId)}`);
  await audit.getByLabel('引用前缀').fill('');
  await audit.getByRole('button', { name: '审计' }).click();
  await expect(audit.getByText('按引用前缀审计轨迹')).toBeVisible();
  return audit;
}

test.beforeEach(async ({ page, request }) => {
  await page.addInitScript((value) => {
    localStorage.setItem('qiuqiu.app.token', value);
  }, token);
  await apiPost(request, `/api/matches/${matchId}/reset`, {});
  await apiPost(request, `/api/matches/${matchId}/config`, {
    homeTeam: '西班牙',
    awayTeam: '德国',
    homePlayers: [
      { number: '10', name: '佩德里', position: 'CM' },
      { number: '8', name: '法比安', position: 'CM' },
      { number: '19', name: '亚马尔', position: 'RW' },
    ],
    awayPlayers: [{ number: '10', name: '穆西亚拉', position: 'AM' }],
  });
  await startMatchClock(request, 1421);
  await apiPost(request, `/api/matches/${matchId}/events`, {
    eventType: 'goal',
    period: 'first_half',
    clock: '23:41',
    teamId: 'home',
    teamName: '西班牙',
    playerName: '佩德里',
    score: { home: 1, away: 0 },
    intensity: 5,
    description: '佩德里禁区前沿推射破门。',
    proactiveText: '__quiet__',
    visibility: 'public',
    participants: [
      { role: 'scorer', name: '佩德里', teamId: 'home', teamName: '西班牙' },
      { role: 'assist', name: '法比安', teamId: 'home', teamName: '西班牙' },
      { role: 'pre_assist', name: '亚马尔', teamId: 'home', teamName: '西班牙' },
    ],
  });
});

test('用户侧展示导演主动线、基于记忆回答追问，并在日志中可追溯', async ({ page, request }) => {
  await page.goto(matchURL());
  await enableAccessibility(page);
  await expect(page.getByText('比赛已连接')).toBeVisible({ timeout: 10_000 });

  await apiPost(request, `/api/matches/${matchId}/events`, {
    eventType: 'operator_note',
    period: 'first_half',
    clock: '24:00',
    teamId: 'home',
    teamName: '西班牙',
    score: { home: 1, away: 0 },
    intensity: 3,
    description: '浏览器主动线评测事件。',
    proactiveText: '浏览器主动线评测：球球已经收到导演事件。',
    visibility: 'public',
  });
  await expect(page.getByText(/浏览器主动线评测/).last()).toBeVisible();

  await openTextMode(page);
  await page.getByLabel('直接和球球说…').fill('刚才谁助攻？');
  await page.getByRole('button', { name: '发送这句话' }).click();
  await expect(page.getByText(/法比安/).last()).toBeVisible({ timeout: 30_000 });
  await expect(page.getByText(/亚马尔/).last()).toBeVisible({ timeout: 30_000 });

  const traces = await apiGet(request, `/api/matches/${matchId}/traces?token=${encodeURIComponent(token)}&limit=20`);
  const trace = traces.traces.find((item) => item.input === '刚才谁助攻？');
  expect(trace.intent).toBe('recent_event_question');
  expect(trace.retrievedEventIds).not.toHaveLength(0);
  expect(JSON.stringify(trace.toolCalls)).toContain('match.search_events');

  // 实时运营面收敛到新台（ADR-0013）：最新一条 trace 即本回合提问。
  const audit = await openConsoleCitationAudit(page.context());
  const firstWhy = audit.getByRole('button', { name: '为什么说话' }).first();
  await firstWhy.click();
  await expect(audit.getByText('刚才谁助攻？')).toBeVisible();
  await audit.close();
  const auditState = await apiGet(request, `/api/matches/${matchId}/state`);
  expect(auditState.snapshot.score).toEqual({ home: 1, away: 0 });
});

test('用户错误赛况不会覆盖比赛事实，并留下核验记录', async ({ page, request }) => {
  await page.goto(matchURL());
  await enableAccessibility(page);
  await openTextMode(page);
  await page.getByLabel('直接和球球说…').fill('德国已经3比0领先了');
  await page.getByRole('button', { name: '发送这句话' }).click();
  await expect(page.getByText(/西班牙 1-0 德国/).last()).toBeVisible({ timeout: 30_000 });

  const traces = await apiGet(request, `/api/matches/${matchId}/traces?limit=20`);
  const trace = traces.traces.find((item) => item.input === '德国已经3比0领先了');
  expect(trace.intent).toBe('match_fact_claim');
  expect(trace.claim).toMatchObject({ kind: 'score', status: 'contradicted' });
  expect(JSON.stringify(trace.toolCalls)).toContain('match.verify_user_claim');

  // 核验语义（contradicted + verify_user_claim）已在上方 API 断言；
  // UI 侧验证引用审计页可打开该回合详情（ADR-0013）。
  const audit = await openConsoleCitationAudit(page.context());
  const firstWhy = audit.getByRole('button', { name: '为什么说话' }).first();
  await firstWhy.click();
  await expect(audit.getByText('德国已经3比0领先了')).toBeVisible();
  await audit.close();
});

test('错误进球者会被纠正，玩笑不会进入事实核验', async ({ page, request }) => {
  await page.goto(matchURL());
  await enableAccessibility(page);
  await openTextMode(page);
  await sendText(page, '刚才哈兰德进球了');
  await expect(page.getByText(/不是哈兰德.*佩德里/).last()).toBeVisible({ timeout: 30_000 });

  await sendText(page, '开玩笑，德国3比0了');
  await expect(page.getByText(/逗我|陪你看/).last()).toBeVisible({ timeout: 30_000 });
  const traces = await apiGet(request, `/api/matches/${matchId}/traces?limit=30`);
  const scorerTrace = traces.traces.find((item) => item.input === '刚才哈兰德进球了');
  expect(scorerTrace.claim).toMatchObject({ kind: 'event', status: 'contradicted', claimedPlayer: '哈兰德', actualPlayer: '佩德里' });
  const jokeTrace = traces.traces.find((item) => item.input === '开玩笑，德国3比0了');
  expect(jokeTrace.intent).toBe('smalltalk');
  expect(jokeTrace.claim).toBeUndefined();
});

test('没有比赛证据时，用户报告的进球保持待确认', async ({ page, request }) => {
  await apiPost(request, `/api/matches/${matchId}/reset`, {});
  await apiPost(request, `/api/matches/${matchId}/config`, { homeTeam: '西班牙', awayTeam: '德国' });
  await startMatchClock(request, 1);
  await apiPost(request, `/api/matches/${matchId}/events`, {
    eventType: 'kickoff',
    period: 'first_half',
    clock: '00:01',
    score: { home: 0, away: 0 },
    description: '比赛开始。',
    proactiveText: '__quiet__',
    visibility: 'public',
    confirmed: true,
  });
  await page.goto(matchURL());
  await enableAccessibility(page);
  await openTextMode(page);
  await sendText(page, '佩德里刚刚进球了吧');
  await expect(page.getByText(/还没跟上/).last()).toBeVisible({ timeout: 30_000 });
  const traces = await apiGet(request, `/api/matches/${matchId}/traces?limit=20`);
  const trace = traces.traces.find((item) => item.input === '佩德里刚刚进球了吧');
  expect(trace.claim).toMatchObject({ kind: 'event', status: 'unverified', certainty: 'uncertain' });
});

test('没有比赛事件时，指代式赞美不会被球球顺着认同', async ({ page, request }) => {
  await apiPost(request, `/api/matches/${matchId}/reset`, {});
  await apiPost(request, `/api/matches/${matchId}/config`, { homeTeam: '西班牙', awayTeam: '德国' });
  await page.goto(matchURL());
  await enableAccessibility(page);
  await openTextMode(page);
  await sendText(page, '刚刚那个球真漂亮吧');
  await expect(page.getByText(/还没看到你说的那一下/).last()).toBeVisible({ timeout: 30_000 });

  const traces = await apiGet(request, `/api/matches/${matchId}/traces?limit=20`);
  const trace = traces.traces.find((item) => item.input === '刚刚那个球真漂亮吧');
  expect(trace.output).not.toContain('确实漂亮');
  expect(trace.claim).toMatchObject({ kind: 'event_reference', status: 'unverified', certainty: 'uncertain' });
  expect(JSON.stringify(trace.toolCalls)).toContain('match.search_events');
  expect(JSON.stringify(trace.toolCalls)).toContain('match.verify_user_claim');
});

test('运行中的比赛时钟不会阻塞客户端状态轮播', async ({ page, request }) => {
  await apiPost(request, `/api/matches/${matchId}/reset`, {});
  await apiPost(request, `/api/matches/${matchId}/config`, { homeTeam: '西班牙', awayTeam: '德国' });
  await setAndStartMatchClock(request, 1500);
  await apiPost(request, `/api/matches/${matchId}/events`, {
    eventType: 'shot',
    period: 'first_half',
    clock: '25:00',
    teamId: 'home',
    teamName: '西班牙',
    playerName: '佩德里',
    score: { home: 0, away: 0 },
    description: '佩德里完成一次射门。',
    proactiveText: '__quiet__',
    visibility: 'public',
  });

  await page.goto(matchURL());
  await enableAccessibility(page);
  await expect.poll(() => page.locator('body').innerText()).toContain('比赛动态：25:00 · 佩德里完成一次射门。');
  await expect.poll(() => page.locator('body').innerText(), { timeout: 7_000 })
    .toContain('比赛动态：上半场 · 西班牙 0—0 德国');
});

test('比赛情况和时间提问读取正在运行的后台时钟', async ({ page, request }) => {
  await apiPost(request, `/api/matches/${matchId}/reset`, {});
  await apiPost(request, `/api/matches/${matchId}/config`, { homeTeam: '西班牙', awayTeam: '德国' });
  await setAndStartMatchClock(request, 1500);

  await page.goto(matchURL());
  await enableAccessibility(page);
  await openTextMode(page);
  await sendText(page, '比赛什么情况了');
  await expect(page.getByText(/现在是西班牙 0-0 德国，时间在上半场 25:/).last()).toBeVisible({ timeout: 30_000 });
  await sendText(page, '比赛时间是多少了？');
  await expect(page.getByText(/现在是西班牙 0-0 德国，时间在上半场 25:/).last()).toBeVisible({ timeout: 30_000 });

  const traces = await apiGet(request, `/api/matches/${matchId}/traces?limit=20`);
  for (const input of ['比赛什么情况了', '比赛时间是多少了？']) {
    const trace = traces.traces.find((item) => item.input === input);
    expect(trace?.intent).toBe('match_status_question');
    expect(trace?.output).not.toBe('嗯，我在。');
  }
});

test('零比零时用户说好球会得到赛场回应且不会被当成进球', async ({ page, request }) => {
  await apiPost(request, `/api/matches/${matchId}/reset`, {});
  await apiPost(request, `/api/matches/${matchId}/config`, { homeTeam: '西班牙', awayTeam: '德国' });
  await setAndStartMatchClock(request, 1500);

  await page.goto(matchURL());
  await enableAccessibility(page);
  await openTextMode(page);
  await sendText(page, '好球！');

  await expect.poll(async () => {
    const traces = await apiGet(request, `/api/matches/${matchId}/traces?limit=20`);
    const trace = traces.traces.find((item) => item.input === '好球！');
    return Boolean(trace && trace.output && trace.output !== '嗯，我在。');
  }, { timeout: 30_000 }).toBe(true);

  const traces = await apiGet(request, `/api/matches/${matchId}/traces?limit=20`);
  const trace = traces.traces.find((item) => item.input === '好球！');
  expect(trace.intent).toBe('emotion_reaction');
  expect(trace.output).not.toMatch(/进球|破门|领先/);
  await expect(page.getByText(trace.output).last()).toBeVisible();

  const state = await apiGet(request, `/api/matches/${matchId}/state`);
  expect(state.snapshot.score).toEqual({ home: 0, away: 0 });
});

test('人工与外部源冲突时，球球暂停确认赛况', async ({ page, request }) => {
  const conflictResponse = await request.post(`/api/matches/${matchId}/events`, {
    data: {
      source: 'api-sports',
      providerEventId: 'browser-source-conflict',
      eventType: 'goal',
      period: 'first_half',
      clock: '24:00',
      teamId: 'away',
      teamName: '德国',
      playerName: '穆西亚拉',
      score: { home: 1, away: 1 },
      description: '穆西亚拉进球。',
    },
    headers: { Authorization: `Bearer ${token}`, 'Idempotency-Key': testIdempotencyKey() },
  });
  expect(conflictResponse.status()).toBe(409);
  const state = await apiGet(request, `/api/matches/${matchId}/state`);
  expect(state.snapshot.integrity.status).toBe('conflict');

  await page.goto(matchURL());
  await enableAccessibility(page);
  await openTextMode(page);
  await sendText(page, '西班牙1比0德国');
  await expect(page.getByText(/还不能确定/).last()).toBeVisible({ timeout: 30_000 });
  const traces = await apiGet(request, `/api/matches/${matchId}/traces?limit=20`);
  const trace = traces.traces.find((item) => item.input === '西班牙1比0德国');
  expect(trace.claim).toMatchObject({ kind: 'score', status: 'unverified' });

  const ledger = await apiGet(request, `/api/matches/${matchId}/events`);
  const conflict = ledger.conflicts.find((item) => item.status === 'open');
  const acceptedFactId = conflict.members.find((member) => member.role === 'accepted').factId;
  await apiPost(request, `/api/matches/${matchId}/conflicts/${conflict.id}/resolve`, {
    chosenFactId: acceptedFactId,
    reason: '浏览器端到端验证保留原事实',
  });
  await expect(page.getByText(/1\s*—\s*0/).first()).toBeVisible({ timeout: 10_000 });
  const resolvedState = await apiGet(request, `/api/matches/${matchId}/state`);
  expect(resolvedState.snapshot.score).toEqual({ home: 1, away: 0 });
  expect(resolvedState.snapshot.integrity.status).toBe('ok');
});

test('导演赛前配置通过页面保存，并同步到事实 API', async ({ page, request }) => {
  // 赛前配置收敛到新台设置区（ADR-0013）。旧页的「缺 N 名首发」为旧页
  // 客户端校验，console 交给后端校验（评测种子一直是短名单）。
  const setup = await page.context().newPage();
  await setup.addInitScript((value) => {
    localStorage.setItem('qiuqiu.console.token', value);
  }, token);
  await setup.goto(`${consoleBaseURL}/console/#/console/match/${matchId}`);
  await expect(setup.getByText('赛前配置')).toBeVisible();
  await setup.getByLabel('主队名').fill('评测主队');
  await setup.getByLabel('客队名').fill('评测客队');
  await setup.getByLabel('主队球员').fill(rosterText('主队', 3));
  await setup.getByLabel('客队球员').fill(rosterText('客队', 2));
  await setup.getByRole('button', { name: '保存阵容' }).click();
  await expect(setup.getByText('阵容已保存').first()).toBeVisible();

  const preservedState = await apiGet(request, `/api/matches/${matchId}/state`);
  expect(preservedState.snapshot.score).toEqual({ home: 1, away: 0 });
  const preservedLedger = await apiGet(request, `/api/matches/${matchId}/events`);
  expect(preservedLedger.events).toHaveLength(1);

  await setup.getByLabel('主队球员').fill(rosterText('主队', 11));
  await setup.getByLabel('客队球员').fill(rosterText('客队', 11));
  await setup.getByRole('button', { name: '保存阵容' }).click();
  await expect(setup.getByText('阵容已保存').first()).toBeVisible();
  await setup.getByRole('button', { name: '开始比赛' }).click();
  await expect(setup.getByText('比赛已开始').first()).toBeVisible();
  await expect(setup.getByLabel('主队名')).toHaveValue('评测主队');

  const config = await apiGet(request, `/api/matches/${matchId}/config`);
  expect(config.config.homeTeam).toBe('评测主队');
  expect(config.config.awayTeam).toBe('评测客队');
  expect(config.config.homePlayers[0].name).toBe('主队球员1');

  const state = await apiGet(request, `/api/matches/${matchId}/state`);
  expect(state.snapshot.score).toEqual({ home: 0, away: 0 });
  const ledger = await apiGet(request, `/api/matches/${matchId}/events`);
  expect(ledger.events).toHaveLength(0);

  const clock = await apiGet(request, `/api/matches/${matchId}/clock`);
  expect(clock.clock).toMatchObject({
    period: 'pre_match',
    elapsedSeconds: 0,
    running: false,
  });

  // 旧页的 #homeScore/#awayScore/#clock 输入框随页面退役删除；
  // 归零语义已由上方 state/clock API 断言承接。
});

function rosterText(prefix, count) {
  return Array.from({ length: count }, (_, index) => `${index + 1} ${prefix}球员${index + 1} ${index === 0 ? 'GK' : 'MF'} 首发`).join('\n');
}

test('麦克风未授权时，用户侧保留可用的文字输入降级路径', async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem('flutter.first_meeting_completed', 'true');
    const denied = () => Promise.reject(new DOMException('Permission denied', 'NotAllowedError'));
    if (navigator.mediaDevices) {
      navigator.mediaDevices.getUserMedia = denied;
    } else {
      Object.defineProperty(navigator, 'mediaDevices', {
        configurable: true,
        value: { getUserMedia: denied },
      });
    }
  });
  await page.goto(matchURL());
  await enableAccessibility(page);
  await expect(page.getByText('没有麦克风权限，先打字也能继续陪看。')).toBeVisible({ timeout: 15_000 });
  await expect(page.getByLabel('直接和球球说…')).toBeEnabled();
});

async function enableAccessibility(page) {
  const button = page.getByRole('button', { name: 'Enable accessibility' });
  try {
    await button.waitFor({ state: 'visible', timeout: 3000 });
    await button.evaluate((element) => element.click());
  } catch {}
}

async function sendText(page, text) {
  const textbox = page.getByLabel('直接和球球说…');
  await expect(textbox).toBeVisible();
  await textbox.click();
  await textbox.pressSequentially(text, { delay: 5 });
  await page.getByRole('button', { name: '发送这句话' }).click();
}

async function apiPost(request, path, body) {
  const response = await request.post(path, {
    data: body,
    headers: { Authorization: `Bearer ${token}`, 'Idempotency-Key': testIdempotencyKey() },
  });
  if (!response.ok()) {
    throw new Error(`POST ${path} failed: ${response.status()} ${await response.text()}`);
  }
  return response.json();
}

async function apiGet(request, path) {
  const response = await request.get(path, {
    headers: { Authorization: `Bearer ${token}` },
  });
  expect(response.ok()).toBeTruthy();
  return response.json();
}

function testIdempotencyKey() {
  return `test-${Date.now()}-${Math.random().toString(16).slice(2)}`;
}

function matchURL() {
  return `/?matchId=${encodeURIComponent(matchId)}`;
}

async function startMatchClock(request, elapsedSeconds) {
  const response = await request.patch(`/api/matches/${matchId}/clock`, {
    data: { action: 'set', period: 'first_half', elapsedSeconds, expectedVersion: 0 },
    headers: { Authorization: `Bearer ${token}`, 'Idempotency-Key': testIdempotencyKey() },
  });
  if (!response.ok()) {
    throw new Error(`PATCH clock failed: ${response.status()} ${await response.text()}`);
  }
}

async function setAndStartMatchClock(request, elapsedSeconds) {
  await startMatchClock(request, elapsedSeconds);
  const response = await request.patch(`/api/matches/${matchId}/clock`, {
    data: { action: 'start', expectedVersion: 1 },
    headers: { Authorization: `Bearer ${token}`, 'Idempotency-Key': testIdempotencyKey() },
  });
  if (!response.ok()) {
    throw new Error(`PATCH clock start failed: ${response.status()} ${await response.text()}`);
  }
}
