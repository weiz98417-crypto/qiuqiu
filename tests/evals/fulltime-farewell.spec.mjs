import { expect, test } from '@playwright/test';
import { openTextMode } from './support/open-text-mode.mjs';

const token = process.env.APP_TOKEN || 'qiuqiu-dev-token';
const matchId = 'test';

test.beforeEach(async ({ page, request }) => {
  await page.addInitScript((value) => {
    localStorage.setItem('qiuqiu.app.token', value);
  }, token);
  // live2d.html iframe 是同源的，init script 也会在 iframe 里执行：在那里
  // 记录 Flutter 侧 live2d 桥（live2d_bridge_web.dart sendLive2dState）发来的
  // 每一次 qiuqiu-live2d-state 表演应用。
  await page.addInitScript(() => {
    window.__qLive2dStates = [];
    window.addEventListener('message', (event) => {
      const data = event.data;
      if (data && data.type === 'qiuqiu-live2d-state') {
        window.__qLive2dStates.push({
          expression: data.expression ?? '',
          motion: data.motion ?? '',
          speaking: data.speaking === true,
          at: Date.now(),
        });
      }
    });
  });
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

test('比赛进入完场时球球挥手的告别动作用一次，随后回到待机', async ({ page, request }) => {
  test.setTimeout(90_000);
  const replyDeliveries = [];
  page.on('websocket', (socket) => {
    socket.on('framereceived', (event) => {
      try {
        const message = JSON.parse(String(event.payload));
        if (message.event === 'qiuqiu_reply') replyDeliveries.push(message.data);
      } catch {
        // 非 JSON 帧（音频等）直接忽略。
      }
    });
  });

  await page.goto(matchURL());
  await enableAccessibility(page);
  await expect(page.getByText('比赛已连接')).toBeVisible({ timeout: 10_000 });

  await openTextMode(page);
  await sendText(page, '这场比赛真精彩');

  await expect.poll(async () => {
    const traces = await apiGet(request, `/api/matches/${matchId}/traces?limit=20`);
    const trace = traces.traces.find((item) => item.input === '这场比赛真精彩');
    return Boolean(trace && trace.output && trace.output !== '嗯，我在。');
  }, { timeout: 30_000 }).toBe(true);
  const traces = await apiGet(request, `/api/matches/${matchId}/traces?limit=20`);
  const trace = traces.traces.find((item) => item.input === '这场比赛真精彩');
  await expect(page.getByText(trace.output).last()).toBeVisible();

  // 等这条回复自带的表演 hold 按协议里的 holdMs 衰减完（衰减定时器在
  // tts_fallback 时才装填，加 1500ms 裕量）。ADR-0007 归属规则：持有中的
  // 后端表演在完场边上会压过告别。
  const replyDelivery = replyDeliveries.find((data) => (data?.text ?? '') === trace.output);
  const holdMS = replyDelivery?.presentation?.holdMs ?? 0;
  await page.waitForTimeout(holdMS + 1500);

  // 先把时钟切到 fulltime——客户端在 clock/snapshot 的完场边
  // （MatchSessionController._fireMatchEndOnEdge → applyMatchEnd）触发
  // phases.match_end = happy/wave 的一次性告别。
  await setMatchClock(request, { action: 'set', period: 'fulltime', elapsedSeconds: 5400 });

  // 可观察对象是客户端 live2d 状态桥：happy/wave 的表演应用要出现，且
  // live2d 舞台确实停在挥手（dataset.mode=event, dataset.motion=wave）。
  const live2dFrame = () => page.frames().find((frame) => frame.url().includes('/live2d.html'));
  await expect
    .poll(async () => {
      const frame = live2dFrame();
      if (!frame) return null;
      const states = await frame.evaluate(() => window.__qLive2dStates ?? []);
      return states.some((state) => state.motion === 'wave' && state.expression === 'happy');
    }, { timeout: 10_000 })
    .toBe(true);
  await expect
    .poll(() => live2dFrame().evaluate(() => {
      const dataset = document.body.dataset;
      return { mode: dataset.mode ?? '', motion: dataset.motion ?? '', mood: dataset.mood ?? '' };
    }), { timeout: 12_000, intervals: [500, 1000, 2000] })
    .toEqual({ mode: 'event', motion: 'wave', mood: 'happy' });

  // 再落一条 fulltime 事件把完场记进事实账本（__quiet__ 不触发口播）。
  // 完场边已经消费过，随后的 match_event 投影不允许再来一次告别。
  await apiPost(request, `/api/matches/${matchId}/events`, {
    eventType: 'fulltime',
    period: 'fulltime',
    clock: '90:00',
    teamId: 'home',
    teamName: '西班牙',
    score: { home: 1, away: 0 },
    intensity: 3,
    description: '全场比赛结束。',
    proactiveText: '__quiet__',
    visibility: 'public',
  });

  // 一次性：wave 的表演应用恰好出现一次。
  const states = await live2dFrame().evaluate(() => window.__qLive2dStates ?? []);
  expect(states.filter((state) => state.motion === 'wave')).toHaveLength(1);

  // wave 是 one-shot：live2d 的空闲调度在 hold 过后把身体交还给待机档。
  await expect
    .poll(() => live2dFrame().evaluate(() => {
      const dataset = document.body.dataset;
      return { mode: dataset.mode ?? '', motion: dataset.motion ?? '' };
    }), { timeout: 20_000, intervals: [1000, 2000, 4000] })
    .toEqual({ mode: 'idle', motion: 'idle' });

  const statesAfterIdle = await live2dFrame().evaluate(() => window.__qLive2dStates ?? []);
  expect(statesAfterIdle.filter((state) => state.motion === 'wave')).toHaveLength(1);
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
  await setMatchClock(request, { action: 'set', period: 'first_half', elapsedSeconds, expectedVersion: 0 });
}

async function setMatchClock(request, command) {
  const data = { ...command };
  if (data.expectedVersion === undefined) {
    // applyClockCommand 校验 ExpectedVersion，先读当前时钟版本再落指令。
    const current = await apiGet(request, `/api/matches/${matchId}/clock`);
    data.expectedVersion = current.clock.version;
  }
  const response = await request.patch(`/api/matches/${matchId}/clock`, {
    data,
    headers: { Authorization: `Bearer ${token}`, 'Idempotency-Key': testIdempotencyKey() },
  });
  if (!response.ok()) {
    throw new Error(`PATCH clock failed: ${response.status()} ${await response.text()}`);
  }
}
