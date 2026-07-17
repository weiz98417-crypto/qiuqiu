import { spawn } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const baseUrl = (process.env.QIUQIU_BASE_URL || 'http://127.0.0.1:8080').replace(/\/+$/, '');
const token = process.env.APP_TOKEN;
const containerName = process.env.QIUQIU_BACKEND_CONTAINER || 'qiuqiu-master-backend-1';
const matchId = 'demo-restart-e2e';

export async function runDockerRestartE2E() {
  if (!token) throw new Error('APP_TOKEN is required');

  await waitForHealth(30_000);
  await operatorPost(`/api/matches/${matchId}/reset`, {});
  try {
    await operatorPost(`/api/matches/${matchId}/config`, {
      homeTeam: 'Restart Home',
      awayTeam: 'Restart Away',
    });
    const created = await operatorPost(`/api/matches/${matchId}/events`, {
      source: 'operator',
      eventType: 'goal',
      period: 'first_half',
      clock: '31:15',
      teamId: 'home',
      teamName: 'Restart Home',
      playerName: 'Restart scorer',
      score: { home: 1, away: 0 },
      description: 'Process restart persistence check',
      proactiveText: '__quiet__',
      visibility: 'public',
      confirmed: true,
    });
    const session = await issueSession();
    const beforeRestart = await websocketSnapshot(session.accessToken);
    assertScore(beforeRestart, 1, 0, 'before restart WebSocket snapshot');

    await runCommand('docker', ['restart', containerName]);
    await waitForHealth(60_000);

    const state = await publicGet(`/api/matches/${matchId}/state`);
    assertScore(state.snapshot, 1, 0, 'after restart HTTP snapshot');
    const events = await operatorGet(`/api/matches/${matchId}/events`);
    const restored = events.events.find((event) => event.id === created.event.id);
    assert(restored?.factStatus === 'confirmed', `confirmed fact was not restored: ${JSON.stringify(restored)}`);

    const afterRestart = await websocketSnapshot(session.accessToken);
    assertScore(afterRestart, 1, 0, 'after restart WebSocket snapshot');

    return {
      suite: 'docker-restart-e2e',
      ok: true,
      eventId: created.event.id,
      sessionId: session.sessionId,
    };
  } finally {
    await waitForHealth(60_000);
    await operatorPost(`/api/matches/${matchId}/reset`, {});
  }
}

async function issueSession() {
  const response = await fetch(`${baseUrl}/api/sessions/anonymous`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ deviceId: `restart-e2e-${Date.now()}` }),
  });
  if (response.status !== 201) throw new Error(`session issue -> ${response.status}: ${await response.text()}`);
  return response.json();
}

async function websocketSnapshot(accessToken) {
  const endpoint = new URL(baseUrl);
  endpoint.protocol = endpoint.protocol === 'https:' ? 'wss:' : 'ws:';
  endpoint.pathname = `/ws/match/${matchId}`;
  const protocol = `qiuqiu-auth.${Buffer.from(accessToken).toString('base64url')}`;
  const socket = new WebSocket(endpoint, [protocol]);
  try {
    const snapshot = await waitForMessage(
      socket,
      (message) => message.type === 'match_snapshot' && message.data,
      8_000,
    );
    return snapshot.data;
  } finally {
    if (socket.readyState === WebSocket.OPEN) socket.close();
  }
}

function waitForMessage(socket, predicate, timeoutMs) {
  return new Promise((resolveWait, rejectWait) => {
    const timer = setTimeout(() => cleanup(rejectWait, new Error('WebSocket snapshot timed out')), timeoutMs);
    const onMessage = (event) => {
      if (typeof event.data !== 'string') return;
      let message;
      try { message = JSON.parse(event.data); } catch { return; }
      if (predicate(message)) cleanup(resolveWait, message);
    };
    const onError = () => cleanup(rejectWait, new Error('WebSocket connection failed'));
    const cleanup = (settle, value) => {
      clearTimeout(timer);
      socket.removeEventListener('message', onMessage);
      socket.removeEventListener('error', onError);
      settle(value);
    };
    socket.addEventListener('message', onMessage);
    socket.addEventListener('error', onError, { once: true });
  });
}

async function waitForHealth(timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  let lastError = 'service unavailable';
  while (Date.now() < deadline) {
    try {
      const response = await fetch(`${baseUrl}/health`);
      if (response.ok && (await response.text()).trim() === 'ok') return;
      lastError = `health returned ${response.status}`;
    } catch (error) {
      lastError = error.message;
    }
    await new Promise((resolveWait) => setTimeout(resolveWait, 250));
  }
  throw new Error(`backend health check timed out: ${lastError}`);
}

async function publicGet(path) {
  return request(path);
}

async function operatorGet(path) {
  return request(path, { token });
}

async function operatorPost(path, body) {
  return request(path, {
    method: 'POST',
    token,
    body: JSON.stringify(body),
  });
}

async function request(path, options = {}) {
  const response = await fetch(`${baseUrl}${path}`, {
    method: options.method || 'GET',
    headers: {
      ...(options.body ? { 'content-type': 'application/json' } : {}),
      ...(options.token ? { Authorization: `Bearer ${options.token}` } : {}),
      ...(options.method === 'POST' ? { 'Idempotency-Key': `restart-e2e-${Date.now()}-${Math.random()}` } : {}),
    },
    body: options.body,
  });
  if (!response.ok) throw new Error(`${options.method || 'GET'} ${path} -> ${response.status}: ${await response.text()}`);
  return response.json();
}

function runCommand(command, args) {
  return new Promise((resolveRun, rejectRun) => {
    const child = spawn(command, args, { stdio: 'inherit' });
    child.once('error', rejectRun);
    child.once('exit', (code) => {
      if (code === 0) resolveRun();
      else rejectRun(new Error(`${command} ${args.join(' ')} exited with ${code}`));
    });
  });
}

function assertScore(snapshot, home, away, label) {
  assert(snapshot?.score?.home === home && snapshot?.score?.away === away, `${label}: ${JSON.stringify(snapshot)}`);
}

function assert(condition, message) {
  if (!condition) throw new Error(message);
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  try {
    console.log(JSON.stringify(await runDockerRestartE2E(), null, 2));
  } catch (error) {
    console.error(error.stack || error.message);
    process.exitCode = 1;
  }
}
