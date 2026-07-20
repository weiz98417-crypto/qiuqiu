import { fileURLToPath } from 'node:url';

const baseUrl = process.env.QIUQIU_BASE_URL;
const token = process.env.APP_TOKEN || 'qiuqiu-dev-token';
const matchId = 'test';

if (!baseUrl) throw new Error('QIUQIU_BASE_URL is required');

export async function runRuntimeEvals() {
  await resetMatch();
  await configureMatch();
  const socket = await openSocket();
  try {
    const beforeGoal = socket.checkpoint();
    const goal = await publish({
      eventType: 'goal',
      period: 'first_half',
      clock: '23:41',
      teamId: 'home',
      teamName: '西班牙',
      playerName: '佩德里',
      score: { home: 1, away: 0 },
      intensity: 5,
      description: '佩德里禁区前沿推射破门。',
      proactiveText: '运行时评测主动线：佩德里进球，法比安助攻。',
      visibility: 'public',
      participants: [
        { role: 'scorer', name: '佩德里', teamId: 'home', teamName: '西班牙' },
        { role: 'assist', name: '法比安', teamId: 'home', teamName: '西班牙' },
        { role: 'pre_assist', name: '亚马尔', teamId: 'home', teamName: '西班牙' },
      ],
    });
    const proactive = await socket.waitFor((message) => message.type === 'event'
      && message.event === 'qiuqiu_reply'
      && message.data?.text?.includes('运行时评测主动线'), 8_000, beforeGoal);
    assert(proactive.data?.traceId, 'proactive reply must include traceId');

    const beforeFollowUp = socket.checkpoint();
    socket.send({ type: 'user_speech', userId: 'runtime-fan', text: '刚才谁助攻？', talkativeness: 'normal' });
    const followUp = await socket.waitFor((message) => message.type === 'event'
      && message.event === 'qiuqiu_reply'
      && message.data?.text?.includes('法比安')
      && message.data?.text?.includes('亚马尔'), 8_000, beforeFollowUp);
    assert(followUp.data.text.includes('亚马尔'), `follow-up must mention pre-assist: ${followUp.data.text}`);

    const traces = await get(`/api/matches/${matchId}/traces?limit=20`);
    const userTrace = traces.traces.find((trace) => trace.input === '刚才谁助攻？');
    assert(userTrace, 'follow-up trace must be persisted');
    assert(userTrace.intent === 'recent_event_question', `unexpected intent ${userTrace.intent}`);
    assert(userTrace.retrievedEventIds?.includes(goal.event.id), 'follow-up trace must reference the goal');
    assert(JSON.stringify(userTrace.toolCalls).includes('match.search_events'), 'follow-up trace must record memory lookup');

    const quietMarker = '运行时评测静默事件';
    await publish({
      eventType: 'operator_note',
      period: 'first_half',
      clock: '24:00',
      teamId: 'home',
      teamName: '西班牙',
      score: { home: 1, away: 0 },
      intensity: 2,
      description: quietMarker,
      proactiveText: '__quiet__',
      visibility: 'public',
    });
    await socket.expectSilence((message) => message.type === 'event' && message.event === 'qiuqiu_reply' && message.data?.text?.includes(quietMarker), 700);

    await resetMatch();
    await configureMatch();
    const original = await publish({
      eventType: 'goal',
      period: 'first_half',
      clock: '12:00',
      teamId: 'home',
      teamName: '西班牙',
      playerName: '佩德里',
      score: { home: 1, away: 0 },
      intensity: 5,
      description: '应被取消的进球。',
      proactiveText: '__quiet__',
      visibility: 'public',
      participants: [{ role: 'assist', name: '法比安' }],
    });
    await post(`/api/matches/${matchId}/events/${encodeURIComponent(original.event.id)}/correct`, {
      eventType: 'var_check',
      period: 'first_half',
      clock: '13:10',
      teamId: 'home',
      teamName: '西班牙',
      score: { home: 0, away: 0 },
      intensity: 4,
      description: 'VAR 取消进球。',
      evidence: { correctionReason: 'VAR 回放确认原进球无效。' },
      proactiveText: '__quiet__',
      visibility: 'public',
    });
    const beforeCorrectedQuestion = socket.checkpoint();
    socket.send({ type: 'user_speech', userId: 'runtime-fan', text: '现在几比几？', talkativeness: 'normal' });
    const corrected = await socket.waitFor((message) => message.type === 'event'
      && message.event === 'qiuqiu_reply'
      && message.data?.text?.includes('0-0'), 8_000, beforeCorrectedQuestion);
    assert(!corrected.data.text.includes('1-0'), `corrected answer leaked old score: ${corrected.data.text}`);

    return {
      suite: 'runtime-e2e',
      ok: true,
      proactiveTraceId: proactive.data.traceId,
      retrievedEventId: goal.event.id,
    };
  } finally {
    await socket.close();
  }
}

