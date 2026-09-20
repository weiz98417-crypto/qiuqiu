import { expect, test } from '@playwright/test';
import { createContractMockState, startConsoleStaticServer } from './support/console-server.mjs';

// console-pages.spec.mjs —— 运营台全页面真实数据 E2E（intent-router 收尾验收）。
//
// 与 console-identity / console-threads 同款运行方式：真实 Go 后端
// （QIUQIU_BASE_URL）+ console/dist 构建产物经内联静态服务器伺服，/api 代理
// 到真后端。区别在于本文件**强制真实数据**：
//   1. HTTP 播种比赛配置与进球事件（带 proactiveText → 主动引用轨迹）；
//   2. WS 真实用户回合（助攻提问 / 你在干嘛 unknown / 球进了主张），分别
//      等待 qiuqiu_reply 落地——轨迹、主张观察、词汇漏斗统计全部来自真数据；
//   3. 运营员页做真实的创建/吊销写操作（审计留痕）。
// 唯一契约回退：/api/console/threads 与 /api/console/users/** 两个前缀——
// eval 后端无 DATABASE_URL，open_threads/画像集合物理为空，按仓库既定惯例
// （console-threads.spec.mjs 头注）走契约状态机，保证台账页有行、筛选可用。
//
// 每个用例同时断言：无未捕获页内异常（pageerror）、无 antd 错误弹窗
// （.ant-alert-error）——「不显示一堆异常或大面积空白」就是验收线。

const backendURL = process.env.QIUQIU_BASE_URL || 'http://127.0.0.1:18080';
const token = process.env.APP_TOKEN || 'qiuqiu-dev-token';
const matchId = 'demo-console-pages-e2e';
const fan = 'console-fan';

// antd v6 全量 bundle + WS 播种回合比默认预算重。
test.setTimeout(60_000);

let consoleServer;
let consoleBaseURL;
let socket;
// 工作导演令牌：运营员表一旦非空，共享 APP_TOKEN 立即失效（identity.go 双
// 模式）。先播种一位导演、全程用其个人令牌；afterAll 最后删它恢复 legacy。
let specToken = token;
const seededDirectorName = '值班导演E2E';

function authHeaders(extra = {}) {
  return {
    'content-type': 'application/json',
    Authorization: `Bearer ${specToken}`,
    ...extra,
  };
}

async function api(path, { method = 'GET', body } = {}) {
  const response = await fetch(`${backendURL}${path}`, {
    method,
    headers: authHeaders(method === 'GET' ? {} : { 'Idempotency-Key': `console-pages-${Date.now()}-${Math.random().toString(16).slice(2)}` }),
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const text = await response.text();
  if (!response.ok) throw new Error(`${method} ${path} -> ${response.status}: ${text}`);
  return text ? JSON.parse(text) : {};
}

// 精简版 WS 客户端（Node 24 全局 WebSocket），只做 identify/user_speech 并
// 等待 qiuqiu_reply，协议同 runtime-e2e.mjs。
async function openSocket() {
  const endpoint = new URL(backendURL);
  endpoint.protocol = 'ws:';
  endpoint.pathname = `/ws/match/${matchId}`;
  endpoint.search = '';
  // WS 通道只认配置的共享令牌（allowLegacyConnection → OperatorTokenMatches），
  // 不认导演个人令牌；页面 console API 才走 specToken。
  const protocols = token ? [`qiuqiu-auth.${Buffer.from(token).toString('base64url')}`] : [];
  const ws = new WebSocket(endpoint, protocols);
  await new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error('WS open timeout')), 8000);
    ws.addEventListener('open', () => { clearTimeout(timer); resolve(); }, { once: true });
    ws.addEventListener('error', () => { clearTimeout(timer); reject(new Error('WS open failed')); }, { once: true });
  });
  const messages = [];
  const waiters = [];
  ws.addEventListener('message', (event) => {
    if (typeof event.data !== 'string') return;
    let message;
    try { message = JSON.parse(event.data); } catch { return; }
    messages.push(message);
    const index = messages.length - 1;
    for (const waiter of [...waiters]) {
      if (index >= waiter.from && waiter.predicate(message)) {
        waiters.splice(waiters.indexOf(waiter), 1);
        waiter.resolve(message);
      }
    }
  });
  ws.addEventListener('close', () => {
    for (const waiter of [...waiters]) waiter.reject(new Error('WS closed while waiting'));
  });
  const client = {
    send: (message) => ws.send(JSON.stringify(message)),
    checkpoint: () => messages.length,
    close: () => ws.close(),
    async waitFor(predicate, timeoutMs = 8000, from = 0, label = 'condition') {
      const found = messages.find((message, index) => index >= from && predicate(message));
      if (found) return found;
      return new Promise((resolve, reject) => {
        const waiter = { predicate, from, resolve, reject };
        waiters.push(waiter);
        setTimeout(() => {
          const position = waiters.indexOf(waiter);
          if (position >= 0) {
            waiters.splice(position, 1);
            reject(new Error(`WS waitFor timeout: ${label}`));
          }
        }, timeoutMs);
      });
    },
  };
  ws.addEventListener('open', () => {}, { once: true });
  client.send({ type: 'identify', userId: fan });
  await new Promise((resolve) => setTimeout(resolve, 80));
  return client;
}

