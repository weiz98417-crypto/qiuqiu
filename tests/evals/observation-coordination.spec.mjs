import { expect, test } from '@playwright/test';

const token = process.env.APP_TOKEN || 'qiuqiu-dev-token';
const matchId = 'test';

test.describe.configure({ timeout: 60_000 });

test('用户领先现场时，导播确认与撤销只跟进对应用户', async ({ browser, page, request }) => {
  await apiPost(request, `/api/matches/${matchId}/reset`, {});
  await apiPost(request, `/api/matches/${matchId}/config`, {
    homeTeam: '西班牙',
    awayTeam: '德国',
    homePlayers: [{ number: '10', name: '佩德里', position: 'CM' }],
  });
  await startMatchClock(request, matchId);
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

  await prepareClient(page);
  const otherSessionResponse = await request.post('/api/sessions/anonymous', {
    data: { deviceId: `observation-other-${Date.now()}` },
  });
  expect(otherSessionResponse.ok()).toBeTruthy();
  const otherSession = await otherSessionResponse.json();
  const otherContext = await browser.newContext();
  const otherPage = await otherContext.newPage();
  await otherPage.goto('/health');
  await connectTestSocket(otherPage, matchId, otherSession);
  await otherPage.evaluate(({ userId }) => {
    window.__testSocket.send(JSON.stringify({ type: 'identify', userId }));
    window.__testSocket.send(JSON.stringify({ type: 'session_opened', userId }));
  }, otherSession);

  await openTextMode(page);
  await sendText(page, '佩德里刚刚进球了吧');
  await expect(page.getByText(/还没跟上/).last()).toBeVisible({ timeout: 30_000 });

  const pendingTrace = await findTrace(request, '佩德里刚刚进球了吧');
  expect(pendingTrace.claim).toMatchObject({ kind: 'event', status: 'unverified' });
  expect(pendingTrace.observation).toMatchObject({ status: 'pending_sync', userId: expect.any(String) });

  const operator = await page.context().newPage();
  await operator.goto(`/operator.html?token=${encodeURIComponent(token)}#live`);
  await operator.locator('#homeChips .player-chip').filter({ hasText: '佩德里' }).click();
  await operator.locator('#behaviorGroups .behavior-button[data-event-type="goal"]').click();
  await operator.locator('#occurredClock').fill('08:20');
  await operator.locator('#description').fill('佩德里禁区前沿推射破门。');
  await operator.locator('#confirmation').selectOption('confirmed');
  await operator.locator('#mode').selectOption('quiet');
  await operator.locator('#draftSubmit').click();
  await expect(operator.locator('#toast')).toContainText('已确认并发送：进球');
  await expect(operator.locator('#timeline')).toContainText('佩德里禁区前沿推射破门');

  await expect(page.getByText(/跟上了.*佩德里进的/).last()).toBeVisible({ timeout: 30_000 });
  expect(await latestSocketReply(otherPage, 'observation_resolution')).toBe('');

  const events = await apiGet(request, `/api/matches/${matchId}/events`);
  const goal = events.events.find((event) => event.eventType === 'goal' && event.playerName === '佩德里');
  expect(goal).toBeTruthy();
  await apiPost(request, `/api/matches/${matchId}/facts/${goal.factId}/revoke`, {});

  await expect(page.getByText(/这球没算/).last()).toBeVisible({ timeout: 30_000 });
  expect(await latestSocketReply(otherPage, 'observation_resolution')).toBe('');

  const traces = await apiGet(request, `/api/matches/${matchId}/traces?limit=50`);
  const resolutions = traces.traces.filter((trace) => trace.observationResolution);
  expect(resolutions.map((trace) => trace.observationResolution.status)).toEqual(
    expect.arrayContaining(['confirmed', 'contradicted']),
  );
  await closeTestSocket(otherPage);
  await otherContext.close();
});

