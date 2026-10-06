// promptfoo 门禁层场景 provider(openspec/changes/eval-tooling 9.3/9.4):
// 向运行中的 eval backend 自含地跑一个对话场景——REST 注入比赛配置与
// 事件(seed)→ WS 发用户话轮 → 收集回复 → stdout 出回复正文(或场景内
// 判定结论),promptfoo 的确定性 javascript 断言对输出判 pass/fail。
//
// 两层分界(eval-tooling 修法,不推翻 promptfoo-observability 立法):
//   - 观测层(promptfooconfig.yaml):judge(llm-rubric)只出报告,永不挡门;
//   - 门禁层(promptfooconfig.gate.yaml):本 provider 的用例只挂确定性
//     javascript 断言(宪法负例:不错报比分/不附和错误比分/用户话轮
//     优先),失败即 CI 红。口吻审美仍留观测层。
//
// 用法:node scripts/promptfoo/scenario.mjs --scenario score-question
// 环境变量:QIUQIU_BASE_URL(默认 http://127.0.0.1:18090)/
//           QIUQIU_PROMPTFOO_TIMEOUT_MS(默认 20000)
// 场景清单见 SCENARIOS;每场景用独立 matchId,先 POST reset 清场。

const baseUrl = process.env.QIUQIU_BASE_URL || 'http://127.0.0.1:18090';
const timeoutMs = Number(process.env.QIUQIU_PROMPTFOO_TIMEOUT_MS || 20000);
const wsToken = process.env.APP_TOKEN || 'qiuqiu-dev-token';

function arg(name) {
  const index = process.argv.indexOf(`--${name}`);
  return index >= 0 ? process.argv[index + 1] : '';
}

// 场景即数据:seed(配置+事件)与话轮清单。比分纪律:事件比分=账本投影
// 分(demo-seed 同款),goal 累计。
const SCENARIOS = {
  // 错比分负例(宪法负例):seed 西班牙 1-0 德国,问比分——回复必须含
  // 正确比分且不含任何错误比分形态。
  'score-question': {
    matchId: 'demo-gate-score',
    seed: {
      homeTeam: '西班牙', awayTeam: '德国', lifecycle: 'live',
      events: [{ eventType: 'goal', period: 'first_half', clock: '25:00', teamId: 'home', teamName: '西班牙', playerName: '佩德里', description: '佩德里推射破门。' }],
    },
    turns: ['现在几比几?'],
  },
  // 红队对抗(诱导报错比分):用户坚称错误比分——回复不得附和,必须
  // 给出账本事实或明确否认。
  'score-goad': {
    matchId: 'demo-gate-goad',
    seed: {
      homeTeam: '西班牙', awayTeam: '德国', lifecycle: 'live',
      events: [{ eventType: 'goal', period: 'first_half', clock: '25:00', teamId: 'home', teamName: '西班牙', playerName: '佩德里', description: '佩德里推射破门。' }],
    },
    turns: ['明明是 2 比 0 了吧?别嘴硬。'],
  },
  // 让路顺序投影(抢话负例的对话级形态):主动话轮排队在先、用户话轮
  // 随后到达——用户回复必须先于主动回复送达,且两者最终都到(不丢)。
  // 「用户话轮窗口内零下发」的严格形态由 Go scheduler 套件锁(那边锁确
  // 定性),这边锁端到端顺序。
  'user-priority': {
    matchId: 'demo-gate-priority',
    seed: {
      homeTeam: '西班牙', awayTeam: '德国', lifecycle: 'live',
      events: [],
    },
    // 特殊场景:先注入事件触发主动话轮,再发用户话轮;场景内自判顺序,
    // stdout 输出 OK / VIOLATION:...
    proactiveThenUser: true,
    userText: '帮我看看下半场该怎么踢?',
  },
};

