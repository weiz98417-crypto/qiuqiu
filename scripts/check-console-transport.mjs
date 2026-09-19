#!/usr/bin/env node
// console 写路径传输策略单测（openspec/changes/console-write-transport）。
//
// 把 console/src/api/client.ts 经 esbuild 转译进 node（可注入的
// fetch/localStorage/window），对齐 operator.html operatorRequestJSON 的
// 已验证语义：并发同请求去重、一次逻辑请求一把幂等键、409 冲突不重试、
// 5xx 单次重试、Idempotency-Replayed 回填。parity harness 只锁 payload
// byte-shape，这层传输语义由本脚本守护。
//
// 用法：node scripts/check-console-transport.mjs（pr 档自动执行）。

import assert from 'node:assert/strict';
import test from 'node:test';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { createRequire } from 'node:module';

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const require = createRequire(import.meta.url);

// —— console client：esbuild 转译后经 data URL 导入 ——
const esbuild = require(join(repoRoot, 'console', 'node_modules', 'esbuild'));
const bundled = await esbuild.build({
  entryPoints: [join(repoRoot, 'console', 'src', 'api', 'client.ts')],
  bundle: true,
  format: 'esm',
  write: false,
  logLevel: 'silent',
});
const moduleUrl = `data:text/javascript;base64,${Buffer.from(bundled.outputFiles[0].text).toString('base64')}`;

// —— node 环境的浏览器全局桩（client.ts 只在请求期触碰它们）——
const storage = new Map();
globalThis.localStorage = {
  getItem: (key) => (storage.has(key) ? storage.get(key) : null),
  setItem: (key, value) => storage.set(key, String(value)),
  removeItem: (key) => storage.delete(key),
};
globalThis.window = { dispatchEvent() {} };
if (typeof globalThis.CustomEvent === 'undefined') {
  globalThis.CustomEvent = class CustomEvent {
    constructor(type, init) {
      this.type = type;
      this.detail = init?.detail;
    }
  };
}

/** fetch 桩：按脚本依次出牌；记录每次请求的 method/path/headers/body。 */
function scriptedFetch(responses) {
  const calls = [];
  const queue = [...responses];
  const fetchImpl = async (path, init = {}) => {
    calls.push({ path, init, headers: init.headers ?? {} });
    const next = queue.shift();
    if (!next) throw new Error(`unexpected fetch #${calls.length}: ${init.method} ${path}`);
    return typeof next === 'function' ? next(calls[calls.length - 1], calls) : next;
  };
  fetchImpl.calls = calls;
  return fetchImpl;
}

function jsonResponse(status, body, headers = {}) {
  return new Response(JSON.stringify(body), { status, headers });
}

const api = (await import(moduleUrl)).api;

test('双击去重：并发同请求只发一次，共享同一结果与幂等键', async () => {
  const fetchImpl = scriptedFetch([
    jsonResponse(200, { event: { id: 'e1' } }),
  ]);
  globalThis.fetch = fetchImpl;
  const body = { eventType: 'goal' };
  const [a, b] = await Promise.all([
    api('/api/matches/test/events', { method: 'POST', body }),
    api('/api/matches/test/events', { method: 'POST', body }),
  ]);
  assert.equal(fetchImpl.calls.length, 1);
  assert.equal(a.event.id, 'e1');
  assert.deepEqual(a, b);
  assert.ok(fetchImpl.calls[0].headers['Idempotency-Key']);
});

test('GET 不去重也不携带幂等键', async () => {
  const fetchImpl = scriptedFetch([
    jsonResponse(200, { events: [] }),
    jsonResponse(200, { events: [] }),
  ]);
  globalThis.fetch = fetchImpl;
  await Promise.all([
    api('/api/matches/test/events'),
    api('/api/matches/test/events'),
  ]);
  assert.equal(fetchImpl.calls.length, 2);
  for (const call of fetchImpl.calls) {
    assert.equal(call.headers['Idempotency-Key'], undefined);
  }
});

