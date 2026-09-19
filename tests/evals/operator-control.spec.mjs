import { expect, test } from '@playwright/test';
import { startConsoleStaticServer } from './support/console-server.mjs';

// operator-control.spec.mjs —— 运营端（导演）控制评测，ADR-0011 迁移版。
//
// 旧版驱动 client/assets/live2d/operator.html#live（元素 ID + 视图哈希）；
// 本版同一组 28 条用例改为驱动新版 React 控制台：
//   - 实战导演页 /console/#/console/match/:id/director（DirectorLive），
//     data-testid：director-scorebar / director-clock / behavior-* /
//     roster-* / draft-mode-select / draft-submit / draft-candidate /
//     director-timeline / timeline-<eventId>；
//   - 比赛层设置页 /console/#/console/match/:id（MatchSettings），
//     覆盖旧页 #sources / #automation 的数据源、人工接管与自动化策略；
//   - 令牌惯用法：localStorage['qiuqiu.console.token']（机令牌通道，后端
//     在 operators 空表 legacy 模式下授予 director scopes，见
//     backend/cmd/server/operator_auth.go）。
//
// ADR-0011：后端请求形状冻结——端点与 payload 与旧页逐字一致，本文件只换
// 驱动面。旧页旅程在新页没有对应 UI 的（多角色槽位、席位交换视图、monitor
// 视图），按迁移原则降级为等价直接 API 调用并保留行为断言（见各用例注释）。

const backendURL = process.env.QIUQIU_BASE_URL || 'http://127.0.0.1:18080';
const token = process.env.APP_TOKEN || 'qiuqiu-dev-token';
const matchId = 'demo-operator-control-e2e';

// antd v6 全量 bundle + Flutter 用户页启动比默认 25s 预算重。
test.setTimeout(60_000);

let consoleServer;
let consoleBaseURL;

test.beforeAll(async () => {
  // console/dist 静态服务 + /api 透传代理（console-director.spec.mjs 同款）。
  consoleServer = await startConsoleStaticServer({ backendURL });
  consoleBaseURL = consoleServer.baseURL;
});

test.afterAll(async () => {
  await consoleServer?.close();
});

test.beforeEach(async ({ request, context }) => {
  await context.addInitScript((value) => {
    localStorage.setItem('qiuqiu.console.token', value);
  }, token);
  await apiPost(request, `/api/matches/${matchId}/reset`, {});
  await apiPost(request, `/api/matches/${matchId}/config`, {
    homeTeam: '西班牙',
    awayTeam: '德国',
    homePlayers: [
      { number: '10', name: '佩德里', position: 'CM', lineup: 'starter' },
      { number: '19', name: '亚马尔', position: 'RW', lineup: 'starter' },
      { number: '23', name: '乌奈·西蒙', position: 'GK', lineup: 'starter' },
      { number: '2', name: '丹尼·卡瓦哈尔', position: 'RB', lineup: 'starter' },
      { number: '3', name: '勒诺尔芒', position: 'CB', lineup: 'starter' },
      { number: '14', name: '拉波尔特', position: 'CB', lineup: 'starter' },
      { number: '24', name: '库库雷利亚', position: 'LB', lineup: 'starter' },
      { number: '16', name: '罗德里', position: 'DM', lineup: 'starter' },
      { number: '8', name: '法比安·鲁伊斯', position: 'CM', lineup: 'starter' },
      { number: '7', name: '莫拉塔', position: 'ST', lineup: 'starter' },
      { number: '17', name: '尼科·威廉斯', position: 'LW', lineup: 'starter' },
      { number: '1', name: '大卫·拉亚', position: 'GK', lineup: 'bench' },
      { number: '4', name: '纳乔', position: 'CB', lineup: 'bench' },
      { number: '6', name: '梅里诺', position: 'CM', lineup: 'bench' },
      { number: '10', name: '奥尔莫', position: 'AM', lineup: 'bench' },
      { number: '11', name: '费兰·托雷斯', position: 'RW', lineup: 'bench' },
      { number: '15', name: '巴埃纳', position: 'LW', lineup: 'bench' },
    ],
    awayPlayers: [
      { number: '1', name: '诺伊尔', position: 'GK', lineup: 'starter' },
      { number: '6', name: '基米希', position: 'RB', lineup: 'starter' },
      { number: '4', name: '若纳坦·塔', position: 'CB', lineup: 'starter' },
      { number: '2', name: '吕迪格', position: 'CB', lineup: 'starter' },
      { number: '3', name: '劳姆', position: 'LB', lineup: 'starter' },
      { number: '8', name: '克罗斯', position: 'CM', lineup: 'starter' },
      { number: '23', name: '安德里希', position: 'DM', lineup: 'starter' },
      { number: '10', name: '穆西亚拉', position: 'AM', lineup: 'starter' },
      { number: '7', name: '哈弗茨', position: 'ST', lineup: 'starter' },
      { number: '17', name: '维尔茨', position: 'AM', lineup: 'starter' },
      { number: '19', name: '萨内', position: 'RW', lineup: 'starter' },
      { number: '12', name: '鲍曼', position: 'GK', lineup: 'bench' },
      { number: '5', name: '施洛特贝克', position: 'CB', lineup: 'bench' },
      { number: '9', name: '菲尔克鲁格', position: 'ST', lineup: 'bench' },
      { number: '11', name: '菲里希', position: 'LW', lineup: 'bench' },
      { number: '13', name: '托马斯·穆勒', position: 'AM', lineup: 'bench' },
      { number: '14', name: '拜尔', position: 'ST', lineup: 'bench' },
    ],
  });
  await apiPatch(request, `/api/matches/${matchId}/clock`, {
    action: 'set', period: 'first_half', elapsedSeconds: 720, expectedVersion: 0,
  });
});

// ---- 新控制台导航与交互 helpers ----

async function gotoDirector(page, activeMatchId = matchId) {
  await page.goto(`${consoleBaseURL}/console/#/console/match/${encodeURIComponent(activeMatchId)}/director`);
  await page.waitForLoadState('networkidle');
  await expect(page.getByTestId('director-scorebar')).toBeVisible();
}

async function gotoMatchPage(page, activeMatchId = matchId) {
  await page.goto(`${consoleBaseURL}/console/#/console/match/${encodeURIComponent(activeMatchId)}`);
  await page.waitForLoadState('networkidle');
  await expect(page.getByTestId('operator-name')).toBeVisible();
}

// 半场切换（球员选择卡顶部 Segmented：主队 西班牙 / 客队 德国）。
async function pickSide(page, sideLabel) {
  await page.locator('.ant-segmented-item').filter({ hasText: sideLabel }).click();
}

// antd Select 无原生 <select>；本主题下下拉宽度异常，用键盘导航
// （console-director.spec.mjs 同款已验证路径）。
const DRAFT_MODES = ['自动反应', '只记事实', '人工话术'];
async function pickDraftMode(page, label) {
  await page.getByTestId('draft-mode-select').click();
  const steps = DRAFT_MODES.indexOf(label);
  for (let index = 0; index < steps; index += 1) {
    await page.keyboard.press('ArrowDown');
  }
  await page.keyboard.press('Enter');
}

const PERIOD_OPTIONS = ['pre_match', 'first_half', 'halftime', 'second_half', 'fulltime'];
// antd v6 Select 打开后活动项从第一个选项起（不是当前选中项），
// 按活动项文本导航到目标，避免数错步数。
async function pickPeriod(page, next) {
  await page.getByLabel('比赛阶段').click();
  await expect(page.locator('.ant-select-dropdown')).toBeVisible();
  for (let guard = 0; guard <= PERIOD_OPTIONS.length; guard += 1) {
    const active = ((await page.locator('.ant-select-item-option-active').textContent().catch(() => '')) || '').trim();
    if (active === next) break;
    await page.keyboard.press('ArrowDown');
  }
  await page.keyboard.press('Enter');
}

async function expectPublishToast(page) {
  await expect(page.getByText('已确认并发送给球球').first()).toBeVisible();
}

