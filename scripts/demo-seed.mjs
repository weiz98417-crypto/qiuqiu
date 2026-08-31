#!/usr/bin/env node

const baseUrl = process.env.QIUQIU_BASE_URL || 'http://localhost:8080';
const token = process.env.APP_TOKEN || 'qiuqiu-dev-token';
const matchId = process.env.QIUQIU_MATCH_ID || 'test';
const seedGoal = process.env.QIUQIU_SEED_GOAL !== '0';

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
    { number: '1', name: '乌奈·西蒙', position: 'GK', lineup: 'starter' },
    { number: '2', name: '卡瓦哈尔', position: 'RB', lineup: 'starter' },
    { number: '3', name: '勒诺尔芒', position: 'CB', lineup: 'starter' },
    { number: '14', name: '拉波尔特', position: 'CB', lineup: 'starter' },
    { number: '24', name: '库库雷利亚', position: 'LB', lineup: 'starter' },
    { number: '16', name: '罗德里', position: 'DM', lineup: 'starter' },
    { number: '8', name: '法比安', position: 'CM', lineup: 'starter' },
    { number: '20', name: '佩德里', position: 'CM', lineup: 'starter' },
    { number: '19', name: '亚马尔', position: 'RW', lineup: 'starter' },
    { number: '7', name: '莫拉塔', position: 'ST', lineup: 'starter' },
    { number: '17', name: '尼科·威廉姆斯', position: 'LW', lineup: 'starter' },
    { number: '13', name: '拉亚', position: 'GK', lineup: 'bench' },
    { number: '4', name: '纳乔', position: 'CB', lineup: 'bench' },
    { number: '5', name: '维维安', position: 'CB', lineup: 'bench' },
    { number: '6', name: '梅里诺', position: 'CM', lineup: 'bench' },
    { number: '10', name: '奥尔莫', position: 'AM', lineup: 'bench' },
    { number: '11', name: '费兰·托雷斯', position: 'RW', lineup: 'bench' },
  ],
  awayPlayers: [
    { number: '1', name: '诺伊尔', position: 'GK', lineup: 'starter' },
    { number: '6', name: '基米希', position: 'RB', lineup: 'starter' },
    { number: '2', name: '吕迪格', position: 'CB', lineup: 'starter' },
    { number: '4', name: '塔', position: 'CB', lineup: 'starter' },
    { number: '18', name: '米特尔施泰特', position: 'LB', lineup: 'starter' },
    { number: '8', name: '克罗斯', position: 'CM', lineup: 'starter' },
    { number: '23', name: '安德里希', position: 'DM', lineup: 'starter' },
    { number: '10', name: '穆西亚拉', position: 'AM', lineup: 'starter' },
    { number: '21', name: '京多安', position: 'AM', lineup: 'starter' },
    { number: '17', name: '维尔茨', position: 'LW', lineup: 'starter' },
    { number: '7', name: '哈弗茨', position: 'ST', lineup: 'starter' },
    { number: '12', name: '特尔施特根', position: 'GK', lineup: 'bench' },
    { number: '3', name: '劳姆', position: 'LB', lineup: 'bench' },
    { number: '5', name: '格罗斯', position: 'CM', lineup: 'bench' },
    { number: '9', name: '菲尔克鲁格', position: 'ST', lineup: 'bench' },
    { number: '13', name: '穆勒', position: 'AM', lineup: 'bench' },
    { number: '19', name: '萨内', position: 'RW', lineup: 'bench' },
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

await request(`/api/matches/${encodeURIComponent(matchId)}/start`, { method: 'POST', body: JSON.stringify(config) });
const created = seedGoal
  ? await request(`/api/matches/${encodeURIComponent(matchId)}/events`, { method: 'POST', body: JSON.stringify(goal) })
  : {};
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