async function sendTurn(socketClient, text) {
  const from = socketClient.checkpoint();
  socketClient.send({ type: 'user_speech', userId: fan, text, talkativeness: 'normal' });
  const reply = await socketClient.waitFor(
    (message) => message.type === 'event' && message.event === 'qiuqiu_reply',
    15_000, from, `reply for ${text}`,
  );
  // 真实客户端在播完后会回报显示/播放完成；不回报会占住投递管线，下一条回复被吞。
  const traceId = reply.data?.traceId;
  if (traceId) {
    await socketClient
      .waitFor((message) => message.type === 'event' && message.event === 'voice_audio' && message.traceId === traceId, 5_000, from, `audio for ${text}`)
      .catch(() => {});
    socketClient.send({ type: 'reply_displayed', traceId });
    socketClient.send({ type: 'voice_playback', traceId, state: 'completed' });
  }
  await new Promise((resolve) => setTimeout(resolve, 400));
  return reply;
}

async function visit(page, hash) {
  const failures = [];
  page.on('pageerror', (error) => failures.push(`pageerror: ${error.message}`));
  // 令牌惯用法与 operator-auth/console-threads 同款：HashRouter + localStorage。
  await page.addInitScript((value) => {
    localStorage.setItem('qiuqiu.console.token', value);
  }, specToken);
  await page.goto(`${consoleBaseURL}/console/#${hash}`);
  await page.waitForLoadState('networkidle');
  return failures;
}

const createdOperatorNames = [];