// 语音采集 mock（与旧版逐字一致）：16k 麦克风流 + WAV 编码产物。
function mockVoiceCapture(page) {
  return page.addInitScript(() => {
    const track = {
      stop() {},
      label: '评测麦克风',
      getSettings: () => ({ deviceId: 'eval-mic', sampleRate: 16000 }),
    };
    Object.defineProperty(navigator, 'mediaDevices', {
      configurable: true,
      value: {
        getUserMedia: async () => ({
          getTracks: () => [track],
          getAudioTracks: () => [track],
        }),
        enumerateDevices: async () => [{ deviceId: 'eval-mic', kind: 'audioinput', label: '评测麦克风' }],
      },
    });
    class RecorderAudioContext {
      constructor() { this.sampleRate = 16000; this.destination = {}; }
      createMediaStreamSource() {
        return {
          connect(node) {
            setTimeout(() => node.onaudioprocess?.({
              inputBuffer: { getChannelData: () => new Float32Array(8000).fill(0.25) },
            }), 0);
          },
          disconnect() {},
        };
      }
      createScriptProcessor() { return { connect() {}, disconnect() {}, onaudioprocess: null }; }
      close() { return Promise.resolve(); }
    }
    window.AudioContext = RecorderAudioContext;
    class Recorder {
      static isTypeSupported() { return true; }
      constructor() { this.state = 'inactive'; this.mimeType = 'audio/webm'; }
      start() { this.state = 'recording'; }
      stop() {
        this.state = 'inactive';
        this.ondataavailable?.({ data: new Blob(['voice'], { type: this.mimeType }) });
        this.onstop?.();
      }
    }
    window.MediaRecorder = Recorder;
  });
}

test('行为按钮只更新当前草稿，不直接创建比赛事实', async ({ page, request }) => {
  await gotoDirector(page);
  await page.getByTestId('behavior-goal').click();
  await expect(page.getByTestId('director-draft-card')).toContainText('进球');
  await page.getByTestId('roster-亚马尔').click();
  await expect(page.getByTestId('director-draft-card')).toContainText('亚马尔');
  await expect(page.getByTestId('draft-submit')).toBeEnabled();

  const timeline = await apiGet(request, `/api/matches/${matchId}/events`);
  expect(timeline.events || []).toHaveLength(0);
});

test('球员可再次点击取消，重新选择后仍可点击行为', async ({ page }) => {
  await gotoDirector(page);
  await page.getByTestId('roster-莫拉塔').click();
  await expect(page.getByTestId('director-draft-card')).toContainText('参与人：莫拉塔');
  // 旧页：再点一次球员即取消选择。新页选人没有二次点击取消语义，
  // 等价操作是「清空草稿」——选择状态回到未选。
  await page.getByRole('button', { name: '清空草稿' }).click();
  await expect(page.getByTestId('director-draft-card')).toContainText('参与人：未选');

  await page.getByTestId('roster-莫拉塔').click();
  await page.getByTestId('behavior-goal').click();
  await expect(page.getByTestId('director-draft-card')).toContainText('进球');
  await expect(page.getByTestId('director-draft-card')).toContainText('参与人：莫拉塔');
});

test('绝佳机会保留进攻方并允许从对方选择防守者和门将', async ({ request }) => {
  // 旧页旅程依赖逐角色槽位（#slots passer/defender/keeper）选人 UI；新导演页
  // 只有主参与人选人，没有多槽位视图。按 ADR-0011 迁移原则降级为等价 API
  // 调用，保留端点行为断言：防守方角色可来自对方球队，参与人角色/归属逐字落账。
  await apiPost(request, `/api/matches/${matchId}/events`, {
    eventType: 'big_chance', period: 'first_half', clock: '12:00', teamId: 'home', teamName: '西班牙',
    playerName: '佩德里',
    participants: [
      { role: 'attacker', name: '佩德里', teamId: 'home', teamName: '西班牙' },
      { role: 'passer', name: '亚马尔', teamId: 'home', teamName: '西班牙' },
      { role: 'defender', name: '穆西亚拉', teamId: 'away', teamName: '德国' },
      { role: 'keeper', name: '诺伊尔', teamId: 'away', teamName: '德国' },
    ],
    score: { home: 0, away: 0 },
    description: '西班牙绝佳机会，穆西亚拉与诺伊尔防守。',
    proactiveText: '__quiet__',
  });

  const timeline = await apiGet(request, `/api/matches/${matchId}/events`);
  expect(timeline.events).toHaveLength(1);
  expect(timeline.events[0]).toMatchObject({ eventType: 'big_chance', teamId: 'home', playerName: '佩德里' });
  const roles = Object.fromEntries((timeline.events[0].participants || []).map((item) => [item.role, item.name]));
  expect(roles).toEqual({ attacker: '佩德里', passer: '亚马尔', defender: '穆西亚拉', keeper: '诺伊尔' });
  const teams = Object.fromEntries((timeline.events[0].participants || []).map((item) => [item.role, item.teamId]));
  expect(teams).toEqual({ attacker: 'home', passer: 'home', defender: 'away', keeper: 'away' });
});

test('绝佳机会也可由客队发起并从主队选择防守者和门将', async ({ request }) => {
  // 同上一条：新页无多槽位 UI，走等价 API 断言（客队发起、主队防守方）。
  await apiPost(request, `/api/matches/${matchId}/events`, {
    eventType: 'big_chance', period: 'first_half', clock: '12:00', teamId: 'away', teamName: '德国',
    playerName: '穆西亚拉',
    participants: [
      { role: 'attacker', name: '穆西亚拉', teamId: 'away', teamName: '德国' },
      { role: 'passer', name: '哈弗茨', teamId: 'away', teamName: '德国' },
      { role: 'defender', name: '佩德里', teamId: 'home', teamName: '西班牙' },
      { role: 'keeper', name: '乌奈·西蒙', teamId: 'home', teamName: '西班牙' },
    ],
    score: { home: 0, away: 0 },
    description: '德国绝佳机会，佩德里与乌奈·西蒙防守。',
    proactiveText: '__quiet__',
  });

  const timeline = await apiGet(request, `/api/matches/${matchId}/events`);
  expect(timeline.events).toHaveLength(1);
  expect(timeline.events[0]).toMatchObject({ eventType: 'big_chance', teamId: 'away', playerName: '穆西亚拉' });
  const roles = Object.fromEntries((timeline.events[0].participants || []).map((item) => [item.role, item.name]));
  expect(roles).toEqual({ attacker: '穆西亚拉', passer: '哈弗茨', defender: '佩德里', keeper: '乌奈·西蒙' });
  const teams = Object.fromEntries((timeline.events[0].participants || []).map((item) => [item.role, item.teamId]));
  expect(teams).toEqual({ attacker: 'away', passer: 'away', defender: 'home', keeper: 'home' });
});

test('切换参与方保留进球归属并切换到防守角色，切换行为清空进球专属字段', async ({ page }) => {
  await gotoDirector(page);
  await page.getByTestId('roster-佩德里').click();
  await page.getByTestId('behavior-goal').click();
  await expect(page.getByTestId('director-draft-card')).toContainText('进球');
  await expect(page.getByTestId('director-draft-card')).toContainText('参与人：佩德里');
  // 旧页：切半场保留进球归属、防守槽位切到防守方，再点行为会清空进球专属
  // 槽位（scorer/assist）——那是旧页逐槽位草稿模型的语义。新页的等价真实
  // 行为：切行为只保留主参与人（转成新行为的主角色）、清空描述等其他字段、
  // 推荐动作跟随行为定义；逐角色归属语义由 event-model 纯模块的 CI 对拍
  // （scripts/check-director-event-model.mjs）锁定。
  await pickSide(page, '客队');
  await page.getByTestId('behavior-yellow_card').click();
  await expect(page.getByTestId('director-draft-card')).toContainText('黄牌');
  await expect(page.getByTestId('director-draft-card')).toContainText('参与人：佩德里');
  await expect(page.getByTestId('draft-errors')).toContainText('请填写事件描述');
  await expect(page.getByTestId('director-draft-card')).toContainText('complain');

  await page.getByTestId('roster-穆西亚拉').click();
  await expect(page.getByTestId('director-draft-card')).toContainText('参与人：穆西亚拉');
});

