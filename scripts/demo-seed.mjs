#!/usr/bin/env node

const baseUrl = process.env.QIUQIU_BASE_URL || 'http://localhost:8080';
const token = process.env.APP_TOKEN || 'qiuqiu-dev-token';
const matchId = process.env.QIUQIU_MATCH_ID || 'test';
const seedGoal = process.env.QIUQIU_SEED_GOAL !== '0';
const seedFinishedMatches = process.env.QIUQIU_SEED_FINISHED !== '0';
const obsoleteDemoMatchIds = [
  'demo-finished-fa-cup-china',
  'demo-finished-fa-cup',
  'demo-finished-champions-league',
  'operator-config-e2e',
];

async function request(path, options = {}) {
  const res = await fetch(`${baseUrl}${path}`, {
    ...options,
    headers: {
      'Content-Type': 'application/json; charset=utf-8',
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
      ...(['POST', 'PATCH'].includes(options.method) ? { 'Idempotency-Key': `seed-${Date.now()}-${Math.random().toString(16).slice(2)}` } : {}),
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
  round: '演示赛 · 小组赛第 1 轮',
  venue: '演示球场 · 主场馆',
  referee: '演示裁判组 · 主裁判：安东尼·泰勒',
  homeCoach: '路易斯·德拉富恩特',
  awayCoach: '朱利安·纳格尔斯曼',
  homeFormation: '4-3-3',
  awayFormation: '4-2-3-1',
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

// The sample history is intentionally limited to matches on/after the 2026
// World Cup.  IDs are from ESPN's public match-summary API; details are
// fetched at seed time so the UI never contains invented lineups or events.
const finishedMatches = [
  { matchId: 'demo-finished-premier-league', sourceLeague: 'eng.1', sourceEventId: '401879301', competition: '英超 · 2026-27' },
  { matchId: 'demo-finished-premier-league-liverpool', sourceLeague: 'eng.1', sourceEventId: '401879314', competition: '英超 · 2026-27' },
  { matchId: 'demo-finished-la-liga', sourceLeague: 'esp.1', sourceEventId: '401882895', competition: '西甲 · 2026-27' },
  { matchId: 'demo-finished-la-liga-levante', sourceLeague: 'esp.1', sourceEventId: '401882902', competition: '西甲 · 2026-27' },
  { matchId: 'demo-finished-serie-a', sourceLeague: 'ita.1', sourceEventId: '401874937', competition: '意甲 · 2026-27' },
  { matchId: 'demo-finished-serie-a-juventus', sourceLeague: 'ita.1', sourceEventId: '401874746', competition: '意甲 · 2026-27' },
  { matchId: 'demo-finished-world-cup', sourceLeague: 'fifa.world', sourceEventId: '760495', competition: '国际足联世界杯 · 2026' },
];

const scheduledMatches = [
  { matchId: 'demo-scheduled-premier-league', sourceLeague: 'eng.1', sourceEventId: '401879284', competition: '英超 · 2026-27' },
];

const teamNameZh = {
  Arsenal: '阿森纳', 'Coventry City': '考文垂城', Liverpool: '利物浦', 'Nottingham Forest': '诺丁汉森林',
  'Aston Villa': '阿斯顿维拉',
  'Athletic Club': '毕尔巴鄂竞技', 'Atlético Madrid': '马德里竞技', Levante: '莱万特', 'Real Betis': '皇家贝蒂斯',
  Internazionale: '国际米兰', Napoli: '那不勒斯', Juventus: '尤文图斯', Parma: '帕尔马', England: '英格兰', 'Congo DR': '刚果（金）',
};
const venueZh = {
  'Emirates Stadium': '酋长球场', Anfield: '安菲尔德球场', 'San Mamés': '圣马梅斯球场',
  'Villa Park': '维拉公园球场',
  'Estadi Ciutat de València': '瓦伦西亚市立球场', 'San Siro': '圣西罗球场', 'Allianz Stadium': '安联球场',
  'Mercedes-Benz Stadium': '梅赛德斯-奔驰体育场',
};
const refereeZh = {
  'Thomas Bramall': '托马斯·布拉莫尔', 'Samuel Barrott': '塞缪尔·巴罗特', 'José Luis Munuera Montero': '何塞·路易斯·穆努埃拉·蒙特罗',
  'Ricardo de Burgos Bengoetxea': '里卡多·德·布尔戈斯·本戈切亚', 'Simone Sozza': '西蒙尼·索扎',
  'Francesco Fourneau': '弗朗切斯科·福尔诺', 'Adham Mohammad': '阿德汉·穆罕默德',
};
const positionZh = { G: '门将', GK: '门将', D: '后卫', 'CD-L': '后卫', 'CD-R': '后卫', M: '中场', F: '前锋', LF: '前锋', RF: '前锋', AM: '前腰', CM: '中场', DM: '后腰', CB: '中后卫', LB: '左后卫', RB: '右后卫', ST: '前锋', LW: '左边锋', RW: '右边锋' };
const statisticDefinitions = [
  { key: 'possessionPct', label: '控球率', unit: '%' },
  { key: 'totalShots', label: '射门' },
  { key: 'shotsOnTarget', label: '射正' },
  { key: 'expectedGoals', label: '预期进球' },
  { key: 'wonCorners', label: '角球' },
  { key: 'foulsCommitted', label: '犯规' },
  { key: 'offsides', label: '越位' },
  { key: 'yellowCards', label: '黄牌' },
  { key: 'redCards', label: '红牌' },
  { key: 'saves', label: '扑救' },
  { key: 'accuratePasses', label: '成功传球' },
  { key: 'totalPasses', label: '传球总数' },
  { key: 'interceptions', label: '拦截' },
  { key: 'totalTackles', label: '抢断' },
  { key: 'blockedShots', label: '封堵射门' },
];

function zhTeam(name) { return teamNameZh[name] || name; }
function zhVenue(name) { return venueZh[name] || name; }
function zhReferee(name) { return refereeZh[name] || name; }
function zhRound(name) {
  return (name || '')
    .replace(/^\d{4}-\d{2} English Premier League$/i, '英格兰超级联赛')
    .replace(/^\d{4}-\d{2} Spanish LALIGA$/i, '西甲联赛')
    .replace(/^\d{4}-\d{2} Italian Serie A$/i, '意甲联赛')
    .replace(/FIFA World Cup/gi, '国际足联世界杯')
    .replace(/Round of 32/gi, '三十二强赛')
    .replace(/Round of 16/gi, '十六强赛')
    .replace(/Quarterfinals?/gi, '四分之一决赛')
    .replace(/Premier League/gi, '英格兰超级联赛')
    .replace(/La Liga/gi, '西甲联赛')
    .replace(/Serie A/gi, '意甲联赛');
}

function eventPeriod(event) {
  const n = event.period?.number;
  if (n === 1) return 'first_half';
  if (n === 2) return 'second_half';
  return 'fulltime';
}

function espnEventType(event) {
  const type = event.type?.type || '';
  if (type === 'goal' || type.startsWith('goal---') || type === 'penalty---scored') return 'goal';
  if (type === 'yellow-card') return 'yellow_card';
  if (type === 'red-card' || type === 'second-yellow') return 'red_card';
  if (type === 'substitution') return 'substitution';
  return null;
}

function teamSide(event, summary) {
  const id = event.team?.id?.toString();
  const roster = summary.rosters?.find((r) => r.team?.id?.toString() === id);
  return roster?.homeAway || '';
}

function playerNames(event) {
  return (event.participants || []).map((p) => p.athlete?.displayName).filter(Boolean);
}

function toPlayers(roster) {
  return (roster.roster || []).map((entry) => ({
    number: entry.jersey || '',
    name: entry.athlete?.displayName || '',
    position: positionZh[entry.position?.abbreviation] || entry.position?.abbreviation || '',
    lineup: entry.starter ? 'starter' : 'bench',
  })).filter((player) => player.name);
}

function statisticValue(statistic) {
  const value = Number(statistic?.value ?? statistic?.displayValue);
  return Number.isFinite(value) ? value : null;
}

function toStatistics(summary) {
  const teams = summary.boxscore?.teams || [];
  const homeStats = new Map((teams.find((team) => team.homeAway === 'home')?.statistics || []).map((stat) => [stat.name, stat]));
  const awayStats = new Map((teams.find((team) => team.homeAway === 'away')?.statistics || []).map((stat) => [stat.name, stat]));
  return statisticDefinitions.map((definition) => {
    const home = statisticValue(homeStats.get(definition.key));
    const away = statisticValue(awayStats.get(definition.key));
    if (home === null || away === null) return null;
    return { ...definition, home, away };
  }).filter(Boolean);
}

async function loadEspnMatch(seed) {
  const url = `https://site.web.api.espn.com/apis/site/v2/sports/soccer/${seed.sourceLeague}/summary?event=${seed.sourceEventId}`;
  const response = await fetch(url);
  if (!response.ok) throw new Error(`ESPN summary ${seed.sourceEventId} -> ${response.status}`);
  const summary = await response.json();
  const competition = summary.header?.competitions?.[0];
  const competitors = competition?.competitors || [];
  const home = competitors.find((c) => c.homeAway === 'home');
  const away = competitors.find((c) => c.homeAway === 'away');
  if (!home || !away) throw new Error(`ESPN summary ${seed.sourceEventId} has no home/away teams`);
  const info = summary.gameInfo || {};
  const officials = (info.officials || []).filter((o) => o.position?.name === 'Referee');
  const venue = info.venue?.fullName || competition.venue?.fullName || '';
  const events = (summary.keyEvents || []).map((event) => {
    const eventType = espnEventType(event);
    if (!eventType || event.shootout) return null;
    const side = teamSide(event, summary);
    const names = playerNames(event);
    const roles = eventType === 'goal'
      ? names.map((name, index) => ({ role: index === 0 ? 'scorer' : index === 1 ? 'assist' : 'pre_assist', name, teamId: side, teamName: zhTeam(event.team?.displayName || '') }))
      : eventType === 'substitution'
        ? names.map((name, index) => ({ role: index === 0 ? 'sub_on' : 'sub_off', name, teamId: side, teamName: zhTeam(event.team?.displayName || '') }))
        : names.map((name) => ({ role: 'player', name, teamId: side, teamName: zhTeam(event.team?.displayName || '') }));
    return {
      source: 'historical-record', providerName: 'espn-match-summary', providerEventId: event.id,
      period: eventPeriod(event), clock: event.clock?.displayValue || '0\'', eventType,
      teamId: side, teamName: zhTeam(event.team?.displayName || ''), playerName: names[0] || '',
      participants: roles,
      score: { home: 0, away: 0 }, intensity: eventType === 'goal' ? 5 : 2,
      sentiment: eventType === 'goal' ? 'celebratory' : 'neutral',
      visibility: 'public', factStatus: 'confirmed', confidence: 0.99,
      description: eventType === 'goal'
        ? `进球：${names[0] || '未知球员'}${names[1] ? `（助攻：${names[1]}）` : ''}`
        : eventType === 'yellow_card'
          ? `黄牌：${names[0] || '未知球员'}`
          : eventType === 'red_card'
            ? `红牌：${names[0] || '未知球员'}`
            : `换人：${names[0] || '未知球员'} 替换 ${names[1] || '未知球员'}`,
      evidence: { provider: 'ESPN', sourceUrl: url, originalDescription: event.text || event.shortText || '' },
    };
  }).filter(Boolean);
  return {
    ...seed,
    homeTeam: zhTeam(home.team.displayName),
    awayTeam: zhTeam(away.team.displayName),
    kickoff: competition.startDate || competition.date || summary.header?.date,
    round: zhRound(summary.header?.season?.name || competition.altGameNote || summary.header?.season?.slug),
    venue: zhVenue(venue),
    referee: officials.map((o) => zhReferee(o.displayName || o.fullName)).join(' / '),
    homeScore: Number(home.score || 0), awayScore: Number(away.score || 0),
    homeFormation: summary.rosters?.find((r) => r.homeAway === 'home')?.formation || '',
    awayFormation: summary.rosters?.find((r) => r.homeAway === 'away')?.formation || '',
    homePlayers: toPlayers(summary.rosters?.find((r) => r.homeAway === 'home') || {}),
    awayPlayers: toPlayers(summary.rosters?.find((r) => r.homeAway === 'away') || {}),
    stats: toStatistics(summary),
    events,
    sourceUrl: url,
  };
}

function finishedConfig(seed) {
  return {
    homeTeam: seed.homeTeam,
    awayTeam: seed.awayTeam,
    competition: seed.competition,
    kickoff: seed.kickoff,
    round: seed.round,
    venue: seed.venue,
    referee: seed.referee,
    homeCoach: seed.homeCoach,
    awayCoach: seed.awayCoach,
    homeFormation: seed.homeFormation,
    awayFormation: seed.awayFormation,
    homePlayers: seed.homePlayers,
    awayPlayers: seed.awayPlayers,
    stats: seed.stats,
    lifecycle: 'draft',
  };
}

function scheduledConfig(seed) {
  return {
    homeTeam: seed.homeTeam,
    awayTeam: seed.awayTeam,
    competition: seed.competition,
    kickoff: seed.kickoff,
    round: seed.round,
    venue: seed.venue,
    referee: seed.referee,
    homeCoach: seed.homeCoach,
    awayCoach: seed.awayCoach,
    homeFormation: seed.homeFormation,
    awayFormation: seed.awayFormation,
    homePlayers: seed.homePlayers,
    awayPlayers: seed.awayPlayers,
    lifecycle: 'draft',
  };
}

async function seedFinishedMatch(seed) {
  seed = await loadEspnMatch(seed);
  await request(`/api/matches/${encodeURIComponent(seed.matchId)}/reset`, {
    method: 'POST', body: JSON.stringify({}),
  });
  await request(`/api/matches/${encodeURIComponent(seed.matchId)}/config`, {
    method: 'POST', body: JSON.stringify(finishedConfig(seed)),
  });
  await request(`/api/matches/${encodeURIComponent(seed.matchId)}/lifecycle`, {
    method: 'POST', body: JSON.stringify({ lifecycle: 'scheduled' }),
  });
  await request(`/api/matches/${encodeURIComponent(seed.matchId)}/lifecycle`, {
    method: 'POST', body: JSON.stringify({ lifecycle: 'live' }),
  });
  let score = { home: 0, away: 0 };
  for (const event of seed.events) {
    if (event.eventType === 'goal') {
      if (event.teamId === 'home') score.home += 1;
      if (event.teamId === 'away') score.away += 1;
    }
    event.score = { ...score };
    await request(`/api/matches/${encodeURIComponent(seed.matchId)}/events`, {
      method: 'POST', body: JSON.stringify(event),
    });
  }
  if (score.home !== seed.homeScore || score.away !== seed.awayScore) {
    throw new Error(`ESPN event goals ${score.home}-${score.away} do not match final score ${seed.homeScore}-${seed.awayScore} for ${seed.sourceEventId}`);
  }
  await request(`/api/matches/${encodeURIComponent(seed.matchId)}/clock`, {
    method: 'PATCH',
    body: JSON.stringify({ action: 'set', period: 'fulltime', elapsedSeconds: 5400, expectedVersion: 0 }),
  });
  await request(`/api/matches/${encodeURIComponent(seed.matchId)}/lifecycle`, {
    method: 'POST', body: JSON.stringify({ lifecycle: 'finished' }),
  });
}

async function seedScheduledMatch(seed) {
  seed = await loadEspnMatch(seed);
  await request(`/api/matches/${encodeURIComponent(seed.matchId)}/reset`, {
    method: 'POST', body: JSON.stringify({}),
  });
  await request(`/api/matches/${encodeURIComponent(seed.matchId)}/config`, {
    method: 'POST', body: JSON.stringify(scheduledConfig(seed)),
  });
  await request(`/api/matches/${encodeURIComponent(seed.matchId)}/lifecycle`, {
    method: 'POST', body: JSON.stringify({ lifecycle: 'scheduled' }),
  });
}

for (const obsoleteMatchId of obsoleteDemoMatchIds) {
  await request(`/api/matches/${encodeURIComponent(obsoleteMatchId)}/reset`, {
    method: 'POST', body: JSON.stringify({}),
  });
}

await request(`/api/matches/${encodeURIComponent(matchId)}/start`, { method: 'POST', body: JSON.stringify(config) });
await request(`/api/matches/${encodeURIComponent(matchId)}/clock`, {
  method: 'PATCH',
  body: JSON.stringify({ action: 'set', period: 'first_half', elapsedSeconds: 1450, expectedVersion: 0 }),
});
await request(`/api/matches/${encodeURIComponent(matchId)}/lifecycle`, {
  method: 'POST', body: JSON.stringify({ lifecycle: 'live' }),
});
const created = seedGoal
  ? await request(`/api/matches/${encodeURIComponent(matchId)}/events`, { method: 'POST', body: JSON.stringify(goal) })
  : {};
const state = await fetch(`${baseUrl}/api/matches/${encodeURIComponent(matchId)}/state`).then((res) => res.json());

if (seedFinishedMatches) {
  for (const seed of finishedMatches) await seedFinishedMatch(seed);
  for (const seed of scheduledMatches) await seedScheduledMatch(seed);
}

console.log(JSON.stringify({
  ok: true,
  matchId,
  eventId: created.event?.id,
  score: state.snapshot?.score,
  clock: state.snapshot?.clock,
  userUrl: `${baseUrl}/`,
  operatorUrl: `${baseUrl}/operator.html?token={APP_TOKEN}#live`,
  traceUrl: `${baseUrl}/operator.html?token={APP_TOKEN}#traces`,
  finishedMatchIds: seedFinishedMatches ? finishedMatches.map((seed) => seed.matchId) : [],
  scheduledMatchIds: seedFinishedMatches ? scheduledMatches.map((seed) => seed.matchId) : [],
}, null, 2));
