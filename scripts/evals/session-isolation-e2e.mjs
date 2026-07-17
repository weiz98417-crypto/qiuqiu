import { fileURLToPath } from 'node:url';

import { startEvalBackend } from './backend.mjs';

export async function runSessionIsolationE2E() {
  const backend = await startEvalBackend({
    environment: {
      AUTH_MODE: 'session',
      SESSION_SIGNING_KEY: 'eval-session-signing-key-0123456789',
    },
  });
  const matchId = 'session-isolation';

  try {
    const sessionA = await issueSession(backend.baseUrl, 'eval-device-a');
    const sessionB = await issueSession(backend.baseUrl, 'eval-device-b');
    assert(sessionA.userId !== sessionB.userId, 'anonymous sessions must have distinct user identities');

    const socketA = await openSocket(backend.baseUrl, matchId, sessionA.accessToken);
    const socketB = await openSocket(backend.baseUrl, matchId, sessionB.accessToken);
    try {
      const inputA = '现在几比几？我是会话一';
      const inputB = '刚才有什么比赛动态？我是会话二';
      const traceA = await sendUserTurn(socketA, inputA, 'session-a-turn');
      const traceB = await sendUserTurn(socketB, inputB, 'session-b-turn');
      assert(traceA !== traceB, 'session turns must produce distinct traces');

      const tracesResponse = await api(backend.baseUrl, `/api/matches/${matchId}/traces?limit=20`, {
        token: backend.token,
      });
      const persistedA = tracesResponse.body.traces.find((trace) => trace.input === inputA);
      const persistedB = tracesResponse.body.traces.find((trace) => trace.input === inputB);
      assert(persistedA?.userId === sessionA.userId, `session A trace leaked identity: ${JSON.stringify(persistedA)}`);
      assert(persistedB?.userId === sessionB.userId, `session B trace leaked identity: ${JSON.stringify(persistedB)}`);

      await assertHTTPStatus(backend.baseUrl, `/api/matches/${matchId}/traces`, 401, sessionA.accessToken);
      await assertHTTPStatus(backend.baseUrl, '/api/me/privacy', 401, backend.token);
      await assertHTTPStatus(backend.baseUrl, `/api/matches/${matchId}/reset`, 401, sessionA.accessToken, {
        method: 'POST',
        body: '{}',
      });

      const privacyA = await api(backend.baseUrl, `/api/me/privacy?userId=${encodeURIComponent(sessionB.userId)}`, {
        token: sessionA.accessToken,
      });
      const privacyB = await api(backend.baseUrl, `/api/me/privacy?userId=${encodeURIComponent(sessionA.userId)}`, {
        token: sessionB.accessToken,
      });
      assert(privacyA.body.userId === sessionA.userId, 'privacy query must remain bound to session A');
      assert(privacyB.body.userId === sessionB.userId, 'privacy query must remain bound to session B');
    } finally {
      await Promise.all([socketA.close(), socketB.close()]);
    }

    const forgedSocket = await openSocket(backend.baseUrl, matchId, sessionA.accessToken);
    try {
      const checkpoint = forgedSocket.checkpoint();
      forgedSocket.send({
        type: 'user_speech',
        userId: sessionB.userId,
        text: '这条消息不应该写入任何用户记录',
      });
      const authError = await forgedSocket.waitFor((message) => message.type === 'auth_error', 5_000, checkpoint);
      assert(authError.reason === 'user identity does not match session', `unexpected auth error: ${JSON.stringify(authError)}`);
    } finally {
      await forgedSocket.close();
    }

    await expectSocketRejected(backend.baseUrl, matchId, backend.token);

    return {
      suite: 'session-isolation-e2e',
      ok: true,
      users: [sessionA.userId, sessionB.userId],
    };
  } finally {
    await backend.stop();
  }
}

async function issueSession(baseUrl, deviceId) {
  const response = await api(baseUrl, '/api/sessions/anonymous', {
    method: 'POST',
    body: JSON.stringify({ deviceId }),
  });
  assert(response.status === 201, `session issue failed: ${response.status} ${JSON.stringify(response.body)}`);
  return response.body;
}

async function sendUserTurn(socket, text, signalId) {
  const checkpoint = socket.checkpoint();
  socket.send({ type: 'user_speech', text, signalId, talkativeness: 'normal' });
  const reply = await socket.waitFor(
    (message) => message.type === 'event' && message.event === 'qiuqiu_reply' && message.data?.traceId,
    8_000,
    checkpoint,
  );
  return reply.data.traceId;
}

async function assertHTTPStatus(baseUrl, path, expected, token, options = {}) {
  const response = await api(baseUrl, path, { ...options, token }, false);
  assert(response.status === expected, `${options.method || 'GET'} ${path} returned ${response.status}, expected ${expected}`);
}

