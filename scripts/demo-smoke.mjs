#!/usr/bin/env node

import crypto from 'node:crypto';
import net from 'node:net';
import tls from 'node:tls';

const baseUrl = process.env.QIUQIU_BASE_URL || 'http://localhost:8080';
const token = process.env.APP_TOKEN || 'qiuqiu-dev-token';
const matchId = process.env.QIUQIU_MATCH_ID || 'test';
const question = '刚才谁助攻？';

function wsUrl() {
  const url = new URL(baseUrl);
  url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:';
  url.pathname = `/ws/match/${encodeURIComponent(matchId)}`;
  url.search = '';
  return url.toString();
}

async function getJSON(path, authenticated = false) {
  const headers = authenticated && token ? { Authorization: `Bearer ${token}` } : {};
  const res = await fetch(`${baseUrl}${path}`, { headers });
  if (!res.ok) throw new Error(`GET ${path} -> ${res.status}: ${await res.text()}`);
  return res.json();
}

async function postJSON(path, body) {
  const res = await fetch(`${baseUrl}${path}`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json; charset=utf-8',
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
    body: JSON.stringify(body),
  });
  const text = await res.text();
  if (!res.ok) throw new Error(`POST ${path} -> ${res.status}: ${text}`);
  return text ? JSON.parse(text) : {};
}

async function assertHealth() {
  const res = await fetch(`${baseUrl}/health`);
  const text = await res.text();
  if (!res.ok || text.trim() !== 'ok') {
    throw new Error(`health check failed: ${res.status} ${text}`);
  }
}

async function askQiuQiu() {
  const url = new URL(wsUrl());
  const messages = [];
  const socket = await openWebSocket(url);
  try {
    writeWebSocketText(socket, JSON.stringify({
      type: 'user_speech',
      text: question,
      userId: 'demo-smoke',
      talkativeness: 'normal',
    }));
    const deadline = Date.now() + 8000;
    while (Date.now() < deadline) {
      const frame = await readWebSocketFrame(socket, deadline - Date.now());
      if (frame.opcode === 8) break;
      if (frame.opcode !== 1) continue;
      let msg;
      try {
        msg = JSON.parse(frame.payload.toString('utf8'));
      } catch {
        continue;
      }
      messages.push(msg);
      if (msg.type === 'event' && msg.event === 'qiuqiu_reply') {
        return msg.data?.text || '';
      }
    }
    throw new Error(`timed out waiting for QiuQiu reply; messages=${JSON.stringify(messages)}`);
  } finally {
    socket.destroy();
  }
}

async function publishAndWaitForProactive() {
  const socket = await openWebSocket(new URL(wsUrl()));
  try {
    const marker = `demo-smoke-${Date.now()}`;
    const publishPromise = postJSON(`/api/matches/${encodeURIComponent(matchId)}/events`, {
      source: 'operator',
      period: 'first_half',
      clock: '25:00',
      eventType: 'operator_note',
      teamId: 'home',
      teamName: '西班牙',
      playerName: '法比安',
      score: { home: 1, away: 0 },
      intensity: 3,
      description: `导演补充：${marker}`,
      proactiveText: `球球主动线验证：${marker}`,
      recommendedAction: 'analysis',
      visibility: 'public',
    });
    const deadline = Date.now() + 8000;
    const seen = [];
    while (Date.now() < deadline) {
      const frame = await readWebSocketFrame(socket, deadline - Date.now());
      if (frame.opcode === 8) break;
      if (frame.opcode !== 1) continue;
      let msg;
      try {
        msg = JSON.parse(frame.payload.toString('utf8'));
      } catch {
        continue;
      }
      seen.push(msg);
      if (msg.type === 'event' && msg.event === 'qiuqiu_reply' && msg.data?.text?.includes(marker)) {
        await publishPromise;
        return { marker, reply: msg.data.text, traceId: msg.data.traceId };
      }
    }
    await publishPromise;
    throw new Error(`timed out waiting for proactive line; messages=${JSON.stringify(seen)}`);
  } finally {
    socket.destroy();
  }
}

async function openWebSocket(url) {
  const isTLS = url.protocol === 'wss:';
  const port = Number(url.port || (isTLS ? 443 : 80));
  const socket = isTLS
    ? tls.connect({ host: url.hostname, port, servername: url.hostname })
    : net.connect({ host: url.hostname, port });
  await once(socket, 'connect', 5000);

  const key = crypto.randomBytes(16).toString('base64');
  const path = `${url.pathname}${url.search}`;
  const headers = [
    `GET ${path} HTTP/1.1`,
    `Host: ${url.host}`,
    'Upgrade: websocket',
    'Connection: Upgrade',
    `Sec-WebSocket-Key: ${key}`,
    'Sec-WebSocket-Version: 13',
  ];
  if (token) headers.push(`Sec-WebSocket-Protocol: qiuqiu-auth.${Buffer.from(token).toString('base64url')}`);
  socket.write([...headers, '', ''].join('\r\n'));

  const header = await readHTTPHeader(socket, 5000);
  if (!header.startsWith('HTTP/1.1 101 ')) {
    throw new Error(`WebSocket upgrade failed: ${header.split('\r\n')[0] || header}`);
  }
  return socket;
}

function once(emitter, event, timeoutMs) {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => cleanup(reject, new Error(`${event} timed out`)), timeoutMs);
    const onEvent = (...args) => cleanup(resolve, args);
    const onError = (err) => cleanup(reject, err);
    const cleanup = (fn, value) => {
      clearTimeout(timer);
      emitter.off(event, onEvent);
      emitter.off('error', onError);
      fn(value);
    };
    emitter.once(event, onEvent);
    emitter.once('error', onError);
  });
}