async function configureMatch() {
  await post(`/api/matches/${matchId}/config`, {
    homeTeam: '西班牙',
    awayTeam: '德国',
    homePlayers: [
      { number: '10', name: '佩德里', position: 'CM' },
      { number: '8', name: '法比安', position: 'CM' },
      { number: '19', name: '亚马尔', position: 'RW' },
    ],
    awayPlayers: [{ number: '10', name: '穆西亚拉', position: 'AM' }],
  });
}

async function resetMatch() {
  await post(`/api/matches/${matchId}/reset`, {});
}

async function publish(event) {
  return post(`/api/matches/${matchId}/events`, {
    confirmed: event.eventType !== 'var_check',
    ...event,
  });
}

async function get(path) {
  const response = await fetch(`${baseUrl}${path}`, {
    headers: token ? { Authorization: `Bearer ${token}` } : {},
  });
  if (!response.ok) throw new Error(`GET ${path} -> ${response.status}: ${await response.text()}`);
  return response.json();
}

async function post(path, body) {
  const response = await fetch(`${baseUrl}${path}`, {
    method: 'POST',
    headers: {
      'content-type': 'application/json',
      'Idempotency-Key': `runtime-${Date.now()}-${Math.random().toString(16).slice(2)}`,
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
    body: JSON.stringify(body),
  });
  if (!response.ok) throw new Error(`POST ${path} -> ${response.status}: ${await response.text()}`);
  return response.json();
}

async function openSocket() {
  const endpoint = new URL(baseUrl);
  endpoint.protocol = endpoint.protocol === 'https:' ? 'wss:' : 'ws:';
  endpoint.pathname = `/ws/match/${matchId}`;
  endpoint.search = '';
  const protocols = token ? [`qiuqiu-auth.${Buffer.from(token).toString('base64url')}`] : [];
  const socket = new WebSocket(endpoint, protocols);
  await once(socket, 'open', 8_000);
  const messages = [];
  const waiters = [];
  socket.addEventListener('message', (event) => {
    if (typeof event.data !== 'string') return;
    let message;
    try { message = JSON.parse(event.data); } catch { return; }
    messages.push(message);
    const messageIndex = messages.length - 1;
    for (const waiter of [...waiters]) {
      if (messageIndex >= waiter.from && waiter.predicate(message)) {
        waiter.resolve(message);
        waiters.splice(waiters.indexOf(waiter), 1);
      }
    }
  });
  socket.send(JSON.stringify({ type: 'identify', userId: 'runtime-fan' }));
  await new Promise((resolve) => setTimeout(resolve, 50));
  return {
    send(message) { socket.send(JSON.stringify(message)); },
    checkpoint() { return messages.length; },
    waitFor(predicate, timeoutMs = 8_000, from = 0) {
      const existing = messages.slice(from).find(predicate);
      if (existing) return Promise.resolve(existing);
      return new Promise((resolveWait, rejectWait) => {
        const waiter = { predicate, from, resolve: (value) => { clearTimeout(timer); resolveWait(value); } };
        const timer = setTimeout(() => {
          const index = waiters.indexOf(waiter);
          if (index >= 0) waiters.splice(index, 1);
          rejectWait(new Error(`WebSocket message timed out: ${JSON.stringify(messages.slice(-8))}`));
        }, timeoutMs);
        waiters.push(waiter);
      });
    },
    async expectSilence(predicate, durationMs) {
      const before = messages.length;
      await new Promise((resolveWait) => setTimeout(resolveWait, durationMs));
      assert(!messages.slice(before).some(predicate), 'unexpected message arrived during required silence');
    },
    async close() {
      if (socket.readyState === 3) return;
      const closed = once(socket, 'close', 3_000).catch(() => undefined);
      socket.close();
      await closed;
    },
  };
}

function once(target, event, timeoutMs) {
  return new Promise((resolveOnce, rejectOnce) => {
    const timer = setTimeout(() => cleanup(rejectOnce, new Error(`${event} timed out`)), timeoutMs);
    const success = () => cleanup(resolveOnce);
    const failure = () => cleanup(rejectOnce, new Error(`WebSocket ${event} failed`));
    const cleanup = (settle, value) => {
      clearTimeout(timer);
      target.removeEventListener(event, success);
      target.removeEventListener('error', failure);
      settle(value);
    };
    target.addEventListener(event, success, { once: true });
    target.addEventListener('error', failure, { once: true });
  });
}

function assert(condition, message) {
  if (!condition) throw new Error(message);
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  try {
    console.log(JSON.stringify(await runRuntimeEvals(), null, 2));
    process.exit(0);
  } catch (error) {
    console.error(error.stack || error.message);
    process.exit(1);
  }
}
