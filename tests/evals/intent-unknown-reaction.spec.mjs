import { expect, test } from '@playwright/test';
import { openTextMode } from './support/open-text-mode.mjs';

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

test('听不懂的话保留固定追问回复，并 ride confused/listening 一次性反应', async ({ page, request }) => {
  const qiuqiuReplies = [];
  page.on('websocket', (socket) => {
    socket.on('framereceived', (event) => {
      try {
        const message = JSON.parse(String(event.payload));
        if (message.event === 'qiuqiu_reply') qiuqiuReplies.push(message.data);
      } catch {
        // 非 JSON 帧（音频等）直接忽略。
      }
    });
  });

  await page.goto(matchURL());
  await enableAccessibility(page);
  await expect(page.getByText('比赛已连接')).toBeVisible({ timeout: 10_000 });

  await openTextMode(page);
  const unknownInput = '请你分析一下今天球场草皮对传控节奏的隐藏影响';
  await sendText(page, unknownInput);
  await expect(page.getByText(/这句我没接明白/).last()).toBeVisible({ timeout: 30_000 });

  const traces = await apiGet(request, `/api/matches/${matchId}/traces?token=${encodeURIComponent(token)}&limit=20`);
  const trace = traces.traces.find((item) => item.input === unknownInput);
  expect(trace.intent).toBe('unknown');
  expect(trace.output).toContain('这句我没接明白');

  // 协议层的 confused/listening 一次性反应：qiuqiu_reply 携带
  // presentation-map.json delivery.interrupted 那一行（confused + listening，
  // 带一次性 HoldMS）。trace 本身不落 presentation 字段，WS 投递是它在
  // 用户侧的可观察载体。
  const unknownReply = qiuqiuReplies.find((data) => (data?.text ?? '').includes('这句我没接明白'));
  expect(unknownReply, `qiuqiuReplies=${JSON.stringify(qiuqiuReplies).slice(0, 2000)}`).toBeTruthy();
  expect(unknownReply.presentation).toMatchObject({
    expression: 'confused',
    motion: 'listening',
  });
  expect(unknownReply.presentation.holdMs).toBeGreaterThan(0);
  // 返回态必须是客户端白名单里的真实目标（ADR-0007 归属规则）。
  expect(['decay_to_focus', 'decay_to_listening', 'decay_to_idle', 'watching'])
    .toContain(unknownReply.presentation.returnMode);
});

test('正常意图的回合仍拿到正常回复，不 ride confused/listening 反应', async ({ page, request }) => {
  const qiuqiuReplies = [];
  page.on('websocket', (socket) => {
    socket.on('framereceived', (event) => {
      try {
        const message = JSON.parse(String(event.payload));
        if (message.event === 'qiuqiu_reply') qiuqiuReplies.push(message.data);
      } catch {
        // 非 JSON 帧（音频等）直接忽略。
      }
    });
  });

  await page.goto(matchURL());
  await enableAccessibility(page);
  await expect(page.getByText('比赛已连接')).toBeVisible({ timeout: 10_000 });

  await openTextMode(page);
  await sendText(page, '在吗？');
  await expect(page.getByText(/在，听着呢。/).last()).toBeVisible({ timeout: 30_000 });

  const traces = await apiGet(request, `/api/matches/${matchId}/traces?token=${encodeURIComponent(token)}&limit=20`);
  const trace = traces.traces.find((item) => item.input === '在吗？');
  expect(trace.intent).not.toBe('unknown');

  const smalltalkReply = qiuqiuReplies.find((data) => (data?.text ?? '').includes('在，听着呢'));
  expect(smalltalkReply, `qiuqiuReplies=${JSON.stringify(qiuqiuReplies).slice(0, 2000)}`).toBeTruthy();
  // 与 TestKnownIntentTurnKeepsDirectorPresentation 语义一致：正常意图的回合
  // 不 ride interrupted 反应（confused/listening 只属于 unknown 回合）。
  const presentation = smalltalkReply.presentation;
  expect(presentation?.expression ?? '').not.toBe('confused');
  expect(presentation?.motion ?? '').not.toBe('listening');
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