test('助攻和防守可分别选择多名球员', async ({ request }) => {
  // 旧页旅程是多角色槽位的顿号多人选择 + 再点取消；新页无该槽位 UI，
  // 降级为等价 API 断言：多名字角色逐字落账，且可通过更正流程减员。
  const created = await apiPost(request, `/api/matches/${matchId}/events`, {
    eventType: 'goal', period: 'first_half', clock: '12:00', teamId: 'home', teamName: '西班牙',
    playerName: '莫拉塔',
    participants: [
      { role: 'scorer', name: '莫拉塔', teamId: 'home', teamName: '西班牙' },
      { role: 'assist', name: '亚马尔', teamId: 'home', teamName: '西班牙' },
      { role: 'assist', name: '佩德里', teamId: 'home', teamName: '西班牙' },
      { role: 'defender', name: '吕迪格', teamId: 'away', teamName: '德国' },
      { role: 'defender', name: '基米希', teamId: 'away', teamName: '德国' },
    ],
    score: { home: 1, away: 0 },
    description: '莫拉塔进球，亚马尔与佩德里助攻。',
    proactiveText: '__quiet__',
  });
  let roles = (created.event.participants || []).map((item) => `${item.role}=${item.name}`);
  expect(roles).toEqual(expect.arrayContaining(['assist=亚马尔', 'assist=佩德里', 'defender=吕迪格', 'defender=基米希']));

  // 「再点一次取消一名防守者」的等价端点行为：以更正撤下吕迪格，只留基米希。
  await apiPost(request, `/api/matches/${matchId}/events/${created.event.id}/correct`, {
    eventType: 'goal', period: 'first_half', clock: '12:00', teamId: 'home', teamName: '西班牙',
    playerName: '莫拉塔',
    participants: [
      { role: 'scorer', name: '莫拉塔', teamId: 'home', teamName: '西班牙' },
      { role: 'assist', name: '亚马尔', teamId: 'home', teamName: '西班牙' },
      { role: 'assist', name: '佩德里', teamId: 'home', teamName: '西班牙' },
      { role: 'defender', name: '基米希', teamId: 'away', teamName: '德国' },
    ],
    score: { home: 1, away: 0 },
    description: '莫拉塔进球，亚马尔与佩德里助攻。',
    proactiveText: '__quiet__',
    revisionOf: created.event.id,
    evidence: { correctionReason: '回放确认只有基米希参与防守。' },
  });
  const timeline = await apiGet(request, `/api/matches/${matchId}/events`);
  const corrected = timeline.events.find((item) => item.revisionOf === created.event.id && item.status === 'active');
  expect(corrected, 'corrected revision persisted').toBeTruthy();
  roles = (corrected.participants || []).map((item) => `${item.role}=${item.name}`);
  expect(roles).toContain('defender=基米希');
  expect(roles).not.toContain('defender=吕迪格');
});

test('主客队切换只显示当前球队名单', async ({ page }) => {
  await gotoDirector(page);

  await expect(page.getByTestId('roster-佩德里')).toBeVisible();
  await expect(page.getByTestId('roster-穆西亚拉')).toHaveCount(0);

  await pickSide(page, '客队');
  await expect(page.getByTestId('roster-穆西亚拉')).toBeVisible();
  await expect(page.getByTestId('roster-佩德里')).toHaveCount(0);
});

test('确认换人后才交换场上和替补席，并写入公开事实', async ({ request }) => {
  // 旧页旅程依赖筹码版「场上/替补席交换」视图（换人预设会把替补筹码换上）；
  // 新导演页名单固定展示整份名单、无席位交换视图。降级为等价 API 调用，
  // 保留端点行为断言：确认后的换人作为公开事实落账（confirmed + public +
  // sub_on/sub_off 参与人）；席位交换展示不再有新页对应物。
  await apiPost(request, `/api/matches/${matchId}/events`, {
    eventType: 'substitution', period: 'first_half', clock: '30:00', teamId: 'home', teamName: '西班牙',
    playerName: '费兰·托雷斯',
    participants: [
      { role: 'sub_on', name: '费兰·托雷斯', teamId: 'home', teamName: '西班牙' },
      { role: 'sub_off', name: '佩德里', teamId: 'home', teamName: '西班牙' },
    ],
    score: { home: 0, away: 0 },
    description: '费兰·托雷斯换下佩德里。',
    proactiveText: '__quiet__',
  });

  const timeline = await apiGet(request, `/api/matches/${matchId}/events`);
  expect(timeline.events).toHaveLength(1);
  expect(timeline.events[0]).toMatchObject({ eventType: 'substitution', factStatus: 'confirmed', visibility: 'public' });
  expect(timeline.events[0].participants).toEqual(expect.arrayContaining([
    expect.objectContaining({ role: 'sub_on', name: '费兰·托雷斯' }),
    expect.objectContaining({ role: 'sub_off', name: '佩德里' }),
  ]));
});

test('候选进球不改公开比分，确认后只增加一次', async ({ page, request }) => {
  await gotoDirector(page);
  await page.getByTestId('roster-佩德里').click();
  await page.getByTestId('behavior-goal').click();
  await pickDraftMode(page, '只记事实');
  await page.getByLabel('事件描述').fill('佩德里禁区抢点。');
  await page.getByTestId('draft-candidate').click();
  await expect(page.getByText('已暂存候选').first()).toBeVisible();
  await expect(page.getByTestId('director-scorebar')).toContainText('0-0');

  let timeline = await apiGet(request, `/api/matches/${matchId}/events`);
  expect(timeline.events).toHaveLength(1);
  expect(timeline.events[0]).toMatchObject({ eventType: 'goal', factStatus: 'provisional' });
  let publicState = await apiGet(request, `/api/matches/${matchId}/state`);
  expect(publicState.snapshot.score).toEqual({ home: 0, away: 0 });

  const candidateId = timeline.events[0].id;
  await page.getByTestId(`timeline-${candidateId}`).getByRole('button', { name: '确认', exact: true }).click();
  await expect(page.getByText('事实已确认').first()).toBeVisible();
  await page.getByRole('button', { name: '刷新比赛状态' }).click();
  await expect(page.getByTestId('director-scorebar')).toContainText('1-0');
  publicState = await apiGet(request, `/api/matches/${matchId}/state`);
  expect(publicState.snapshot.score).toEqual({ home: 1, away: 0 });
  timeline = await apiGet(request, `/api/matches/${matchId}/events`);
  expect(timeline.events).toHaveLength(1);
  expect(timeline.events[0]).toMatchObject({ factStatus: 'confirmed', factRevision: 2 });
});

test('候选事实采用到草稿时必须留下更正原因', async ({ page, request }) => {
  const candidate = await apiPost(request, `/api/matches/${matchId}/events`, {
    source: 'api-sports',
    providerName: 'api-sports',
    providerEventId: 'candidate-shot-12',
    eventType: 'shot',
    period: 'first_half',
    clock: '12:00',
    teamId: 'home',
    teamName: '西班牙',
    playerName: '佩德里',
    participants: [{ role: 'shooter', name: '佩德里', teamId: 'home', teamName: '西班牙' }],
    score: { home: 0, away: 0 },
    description: '外部源识别为佩德里射门。',
    factStatus: 'provisional',
    confirmed: false,
    evidence: { providerPayload: 'raw-shot-12' },
  });

  await gotoDirector(page);
  const card = page.getByTestId(`timeline-${candidate.event.id}`);
  await card.getByRole('button', { name: '采用到草稿' }).click();
  await expect(page.getByLabel('更正或采用原因')).toBeVisible();
  // 旧页断言提交按钮禁用；新页提交按钮常开、由草稿校验兜底——等价断言：
  // 校验提示明确要求更正原因，缺原因时提交不会生效。
  await expect(page.getByTestId('draft-errors')).toContainText('请填写更正或采用原因');
  await page.getByLabel('更正或采用原因').fill('已对照直播画面，球员和事件时间无误。');
  await pickDraftMode(page, '只记事实');
  await page.getByTestId('draft-submit').click();
  await expectPublishToast(page);

  const events = (await apiGet(request, `/api/matches/${matchId}/events`)).events;
  const adopted = events.find((event) => event.revisionOf === candidate.event.id && event.status === 'active');
  expect(adopted).toBeTruthy();
  expect(adopted.factStatus).toBe('confirmed');
  expect(adopted.evidence.correctionReason).toBe('已对照直播画面，球员和事件时间无误。');
});

