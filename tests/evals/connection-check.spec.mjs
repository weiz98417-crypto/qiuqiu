import { expect, test } from '@playwright/test';

const token = process.env.APP_TOKEN;

test('connection checks stay natural and never expose routing policy', async ({ page, request }) => {
  expect(token, 'APP_TOKEN is required for trace verification').toBeTruthy();
  const matchId = `connection-check-e2e-${Date.now()}`;
  const deviceId = `connection-check-device-${Date.now()}`;

  const sessionResponse = await request.post('/api/sessions/anonymous', {
    data: { deviceId },
  });
  expect(sessionResponse.ok()).toBeTruthy();
  const session = await sessionResponse.json();

  await page.goto('/health');
  await connectSocket(page, matchId, session.accessToken);
  await sendUserSpeech(page, session.userId, '喂，球球，能听到我说话吗？');

  const connectionReply = await waitForReply(page);
  expect(connectionReply.text).toMatch(/听得到|听得见|听到|听见|听着/);
  expect(connectionReply.text).not.toMatch(/按陪看|已经确认的比赛信息|先.*理解/);

  await page.evaluate(() => { window.__connectionCheckMessages = []; });
  await sendUserSpeech(page, session.userId, '请你分析一下今天球场草皮对传控节奏的隐藏影响');

  const unknownReply = await waitForReply(page);
  expect(unknownReply.text).not.toMatch(/按陪看|已经确认的比赛信息|先.*理解/);

  const traces = await operatorRequest(request, `/api/matches/${matchId}/traces?limit=20`);
  const connectionTrace = traces.traces.find((item) => item.input === '喂，球球，能听到我说话吗？');
  expect(connectionTrace?.intent).toBe('smalltalk');
  expect(connectionTrace?.output).not.toMatch(/按陪看|已经确认的比赛信息|先.*理解/);
  const unknownTrace = traces.traces.find((item) => item.input === '请你分析一下今天球场草皮对传控节奏的隐藏影响');
  expect(unknownTrace?.intent).toBe('unknown');
  expect(unknownTrace?.output).not.toMatch(/按陪看|已经确认的比赛信息|先.*理解/);

  await page.evaluate(() => window.__connectionCheckSocket.close());
});

test('compliment follow-ups remain natural conversation', async ({ page, request }) => {
  expect(token, 'APP_TOKEN is required for trace verification').toBeTruthy();
  const matchId = `compliment-thread-e2e-${Date.now()}`;
  const deviceId = `compliment-thread-device-${Date.now()}`;

  const sessionResponse = await request.post('/api/sessions/anonymous', {
    data: { deviceId },
  });
  expect(sessionResponse.ok()).toBeTruthy();
  const session = await sessionResponse.json();

  await page.goto('/health');
  await connectSocket(page, matchId, session.accessToken);
  await sendUserSpeech(page, session.userId, '你今天看起来精神不错呀！');

  const complimentReply = await waitForReply(page);
  expect(complimentReply.text).not.toMatch(/这话有点意思|没接明白/);

  await page.evaluate(() => { window.__connectionCheckMessages = []; });
  await sendUserSpeech(page, session.userId, '有点意思吗？我觉得你是有点意思。');

  const followUpReply = await waitForReply(page);
  expect(followUpReply.text).not.toMatch(/这话有点意思|没接明白/);

  const traces = await operatorRequest(request, `/api/matches/${matchId}/traces?limit=20`);
  const complimentTrace = traces.traces.find((item) => item.input === '你今天看起来精神不错呀！');
  expect(complimentTrace?.intent).toBe('smalltalk');
  expect(complimentTrace?.output).not.toMatch(/这话有点意思|没接明白/);
  const followUpTrace = traces.traces.find((item) => item.input === '有点意思吗？我觉得你是有点意思。');
  expect(followUpTrace?.intent).toBe('smalltalk');
  expect(followUpTrace?.output).not.toMatch(/这话有点意思|没接明白/);

  await page.evaluate(() => window.__connectionCheckSocket.close());
});