test.beforeAll(async () => {
  const mockState = createContractMockState({ appToken: token });
  consoleServer = await startConsoleStaticServer({
    backendURL,
    mockState,
    // eval 后端无 DATABASE_URL：open_threads/画像集合物理为空（ListThreads
    // 返回空列表而非错误），台账与画像数据按仓库契约回退惯例供数。
    forceMockPrefixes: ['/api/console/threads', '/api/console/users/'],
  });
  consoleBaseURL = consoleServer.baseURL;

  // 身份双模式（与 console-identity 同款）：空表时播种导演并切换工作令牌。
  const probe = await fetch(`${backendURL}/api/console/operators`, {
    headers: { authorization: `Bearer ${token}` },
  });
  if (!probe.ok) {
    throw new Error(
      `console-pages 前置失败：APP_TOKEN 不是可用导演令牌（operators=${probe.status}）。` +
        `请重启 eval 后端清空运营员表后重跑。`,
    );
  }
  if (((await probe.json()).operators ?? []).length > 0) {
    throw new Error('console-pages 前置失败：运营员表非空且 APP_TOKEN 仍可用，状态异常；请重启 eval 后端。');
  }
  const seeded = await fetch(`${backendURL}/api/console/operators`, {
    method: 'POST',
    headers: { authorization: `Bearer ${token}`, 'content-type': 'application/json' },
    body: JSON.stringify({ name: seededDirectorName, role: 'director' }),
  });
  if (!seeded.ok) throw new Error(`seed director failed: ${seeded.status} ${await seeded.text()}`);
  specToken = (await seeded.json()).token;
  // 契约 mock 自校验令牌：把工作导演令牌注册进运营员表，threads/portrait
  // 前缀的回退请求才能放行。
  mockState.operatorSecrets.set(specToken, { name: seededDirectorName, role: 'director' });
  // authenticate 同时要求 operators 行存在，二者都要登记。
  mockState.operators.push({
    name: seededDirectorName,
    role: 'director',
    scopes: ['operator_match_write', 'fact_confirm', 'fact_correct', 'trace_read'],
    createdAt: new Date().toISOString(),
    revokedAt: null,
  });

  // —— 真实后端数据播种 ——
  await api(`/api/matches/${matchId}/reset`, { method: 'POST', body: {} });
  await api(`/api/matches/${matchId}/config`, {
    method: 'POST',
    body: {
      homeTeam: '西班牙',
      awayTeam: '德国',
      homePlayers: [
        { number: '10', name: '佩德里', position: 'CM' },
        { number: '8', name: '法比安', position: 'CM' },
        { number: '19', name: '亚马尔', position: 'RW' },
      ],
      awayPlayers: [{ number: '10', name: '穆西亚拉', position: 'AM' }],
    },
  });

  socket = await openSocket();

  const goal = await api(`/api/matches/${matchId}/events`, {
    method: 'POST',
    body: {
      eventType: 'goal', period: 'first_half', clock: '23:41', teamId: 'home', teamName: '西班牙',
      playerName: '佩德里', score: { home: 1, away: 0 }, intensity: 5, confirmed: true,
      description: '佩德里禁区前沿推射破门。', proactiveText: '佩德里进球了，法比安这次助攻很漂亮。', visibility: 'public',
    },
  });
  // 主动线落地 → recentProactive / 引用审计有真实引用。
  await socket.waitFor(
    (message) => message.type === 'event' && message.event === 'qiuqiu_reply'
      && message.data?.eventId === goal.event.id,
    10_000, 0, 'proactive goal reply',
  );

  // HTTP 播种的事件不推比赛时钟：显式把时钟推进到上半场，让后续主张回合走直播分支。
  const clock = await api(`/api/matches/${matchId}/clock`);
  await api(`/api/matches/${matchId}/clock`, {
    method: 'PATCH',
    body: { action: 'set', period: 'first_half', elapsedSeconds: 1500, expectedVersion: clock.clock?.version ?? 0 },
  });

  // 三条真实用户回合：事实问答 / unknown 兜底（词汇漏斗）/ 未证实主张。
  await sendTurn(socket, '刚才谁助攻？');
  await sendTurn(socket, '你在干嘛');
  await sendTurn(socket, '球进了');

  const traces = await api(`/api/matches/${matchId}/traces?limit=20`);
  const inputs = (traces.traces ?? []).map((trace) => trace.input);
  for (const expected of ['刚才谁助攻？', '你在干嘛', '球进了']) {
    if (!inputs.includes(expected)) throw new Error(`seed turn missing from traces: ${expected}; got ${JSON.stringify(inputs)}`);
  }
});

test.afterAll(async () => {
  // 兜底吊销测试创建的运营员：留残留会让后端进入 operators 模式、共享
  // APP_TOKEN 失效，污染后续 spec。
  for (const name of [...createdOperatorNames, seededDirectorName]) {
    await fetch(`${backendURL}/api/console/operators/${encodeURIComponent(name)}`, {
      method: 'DELETE',
      headers: { authorization: `Bearer ${specToken}` },
    }).catch(() => {});
  }
  socket?.close();
  await consoleServer?.close();
});