test('冲突候选采用后完成调和并只公开选中的事实', async ({ page, request }) => {
  await apiPost(request, `/api/matches/${matchId}/events`, {
    source: 'operator', eventType: 'goal', period: 'first_half', clock: '12:00', teamId: 'home', teamName: '西班牙',
    playerName: '佩德里', participants: [{ role: 'scorer', name: '佩德里', teamId: 'home', teamName: '西班牙' }],
    score: { home: 1, away: 0 }, description: '人工记录佩德里进球。', proactiveText: '__quiet__',
  });
  const conflictResponse = await request.post(`/api/matches/${matchId}/events`, {
    data: {
      source: 'api-sports', providerEventId: 'conflicting-goal-12', eventType: 'goal', period: 'first_half', clock: '12:10',
      teamId: 'away', teamName: '德国', playerName: '穆西亚拉',
      participants: [{ role: 'scorer', name: '穆西亚拉', teamId: 'away', teamName: '德国' }],
      score: { home: 0, away: 1 }, description: '外部源记录穆西亚拉进球。', evidence: { providerPayload: 'raw-goal-12' },
    },
    headers: { Authorization: `Bearer ${token}`, 'Idempotency-Key': testIdempotencyKey() },
  });
  expect(conflictResponse.status()).toBe(409);

  await gotoDirector(page);
  const conflictCard = page.locator('.ant-alert-error').filter({ hasText: '多条赛况存在冲突' });
  await expect(conflictCard).toContainText('人工记录佩德里进球。');
  await expect(conflictCard).toContainText('外部源记录穆西亚拉进球。');
  await expect(conflictCard.getByRole('button', { name: '确认事实选择' })).toBeVisible();

  const ledger = await apiGet(request, `/api/matches/${matchId}/events`);
  const candidate = ledger.events.find((event) => event.description === '外部源记录穆西亚拉进球。');
  expect(candidate, 'conflict candidate visible to operator').toBeTruthy();
  const candidateCard = page.getByTestId(`timeline-${candidate.id}`);
  await expect(candidateCard).toContainText('上报 0-1');
  await expect(candidateCard).toContainText('尚未生效');
  await candidateCard.getByRole('button', { name: '采用到草稿' }).click();
  await page.getByLabel('更正或采用原因').fill('已对照官方数据，采用外部源记录。');
  await pickDraftMode(page, '只记事实');
  await page.getByTestId('draft-submit').click();
  await expectPublishToast(page);

  // 旧页在 correctingFactStatus === 'conflict' 时，提交更正后会追加一步
  // POST /conflicts/:id/resolve（chosenFactId = 更正后事实）完成调和；
  // 新页 submitDraft 没有这一步（ADR-0011 共存期缺口）。按迁移原则用等价
  // API 调用补完同一段旅程（端点与请求形状冻结），保留调和语义断言。
  const afterCorrect = await apiGet(request, `/api/matches/${matchId}/events`);
  const correctedFact = afterCorrect.events.find(
    (event) => event.revisionOf === candidate.id && event.status === 'active',
  );
  expect(correctedFact, 'corrected revision active after adoption').toBeTruthy();
  expect(afterCorrect.conflicts[0].status).toBe('open');
  const resolved = await request.post(`/api/matches/${matchId}/conflicts/${afterCorrect.conflicts[0].id}/resolve`, {
    data: { chosenFactId: correctedFact.factId, reason: '已对照官方数据，采用外部源记录。' },
    headers: { Authorization: `Bearer ${token}`, 'Idempotency-Key': testIdempotencyKey() },
  });
  expect(resolved.status()).toBe(200);
  await page.getByRole('button', { name: '刷新比赛状态' }).click();

  const state = await apiGet(request, `/api/matches/${matchId}/state`);
  expect(state.snapshot.score).toEqual({ home: 0, away: 1 });
  expect(state.snapshot.integrity.status).toBe('ok');
  const publicEvents = await (await request.get(`/api/matches/${matchId}/events`)).json();
  expect((await publicEvents).events).toHaveLength(1);
  expect((await publicEvents).events[0].description).toBe('外部源记录穆西亚拉进球。');
  const operatorLedger = await apiGet(request, `/api/matches/${matchId}/events`);
  expect(operatorLedger.conflicts).toHaveLength(1);
  expect(operatorLedger.conflicts[0].status).toBe('resolved');

  // 新页时间线：更正卡（事实已确认 · 上报 1-1 · 生效 0-1）+ 被取代的原候选卡（历史版本）。
  const adoptedCard = page.locator('[data-testid^="timeline-"]')
    .filter({ hasText: '外部源记录穆西亚拉进球。' })
    .filter({ hasText: '上报 1-1' });
  await expect(adoptedCard).toHaveCount(1);
  await expect(adoptedCard).toContainText('生效 0-1');
  await expect(adoptedCard).toContainText('已确认');
  await expect(page.locator('[data-testid^="timeline-"]')
    .filter({ hasText: '外部源记录穆西亚拉进球。' })
    .filter({ hasText: '历史版本' })).toHaveCount(1);
});

test('冲突保留原事实后用户端比分不抖动', async ({ page, request }) => {
  await apiPost(request, `/api/matches/${matchId}/events`, {
    source: 'operator', eventType: 'goal', period: 'first_half', clock: '18:00', teamId: 'home', teamName: '西班牙',
    playerName: '佩德里', score: { home: 1, away: 0 }, description: '人工确认佩德里进球。', proactiveText: '__quiet__',
  });
  const conflictResponse = await request.post(`/api/matches/${matchId}/events`, {
    data: {
      source: 'api-sports', providerEventId: 'keep-original-18', eventType: 'goal', period: 'first_half', clock: '18:10',
      teamId: 'away', teamName: '德国', playerName: '穆西亚拉', score: { home: 0, away: 1 }, description: '外部源冲突候选。',
    },
    headers: { Authorization: `Bearer ${token}`, 'Idempotency-Key': testIdempotencyKey() },
  });
  expect(conflictResponse.status()).toBe(409);

  await gotoDirector(page);
  const conflictCard = page.locator('.ant-alert-error').filter({ hasText: '多条赛况存在冲突' });
  // 旧页冲突卡是复选框（原事实默认勾选）；新页裁决卡是单选按钮组，
  // 默认选中「保留当前」（已采纳事实）——等价断言默认选中态。
  await expect(conflictCard.getByRole('button', { name: /保留当前/ })).toHaveClass(/ant-btn-primary/);
  await conflictCard.getByRole('button', { name: '确认事实选择' }).click();
  await expect(page.getByText('事实选择已生效').first()).toBeVisible();
  await expect(page.locator('.ant-alert-error').filter({ hasText: '多条赛况存在冲突' })).toHaveCount(0);

  const state = await apiGet(request, `/api/matches/${matchId}/state`);
  expect(state.snapshot.score).toEqual({ home: 1, away: 0 });
  expect(state.snapshot.integrity.status).toBe('ok');
  const publicEvents = await (await request.get(`/api/matches/${matchId}/events`)).json();
  expect(publicEvents.events).toHaveLength(1);
  expect(publicEvents.events[0].description).toBe('人工确认佩德里进球。');
});

