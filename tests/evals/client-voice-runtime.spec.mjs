import { expect, test } from '@playwright/test';
import { qiuqiuBaseURL } from './support/base-url.mjs';

const token = process.env.APP_TOKEN;
const voiceRuntimeEnabled = process.env.QIUQIU_RUNTIME_TTS === '1';
const backend = qiuqiuBaseURL();
const client = backend;
const matchId = 'test';

test.skip(!token, 'APP_TOKEN is required for the runtime voice test');
test.setTimeout(120_000);

test.beforeEach(async ({ request }) => {
  await apiPost(request, `/api/matches/${matchId}/reset`, {});
  await apiPost(request, `/api/matches/${matchId}/config`, {
    homeTeam: '利物浦',
    awayTeam: '切尔西',
    homePlayers: [{ number: '11', name: '萨拉赫', position: 'RW' }],
    awayPlayers: [],
  });
  await apiPost(request, `/api/matches/${matchId}/events`, {
    eventType: 'goal',
    period: 'first_half',
    clock: '77:10',
    teamId: 'home',
    teamName: '利物浦',
    playerName: '萨拉赫',
    participants: [{ role: 'scorer', name: '萨拉赫', teamId: 'home', teamName: '利物浦' }],
    score: { home: 1, away: 0 },
    intensity: 5,
    description: '萨拉赫禁区内推射破门。',
    proactiveText: '__quiet__',
    visibility: 'public',
  });
  await apiPost(request, `/api/matches/${matchId}/events`, {
    eventType: 'pressure',
    period: 'first_half',
    clock: '78:30',
    teamId: 'home',
    teamName: '利物浦',
    score: { home: 1, away: 0 },
    intensity: 3,
    description: '利物浦持续压迫。',
    proactiveText: '__quiet__',
    visibility: 'public',
  });
});

test('客户端连续问答使用正确比赛事实并保留文字兜底', async ({ page, request }) => {
  await page.goto(client);
  await page
    .getByRole('button', { name: 'Enable accessibility' })
    .evaluate((element) => element.click());
  await expect(page.getByText('进入球球的看台')).toHaveCount(0);
  await expect(page.getByRole('button', { name: '更多陪看方式' })).toBeVisible();
  await openTextMode(page);

  const goalQuestion = '刚才谁进的球？';
  await sendText(page, goalQuestion);
  await expect(page.getByText(/萨拉赫/).last()).toBeVisible({ timeout: 30_000 });
  await expect(page.getByText(/导演台|导播台/)).toHaveCount(0);
  await waitForDelivery(request, goalQuestion);

  const scoreQuestion = '现在比分多少？';
  await sendText(page, scoreQuestion);
  await expect(page.getByText(/利物浦 1-0 切尔西/).last()).toBeVisible({ timeout: 30_000 });
  await expect(page.getByText(/pre_match/)).toHaveCount(0);
  await waitForDelivery(request, scoreQuestion);
});

test('浏览器拦截首屏语音后，首次触碰会恢复播放', async ({ page, request }) => {
  test.skip(!voiceRuntimeEnabled, 'runtime backend has no TTS provider');
  await page.addInitScript(() => {
    const originalPlay = HTMLMediaElement.prototype.play;
    let unlocked = false;
    document.addEventListener('pointerdown', () => {
      unlocked = true;
    }, true);
    HTMLMediaElement.prototype.play = function playAfterGesture() {
      if (!unlocked) {
        return Promise.reject(new DOMException('User gesture required', 'NotAllowedError'));
      }
      return originalPlay.call(this);
    };
  });
  await page.goto(client);
  await waitForPlaybackStatus(request, 'first_meeting', 'blocked:autoplay');
  await page.mouse.click(20, 20);
  await waitForPlaybackStatus(request, 'first_meeting', 'ok');
});

async function openTextMode(page) {
  await page.getByRole('button', { name: '更多陪看方式' }).click();
  await page.getByRole('menuitem', { name: '改用文字说' }).click();
  await expect(page.getByRole('textbox')).toBeVisible();
}

async function sendText(page, text) {
  const textbox = page.getByRole('textbox');
  await textbox.click();
  await textbox.pressSequentially(text, { delay: 5 });
  await page.getByRole('button', { name: '发送这句话' }).click();
}

async function waitForDelivery(request, input) {
  if (voiceRuntimeEnabled) {
    await waitForPlaybackStatus(request, input, 'ok');
    return;
  }
  await expect
    .poll(async () => {
      const trace = await findTrace(request, input);
      return trace?.output || '';
    }, { timeout: 30_000 })
    .not.toBe('');
}

async function waitForPlaybackStatus(request, input, status) {
  await expect
    .poll(async () => {
      const trace = await findTrace(request, input);
      return trace?.voice?.playbackStatus || '';
    }, { timeout: 30_000 })
    .toBe(status);
}

async function findTrace(request, input) {
  const response = await request.get(`${backend}/api/matches/${matchId}/traces?limit=50`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  if (!response.ok()) return null;
  const body = await response.json();
  const traces = Array.isArray(body.traces) ? body.traces : [];
  return traces.find((item) => item.input === input) || null;
}

function testIdempotencyKey() {
  return `test-${Date.now()}-${Math.random().toString(16).slice(2)}`;
}

async function apiPost(request, path, data) {
  const response = await request.post(`${backend}${path}`, {
    data,
    headers: { Authorization: `Bearer ${token}`, 'Idempotency-Key': testIdempotencyKey() },
  });
  if (!response.ok()) {
    throw new Error(`POST ${path} failed: ${response.status()} ${await response.text()}`);
  }
}