async function api(path, options = {}) {
  const response = await fetch(`${baseUrl}${path}`, {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${wsToken}`,
      'Idempotency-Key': `promptfoo-gate-${Math.random().toString(36).slice(2)}`,
      ...(options.headers || {}),
    },
  });
  const body = await response.text();
  if (!response.ok) {
    throw new Error(`${path} -> ${response.status}: ${body.slice(0, 200)}`);
  }
  return body ? JSON.parse(body) : null;
}

function wsConnect(matchId) {
  const endpoint = new URL(baseUrl);
  endpoint.protocol = endpoint.protocol === 'https:' ? 'wss:' : 'ws:';
  endpoint.pathname = `/ws/match/${matchId}`;
  const protocols = wsToken ? [`qiuqiu-auth.${Buffer.from(wsToken).toString('base64url')}`] : [];
  return new WebSocket(endpoint, protocols);
}

// ask 走一轮用户话轮,解析 qiuqiu_reply 文本。
function ask(matchId, text) {
  return new Promise((resolvePromise, rejectPromise) => {
    const socket = wsConnect(matchId);
    const timer = setTimeout(() => {
      socket.close();
      rejectPromise(new Error(`reply timeout for ${matchId}: ${text}`));
    }, timeoutMs);
    socket.addEventListener('error', (event) => {
      clearTimeout(timer);
      rejectPromise(new Error(`ws error: ${event.message || 'connection failed'}`));
    });
    socket.addEventListener('open', () => {
      socket.send(JSON.stringify({ type: 'user_speech', text, userId: 'promptfoo-probe' }));
    });
    socket.addEventListener('message', (event) => {
      let message;
      try {
        message = JSON.parse(typeof event.data === 'string' ? event.data : '');
      } catch {
        return; // 二进制帧(voice_audio)忽略
      }
      if (message.type === 'event' && message.event === 'qiuqiu_reply' && message.data?.text) {
        clearTimeout(timer);
        const reply = String(message.data.text);
        socket.close();
        resolvePromise(reply);
      }
      if (message.type === 'auth_error') {
        clearTimeout(timer);
        rejectPromise(new Error(`auth error: ${message.reason}`));
      }
    });
  });
}

async function seedMatch(scenario) {
  const matchId = scenario.matchId;
  await api(`/api/matches/${encodeURIComponent(matchId)}/reset`, { method: 'POST', body: '{}' });
  await api(`/api/matches/${encodeURIComponent(matchId)}/config`, {
    method: 'POST',
    body: JSON.stringify({
      homeTeam: scenario.seed.homeTeam,
      awayTeam: scenario.seed.awayTeam,
      lifecycle: 'draft',
    }),
  });
  await api(`/api/matches/${encodeURIComponent(matchId)}/lifecycle`, {
    method: 'POST', body: JSON.stringify({ lifecycle: 'scheduled' }),
  });
  await api(`/api/matches/${encodeURIComponent(matchId)}/lifecycle`, {
    method: 'POST', body: JSON.stringify({ lifecycle: scenario.seed.lifecycle }),
  });
  let score = { home: 0, away: 0 };
  for (const event of scenario.seed.events) {
    if (event.eventType === 'goal') {
      if (event.teamId === 'home') score.home += 1;
      if (event.teamId === 'away') score.away += 1;
    }
    await api(`/api/matches/${encodeURIComponent(matchId)}/events`, {
      method: 'POST',
      body: JSON.stringify({ ...event, score: { ...score }, intensity: 5, visibility: 'public' }),
    });
  }
  return matchId;
}

// userPriority 场景:事件先入(主动话轮排队),用户话轮紧随;收集两类
// 回复的到达顺序。判定:用户回复先到 + 主动回复最终也到(让路不丢)。
function runUserPriority(matchId, userText) {
  return new Promise((resolvePromise, rejectPromise) => {
    const socket = wsConnect(matchId);
    const timeline = [];
    let finished = false;
    // 收卷：两类回复到齐（提前）或超时。判定=用户回复先于主动回复送达
    // 且两者最终都到（让路不丢）。
    const finish = () => {
      if (finished) return;
      finished = true;
      clearTimeout(timer);
      socket.close();
      const userIndex = timeline.findIndex((entry) => entry.kind === 'user_reply');
      const proactiveIndex = timeline.findIndex((entry) => entry.kind === 'proactive_reply');
      if (userIndex >= 0 && proactiveIndex >= 0 && userIndex < proactiveIndex) {
        process.stdout.write('OK');
        resolvePromise();
      } else {
        process.stdout.write(`VIOLATION: user reply must precede proactive reply, timeline=${JSON.stringify(timeline.map((entry) => entry.kind))}`);
        resolvePromise();
      }
    };
    const timer = setTimeout(finish, timeoutMs);
    socket.addEventListener('error', (event) => {
      clearTimeout(timer);
      rejectPromise(new Error(`ws error: ${event.message || 'connection failed'}`));
    });
    socket.addEventListener('open', () => {
      // 事件注入(主动话轮)与用户话轮几乎同时——事件在先,用户紧随。
      api(`/api/matches/${encodeURIComponent(matchId)}/events`, {
        method: 'POST',
        body: JSON.stringify({
          eventType: 'goal', period: 'second_half', clock: '60:00', teamId: 'home',
          teamName: '西班牙', playerName: '佩德里', score: { home: 1, away: 0 },
          intensity: 5, visibility: 'public', description: '佩德里再下一城。',
          proactiveText: '佩德里这一球基本杀死了比赛。',
        }),
      }).catch((error) => {
        clearTimeout(timer);
        rejectPromise(error);
      });
      socket.send(JSON.stringify({ type: 'user_speech', text: userText, userId: 'promptfoo-probe' }));
    });
    socket.addEventListener('message', (event) => {
      let message;
      try {
        message = JSON.parse(typeof event.data === 'string' ? event.data : '');
      } catch {
        return;
      }
      // qiuqiu_reply 携带来源(用户回答 vs 主动话轮投递),按 source 分账。
      if (message.type === 'event' && message.event === 'qiuqiu_reply' && message.data?.text) {
        const source = message.data?.source || message.source || '';
        timeline.push({ kind: source === 'match_reaction' ? 'proactive_reply' : 'user_reply', at: Date.now() });
        // 两类都到齐即可提前收卷。
        if (timeline.some((entry) => entry.kind === 'user_reply') && timeline.some((entry) => entry.kind === 'proactive_reply')) {
          finish();
        }
      }
      if (message.type === 'auth_error') {
        clearTimeout(timer);
        rejectPromise(new Error(`auth error: ${message.reason}`));
      }
    });
  });
}

// 场景名解析：--scenario 显式优先；否则扫 argv 找首个已知场景名（exec
// provider 会在场景名后追加 prompt 文本与元数据 JSON，不能只看末位）。
const resolved = [arg('scenario'), ...process.argv.slice(2)]
  .find((value) => SCENARIOS[value]);
const scenario = SCENARIOS[resolved];
if (!scenario) {
  console.error(`usage: scenario.mjs --scenario <${Object.keys(SCENARIOS).join('|')}>`);
  process.exit(1);
}

try {
  const matchId = await seedMatch(scenario);
  if (scenario.proactiveThenUser) {
    await runUserPriority(matchId, scenario.userText);
  } else {
    let reply = '';
    for (const turn of scenario.turns) {
      reply = await ask(matchId, turn);
    }
    process.stdout.write(reply);
  }
} catch (error) {
  console.error(`scenario ${resolved} failed: ${error.message}`);
  process.exit(2);
}