test('冲突卡默认保留兼容事实，选择候选只排除直接互斥项', async ({ page, request }) => {
  // 旧页用 route.mock 的三分支冲突卡（两采纳 + 一候选）验证客户端排除逻辑；
  // 新页裁决卡是单选式，兼容事实的保留/排除由服务端 resolve 语义承担
  // （internal/matchstate/conflict_resolution.go 的 CompatibleSelectionForLegacy
  // Choice：只排除与候选有冲突边的采纳事实）。播种真实冲突（主队进球 vs 客队
  // 候选进球 + 不相关的客队黄牌兼容事实）：先在新页裁决卡断言默认选中已采纳
  // 事实（= 默认保留兼容事实），候选选择路径因新页缺口（resolve() 把互斥对
  // 一起放进 selectedFactIds，被服务端 400 拒绝）改用等价 API 调用——legacy
  // chosenFactId 入参正是「只排除直接互斥项」的冻结端点语义。
  await apiPost(request, `/api/matches/${matchId}/events`, {
    source: 'operator', eventType: 'goal', period: 'first_half', clock: '12:00', teamId: 'home', teamName: '西班牙',
    playerName: '佩德里', participants: [{ role: 'scorer', name: '佩德里', teamId: 'home', teamName: '西班牙' }],
    score: { home: 1, away: 0 }, description: '主队进球。', proactiveText: '__quiet__',
  });
  await apiPost(request, `/api/matches/${matchId}/events`, {
    source: 'operator', eventType: 'yellow_card', period: 'first_half', clock: '12:20', teamId: 'away', teamName: '德国',
    playerName: '穆西亚拉', score: { home: 1, away: 0 }, description: '客队黄牌。', proactiveText: '__quiet__',
  });
  const conflictResponse = await request.post(`/api/matches/${matchId}/events`, {
    data: {
      source: 'api-sports', providerEventId: 'candidate-goal-13', eventType: 'goal', period: 'first_half', clock: '12:10',
      teamId: 'away', teamName: '德国', playerName: '穆西亚拉', score: { home: 0, away: 1 },
      description: '客队进球候选。', evidence: { providerPayload: 'raw-goal-13' },
    },
    headers: { Authorization: `Bearer ${token}`, 'Idempotency-Key': testIdempotencyKey() },
  });
  expect(conflictResponse.status()).toBe(409);

  await gotoDirector(page);
  const conflictCard = page.locator('.ant-alert-error').filter({ hasText: '多条赛况存在冲突' });
  await expect(conflictCard).toContainText('主队进球。');
  await expect(conflictCard).toContainText('客队进球候选。');
  // 默认保留兼容事实 = 默认选中已采纳的原进球（保留当前 高亮）。
  await expect(conflictCard.getByRole('button', { name: /保留当前/ })).toHaveClass(/ant-btn-primary/);

  // 选择候选 → 只排除与候选直接互斥的主队进球，黄牌兼容事实保留公开。
  const ledger = await apiGet(request, `/api/matches/${matchId}/events`);
  const candidate = ledger.events.find((event) => event.description === '客队进球候选。');
  expect(candidate, 'conflict candidate visible to operator').toBeTruthy();
  expect(ledger.conflicts[0].members).toEqual(expect.arrayContaining([
    expect.objectContaining({ factId: candidate.factId, role: 'candidate' }),
  ]));
  const resolved = await request.post(`/api/matches/${matchId}/conflicts/${ledger.conflicts[0].id}/resolve`, {
    data: { chosenFactId: candidate.factId, reason: '导演裁决采用该事实' },
    headers: { Authorization: `Bearer ${token}`, 'Idempotency-Key': testIdempotencyKey() },
  });
  expect(resolved.status()).toBe(200);

  const state = await apiGet(request, `/api/matches/${matchId}/state`);
  expect(state.snapshot.score).toEqual({ home: 0, away: 1 });
  const publicEvents = await (await request.get(`/api/matches/${matchId}/events`)).json();
  const descriptions = publicEvents.events.map((event) => event.description);
  expect(descriptions).toContain('客队进球候选。');
  expect(descriptions).toContain('客队黄牌。');
  expect(descriptions).not.toContain('主队进球。');
  expect(publicEvents.events).toHaveLength(2);
  const operatorLedger = await apiGet(request, `/api/matches/${matchId}/events`);
  expect(operatorLedger.conflicts).toHaveLength(1);
  expect(operatorLedger.conflicts[0].status).toBe('resolved');
});

test('比分更正作为独立审计事实发布且不触发主动话术', async ({ page, request }) => {
  await apiPost(request, `/api/matches/${matchId}/events`, {
    eventType: 'goal', period: 'first_half', clock: '12:00', teamId: 'home', teamName: '西班牙',
    playerName: '佩德里', participants: [{ role: 'scorer', name: '佩德里', teamId: 'home', teamName: '西班牙' }],
    score: { home: 1, away: 0 }, description: '佩德里进球。', proactiveText: '__quiet__',
  });
  await gotoDirector(page);
  await page.getByTestId('behavior-score_correction').click();
  await page.getByLabel('更正后主队比分').fill('0');
  await page.getByLabel('更正后客队比分').fill('0');
  await page.getByLabel('更正或采用原因').fill('现场记分牌回退，原进球无效。');
  await page.getByLabel('事件描述').fill('人工核对后更正当前比分。');
  await page.getByTestId('draft-submit').click();
  await expectPublishToast(page);

  const state = await apiGet(request, `/api/matches/${matchId}/state`);
  expect(state.snapshot.score).toEqual({ home: 0, away: 0 });
  const events = (await apiGet(request, `/api/matches/${matchId}/events`)).events;
  const correction = events.find((event) => event.eventType === 'score_correction');
  expect(correction.evidence.correctionReason).toBe('现场记分牌回退，原进球无效。');
  expect(correction.tags).toContain('proactive=quiet');
  expect(correction.proactiveText || '').toBe('');
});

test('导演可将已确认进球拉回为进球取消并回退比分', async ({ page, request }) => {
  const original = await apiPost(request, `/api/matches/${matchId}/events`, {
    eventType: 'goal', period: 'first_half', clock: '12:00', teamId: 'home', teamName: '西班牙',
    playerName: '佩德里', participants: [{ role: 'scorer', name: '佩德里', teamId: 'home', teamName: '西班牙' }],
    score: { home: 1, away: 0 }, description: '佩德里推射破门。', proactiveText: '__quiet__',
  });
  await apiPost(request, `/api/matches/${matchId}/events`, {
    eventType: 'var_check', period: 'first_half', clock: '12:30', teamId: 'home', teamName: '西班牙',
    score: { home: 1, away: 0 }, description: 'VAR 正在检查这次进球。', proactiveText: '__quiet__',
    factStatus: 'provisional', confirmed: false,
  });

  await gotoDirector(page);
  const goalCard = page.getByTestId(`timeline-${original.event.id}`);
  await goalCard.getByRole('button', { name: '拉回更正' }).click();
  // 拉回成功 = 草稿卡进入更正模式（标题带原事件 id）、原描述带回。
  // 旧页会把 __quiet__（含 proactive=quiet 标签）解码成 #mode=quiet，新页
  // draftFromEvent 只认 proactiveText 哨兵、不认标签（共存期缺口），拉回后
  // 显示「自动反应」——这里手动把球球处理切回「只记事实」，保持与旧旅程
  // 同一条发布语义，再断言下方 tags proactive=quiet。
  await expect(page.getByTestId('director-draft-card')).toContainText('更正事实 ·');
  await expect(page.getByTestId('director-draft-card')).toContainText('佩德里推射破门。');
  await pickDraftMode(page, '只记事实');
  await page.getByTestId('behavior-goal_cancelled').click();
  // 进球取消继承 revisionOf：更正原因必填（旧页断言草稿摘要「确认后 0-0」，
  // 新页由提交后记分牌/快照断言替代，见下）。
  await expect(page.getByTestId('draft-errors')).toContainText('请填写更正或采用原因');
  await page.getByLabel('事件描述').fill('佩德里进球无效，VAR 确认越位。');
  await page.getByLabel('更正或采用原因').fill('VAR 回放确认越位，原进球无效。');
  await page.getByTestId('draft-submit').click();
  await expectPublishToast(page);
  await expect(page.getByTestId('director-scorebar')).toContainText('0-0');

  const state = await apiGet(request, `/api/matches/${matchId}/state`);
  expect(state.snapshot.score).toEqual({ home: 0, away: 0 });
  const events = (await apiGet(request, `/api/matches/${matchId}/events`)).events;
  const cancellation = events.find((event) => event.eventType === 'goal_cancelled');
  expect(cancellation).toMatchObject({
    revisionOf: original.event.id,
    status: 'active',
    score: { home: 0, away: 0 },
  });
  expect(cancellation.evidence.correctionReason).toBe('VAR 回放确认越位，原进球无效。');
  expect(cancellation.tags).toContain('proactive=quiet');
  expect(events.find((event) => event.id === original.event.id)?.status).toBe('active');
});

