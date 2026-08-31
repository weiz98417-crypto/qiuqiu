import { expect, test } from '@playwright/test';

const token = process.env.APP_TOKEN;

test('confirmed scorer survives event delivery, proactive speech, and a natural follow-up', async ({ page, request }) => {
  expect(token, 'APP_TOKEN is required for operator API calls').toBeTruthy();
  const matchId = `goal-follow-up-e2e-${Date.now()}`;
  const deviceId = `goal-follow-up-device-${Date.now()}`;

  await operatorRequest(request, 'post', `/api/matches/${matchId}/config`, {
    homeTeam: '西班牙',
    awayTeam: '德国',
    homePlayers: [{ number: '8', name: '佩德里', position: 'CM' }],
  });
  await operatorRequest(request, 'patch', `/api/matches/${matchId}/clock`, {
    action: 'set',
    period: 'first_half',
    elapsedSeconds: 2718,
    expectedVersion: 0,
  });

  const sessionResponse = await request.post('/api/sessions/anonymous', {
    data: { deviceId },
  });
  expect(sessionResponse.ok()).toBeTruthy();
  const session = await sessionResponse.json();

  await page.goto('/health');
  await connectSocket(page, matchId, session.accessToken);
  await page.evaluate(({ userId }) => {
    window.__goalFollowUpSocket.send(JSON.stringify({ type: 'session_opened', userId }));
  }, session);

  const created = await operatorRequest(request, 'post', `/api/matches/${matchId}/events`, {
    source: 'operator',
    eventType: 'goal',
    period: 'first_half',
    clock: '45:18',
    teamId: 'home',
    teamName: '西班牙',
    playerName: '佩德里',
    participants: [{ role: 'scorer', name: '佩德里', teamId: 'home', teamName: '西班牙' }],
    score: { home: 1, away: 0 },
    intensity: 5,
    confirmed: true,
    factStatus: 'confirmed',
    description: '佩德里进球了！',
    visibility: 'public',
  });
  const goal = created.event;
  expect(goal.playerName).toBe('佩德里');
  expect(goal.description).toContain('佩德里');
  expect(goal.proactiveText).toContain('佩德里');
  expect(goal.proactiveText).toContain('1比0');

  await expect.poll(() => socketMessage(page, { type: 'match_event', eventId: goal.id }), {
    timeout: 15_000,
  }).not.toBeNull();
  const proactive = await waitForReply(page, 'match_reaction');
  expect(proactive.text).toContain('佩德里');
  expect(proactive.text).toContain('1比0');

  await page.evaluate(({ userId }) => {
    window.__goalFollowUpSocket.send(JSON.stringify({
      type: 'user_speech',
      userId,
      signalId: `goal-follow-up-${Date.now()}`,
      text: '谁进了？',
      mode: 'text',
      audio: '',
    }));
  }, session);
  const followUp = await waitForReply(page, 'conversation');
  expect(followUp.text).toContain('佩德里');
  expect(followUp.text).not.toBe('嗯，我在。');

  const traces = await operatorRequest(request, 'get', `/api/matches/${matchId}/traces?limit=20`);
  const trace = traces.traces.find((item) => item.input === '谁进了？');
  expect(trace?.intent).toBe('recent_event_question');
  expect(trace?.output).toContain('佩德里');
  expect(trace?.retrievedEventIds).toContain(goal.id);

  await page.evaluate(() => window.__goalFollowUpSocket.close());
});

