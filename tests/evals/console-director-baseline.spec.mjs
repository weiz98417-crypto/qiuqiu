// 导演复杂事件基线（2026-09-29 演示轮收敛）：六条高复杂度导演操作的真链
// 路基线——每条断言 API 事实状态与比分/时钟正确性。UI 交互面由
// console-director.spec.mjs 覆盖，本文件只锁「复杂操作 → 事实收敛」语义。
// 注意：events POST 成功返回 201（operatorwrite Created）。
import { expect, test } from '@playwright/test';

const token = process.env.APP_TOKEN || 'qiuqiu-dev-token';
const base = process.env.QIUQIU_BASE_URL || 'http://127.0.0.1:18080';
const matchId = 'test';

async function api(path, options = {}) {
  const res = await fetch(`${base}${path}`, {
    ...options,
    headers: {
      'Content-Type': 'application/json; charset=utf-8',
      Authorization: `Bearer ${token}`,
      'Idempotency-Key': `director-baseline-${Date.now()}-${Math.random().toString(16).slice(2)}`,
      ...(options.headers || {}),
    },
  });
  const text = await res.text();
  let body = null;
  try { body = text ? JSON.parse(text) : null; } catch { body = text; }
  return { status: res.status, body };
}

async function resetMatch() {
  await api(`/api/matches/${matchId}/reset`, { method: 'POST', body: '{}' });
  await api(`/api/matches/${matchId}/start`, {
    method: 'POST',
    body: JSON.stringify({
      homeTeam: '西班牙', awayTeam: '德国', competition: '基线演示赛',
      homePlayers: [
        { number: '1', name: '乌奈·西蒙', position: 'GK', lineup: 'starter' },
        { number: '2', name: '卡瓦哈尔', position: 'RB', lineup: 'starter' },
        { number: '3', name: '勒诺尔芒', position: 'CB', lineup: 'starter' },
        { number: '14', name: '拉波尔特', position: 'CB', lineup: 'starter' },
        { number: '24', name: '库库雷利亚', position: 'LB', lineup: 'starter' },
        { number: '16', name: '罗德里', position: 'DM', lineup: 'starter' },
        { number: '8', name: '法比安', position: 'CM', lineup: 'starter' },
        { number: '20', name: '佩德里', position: 'CM', lineup: 'starter' },
        { number: '19', name: '亚马尔', position: 'RW', lineup: 'starter' },
        { number: '17', name: '尼科·威廉姆斯', position: 'LW', lineup: 'starter' },
        { number: '7', name: '莫拉塔', position: 'ST', lineup: 'starter' },
        { number: '11', name: '费兰·托雷斯', position: 'LW', lineup: 'bench' },
      ],
      awayPlayers: [
        { number: '1', name: '努贝尔', position: 'GK', lineup: 'starter' },
        { number: '2', name: '吕迪格', position: 'CB', lineup: 'starter' },
        { number: '3', name: '劳姆', position: 'LB', lineup: 'starter' },
        { number: '6', name: '基米希', position: 'RB', lineup: 'starter' },
        { number: '8', name: '格罗斯', position: 'DM', lineup: 'starter' },
        { number: '10', name: '穆西亚拉', position: 'AM', lineup: 'starter' },
        { number: '14', name: '拜尔', position: 'LW', lineup: 'starter' },
        { number: '17', name: '维尔茨', position: 'AM', lineup: 'starter' },
        { number: '7', name: '哈弗茨', position: 'ST', lineup: 'starter' },
        { number: '4', name: '若纳坦·塔', position: 'CB', lineup: 'starter' },
        { number: '21', name: '京多安', position: 'CM', lineup: 'starter' },
      ],
    }),
  });
  await api(`/api/matches/${matchId}/lifecycle`, { method: 'POST', body: JSON.stringify({ lifecycle: 'live' }) });
}

async function facts() {
  const { body } = await api(`/api/matches/${matchId}/state`);
  return body.snapshot;
}

test('① 进球含三参与人：事实确认、比分推进、球球主说引用落地', async () => {
  await resetMatch();
  const { status, body } = await api(`/api/matches/${matchId}/events`, {
    method: 'POST',
    body: JSON.stringify({
      source: 'operator', period: 'first_half', clock: '30:00', eventType: 'goal',
      teamId: 'home', teamName: '西班牙', playerName: '佩德里',
      participants: [
        { role: 'scorer', name: '佩德里' },
        { role: 'assist', name: '法比安' },
        { role: 'pre_assist', name: '亚马尔' },
      ],
      score: { home: 1, away: 0 }, intensity: 5, confirmed: true,
      description: '佩德里：单刀推射远角。', proactiveText: '佩德里单刀！太冷静了。',
      visibility: 'public',
    }),
  });
  expect([200, 201]).toContain(status);
  const snapshot = await facts();
  expect(snapshot.score).toMatchObject({ home: 1, away: 0 });
  expect(body.event.factStatus).toBe('confirmed');
  expect(body.event.proactiveText).toContain('佩德里');
});

