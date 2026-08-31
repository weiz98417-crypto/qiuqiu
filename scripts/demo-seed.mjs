#!/usr/bin/env node

const baseUrl = process.env.QIUQIU_BASE_URL || 'http://localhost:8080';
const token = process.env.APP_TOKEN || 'qiuqiu-dev-token';
const matchId = process.env.QIUQIU_MATCH_ID || 'test';

async function request(path, options = {}) {
  const res = await fetch(`${baseUrl}${path}`, {
    ...options,
    headers: {
      'Content-Type': 'application/json; charset=utf-8',
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
      ...(options.method === 'POST' ? { 'Idempotency-Key': `seed-${Date.now()}-${Math.random().toString(16).slice(2)}` } : {}),
      ...(options.headers || {}),
    },
  });
  const text = await res.text();
  if (!res.ok) {
    throw new Error(`${options.method || 'GET'} ${path} -> ${res.status}: ${text}`);
  }
  return text ? JSON.parse(text) : {};
}

const config = {
  homeTeam: '西班牙',
  awayTeam: '德国',
  competition: '足球数字人演示赛',
  kickoff: '2026-06-16T20:00',
  homePlayers: [
    { number: '10', name: '佩德里', position: 'CM' },
    { number: '8', name: '法比安', position: 'CM' },
    { number: '19', name: '亚马尔', position: 'RW' },
  ],
  awayPlayers: [
    { number: '10', name: '穆西亚拉', position: 'AM' },
    { number: '9', name: '菲尔克鲁格', position: 'ST' },
  ],
};

const goal = {
  source: 'operator',
  providerName: 'demo-seed',
  period: 'first_half',
  clock: '24:10',
  eventType: 'goal',
  teamId: 'home',
  teamName: '西班牙',
  playerName: '佩德里',
  participants: [
    { role: 'scorer', name: '佩德里', teamId: 'home', teamName: '西班牙' },
    { role: 'assist', name: '法比安', teamId: 'home', teamName: '西班牙' },
    { role: 'pre_assist', name: '亚马尔', teamId: 'home', teamName: '西班牙' },
  ],
  score: { home: 1, away: 0 },
  intensity: 5,
  sentiment: 'celebratory',
  description: '佩德里：禁区内抢点破门。',
  proactiveText: '佩德里这一下太关键了，法比安的助攻也很漂亮。',
  recommendedAction: 'celebrate',
  visibility: 'public',
};

await request(`/api/matches/${encodeURIComponent(matchId)}/reset`, { method: 'POST', body: '{}' });
await request(`/api/matches/${encodeURIComponent(matchId)}/config`, { method: 'POST', body: JSON.stringify(config) });
const created = await request(`/api/matches/${encodeURIComponent(matchId)}/events`, { method: 'POST', body: JSON.stringify(goal) });
const state = await fetch(`${baseUrl}/api/matches/${encodeURIComponent(matchId)}/state`).then((res) => res.json());

console.log(JSON.stringify({
  ok: true,
  matchId,
  eventId: created.event?.id,
  score: state.snapshot?.score,
  clock: state.snapshot?.clock,
  userUrl: `${baseUrl}/`,
  operatorUrl: `${baseUrl}/operator.html?token={APP_TOKEN}#live`,
  traceUrl: `${baseUrl}/operator.html?token={APP_TOKEN}#traces`,
}, null, 2));