test('5xx 单次重试：同一把幂等键，第二次成功', async () => {
  const fetchImpl = scriptedFetch([
    jsonResponse(502, { error: 'upstream down' }),
    jsonResponse(200, { event: { id: 'e2' } }),
  ]);
  globalThis.fetch = fetchImpl;
  const result = await api('/api/matches/test/events', { method: 'POST', body: { eventType: 'goal' } });
  assert.equal(result.event.id, 'e2');
  assert.equal(fetchImpl.calls.length, 2);
  assert.equal(fetchImpl.calls[0].headers['Idempotency-Key'], fetchImpl.calls[1].headers['Idempotency-Key']);
});

test('5xx 两次：两次尝试后抛错', async () => {
  const fetchImpl = scriptedFetch([
    jsonResponse(500, { error: 'boom' }),
    jsonResponse(500, { error: 'boom' }),
  ]);
  globalThis.fetch = fetchImpl;
  await assert.rejects(
    api('/api/matches/test/events', { method: 'POST', body: {} }),
    (error) => error.status === 500,
  );
  assert.equal(fetchImpl.calls.length, 2);
});

test('409 冲突：不重试，文案取响应体 error 字段', async () => {
  const fetchImpl = scriptedFetch([
    jsonResponse(409, { error: '比分已被他人更正' }),
  ]);
  globalThis.fetch = fetchImpl;
  await assert.rejects(
    api('/api/matches/test/events', { method: 'POST', body: {} }),
    (error) => error.status === 409 && error.message.includes('提交内容冲突：比分已被他人更正'),
  );
  assert.equal(fetchImpl.calls.length, 1);
});

test('4xx 不重试（400）', async () => {
  const fetchImpl = scriptedFetch([jsonResponse(400, { error: 'bad payload' })]);
  globalThis.fetch = fetchImpl;
  await assert.rejects(api('/api/matches/test/events', { method: 'POST', body: {} }), { status: 400 });
  assert.equal(fetchImpl.calls.length, 1);
});

test('Idempotency-Replayed 头回填到响应对象', async () => {
  const fetchImpl = scriptedFetch([
    jsonResponse(200, { event: { id: 'e3' } }, { 'Idempotency-Replayed': 'true' }),
    jsonResponse(200, { event: { id: 'e4' } }),
  ]);
  globalThis.fetch = fetchImpl;
  const replayed = await api('/api/matches/test/events', { method: 'POST', body: { n: 1 } });
  const fresh = await api('/api/matches/test/events', { method: 'POST', body: { n: 2 } });
  assert.equal(replayed.idempotencyReplayed, true);
  assert.equal(fresh.idempotencyReplayed, false);
});

test('401 且无刷新令牌：不重试，抛 401（全局令牌页接管）', async () => {
  const fetchImpl = scriptedFetch([jsonResponse(401, { error: 'token expired' })]);
  globalThis.fetch = fetchImpl;
  await assert.rejects(api('/api/matches/test/events', { method: 'POST', body: {} }), { status: 401 });
  assert.equal(fetchImpl.calls.length, 1);
});

test('401 续期重放：原请求复用同一把幂等键', async () => {
  // 预置刷新令牌 → 401 → 无感续期成功 → 原请求重放一次。
  storage.set('qiuqiu.console.refresh', 'refresh-token-stub');
  const fetchImpl = scriptedFetch([
    jsonResponse(401, { error: 'access expired' }),
    jsonResponse(200, { accessToken: 'header.payload.sig', refreshToken: 'rotated', operator: { name: '阿琴', role: 'director', scopes: [] } }),
    jsonResponse(200, { event: { id: 'e-replayed' } }),
  ]);
  globalThis.fetch = fetchImpl;
  const result = await api('/api/matches/test/events', { method: 'POST', body: { eventType: 'goal' } });
  assert.equal(result.event.id, 'e-replayed');
  assert.equal(fetchImpl.calls.length, 3);
  const [, refresh, replay] = fetchImpl.calls;
  assert.equal(refresh.path, '/api/console/auth/refresh');
  // 关键断言：重放请求携带与原始请求相同的幂等键。
  assert.equal(
    replay.headers['Idempotency-Key'],
    fetchImpl.calls[0].headers['Idempotency-Key'],
  );
  storage.delete('qiuqiu.console.refresh');
});

console.log(`console transport checks: done`);