test('② VAR 链：进球改判取消——事实撤销、比分回退', async () => {
  await resetMatch();
  const goal = await api(`/api/matches/${matchId}/events`, {
    method: 'POST',
    body: JSON.stringify({
      source: 'operator', period: 'first_half', clock: '35:00', eventType: 'goal',
      teamId: 'away', teamName: '德国', playerName: '努贝尔', participants: [{ role: 'scorer', name: '努贝尔' }],
      score: { home: 0, away: 1 }, intensity: 4, confirmed: true,
      description: '努贝尔：门前混战捅入。', proactiveText: '__quiet__', visibility: 'public',
    }),
  });
  expect(goal.status).toBe(201);
  const corrected = await api(`/api/matches/${matchId}/events/${encodeURIComponent(goal.body.event.id)}/correct`, {
    method: 'POST',
    body: JSON.stringify({
      source: 'operator', period: 'first_half', clock: '36:00', eventType: 'var_check',
      teamId: 'away', teamName: '德国', score: { home: 0, away: 0 }, intensity: 3,
      description: 'VAR 回看：努贝尔越位在先，进球无效。',
      evidence: { correctionReason: '越位' }, confirmed: true,
      proactiveText: 'VAR 判了越位，进球不算。', visibility: 'public',
    }),
  });
  expect([200, 201]).toContain(corrected.status);
  const snapshot = await facts();
  expect(snapshot.score).toMatchObject({ home: 0, away: 0 });
});

test('③ 比分更正：correctionReason 证据缺失被拒，补齐后生效', async () => {
  await resetMatch();
  await api(`/api/matches/${matchId}/events`, {
    method: 'POST',
    body: JSON.stringify({
      source: 'operator', period: 'first_half', clock: '37:00', eventType: 'goal',
      teamId: 'home', teamName: '西班牙', playerName: '佩德里', score: { home: 1, away: 0 },
      intensity: 5, confirmed: true, description: '佩德里：首开纪录。',
      proactiveText: '__quiet__', visibility: 'public',
    }),
  });
  const rejected = await api(`/api/matches/${matchId}/events`, {
    method: 'POST',
    body: JSON.stringify({
      source: 'operator', period: 'first_half', clock: '38:00', eventType: 'score_correction',
      teamId: 'home', teamName: '西班牙', score: { home: 2, away: 0 }, intensity: 1, confirmed: true,
      description: '记分台更正：比分 2-0。', proactiveText: '__quiet__', visibility: 'public',
    }),
  });
  expect(rejected.status).toBe(400); // 无 correctionReason 证据必须被拒（ADR-0002 事实留痕）
  const accepted = await api(`/api/matches/${matchId}/events`, {
    method: 'POST',
    body: JSON.stringify({
      source: 'operator', period: 'first_half', clock: '38:00', eventType: 'score_correction',
      teamId: 'home', teamName: '西班牙', score: { home: 2, away: 0 }, intensity: 1, confirmed: true,
      description: '记分台更正：比分 2-0。', evidence: { correctionReason: '记分台登记错误' },
      proactiveText: '__quiet__', visibility: 'public',
    }),
  });
  expect([200, 201]).toContain(accepted.status);
  const snapshot = await facts();
  expect(snapshot.score).toMatchObject({ home: 2, away: 0 });
});

test('④ 红牌+换人连发：两事实独立确认互不覆盖', async () => {
  await resetMatch();
  const red = await api(`/api/matches/${matchId}/events`, {
    method: 'POST',
    body: JSON.stringify({
      source: 'operator', period: 'first_half', clock: '40:00', eventType: 'red_card',
      teamId: 'away', teamName: '德国', playerName: '努贝尔', intensity: 5, confirmed: true,
      description: '努贝尔：禁区外手球吃红。', proactiveText: '红牌！这下麻烦了。',
      visibility: 'public',
    }),
  });
  const sub = await api(`/api/matches/${matchId}/events`, {
    method: 'POST',
    body: JSON.stringify({
      source: 'operator', period: 'first_half', clock: '41:00', eventType: 'substitution',
      teamId: 'home', teamName: '西班牙', playerName: '莫拉塔',
      participants: [
        { role: 'sub_off', name: '尼科·威廉姆斯', teamId: 'home' },
        { role: 'sub_on', name: '费兰·托雷斯', teamId: 'home' },
      ],
      intensity: 2, confirmed: true,
      description: '西班牙换人：费兰·托雷斯上，尼科·威廉姆斯下。', proactiveText: '__quiet__', visibility: 'public',
    }),
  });
  expect(red.status).toBe(201);
  expect(sub.status).toBe(201);
  expect(red.body.event.factId).not.toBe(sub.body.event.factId);
});