test('colloquial goal reports and personal shares never collapse to a presence acknowledgement', async ({ page, request }) => {
  expect(token, 'APP_TOKEN is required for operator API calls').toBeTruthy();
  const matchId = `natural-turn-e2e-${Date.now()}`;
  const deviceId = `natural-turn-device-${Date.now()}`;

  await operatorRequest(request, 'post', `/api/matches/${matchId}/config`, {
    homeTeam: '西班牙',
    awayTeam: '德国',
  });
  await operatorRequest(request, 'patch', `/api/matches/${matchId}/clock`, {
    action: 'set',
    period: 'first_half',
    elapsedSeconds: 600,
    expectedVersion: 0,
  });

  const sessionResponse = await request.post('/api/sessions/anonymous', {
    data: { deviceId },
  });
  expect(sessionResponse.ok()).toBeTruthy();
  const session = await sessionResponse.json();

  await page.goto('/health');
  await connectSocket(page, matchId, session.accessToken);
  await page.evaluate(({ userId }) => {
    window.__goalFollowUpSocket.send(JSON.stringify({ type: 'session_opened', userId }));
    window.__goalFollowUpSocket.send(JSON.stringify({
      type: 'user_speech',
      userId,
      signalId: `colloquial-goal-${Date.now()}`,
      text: '进啦',
      mode: 'text',
      audio: '',
    }));
  }, session);
  const goalReport = await waitForReply(page, 'conversation');
  expect(goalReport.text).not.toBe('嗯，我在。');
  expect(goalReport.text).toMatch(/没跟上|等一下|还不能/);

  await page.evaluate(({ userId }) => {
    window.__goalFollowUpMessages = [];
    window.__goalFollowUpSocket.send(JSON.stringify({
      type: 'user_speech',
      userId,
      signalId: `personal-goal-${Date.now()}`,
      text: '我打进啦',
      mode: 'text',
      audio: '',
    }));
  }, session);
  const personalShare = await waitForReply(page, 'conversation');
  expect(personalShare.text).not.toBe('嗯，我在。');
  expect(personalShare.text).not.toBe('在，看着呢。');

  const traces = await operatorRequest(request, 'get', `/api/matches/${matchId}/traces?limit=20`);
  const colloquialTrace = traces.traces.find((item) => item.input === '进啦');
  expect(colloquialTrace?.intent).toBe('match_fact_claim');
  expect(colloquialTrace?.claim).toMatchObject({ kind: 'event', status: 'unverified' });
  const personalTrace = traces.traces.find((item) => item.input === '我打进啦');
  expect(personalTrace?.intent).toBe('personal_share');
  expect(personalTrace?.output).not.toBe('嗯，我在。');

  await page.evaluate(() => window.__goalFollowUpSocket.close());
});

async function connectSocket(page, matchId, accessToken) {
  await page.evaluate(({ targetMatchId, tokenValue }) => new Promise((resolve, reject) => {
    const encoded = btoa(tokenValue).replaceAll('+', '-').replaceAll('/', '_').replaceAll('=', '');
    window.__goalFollowUpMessages = [];
    window.__goalFollowUpSocket = new WebSocket(
      `${location.protocol === 'https:' ? 'wss:' : 'ws:'}//${location.host}/ws/match/${targetMatchId}`,
      `qiuqiu-auth.${encoded}`,
    );
    window.__goalFollowUpSocket.onmessage = (event) => {
      if (typeof event.data !== 'string') return;
      try { window.__goalFollowUpMessages.push(JSON.parse(event.data)); } catch (_) {}
    };
    window.__goalFollowUpSocket.onerror = () => reject(new Error('goal follow-up websocket failed'));
    window.__goalFollowUpSocket.onopen = () => resolve();
  }), { targetMatchId: matchId, tokenValue: accessToken });
}

async function waitForReply(page, source) {
  await expect.poll(() => socketMessage(page, { event: 'qiuqiu_reply', source }), {
    timeout: 20_000,
  }).not.toBeNull();
  const message = await socketMessage(page, { event: 'qiuqiu_reply', source });
  return message.data;
}

async function socketMessage(page, criteria) {
  return page.evaluate((wanted) => window.__goalFollowUpMessages.findLast((message) => (
    (!wanted.type || message?.type === wanted.type)
    && (!wanted.event || message?.event === wanted.event)
    && (!wanted.source || message?.data?.source === wanted.source)
    && (!wanted.eventId || message?.data?.id === wanted.eventId)
  )) || null, criteria);
}

async function operatorRequest(request, method, path, data) {
  const response = await request[method](path, {
    ...(data === undefined ? {} : { data }),
    headers: {
      Authorization: `Bearer ${token}`,
      'Idempotency-Key': `goal-follow-up-${Date.now()}-${Math.random().toString(16).slice(2)}`,
    },
  });
  if (!response.ok()) {
    throw new Error(`${method.toUpperCase()} ${path} failed: ${response.status()} ${await response.text()}`);
  }
  return response.json();
}