test('离线期间确认的事实会在重连后跟进且展示后不重复', async ({ page, request }) => {
  await apiPost(request, `/api/matches/${matchId}/reset`, {});
  await apiPost(request, `/api/matches/${matchId}/config`, {
    homeTeam: '西班牙',
    awayTeam: '德国',
    homePlayers: [{ number: '10', name: '佩德里', position: 'CM' }],
  });
  await startMatchClock(request, matchId);
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

  await prepareClient(page);
  await openTextMode(page);
  await sendText(page, '佩德里刚刚进球了吧');
  await expect(page.getByText(/还没跟上/).last()).toBeVisible({ timeout: 30_000 });
  await page.close();

  await apiPost(request, `/api/matches/${matchId}/events`, {
    eventType: 'goal',
    period: 'first_half',
    clock: '09:10',
    teamId: 'home',
    teamName: '西班牙',
    playerName: '佩德里',
    score: { home: 1, away: 0 },
    description: '佩德里推射破门。',
    proactiveText: '__quiet__',
    visibility: 'public',
    confirmed: true,
  });

  const recoveredPage = await page.context().newPage();
  await prepareClient(recoveredPage);
  await expect(recoveredPage.getByText(/跟上了.*佩德里进的/).last()).toBeVisible({ timeout: 12_000 });
  await recoveredPage.waitForTimeout(500);
  await recoveredPage.close();

  const secondReconnect = await page.context().newPage();
  await prepareClient(secondReconnect);
  await secondReconnect.waitForTimeout(1500);
  await expect(secondReconnect.getByText(/跟上了.*佩德里进的/)).toHaveCount(0);
});

test('没有其他在线客户端时也能调和离线用户的观察', async ({ page, request }) => {
  const isolatedMatchId = `demo-offline-observation-${Date.now()}`;
  const clientContext = page.context();
  await apiPost(request, `/api/matches/${isolatedMatchId}/reset`, {});
  await apiPost(request, `/api/matches/${isolatedMatchId}/config`, {
    homeTeam: '西班牙',
    awayTeam: '德国',
    homePlayers: [{ number: '10', name: '佩德里', position: 'CM' }],
  });
  await startMatchClock(request, isolatedMatchId);
  await apiPost(request, `/api/matches/${isolatedMatchId}/events`, {
    eventType: 'kickoff',
    period: 'first_half',
    clock: '00:01',
    score: { home: 0, away: 0 },
    description: '比赛开始。',
    proactiveText: '__quiet__',
    visibility: 'public',
    confirmed: true,
  });
  const sessionResponse = await request.post('/api/sessions/anonymous', {
    data: { deviceId: `offline-${Date.now()}` },
  });
  expect(sessionResponse.ok()).toBeTruthy();
  const session = await sessionResponse.json();

  await page.goto('/health');
  await connectTestSocket(page, isolatedMatchId, session);
  await page.evaluate(({ userId }) => {
    window.__testSocket.send(JSON.stringify({ type: 'identify', userId }));
    window.__testSocket.send(JSON.stringify({ type: 'session_opened', userId }));
    window.__testSocket.send(JSON.stringify({
      type: 'user_speech',
      userId,
      signalId: `offline-claim-${Date.now()}`,
      text: '佩德里刚刚进球了吧',
      mode: 'text',
      audio: '',
    }));
  }, session);
  await expect.poll(() => latestSocketReply(page), { timeout: 15_000 }).toContain('还没跟上');
  await closeTestSocket(page);
  await page.close();
  await new Promise((resolve) => setTimeout(resolve, 1000));

  await apiPost(request, `/api/matches/${isolatedMatchId}/events`, {
    eventType: 'goal',
    period: 'first_half',
    clock: '10:30',
    teamId: 'home',
    teamName: '西班牙',
    playerName: '佩德里',
    score: { home: 1, away: 0 },
    description: '佩德里推射破门。',
    proactiveText: '__quiet__',
    visibility: 'public',
    confirmed: true,
  });
  await new Promise((resolve) => setTimeout(resolve, 1500));

  const recoveredSocketPage = await clientContext.newPage();
  await recoveredSocketPage.goto('/health');
  await connectTestSocket(recoveredSocketPage, isolatedMatchId, session);
  await recoveredSocketPage.evaluate(({ userId }) => {
    window.__testSocket.send(JSON.stringify({ type: 'identify', userId }));
    window.__testSocket.send(JSON.stringify({ type: 'session_opened', userId }));
  }, session);
  await expect.poll(() => latestSocketReply(recoveredSocketPage, 'observation_resolution'), { timeout: 12_000 })
    .toContain('跟上了');
  const recovered = await recoveredSocketPage.evaluate(() => window.__testMessages
    .filter((message) => message?.event === 'qiuqiu_reply' && message?.data?.source === 'observation_resolution')
    .at(-1));
  await recoveredSocketPage.evaluate((traceId) => {
    window.__testSocket.send(JSON.stringify({ type: 'reply_displayed', traceId }));
  }, recovered.data.traceId);
  await recoveredSocketPage.waitForTimeout(500);
  await closeTestSocket(recoveredSocketPage);

  await connectTestSocket(recoveredSocketPage, isolatedMatchId, session);
  await recoveredSocketPage.evaluate(({ userId }) => {
    window.__testSocket.send(JSON.stringify({ type: 'identify', userId }));
    window.__testSocket.send(JSON.stringify({ type: 'session_opened', userId }));
  }, session);
  await recoveredSocketPage.waitForTimeout(1500);
  expect(await latestSocketReply(recoveredSocketPage, 'observation_resolution')).toBe('');
});