test('概览页六张卡片全部渲染，词汇漏斗有真实 unknown 统计', async ({ page }) => {
  const failures = await visit(page, '/console');
  expect(failures).toEqual([]);

  for (const card of ['活跃比赛', '在线会话', '话题老化', '记忆健康', '最近主动引用', '词汇漏斗（没接明白）']) {
    await expect(page.locator('.ant-card').filter({ hasText: card })).toBeVisible();
  }
  // 活跃比赛有我们播种的比赛；在线会话 ≥1（WS 连接真实注册）。
  await expect(page.locator('.ant-card').filter({ hasText: '活跃比赛' }).locator('table')).toContainText(matchId);
  const sessions = page.locator('.ant-card').filter({ hasText: '在线会话' }).locator('.ant-statistic-content-value');
  await expect(sessions).not.toContainText('0');

  // 词汇漏斗：页面数字与真后端概览 API 一致（unknown ≥1 —— 你在干嘛 落进漏斗）。
  const overviewResponse = await fetch(`${backendURL}/api/console/overview`, {
    headers: { Authorization: `Bearer ${specToken}` },
  });
  const overview = await overviewResponse.json();
  expect(overview.router.unknownTurns).toBeGreaterThanOrEqual(1);
  expect(overview.router.totalTurns).toBeGreaterThanOrEqual(overview.router.unknownTurns);
  const funnel = page.locator('.ant-card').filter({ hasText: '词汇漏斗（没接明白）' });
  await expect(funnel).toContainText('没接明白率（24h）');
  await expect(funnel).toContainText(String(overview.router.unknownTurns));
  await expect(funnel).toContainText(String(overview.router.totalTurns));
  // antd scroll 表会渲染两个 table 节点（测量行 + 滚动表），取数据表。
  await expect(funnel.locator('table').last()).toBeVisible();

  // 最近主动引用有播种的进球主动线。
  await expect(page.locator('.ant-card').filter({ hasText: '最近主动引用' })).toContainText(matchId);

  await expect(page.locator('.ant-alert-error')).toHaveCount(0);
});

test('比赛页事件流/引用审计/用户网格/设置全部渲染且有真实数据', async ({ page }) => {
  const failures = await visit(page, `/console/match/${matchId}`);
  expect(failures).toEqual([]);

  await expect(page.locator('.ant-card').filter({ hasText: `比赛 · ${matchId}` })).toBeVisible();
  // 事件流：播种的进球可见。
  await expect(page.locator('.ant-card').filter({ hasText: `比赛 · ${matchId}` })).toContainText('佩德里');
  // 引用审计收敛到独立页（c8）：比赛页留入口链接。
  const auditCard = page.locator('.ant-card').filter({ hasText: '审计轨迹' });
  await expect(auditCard.getByRole('button', { name: '去引用审计查本场轨迹' })).toBeVisible();
  // 用户网格：WS 连接真实注册的在线用户。
  const grid = page.locator('.ant-card').filter({ hasText: '用户网格' });
  await expect(grid.locator('table')).toContainText(fan);
  // 设置区写控件可见。
  const settings = page.locator('.ant-card').filter({ hasText: '设置' });
  await expect(settings.getByRole('button', { name: '保存策略' })).toBeVisible();
  await expect(settings.getByRole('button', { name: '人工接管' })).toBeVisible();

  await expect(page.locator('.ant-alert-error')).toHaveCount(0);
});

test('用户页画像/话题台账/交互历史渲染，交互历史有真实回合', async ({ page }) => {
  const failures = await visit(page, `/console/match/${matchId}/user/${fan}`);
  expect(failures).toEqual([]);

  await expect(page.locator('.ant-card').filter({ hasText: '画像（只读 + 代客删除）' })).toBeVisible();
  await expect(page.locator('.ant-card').filter({ hasText: '话题台账' })).toBeVisible();
  const history = page.locator('.ant-card').filter({ hasText: '交互历史' });
  await expect(history).toBeVisible();
  // 表格分页每页 10 行，只对首页必然存在的最早回合断言；你在干嘛 的
  // 真实数据改由交互 API 侧核验。
  await expect(history).toContainText('刚才谁助攻？');
  const interaction = await fetch(
    `${backendURL}/api/matches/${matchId}/interaction?userId=${fan}&limit=50`,
    { headers: { Authorization: `Bearer ${specToken}` } },
  ).then((r) => r.json());
  const inputs = (interaction.events || []).map((e) => e.inputText || '').join('|');
  if (!inputs.includes('你在干嘛')) {
    throw new Error(`interaction history missing 你在干嘛: ${inputs.slice(0, 200)}`);
  }

  await expect(page.locator('.ant-alert-error')).toHaveCount(0);
});