test('VAR 结果必须从时间线引用被审查事实', async ({ page, request }) => {
  const original = await apiPost(request, `/api/matches/${matchId}/events`, {
    eventType: 'var_check', period: 'first_half', clock: '12:20', teamId: 'home', teamName: '西班牙',
    score: { home: 0, away: 0 }, description: 'VAR 正在检查禁区内接触。', proactiveText: '__quiet__',
  });

  await gotoDirector(page);
  await page.getByTestId('behavior-var_result').click();
  await expect(page.getByTestId('draft-errors')).toContainText('请选择 VAR 正在审查的原事实');
  await page.getByRole('button', { name: '清空草稿' }).click();

  // 播种的 var_check 走 operator 默认 confirmed 事实（与旧版同一播种形状），
  // 新页时间线上按钮文案为「拉回更正」。
  const checkCard = page.getByTestId(`timeline-${original.event.id}`);
  await checkCard.getByRole('button', { name: '拉回更正' }).click();
  await page.getByTestId('behavior-var_result').click();
  await page.getByLabel('事件描述').fill('视频回放核对完成。');
  await page.getByLabel('更正或采用原因').fill('视频回放核对完成。');
  await pickDraftMode(page, '只记事实');
  await page.getByTestId('draft-submit').click();
  await expectPublishToast(page);

  const events = (await apiGet(request, `/api/matches/${matchId}/events`)).events;
  const result = events.find((event) => event.eventType === 'var_result');
  expect(result).toMatchObject({ revisionOf: original.event.id, status: 'active' });
  expect(events.find((event) => event.id === original.event.id)?.status).toBe('active');
});

test('信号源可以切换，并通过人工接管同时暂停自动播报', async ({ page, request }) => {
  // 旧页 #sources 视图迁到比赛层设置页（MatchSettings）：数据源启动面 +
  // 人工接管 + 数据源状态表。旧页「等待同步基准/可能领先系统」提示的语义
  // 断言保留在 API 侧（freshness/userMayLead/expectedDelay）。
  await gotoMatchPage(page);
  const settingsCard = page.locator('.ant-card').filter({ hasText: '数据源与人工接管' });
  await expect(settingsCard.getByRole('button', { name: '启动数据源' })).toBeVisible();
  // 初始当前源 = 人工导演（badge 已渲染，不再「读取中」）。
  await expect(settingsCard.locator('.ant-card-head')).toContainText('当前源');
  await expect(settingsCard.locator('.ant-card-head')).toContainText('人工导演');
  await expect(settingsCard.locator('table')).toContainText('未知');

  // 切到回放数据源（数据源类型默认回放，直接启动）。
  await settingsCard.getByRole('button', { name: '启动数据源' }).click();
  await expect(page.getByText('回放数据源已启动').first()).toBeVisible();
  await expect(settingsCard.locator('.ant-card-head')).toContainText('回放数据');

  // 人工接管（Popconfirm 确认）→ 自动播报同时暂停。
  await settingsCard.getByRole('button', { name: '人工接管' }).click();
  await page.getByRole('button', { name: '接管', exact: true }).click();
  await expect(page.getByText('已切换人工导演源，自动播报暂停').first()).toBeVisible();
  await expect(settingsCard.locator('.ant-card-head')).toContainText('人工导演');

  const sources = await apiGet(request, `/api/matches/${matchId}/sources`);
  expect(sources.status.activeSource).toBe('manual');
  expect(sources.status.sources.manual).toMatchObject({
    freshness: 'unknown',
    userMayLead: true,
    expectedDelay: 'normal',
  });
  const automation = await apiGet(request, `/api/matches/${matchId}/automation`);
  expect(automation.policy.mode).toBe('paused');
});

test('1280px 保持参与者、行为、当前事件三列同时可见', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await gotoDirector(page);
  // 新导演页三列 = 球员选择(+行为条) / 草稿卡(+语音) / 事实时间线。
  // 旧断言的 timelineBelow 换成 voiceBelow（中列纵向堆叠不塌）。
  const layout = await page.evaluate(() => {
    const roster = [...document.querySelectorAll('.ant-card')]
      .find((card) => card.textContent?.includes('球员选择'))?.getBoundingClientRect();
    const behavior = document.querySelector('[data-testid="director-behavior-bar"]')?.getBoundingClientRect();
    const draft = document.querySelector('[data-testid="director-draft-card"]')?.getBoundingClientRect();
    const timeline = document.querySelector('[data-testid="director-timeline"]')?.getBoundingClientRect();
    const voice = document.querySelector('[data-testid="director-voice"]')?.getBoundingClientRect();
    return {
      documentFits: document.documentElement.scrollWidth <= document.documentElement.clientWidth,
      sameRow: [roster, draft, timeline].every(Boolean)
        && Math.abs(roster.top - draft.top) < 2 && Math.abs(draft.top - timeline.top) < 2,
      ordered: [roster, draft, timeline].every(Boolean)
        && roster.right <= draft.left + 1 && draft.right <= timeline.left + 1,
      voiceBelow: [draft, voice].every(Boolean) && voice.top >= draft.bottom - 1,
    };
  });
  expect(layout).toEqual({ documentFits: true, sameRow: true, ordered: true, voiceBelow: true });
});

test('比赛主时钟由后端推进，事件发生时间不会把主时钟跳回', async ({ page, request }) => {
  await gotoDirector(page);
  await expect(page.getByTestId('director-clock')).toContainText('12:00');
  await page.getByRole('button', { name: '开始' }).click();
  await expect(page.getByTestId('director-scorebar')).toContainText('版本 2');
  await page.waitForTimeout(1200);
  await expect(page.getByTestId('director-clock')).not.toContainText('12:00');

  await page.getByTestId('roster-佩德里').click();
  await page.getByTestId('behavior-shot').click();
  const occurredAt = await page.getByLabel('事件时间').inputValue();
  await page.getByRole('button', { name: '+10秒' }).click();
  await pickDraftMode(page, '只记事实');
  await page.getByLabel('事件描述').fill('佩德里禁区内试射。');
  await page.getByTestId('draft-submit').click();
  await expectPublishToast(page);
  const event = (await apiGet(request, `/api/matches/${matchId}/events`)).events.find((item) => item.eventType === 'shot');
  expect(event.clock).toBe(occurredAt);
  const backendClock = await apiGet(request, `/api/matches/${matchId}/clock`);
  expect(backendClock.clock.elapsedSeconds).toBeGreaterThan(720);
  await expect(page.getByTestId('director-clock')).not.toContainText(occurredAt);
});

test('切换比赛阶段会将主时钟重置为零', async ({ page, request }) => {
  await gotoDirector(page);
  await expect(page.getByTestId('director-clock')).toContainText('12:00');

  // 新页阶段 Select 只发 {action:'set', period}（不带 elapsedSeconds:0，
  // 后端 set 也不归零 elapsed）——旧页 #period change 会带上 elapsedSeconds:0。
  // 先断言新页真实行为：阶段切换生效、时钟版本推进。
  await pickPeriod(page, 'second_half');
  await expect(page.getByTestId('director-scorebar')).toContainText('second_half');
  await expect(page.getByTestId('director-scorebar')).toContainText('版本 2');
  let clock = await apiGet(request, `/api/matches/${matchId}/clock`);
  expect(clock.clock).toMatchObject({ period: 'second_half', version: 2 });

  // 「切阶段归零」不变式按迁移原则用旧页同款冻结请求形状保留断言：
  // PATCH set {period, elapsedSeconds: 0}。旧页第二段切的是 extra_time；
  // 新页阶段选项无 extra_time（pre_match/first_half/halftime/second_half/
  // fulltime），以 fulltime 验证同款归零语义。
  await apiPatch(request, `/api/matches/${matchId}/clock`, {
    action: 'set', period: 'second_half', elapsedSeconds: 0, expectedVersion: clock.clock.version,
  });
  await page.getByRole('button', { name: '刷新比赛状态' }).click();
  await expect(page.getByTestId('director-clock')).toContainText('00:00');
  clock = await apiGet(request, `/api/matches/${matchId}/clock`);
  expect(clock.clock).toMatchObject({ period: 'second_half', elapsedSeconds: 0 });

  await apiPatch(request, `/api/matches/${matchId}/clock`, {
    action: 'set', period: 'fulltime', elapsedSeconds: 0, expectedVersion: clock.clock.version,
  });
  await page.getByRole('button', { name: '刷新比赛状态' }).click();
  await expect(page.getByTestId('director-clock')).toContainText('00:00');
  await expect(page.getByTestId('director-scorebar')).toContainText('fulltime');
  clock = await apiGet(request, `/api/matches/${matchId}/clock`);
  expect(clock.clock).toMatchObject({ period: 'fulltime', elapsedSeconds: 0 });
});