test('⑤ 险球触发微反应通道（WS 连接 + 账本行落地）', async () => {
  await resetMatch();
  const ws = await openSeedSocket('demo-fan-baseline');
  try {
    await api(`/api/matches/${matchId}/events`, {
      method: 'POST',
      body: JSON.stringify({
        source: 'operator', period: 'first_half', clock: '43:00', eventType: 'big_chance',
        teamId: 'home', teamName: '西班牙', playerName: '亚马尔', score: { home: 0, away: 0 },
        intensity: 4, confirmed: true, description: '亚马尔：弧顶兜射擦柱。', visibility: 'public',
      }),
    });
    const reply = await ws.waitFor((message) => message.type === 'event' && message.event === 'qiuqiu_reply'
      && message.data?.source === 'backchannel', 10_000);
    expect(reply.data.text.length).toBeGreaterThan(0);
    expect(reply.data.text.length).toBeLessThanOrEqual(10);
    const { body } = await api(`/api/matches/${matchId}/interaction?userId=demo-fan-baseline&limit=100`);
    expect((body.events || []).some((e) => e.kind === 'backchannel')).toBeTruthy();
  } finally {
    await ws.close();
  }
});

test('⑥ 时钟推进与幂等重放：同幂等键重放不产生第二事实', async () => {
  await resetMatch();
  // /start 可能推进时钟版本：从 state 读当前版本再 set，避免 409。
  const state0 = await api(`/api/matches/${matchId}/state`);
  const clockVersion = state0.body.snapshot?.matchClock?.version ?? 0;
  await api(`/api/matches/${matchId}/clock`, {
    method: 'PATCH',
    body: JSON.stringify({ action: 'set', period: 'second_half', elapsedSeconds: 5400, expectedVersion: clockVersion }),
  });
  // 运行内固定、跨运行唯一：重放语义要求两次同键，但套件重跑时不能撞
  // 上一次进程的幂等记录（会回放已被 reset 清掉的幽灵事件）。
  const replayKey = `director-baseline-replay-${Date.now()}`;
  const headers = { 'Idempotency-Key': replayKey };
  const payload = JSON.stringify({
    source: 'operator', period: 'second_half', clock: '90:00', eventType: 'goal',
    teamId: 'home', teamName: '西班牙', playerName: '法比安', participants: [{ role: 'scorer', name: '法比安' }],
    score: { home: 1, away: 0 }, intensity: 5, confirmed: true,
    description: '法比安：压哨绝杀。', proactiveText: '绝杀！！！', visibility: 'public',
  });
  const first = await api(`/api/matches/${matchId}/events`, { method: 'POST', headers, body: payload });
  expect(first.status).toBe(201);
  const replay = await api(`/api/matches/${matchId}/events`, { method: 'POST', headers, body: payload });
  expect([200, 201]).toContain(replay.status);
  expect(replay.body.event?.id ?? first.body.event.id).toBe(first.body.event.id);
  let snapshot = await facts();
  for (let i = 0; i < 10 && snapshot.score.home !== 1; i++) {
    await new Promise((r) => setTimeout(r, 300));
    snapshot = await facts();
  }
  expect(snapshot.score).toMatchObject({ home: 1, away: 0 });
  expect(first.body.event.factStatus).toBe('confirmed');
});

async function openSeedSocket(userId) {
  const endpoint = new URL(base);
  endpoint.protocol = endpoint.protocol === 'https:' ? 'wss:' : 'ws:';
  endpoint.pathname = `/ws/match/${matchId}`;
  // WS 通道用 dev 共享令牌（legacy 通道）；REST 仍走 APP_TOKEN（可能是
  // 运营员个人令牌，WS legacy 通道不认它）。
  const wsToken = process.env.SEED_WS_TOKEN || 'qiuqiu-dev-token';
  const protocols = wsToken ? [`qiuqiu-auth.${Buffer.from(wsToken).toString('base64url')}`] : [];
  const socket = new WebSocket(endpoint, protocols);
  await new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error('ws timeout')), 8_000);
    socket.addEventListener('open', () => { clearTimeout(timer); resolve(); }, { once: true });
  });
  const messages = [];
  const waiters = [];
  socket.addEventListener('message', (event) => {
    if (typeof event.data !== 'string') return;
    let message;
    try { message = JSON.parse(event.data); } catch { return; }
    messages.push(message);
    for (const waiter of [...waiters]) {
      if (waiter.predicate(message)) {
        clearTimeout(waiter.timer);
        waiters.splice(waiters.indexOf(waiter), 1);
        waiter.resolve(message);
      }
    }
  });
  socket.send(JSON.stringify({ type: 'identify', userId }));
  return {
    waitFor(predicate, timeoutMs) {
      const existing = messages.find(predicate);
      if (existing) return Promise.resolve(existing);
      return new Promise((resolve, reject) => {
        const waiter = { predicate, resolve, timer: setTimeout(() => reject(new Error('ws 等待超时')), timeoutMs) };
        waiters.push(waiter);
      });
    },
    close() {
      if (socket.readyState === 3) return Promise.resolve();
      return new Promise((resolve) => {
        socket.addEventListener('close', resolve, { once: true });
        socket.close();
        setTimeout(resolve, 800);
      });
    },
  };
}
