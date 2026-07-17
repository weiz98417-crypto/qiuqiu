import { expect, test } from '@playwright/test';

const token = process.env.APP_TOKEN || 'qiuqiu-dev-token';
const matchId = 'test';

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
  await page.goto('/');
  await enableAccessibility(page);
  await expect(page.getByRole('button', { name: '更多陪看方式' })).toBeVisible();

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
  await page.getByRole('textbox').fill('刚才谁助攻？');
  await page.getByRole('button', { name: '发送这句话' }).click();
  await expect(page.getByText(/法比安/).last()).toBeVisible({ timeout: 30_000 });
  await expect(page.getByText(/亚马尔/).last()).toBeVisible({ timeout: 30_000 });

  const traces = await apiGet(request, `/api/matches/${matchId}/traces?token=${encodeURIComponent(token)}&limit=20`);
  const trace = traces.traces.find((item) => item.input === '刚才谁助攻？');
  expect(trace.intent).toBe('recent_event_question');
  expect(trace.retrievedEventIds).not.toHaveLength(0);
  expect(JSON.stringify(trace.toolCalls)).toContain('match.search_events');

  await page.goto(`/operator.html?token=${encodeURIComponent(token)}#traces`);
  await page.locator('#refreshTraces').click();
  await expect(page.locator('#traceList')).toContainText('刚才谁助攻？');
  await expect(page.locator('#memorySnapshot')).toContainText('1-0');
});

test('用户错误赛况不会覆盖比赛事实，并留下核验记录', async ({ page, request }) => {
  await page.goto('/');
  await enableAccessibility(page);
  await openTextMode(page);
  await page.getByRole('textbox').fill('德国已经3比0领先了');
  await page.getByRole('button', { name: '发送这句话' }).click();
  await expect(page.getByText(/西班牙 1-0 德国/).last()).toBeVisible({ timeout: 30_000 });

  const traces = await apiGet(request, `/api/matches/${matchId}/traces?limit=20`);
  const trace = traces.traces.find((item) => item.input === '德国已经3比0领先了');
  expect(trace.intent).toBe('match_fact_claim');
  expect(trace.claim).toMatchObject({ kind: 'score', status: 'contradicted' });
  expect(JSON.stringify(trace.toolCalls)).toContain('match.verify_user_claim');

  await page.goto(`/operator.html?token=${encodeURIComponent(token)}#traces`);
  await page.locator('#refreshTraces').click();
  await page.locator('#traceList').getByText('德国已经3比0领先了').click();
  await expect(page.locator('#traceDetail')).toContainText('用户赛况核验');
  await expect(page.locator('#traceDetail')).toContainText('contradicted');
});

test('错误进球者会被纠正，玩笑不会进入事实核验', async ({ page, request }) => {
  await page.goto('/');
  await enableAccessibility(page);
  await openTextMode(page);
  await sendText(page, '刚才哈兰德进球了');
  await expect(page.getByText(/不是哈兰德.*佩德里/).last()).toBeVisible({ timeout: 30_000 });

  await sendText(page, '开玩笑，德国3比0了');
  await expect(page.getByText(/我在，陪你看/).last()).toBeVisible({ timeout: 30_000 });
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
  await page.goto('/');
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
  await page.goto('/');
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

  await page.goto('/');
  await enableAccessibility(page);
  await openTextMode(page);
  await sendText(page, '西班牙1比0德国');
  await expect(page.getByText(/还不能确定/).last()).toBeVisible({ timeout: 30_000 });
  const traces = await apiGet(request, `/api/matches/${matchId}/traces?limit=20`);
  const trace = traces.traces.find((item) => item.input === '西班牙1比0德国');
  expect(trace.claim).toMatchObject({ kind: 'score', status: 'unverified' });
});

test('导演赛前配置通过页面保存，并同步到事实 API', async ({ page, request }) => {
  await page.goto(`/operator.html?token=${encodeURIComponent(token)}#setup`);
  await page.locator('#preHomeTeam').fill('评测主队');
  await page.locator('#preAwayTeam').fill('评测客队');
  await page.locator('#preHomePlayers').fill('10 测试前锋 ST');
  await page.locator('#preAwayPlayers').fill('9 测试门将 GK');
  await page.locator('#preSubmit').click();
  await expect(page.locator('#toast')).toContainText('赛前配置已同步到导演台');
  await expect(page.locator('#homeTeam')).toHaveValue('评测主队');

  const config = await apiGet(request, `/api/matches/${matchId}/config`);
  expect(config.config.homeTeam).toBe('评测主队');
  expect(config.config.awayTeam).toBe('评测客队');
  expect(config.config.homePlayers[0].name).toBe('测试前锋');
});

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
  await page.goto('/');
  await enableAccessibility(page);
  await expect(page.getByText('没有麦克风权限，先打字也能继续陪看。')).toBeVisible({ timeout: 15_000 });
  await expect(page.getByLabel('直接和球球说…')).toBeEnabled();
});

async function enableAccessibility(page) {
  await page
    .getByRole('button', { name: 'Enable accessibility' })
    .evaluate((element) => element.click());
}

async function openTextMode(page) {
  await page.getByRole('button', { name: '更多陪看方式' }).click();
  await page.getByRole('menuitem', { name: '改用文字说' }).click();
  await expect(page.getByRole('textbox')).toBeVisible();
}

async function sendText(page, text) {
  const textbox = page.getByRole('textbox');
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