test('mixed-language time greetings use the social intent family', async ({ page, request }) => {
  expect(token, 'APP_TOKEN is required for trace verification').toBeTruthy();
  const matchId = `mixed-greeting-e2e-${Date.now()}`;
  const deviceId = `mixed-greeting-device-${Date.now()}`;

  const sessionResponse = await request.post('/api/sessions/anonymous', {
    data: { deviceId },
  });
  expect(sessionResponse.ok()).toBeTruthy();
  const session = await sessionResponse.json();

  await page.goto('/health');
  await connectSocket(page, matchId, session.accessToken);
  await sendUserSpeech(page, session.userId, '好，Hello，球球，下午好啊。');

  const reply = await waitForReply(page);
  expect(reply.text).toContain('下午好');
  expect(reply.text).not.toMatch(/没接明白|换个说法/);

  const traces = await operatorRequest(request, `/api/matches/${matchId}/traces?limit=20`);
  const trace = traces.traces.find((item) => item.input === '好，Hello，球球，下午好啊。');
  expect(trace?.intent).toBe('smalltalk');
  expect(trace?.output).toContain('下午好');

  await page.evaluate(() => window.__connectionCheckSocket.close());
});

test('schedule questions do not fall back to a listening acknowledgement', async ({ page, request }) => {
  expect(token, 'APP_TOKEN is required for trace verification').toBeTruthy();
  const matchId = `schedule-question-e2e-${Date.now()}`;
  const deviceId = `schedule-question-device-${Date.now()}`;

  const sessionResponse = await request.post('/api/sessions/anonymous', {
    data: { deviceId },
  });
  expect(sessionResponse.ok()).toBeTruthy();
  const session = await sessionResponse.json();

  await page.goto('/health');
  await connectSocket(page, matchId, session.accessToken);
  await sendUserSpeech(page, session.userId, '有什么比赛吗？');

  const reply = await waitForReply(page);
  expect(reply.text).not.toMatch(/听着呢|我在|没接明白|换个说法/);

  const traces = await operatorRequest(request, `/api/matches/${matchId}/traces?limit=20`);
  const trace = traces.traces.find((item) => item.input === '有什么比赛吗？');
  expect(trace?.intent).toBe('schedule_question');
  expect(trace?.output).not.toMatch(/听着呢|我在|没接明白|换个说法/);

  await page.evaluate(() => window.__connectionCheckSocket.close());
});

test('schedule lookups acknowledge, report results, and stop after interruption', async ({ page, request }) => {
  expect(token, 'APP_TOKEN is required for trace verification').toBeTruthy();
  const matchId = `progressive-schedule-e2e-${Date.now()}`;
  const deviceId = `progressive-schedule-device-${Date.now()}`;

  const sessionResponse = await request.post('/api/sessions/anonymous', {
    data: { deviceId },
  });
  expect(sessionResponse.ok()).toBeTruthy();
  const session = await sessionResponse.json();

  await page.goto('/health');
  await connectSocket(page, matchId, session.accessToken);
  await sendUserSpeech(page, session.userId, '明天有什么比赛？');

  const acknowledgement = await waitForReplyFromSource(page, 'conversation');
  expect(acknowledgement.data.text).toContain('我去找找');

  const result = await waitForReplyFromSource(page, 'schedule_lookup', acknowledgement.index + 1);
  expect(result.data.text).toContain('西班牙');
  expect(result.data.text).toContain('德国');
  expect(result.data.deliveryKey).toBeTruthy();

  await expect.poll(async () => {
    const traces = await operatorRequest(request, `/api/matches/${matchId}/traces?limit=20`);
    const acknowledgementTrace = traces.traces.find((item) => item.id === acknowledgement.data.traceId);
    const resultTrace = traces.traces.find((item) => item.id === result.data.traceId);
    return {
      acknowledgementLookupId: acknowledgementTrace?.lookupId,
      resultLookupId: resultTrace?.lookupId,
      resultParentTraceId: resultTrace?.parentTraceId,
    };
  }).toEqual({
    acknowledgementLookupId: result.data.deliveryKey,
    resultLookupId: result.data.deliveryKey,
    resultParentTraceId: acknowledgement.data.traceId,
  });

  await page.evaluate(() => { window.__connectionCheckMessages = []; });
  await sendUserSpeech(page, session.userId, '今天还有什么比赛？');
  await waitForReplyFromSource(page, 'conversation');
  await page.evaluate(() => {
    window.__connectionCheckSocket.send(JSON.stringify({ type: 'interrupt' }));
  });
  await page.waitForTimeout(750);
  const lateResults = await page.evaluate(() => window.__connectionCheckMessages.filter((message) => (
    message?.event === 'qiuqiu_reply' && message?.data?.source === 'schedule_lookup'
  )));
  expect(lateResults).toHaveLength(0);

  await page.evaluate(() => window.__connectionCheckSocket.close());
});