async function prepareClient(page) {
  await page.addInitScript((value) => {
    localStorage.setItem('qiuqiu.app.token', value);
    localStorage.setItem('flutter.first_meeting_completed', 'true');
  }, token);
  await page.goto('/');
  await page.getByRole('button', { name: 'Enable accessibility' }).evaluate((element) => element.click());
  await expect(page.getByRole('button', { name: '更多陪看方式' })).toBeVisible();
}

async function openTextMode(page) {
  const moreButton = page.getByRole('button', { name: '更多陪看方式' });
  await expect(moreButton).toBeVisible();
  await moreButton.evaluate((element) => element.click());
  const textModeItem = page.getByRole('menuitem', { name: '改用文字说' });
  await expect(textModeItem).toBeVisible();
  await textModeItem.evaluate((element) => element.click());
  await expect(page.getByRole('textbox')).toBeVisible();
}

async function sendText(page, text) {
  const textbox = page.getByRole('textbox');
  await textbox.fill(text);
  await page.getByRole('button', { name: '发送这句话' }).click();
}

async function findTrace(request, input) {
  await expect.poll(async () => {
    const traces = await apiGet(request, `/api/matches/${matchId}/traces?limit=50`);
    return traces.traces.find((trace) => trace.input === input) || null;
  }, { timeout: 30_000 }).not.toBeNull();
  const traces = await apiGet(request, `/api/matches/${matchId}/traces?limit=50`);
  return traces.traces.find((trace) => trace.input === input);
}

async function connectTestSocket(page, targetMatchId, session) {
  await page.evaluate(({ matchId, accessToken }) => new Promise((resolve, reject) => {
    const encoded = btoa(accessToken).replaceAll('+', '-').replaceAll('/', '_').replaceAll('=', '');
    window.__testMessages = [];
    window.__testSocket = new WebSocket(
      `${location.protocol === 'https:' ? 'wss:' : 'ws:'}//${location.host}/ws/match/${matchId}`,
      `qiuqiu-auth.${encoded}`,
    );
    window.__testSocket.onmessage = (event) => {
      if (typeof event.data !== 'string') return;
      try { window.__testMessages.push(JSON.parse(event.data)); } catch (_) {}
    };
    window.__testSocket.onerror = () => reject(new Error('test websocket failed'));
    window.__testSocket.onopen = () => resolve();
  }), { matchId: targetMatchId, accessToken: session.accessToken });
}

async function closeTestSocket(page) {
  await page.evaluate(() => new Promise((resolve) => {
    const socket = window.__testSocket;
    if (!socket || socket.readyState === WebSocket.CLOSED) return resolve();
    socket.addEventListener('close', () => resolve(), { once: true });
    socket.close();
  }));
}

async function latestSocketReply(page, source = '') {
  return page.evaluate((wantedSource) => {
    const reply = window.__testMessages
      .filter((message) => message?.event === 'qiuqiu_reply'
        && (!wantedSource || message?.data?.source === wantedSource))
      .at(-1);
    return reply?.data?.text || '';
  }, source);
}

async function apiPost(request, path, body) {
  const response = await request.post(path, {
    data: body,
    headers: {
      Authorization: `Bearer ${token}`,
      'Idempotency-Key': `observation-${Date.now()}-${Math.random().toString(16).slice(2)}`,
    },
  });
  if (!response.ok()) throw new Error(`POST ${path} -> ${response.status()}: ${await response.text()}`);
  return response.json();
}

async function apiGet(request, path) {
  const response = await request.get(path, { headers: { Authorization: `Bearer ${token}` } });
  if (!response.ok()) throw new Error(`GET ${path} -> ${response.status()}: ${await response.text()}`);
  return response.json();
}

async function startMatchClock(request, targetMatchId) {
  const response = await request.patch(`/api/matches/${targetMatchId}/clock`, {
    data: { action: 'set', period: 'first_half', elapsedSeconds: 1, expectedVersion: 0 },
    headers: {
      Authorization: `Bearer ${token}`,
      'Idempotency-Key': `observation-clock-${Date.now()}-${Math.random().toString(16).slice(2)}`,
    },
  });
  if (!response.ok()) throw new Error(`PATCH clock -> ${response.status()}: ${await response.text()}`);
}