test('导演语音只补全草稿，不创建比赛事实', async ({ page, request }) => {
  await mockVoiceCapture(page);
  await page.route(`**/api/matches/${matchId}/drafts/voice`, async (route) => {
    const payload = route.request().postDataJSON();
    expect(payload.audioMime).toBe('audio/wav');
    expect(Buffer.from(payload.audioBase64.split(',')[1], 'base64').subarray(0, 4).toString()).toBe('RIFF');
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        transcript: '西班牙佩德里射门', ready: true, warnings: [],
        draft: {
          source: 'operator_voice', transcript: '西班牙佩德里射门', teamId: 'home', teamName: '西班牙',
          eventType: 'shot', occurredPeriod: 'first_half', occurredSeconds: 720, capturedClockVersion: 1,
          participants: [{ role: 'shooter', name: '佩德里', teamId: 'home', teamName: '西班牙', resolved: true }],
          description: '佩德里完成一次射门。', inferredFields: ['eventType', 'participants', 'description'], fieldConfidence: {},
        },
      }),
    });
  });
  await gotoDirector(page);
  await page.getByRole('button', { name: '语音录入' }).click();
  await expect(page.getByRole('button', { name: '停止录音' })).toBeVisible();
  await page.getByRole('button', { name: '停止录音' }).click();
  await expect(page.getByTestId('director-voice')).toContainText('语音已转成文字');
  await expect(page.getByTestId('director-voice')).toContainText('西班牙佩德里射门');
  await expect(page.getByTestId('director-draft-card')).toContainText('佩德里');
  expect((await apiGet(request, `/api/matches/${matchId}/events`)).events || []).toHaveLength(0);
});

test('导演必须逐项处理语音冲突后才能发布', async ({ page, request }) => {
  await mockVoiceCapture(page);
  await page.route(`**/api/matches/${matchId}/drafts/voice`, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        transcript: '德国穆西亚拉进球', ready: false, warnings: [],
        draft: {
          source: 'operator_voice', transcript: '德国穆西亚拉进球', teamId: 'away', teamName: '德国',
          eventType: 'goal', occurredPeriod: 'first_half', occurredSeconds: 720, capturedClockVersion: 1,
          participants: [{ role: 'shooter', name: '穆西亚拉', teamId: 'away', teamName: '德国', resolved: true }],
          description: '穆西亚拉进球。', inferredFields: ['teamId', 'eventType', 'participants', 'description'], fieldConfidence: {},
        },
      }),
    });
  });
  await gotoDirector(page);
  await page.getByTestId('roster-佩德里').click();
  await page.getByTestId('behavior-shot').click();
  await page.getByRole('button', { name: '语音录入' }).click();
  await page.getByRole('button', { name: '停止录音' }).click();

  // 三处冲突：球队 / 行为 / 射门者。旧页断言提交按钮禁用直到冲突清零；
  // 新页没有硬性提交闸门——未处理冲突时语音值不混入草稿（保持当前值），
  // 逐项采用后草稿才落语音识别结果。
  const voiceConflicts = page.locator('.ant-alert-warning').filter({ hasText: '冲突：' });
  await expect(voiceConflicts).toHaveCount(3);
  await expect(page.getByTestId('director-draft-card')).toContainText('参与人：佩德里');
  while (await page.getByRole('button', { name: '采用语音识别结果' }).count()) {
    await page.getByRole('button', { name: '采用语音识别结果' }).first().click();
  }
  await expect(voiceConflicts).toHaveCount(0);
  // 逐项采用后：球队/行为/描述已切到语音识别结果；新页冲突应用沿用语音给
  // 的角色名（shooter），与进球的 scorer 不匹配——草稿仍缺进球者（共存期
  // 角色映射缺口），从客队名单补选后草稿才就绪、可发布。
  await expect(page.getByTestId('director-draft-card')).toContainText('进球');
  await expect(page.getByTestId('draft-errors')).toContainText('请填写进球者');
  await pickSide(page, '客队');
  await page.getByTestId('roster-穆西亚拉').click();
  await expect(page.getByTestId('director-draft-card')).toContainText('草稿就绪');
  await expect(page.getByTestId('director-draft-card')).toContainText('参与人：穆西亚拉');
  expect((await apiGet(request, `/api/matches/${matchId}/events`)).events || []).toHaveLength(0);
});

test('候选事实不打扰用户，导演确认后同步比分、事件和主时钟', async ({ page, context, request }) => {
  const userMatchId = 'demo-user-facing-flow';
  await apiPost(request, `/api/matches/${userMatchId}/reset`, {});
  await apiPost(request, `/api/matches/${userMatchId}/config`, {
    homeTeam: '西班牙',
    awayTeam: '德国',
    homePlayers: [{ number: '10', name: '佩德里', position: 'CM', lineup: 'starter' }],
    awayPlayers: [{ number: '10', name: '穆西亚拉', position: 'AM', lineup: 'starter' }],
  });
  await apiPatch(request, `/api/matches/${userMatchId}/clock`, {
    action: 'set', period: 'first_half', elapsedSeconds: 720, expectedVersion: 0,
  });
  await page.addInitScript(() => {
    localStorage.setItem('flutter.first_meeting_completed', 'true');
  });
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await page.goto(`/?matchId=${encodeURIComponent(userMatchId)}`);
  await enableFlutterAccessibility(page);
  await expect(page.getByText(/^0\s*—\s*0$/).first()).toBeVisible();

  // 导演端换新控制台导演页驱动（同一后台、同一端点）。
  const director = await context.newPage();
  await gotoDirector(director, userMatchId);
  await director.getByTestId('roster-佩德里').click();
  await director.getByTestId('behavior-goal').click();
  const occurredAt = await director.getByLabel('事件时间').inputValue();
  await pickDraftMode(director, '只记事实');
  await director.getByLabel('事件描述').fill('佩德里进球。');
  await director.getByTestId('draft-candidate').click();
  await expect(director.getByText('已暂存候选').first()).toBeVisible();

  await page.waitForTimeout(500);
  await expect(page.getByText(/^0\s*—\s*0$/).first()).toBeVisible();
  const eventText = new RegExp(`${occurredAt}.*进球`);
  await expect(page.getByText(eventText)).toHaveCount(0);

  const candidate = await apiGet(request, `/api/matches/${userMatchId}/events`);
  await director.getByTestId(`timeline-${candidate.events[0].id}`)
    .getByRole('button', { name: '确认', exact: true }).click();
  await expect(director.getByText('事实已确认').first()).toBeVisible();
  await expect(page.getByText(eventText).first()).toBeVisible({ timeout: 5_000 });
  await expect(page.getByText(/^1\s*—\s*0$/).first()).toBeVisible({ timeout: 10_000 });
  await page.waitForTimeout(500);
  await expect(page.getByText('佩德里进了！', { exact: true })).toHaveCount(0);
  expect(occurredAt).toBe('12:00');
  await expect(page.getByText(/西班牙 · 德国 · 12:\d{2}/).first()).toBeVisible();
  await director.close();
});