async function readHTTPHeader(socket, timeoutMs) {
  let chunks = [];
  let buffered = Buffer.alloc(0);
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    const chunk = await readChunk(socket, deadline - Date.now());
    chunks.push(chunk);
    buffered = Buffer.concat(chunks);
    const end = buffered.indexOf('\r\n\r\n');
    if (end >= 0) {
      const rest = buffered.subarray(end + 4);
      if (rest.length) socket.unshift(rest);
      return buffered.subarray(0, end).toString('utf8');
    }
  }
  throw new Error('WebSocket upgrade header timed out');
}

function writeWebSocketText(socket, text) {
  const payload = Buffer.from(text, 'utf8');
  const mask = crypto.randomBytes(4);
  const header = [];
  header.push(0x81);
  if (payload.length < 126) {
    header.push(0x80 | payload.length);
  } else if (payload.length <= 0xffff) {
    header.push(0x80 | 126, (payload.length >> 8) & 0xff, payload.length & 0xff);
  } else {
    throw new Error('WebSocket smoke payload is too large');
  }
  const masked = Buffer.alloc(payload.length);
  for (let i = 0; i < payload.length; i += 1) masked[i] = payload[i] ^ mask[i % 4];
  socket.write(Buffer.concat([Buffer.from(header), mask, masked]));
}

async function readWebSocketFrame(socket, timeoutMs) {
  const first = await readExactly(socket, 2, timeoutMs);
  const opcode = first[0] & 0x0f;
  let length = first[1] & 0x7f;
  if (length === 126) {
    length = (await readExactly(socket, 2, timeoutMs)).readUInt16BE(0);
  } else if (length === 127) {
    const extended = await readExactly(socket, 8, timeoutMs);
    const big = extended.readBigUInt64BE(0);
    if (big > BigInt(Number.MAX_SAFE_INTEGER)) throw new Error('WebSocket frame is too large');
    length = Number(big);
  }
  const masked = Boolean(first[1] & 0x80);
  const mask = masked ? await readExactly(socket, 4, timeoutMs) : null;
  const payload = await readExactly(socket, length, timeoutMs);
  if (mask) {
    for (let i = 0; i < payload.length; i += 1) payload[i] ^= mask[i % 4];
  }
  return { opcode, payload };
}

async function readExactly(socket, length, timeoutMs) {
  const chunks = [];
  let total = 0;
  while (total < length) {
    const chunk = await readChunk(socket, timeoutMs);
    chunks.push(chunk);
    total += chunk.length;
  }
  const buffer = Buffer.concat(chunks);
  const rest = buffer.subarray(length);
  if (rest.length) socket.unshift(rest);
  return buffer.subarray(0, length);
}

function readChunk(socket, timeoutMs) {
  const existing = socket.read();
  if (existing) return Promise.resolve(existing);
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => cleanup(reject, new Error('socket read timed out')), Math.max(1, timeoutMs));
    const onReadable = () => {
      const data = socket.read();
      if (data) cleanup(resolve, data);
    };
    const onClose = () => cleanup(reject, new Error('socket closed'));
    const onError = (err) => cleanup(reject, err);
    const cleanup = (fn, value) => {
      clearTimeout(timer);
      socket.off('readable', onReadable);
      socket.off('close', onClose);
      socket.off('error', onError);
      fn(value);
    };
    socket.on('readable', onReadable);
    socket.once('close', onClose);
    socket.once('error', onError);
  });
}

await assertHealth();
const state = await getJSON(`/api/matches/${encodeURIComponent(matchId)}/state`);
const eventText = JSON.stringify(state.snapshot?.recentEvents || []);
if (eventText.includes('???')) throw new Error('demo state contains corrupted ??? text');
if (state.snapshot?.score?.home !== 1 || state.snapshot?.score?.away !== 0) {
  throw new Error(`unexpected score: ${JSON.stringify(state.snapshot?.score)}`);
}

const proactive = await publishAndWaitForProactive();
if (!proactive.traceId) throw new Error(`proactive reply missing trace id: ${JSON.stringify(proactive)}`);

const reply = await askQiuQiu();
if (!reply.includes('法比安') || !reply.includes('亚马尔')) {
  throw new Error(`unexpected reply: ${reply}`);
}

const traces = await getJSON(`/api/matches/${encodeURIComponent(matchId)}/traces?limit=5`, true);
const trace = (traces.traces || []).find((item) => item.input === question || item.userId === 'demo-smoke');
if (!trace) throw new Error('expected trace for demo smoke question');
if (trace.intent !== 'recent_event_question') throw new Error(`unexpected trace intent: ${trace.intent}`);
if (!JSON.stringify(trace.toolCalls || []).includes('match.search_events')) {
  throw new Error(`trace missing match.search_events: ${JSON.stringify(trace.toolCalls)}`);
}
if (!trace.retrievedEventIds?.length) throw new Error('trace missing retrieved event IDs');

const proactiveTrace = (traces.traces || []).find((item) => item.id === proactive.traceId || item.output?.includes(proactive.marker));
if (!proactiveTrace) throw new Error(`expected proactive trace for ${proactive.marker}`);
if (proactiveTrace.reason !== 'operator_event_proactive_line') throw new Error(`unexpected proactive reason: ${proactiveTrace.reason}`);

console.log(JSON.stringify({
  ok: true,
  matchId,
  score: state.snapshot.score,
  proactive,
  question,
  reply,
  traceId: trace.id,
  retrievedEventIds: trace.retrievedEventIds,
}, null, 2));