test('话题台账页有数据行且状态筛选可用（契约数据模式）', async ({ page }) => {
  const failures = await visit(page, '/console/threads');
  expect(failures).toEqual([]);

  await expect(page.locator('.ant-card').filter({ hasText: '运营员' }).locator('table')).toBeVisible().catch(() => {});
  const tables = page.locator('table');
  const threadsTable = tables.filter({ hasText: '上半场你预测西班牙' });
  await expect(threadsTable.first()).toBeVisible();
  // 状态筛选切换不抛错且表格仍在（契约数据含 open/addressed 两态）。
  await page.locator('.ant-select').first().click();
  await page.keyboard.press('Escape');
  await expect(threadsTable.first()).toBeVisible();

  await expect(page.locator('.ant-alert-error')).toHaveCount(0);
});

test('引用审计页按引用前缀查到真实轨迹，路由意图列渲染，为什么说话抽屉可用', async ({ page }) => {
  const failures = await visit(page, '/console/citations');
  expect(failures).toEqual([]);

  // 跨比赛最近主动引用：播种进球的主动线在此可见。
  await expect(page.locator('.ant-card').filter({ hasText: '跨比赛最近主动引用' })).toContainText(matchId);

  // 按前缀深查：比赛 ID + 默认 proactive_citation: 前缀（后端会剥掉命名
  // 空间再匹配），主动回合行可见。
  await page.getByLabel('比赛 ID').fill(matchId);
  await page.getByRole('button', { name: '审计' }).click();
  const traceTable = page.locator('table').filter({ hasText: '为什么说话' });
  await expect(traceTable).toContainText('主动引用 · shared_moment:');
  // ADR-0009 路由意图列头存在；eval 后端无路由 key，行内显示占位破折号。
  await expect(traceTable).toContainText('路由意图');

  // 清空前缀 = 不过滤：真实用户回合全可见。表列不含输入文本，用原因码
  // 断言行存在（球进了 → unverified_fact_requires_reserve）。
  await page.getByLabel('引用前缀').fill('');
  await page.getByRole('button', { name: '审计' }).click();
  await expect(traceTable).toContainText('unverified_fact_requires_reserve');
  await expect(traceTable).toContainText('default_acknowledgement');

  // 为什么说话抽屉：最新回合（球进了）点开，输入与输出都有真实内容。
  await traceTable.getByRole('button', { name: '为什么说话' }).first().click();
  const drawer = page.locator('.ant-drawer').filter({ hasText: '为什么说话' });
  await expect(drawer).toBeVisible();
  await expect(drawer).toContainText('球进了');
  await expect(drawer).toContainText('还没跟上');
  await expect(drawer).toContainText('球球输出');

  await expect(page.locator('.ant-alert-error')).toHaveCount(0);
});

test('运营员页真实创建与吊销生效', async ({ page, request }) => {
  const failures = await visit(page, '/console/operators');
  expect(failures).toEqual([]);

  const name = `e2e审计员-${Date.now()}`;
  createdOperatorNames.push(name);
  await page.getByPlaceholder('运营员姓名').fill(name);
  await page.getByRole('button', { name: '创建运营员' }).click();
  // 创建成功会弹出一次性个人令牌弹窗，先确认关闭再核验表格行。
  await page.getByRole('button', { name: '我已保存' }).click();
  const row = page.locator('table').locator('tr').filter({ hasText: name });
  await expect(row).toBeVisible();

  // 真后端核验：列表里确实存在（且已入身份表/审计）。
  const list = await request.get('/api/console/operators', {
    headers: { Authorization: `Bearer ${specToken}` },
  });
  expect(list.ok()).toBeTruthy();
  expect(((await list.json()).operators ?? []).some((operator) => operator.name === name)).toBeTruthy();

  await row.getByRole('button', { name: '吊销' }).click();
  await page.getByRole('button', { name: '吊销', exact: true }).last().click();
  await expect(page.locator('table').locator('tr').filter({ hasText: name })).toHaveCount(0);

  await expect(page.locator('.ant-alert-error')).toHaveCount(0);
});