async function api(baseUrl, path, options = {}, requireOK = true) {
  const response = await fetch(`${baseUrl}${path}`, {
    method: options.method || 'GET',
    headers: {
      ...(options.body ? { 'content-type': 'application/json' } : {}),
      ...(options.token ? { Authorization: `Bearer ${options.token}` } : {}),
      ...(options.method === 'POST' ? { 'Idempotency-Key': `session-e2e-${Date.now()}-${Math.random()}` } : {}),
    },
    body: options.body,
  });
  const text = await response.text();
  let body = text;
  try { body = text ? JSON.parse(text) : {}; } catch { /* retain text response */ }
  if (requireOK && !response.ok) throw new Error(`${options.method || 'GET'} ${path} -> ${response.status}: ${text}`);
  return { status: response.status, body };
}

async function openSocket(baseUrl, matchId, token) {
  const endpoint = new URL(baseUrl);
  endpoint.protocol = endpoint.protocol === 'https:' ? 'wss:' : 'ws:';
  endpoint.pathname = `/ws/match/${matchId}`;
  const protocol = `qiuqiu-auth.${Buffer.from(token).toString('base64url')}`;
  const socket = new WebSocket(endpoint, [protocol]);
  const messages = [];
  const waiters = [];
  socket.addEventListener('message', (event) => {
    if (typeof event.data !== 'string') return;
    let message;
    try { message = JSON.parse(event.data); } catch { return; }
    messages.push(message);
    const index = messages.length - 1;
    for (const waiter of [...waiters]) {
      if (index >= waiter.from && waiter.predicate(message)) {
        waiter.resolve(message);
        waiters.splice(waiters.indexOf(waiter), 1);
      }
    }
  });
  await once(socket, 'open', 5_000);
  return {
    checkpoint() { return messages.length; },
    send(message) { socket.send(JSON.stringify(message)); },
    waitFor(predicate, timeoutMs = 5_000, from = 0) {
      const existing = messages.slice(from).find(predicate);
      if (existing) return Promise.resolve(existing);
      return new Promise((resolveWait, rejectWait) => {
        const waiter = {
          predicate,
          from,
          resolve(value) {
            clearTimeout(timer);
            resolveWait(value);
          },
        };
        const timer = setTimeout(() => {
          const index = waiters.indexOf(waiter);
          if (index >= 0) waiters.splice(index, 1);
          rejectWait(new Error(`WebSocket message timed out: ${JSON.stringify(messages.slice(-8))}`));
        }, timeoutMs);
        waiters.push(waiter);
      });
    },
    async close() {
      if (socket.readyState === WebSocket.CLOSED) return;
      const closed = once(socket, 'close', 2_000).catch(() => undefined);
      socket.close();
      await closed;
    },
  };
}

async function expectSocketRejected(baseUrl, matchId, token) {
  const endpoint = new URL(baseUrl);
  endpoint.protocol = endpoint.protocol === 'https:' ? 'wss:' : 'ws:';
  endpoint.pathname = `/ws/match/${matchId}`;
  const protocol = `qiuqiu-auth.${Buffer.from(token).toString('base64url')}`;
  const socket = new WebSocket(endpoint, [protocol]);
  try {
    await new Promise((resolveRejected, rejectRejected) => {
      const timer = setTimeout(
        () => cleanup(rejectRejected, new Error('operator WebSocket authentication did not resolve')),
        3_000,
      );
      const onOpen = () => cleanup(rejectRejected, new Error('operator token authenticated the user WebSocket'));
      const onError = () => cleanup(resolveRejected);
      const onClose = () => cleanup(resolveRejected);
      const cleanup = (settle, value) => {
        clearTimeout(timer);
        socket.removeEventListener('open', onOpen);
        socket.removeEventListener('error', onError);
        socket.removeEventListener('close', onClose);
        settle(value);
      };
      socket.addEventListener('open', onOpen, { once: true });
      socket.addEventListener('error', onError, { once: true });
      socket.addEventListener('close', onClose, { once: true });
    });
  } finally {
    if (socket.readyState === WebSocket.OPEN) socket.close();
  }
}

function once(target, event, timeoutMs) {
  return new Promise((resolveOnce, rejectOnce) => {
    const timer = setTimeout(() => cleanup(rejectOnce, new Error(`${event} timed out`)), timeoutMs);
    const success = (value) => cleanup(resolveOnce, value);
    const cleanup = (settle, value) => {
      clearTimeout(timer);
      target.removeEventListener(event, success);
      settle(value);
    };
    target.addEventListener(event, success, { once: true });
  });
}

function assert(condition, message) {
  if (!condition) throw new Error(message);
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  try {
    console.log(JSON.stringify(await runSessionIsolationE2E(), null, 2));
  } catch (error) {
    console.error(error.stack || error.message);
    process.exitCode = 1;
  }
}