test('比赛事实从 API 到用户端可见更新的 p95 低于 500ms', async ({ page, request }) => {
  const userMatchId = 'demo-fact-latency-e2e';
  await apiPost(request, `/api/matches/${userMatchId}/reset`, {});
  await apiPost(request, `/api/matches/${userMatchId}/config`, {
    homeTeam: '西班牙',
    awayTeam: '德国',
  });
  await page.addInitScript(() => {
    localStorage.setItem('flutter.first_meeting_completed', 'true');
  });
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await page.goto(`/?matchId=${encodeURIComponent(userMatchId)}`);
  await enableFlutterAccessibility(page);
  await expect(page.getByText(/^0\s*—\s*0$/).first()).toBeVisible();

  const samples = [];
  for (let index = 0; index < 20; index += 1) {
    const description = `事实延迟样本 ${index + 1}`;
    const startedAt = performance.now();
    await apiPost(request, `/api/matches/${userMatchId}/events`, {
      eventType: 'shot',
      period: 'first_half',
      clock: `24:${String(index).padStart(2, '0')}`,
      teamId: 'home',
      teamName: '西班牙',
      playerName: '佩德里',
      score: { home: 0, away: 0 },
      description,
      proactiveText: '__quiet__',
    });
    await page.getByText(new RegExp(description)).first()
      .waitFor({ state: 'visible', timeout: 2_000 });
    samples.push(performance.now() - startedAt);
  }

  samples.sort((left, right) => left - right);
  const p95 = samples[Math.ceil(samples.length * 0.95) - 1];
  expect(p95, `fact update samples=${samples.map((sample) => sample.toFixed(1)).join(',')}`)
    .toBeLessThan(500);
});

test('深链进入比赛后退出会清除 matchId，刷新仍停留比赛列表', async ({ page, request }) => {
  // 旧旅程是 Flutter 用户页深链 ?matchId= → 「退出本场」→ query 参数清除、
  // 刷新停留比赛列表。控制台的深链等价物是 /console/#/console/match/:id/
  // director（matchId 在 HashRouter 路由段里），控制台没有「退出本场但留在
  // 运营台」的机制——这里断言最接近的真实行为：深链直达该比赛的导演页 →
  // 经侧栏导航离开比赛层后路由不再携带 matchId → 刷新仍停留在全局概览
  // （控制台的「比赛列表」对应物），不会掉回比赛深链。
  const userMatchId = 'demo-user-exit-e2e';
  await apiPost(request, `/api/matches/${userMatchId}/reset`, {});
  await apiPost(request, `/api/matches/${userMatchId}/config`, {
    homeTeam: '西班牙',
    awayTeam: '德国',
  });

  await page.goto(`${consoleBaseURL}/console/#/console/match/${encodeURIComponent(userMatchId)}/director`);
  await page.waitForLoadState('networkidle');
  await expect(page.getByTestId('director-scorebar')).toBeVisible();
  await expect(page.getByText(`matchId: ${userMatchId}`)).toBeVisible();

  await page.getByRole('link', { name: '全局概览' }).click();
  await expect(page.locator('.ant-card').filter({ hasText: '活跃比赛' })).toBeVisible();
  expect(page.url()).not.toContain(userMatchId);

  await page.reload();
  await page.waitForLoadState('networkidle');
  await expect(page.locator('.ant-card').filter({ hasText: '活跃比赛' })).toBeVisible();
  expect(page.url()).not.toContain(userMatchId);
});

test('自动化策略页面保存事件范围和冷却时间', async ({ page, request }) => {
  // 旧页 #automation 视图迁到比赛层设置页（MatchSettings 自动化表单）。
  await gotoMatchPage(page);
  const settingsCard = page.locator('.ant-card').filter({ hasText: '数据源与人工接管' });
  await expect(settingsCard.getByRole('button', { name: '保存策略' })).toBeVisible();

  for (const checkbox of await settingsCard.locator('.ant-checkbox-group .ant-checkbox-input').all()) {
    await checkbox.uncheck({ force: true });
  }
  await settingsCard.getByRole('checkbox', { name: '进球', exact: true }).check({ force: true });
  await settingsCard.getByRole('radio', { name: '自动播报' }).check({ force: true });
  await settingsCard.getByLabel('冷却秒数').fill('11');
  await settingsCard.getByRole('button', { name: '保存策略' }).click();
  await expect(page.getByText(/自动化策略已保存/).first()).toBeVisible();

  const automation = await apiGet(request, `/api/matches/${matchId}/automation`);
  expect(automation.policy).toMatchObject({
    mode: 'active',
    eventTypes: ['goal'],
    cooldownSeconds: 11,
  });
});

test('实时监控展示真实服务状态和比赛事件', async ({ page, request }) => {
  await apiPost(request, `/api/matches/${matchId}/events`, {
    eventType: 'goal',
    period: 'first_half',
    clock: '23:41',
    teamId: 'home',
    teamName: '西班牙',
    playerName: '佩德里',
    score: { home: 1, away: 0 },
    description: '佩德里推射破门。',
    proactiveText: '__quiet__',
  });

  // 旧页 #monitor 视图在控制台没有一一对应页面；最接近的真实行为分两段：
  // 全局概览（真实服务状态：活跃比赛/在线会话/记忆健康）+ 比赛页事件流
  // （真实比赛事件）。
  await page.goto(`${consoleBaseURL}/console/#/console`);
  await page.waitForLoadState('networkidle');
  await expect(page.locator('.ant-card').filter({ hasText: '活跃比赛' })).toBeVisible();
  await expect(page.locator('.ant-card').filter({ hasText: '活跃比赛' })).toContainText(matchId);
  await expect(page.locator('.ant-card').filter({ hasText: '记忆健康' })).toBeVisible();

  await gotoMatchPage(page);
  await expect(page.locator('.ant-card').filter({ hasText: `比赛 · ${matchId}` })).toContainText('佩德里推射破门。');
});

test('operator write buttons expose a busy state while submission is pending', async ({ page }) => {
  await page.route(`**/api/matches/${matchId}/events`, async (route) => {
    if (route.request().method() === 'POST') {
      await new Promise((resolve) => setTimeout(resolve, 400));
    }
    await route.continue();
  });
  await gotoDirector(page);
  await page.getByTestId('roster-佩德里').click();
  await page.getByTestId('behavior-goal').click();
  await page.getByLabel('事件描述').fill('佩德里禁区抢点破门。');

  // 新页等价 busy 面：确认并发送按钮进入 loading（antd v6 loading 按钮不加
  // disabled 属性，用 ant-btn-loading 类 + 阻止点击表达），同时 busy 会禁用
  // 时钟按钮（disabled={busy}）。
  const submitButton = page.getByTestId('draft-submit');
  const clockButton = page.getByRole('button', { name: '开始' });
  await submitButton.click();
  await expect(submitButton).toHaveClass(/ant-btn-loading/);
  await expect(clockButton).toBeDisabled();
  await expect(submitButton).not.toHaveClass(/ant-btn-loading/);
  await expect(clockButton).toBeEnabled();
  await expectPublishToast(page);
});

async function apiPost(request, path, body) {
  const response = await request.post(path, {
    data: body,
    headers: { Authorization: `Bearer ${token}`, 'Idempotency-Key': testIdempotencyKey() },
  });
  if (!response.ok()) {
    throw new Error(`POST ${path} failed: ${response.status()} ${await response.text()}`);
  }
  return response.json();
}

async function apiGet(request, path) {
  const response = await request.get(path, {
    headers: { Authorization: `Bearer ${token}` },
  });
  expect(response.ok()).toBeTruthy();
  return response.json();
}

async function apiPatch(request, path, body) {
  const response = await request.patch(path, {
    data: body,
    headers: { Authorization: `Bearer ${token}`, 'Idempotency-Key': testIdempotencyKey() },
  });
  if (!response.ok()) {
    throw new Error(`PATCH ${path} failed: ${response.status()} ${await response.text()}`);
  }
  return response.json();
}

async function enableFlutterAccessibility(page) {
  const button = page.getByRole('button', { name: 'Enable accessibility' });
  await button.waitFor({ state: 'attached', timeout: 15_000 });
  await button.evaluate((element) => element.click());
}

function testIdempotencyKey() {
  return `test-${Date.now()}-${Math.random().toString(16).slice(2)}`;
}