async function connectSocket(page, matchId, accessToken) {
  await page.evaluate(({ targetMatchId, tokenValue }) => new Promise((resolve, reject) => {
    const encoded = btoa(tokenValue).replaceAll('+', '-').replaceAll('/', '_').replaceAll('=', '');
    window.__connectionCheckMessages = [];
    window.__connectionCheckSocket = new WebSocket(
      `${location.protocol === 'https:' ? 'wss:' : 'ws:'}//${location.host}/ws/match/${targetMatchId}`,
      `qiuqiu-auth.${encoded}`,
    );
    window.__connectionCheckSocket.onmessage = (event) => {
      if (typeof event.data !== 'string') return;
      try { window.__connectionCheckMessages.push(JSON.parse(event.data)); } catch (_) {}
    };
    window.__connectionCheckSocket.onerror = () => reject(new Error('connection-check websocket failed'));
    window.__connectionCheckSocket.onopen = () => resolve();
  }), { targetMatchId: matchId, tokenValue: accessToken });
}

async function sendUserSpeech(page, userId, text) {
  await page.evaluate(({ activeUserId, input }) => {
    window.__connectionCheckSocket.send(JSON.stringify({
      type: 'user_speech',
      userId: activeUserId,
      signalId: `connection-check-${Date.now()}-${Math.random().toString(16).slice(2)}`,
      text: input,
      mode: 'text',
      audio: '',
    }));
  }, { activeUserId: userId, input: text });
}

async function waitForReply(page) {
  await expect.poll(() => page.evaluate(() => (
    window.__connectionCheckMessages.findLast((message) => (
      message?.event === 'qiuqiu_reply' && message?.data?.source === 'conversation'
    ))?.data || null
  )), { timeout: 20_000 }).not.toBeNull();
  return page.evaluate(() => window.__connectionCheckMessages.findLast((message) => (
    message?.event === 'qiuqiu_reply' && message?.data?.source === 'conversation'
  )).data);
}

async function waitForReplyFromSource(page, source, startIndex = 0) {
  const findReply = () => page.evaluate(({ expectedSource, fromIndex }) => {
    const messages = window.__connectionCheckMessages || [];
    for (let index = fromIndex; index < messages.length; index += 1) {
      const message = messages[index];
      if (message?.event === 'qiuqiu_reply' && message?.data?.source === expectedSource) {
        return { index, data: message.data };
      }
    }
    return null;
  }, { expectedSource: source, fromIndex: startIndex });

  await expect.poll(findReply, { timeout: 20_000 }).not.toBeNull();
  return findReply();
}

async function operatorRequest(request, path) {
  const response = await request.get(path, {
    headers: { Authorization: `Bearer ${token}` },
  });
  if (!response.ok()) {
    throw new Error(`GET ${path} failed: ${response.status()} ${await response.text()}`);
  }
  return response.json();
}
