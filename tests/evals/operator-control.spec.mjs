import { expect, test } from '@playwright/test';

const token = process.env.APP_TOKEN || 'qiuqiu-dev-token';
const matchId = 'demo-operator-control-e2e';
const operatorURL = (view, activeMatchId = matchId) => `/operator.html?matchId=${encodeURIComponent(activeMatchId)}#${view}`;

test.beforeEach(async ({ request, context }) => {
  await context.addInitScript((value) => {
    localStorage.setItem('qiuqiu.operator.token', value);
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

test('行为按钮只更新当前草稿，不直接创建比赛事实', async ({ page, request }) => {
  await page.goto(operatorURL('live'));
  await page.locator('#homeChips .player-chip').filter({ hasText: '亚马尔' }).click();
  await page.locator('#behaviorGroups .behavior-button[data-event-type="goal"]').click();
  await expect(page.locator('#toast')).toContainText('已加入草稿');
  await expect(page.locator('#draftSummary')).toContainText('西班牙 · 进球');
  await expect(page.locator('#draftSummary')).toContainText('亚马尔');
  await expect(page.locator('#draftSubmit')).toBeEnabled();

  const timeline = await apiGet(request, `/api/matches/${matchId}/events`);
  expect(timeline.events || []).toHaveLength(0);
});

test('球员可再次点击取消，重新选择后仍可点击行为', async ({ page }) => {
  await page.goto(operatorURL('live'));
  const player = page.locator('#homeChips .player-chip').filter({ hasText: '莫拉塔' });

  await player.click();
  await expect(page.locator('#participantSelection')).toContainText('莫拉塔');
  await page.locator('#homeChips .player-chip').filter({ hasText: '莫拉塔' }).click();
  await expect(page.locator('#participantSelection')).toContainText('尚未选择球员');

  await page.locator('#homeChips .player-chip').filter({ hasText: '莫拉塔' }).click();
  await page.locator('#behaviorGroups .behavior-button[data-event-type="goal"]').click();
  await expect(page.locator('#draftSummary')).toContainText('西班牙 · 进球');
  await expect(page.locator('#slot_scorer')).toHaveValue('莫拉塔');
});

test('绝佳机会保留进攻方并允许从对方选择防守者和门将', async ({ page }) => {
  await page.goto(operatorURL('live'));
  await page.locator('#homeSideButton').click();
  await page.locator('#behaviorGroups .behavior-button[data-event-type="big_chance"]').click();
  await page.locator('#homeChips .player-chip').filter({ hasText: '佩德里' }).click();
  await page.locator('#slots .slot[data-role="passer"]').click();
  await page.locator('#homeChips .player-chip').filter({ hasText: '亚马尔' }).click();

  await page.locator('#awaySideButton').click();
  await expect(page.locator('#draftSummary')).toContainText('西班牙 · 绝佳机会');
  await expect(page.locator('#slot_attacker')).toHaveValue('佩德里');
  await expect(page.locator('#slot_passer')).toHaveValue('亚马尔');

  await page.locator('#slots .slot[data-role="defender"]').click();
  await page.locator('#awayChips .player-chip').filter({ hasText: '穆西亚拉' }).click();
  await page.locator('#slots .slot[data-role="keeper"]').click();
  await page.locator('#awayChips .player-chip').filter({ hasText: '诺伊尔' }).click();
  await expect(page.locator('#slot_defender')).toHaveValue('穆西亚拉');
  await expect(page.locator('#slot_keeper')).toHaveValue('诺伊尔');
  await expect(page.locator('#draftSummary')).toContainText('西班牙 · 绝佳机会');
});

test('绝佳机会也可由客队发起并从主队选择防守者和门将', async ({ page }) => {
  await page.goto(operatorURL('live'));
  await page.locator('#awaySideButton').click();
  await page.locator('#behaviorGroups .behavior-button[data-event-type="big_chance"]').click();
  await page.locator('#awayChips .player-chip').filter({ hasText: '穆西亚拉' }).click();
  await page.locator('#slots .slot[data-role="passer"]').click();
  await page.locator('#awayChips .player-chip').filter({ hasText: '哈弗茨' }).click();

  await page.locator('#homeSideButton').click();
  await expect(page.locator('#draftSummary')).toContainText('德国 · 绝佳机会');
  await expect(page.locator('#slot_attacker')).toHaveValue('穆西亚拉');
  await expect(page.locator('#slot_passer')).toHaveValue('哈弗茨');

  await page.locator('#slots .slot[data-role="defender"]').click();
  await page.locator('#homeChips .player-chip').filter({ hasText: '佩德里' }).click();
  await page.locator('#slots .slot[data-role="keeper"]').click();
  await page.locator('#homeChips .player-chip').filter({ hasText: '乌奈·西蒙' }).click();
  await expect(page.locator('#slot_defender')).toHaveValue('佩德里');
  await expect(page.locator('#slot_keeper')).toHaveValue('乌奈·西蒙');
  await expect(page.locator('#draftSummary')).toContainText('德国 · 绝佳机会');
});

test('切换参与方保留进球归属并切换到防守角色，切换行为清空进球专属字段', async ({ page }) => {
  await page.goto(operatorURL('live'));
  await page.locator('#homeChips .player-chip').filter({ hasText: '佩德里' }).click();
  await page.locator('#behaviorGroups .behavior-button[data-event-type="goal"]').click();
  await expect(page.locator('#slots .slot[data-role="defender"] .slot-label')).toContainText('防守相关 · 德国');
  await page.locator('#slot_assist').fill('亚马尔');
  await page.locator('#awaySideButton').click();
  await expect(page.locator('#draftSummary')).toContainText('西班牙 · 进球');
  await expect(page.locator('#slot_scorer')).toHaveValue('佩德里');
  await expect(page.locator('#slot_assist')).toHaveValue('亚马尔');
  await expect(page.locator('#slot_defender')).toHaveAttribute('placeholder', '点左侧球员或输入多个姓名（顿号分隔）');
  await expect(page.locator('#slot_defender')).toBeVisible();

  await page.locator('#awayChips .player-chip').filter({ hasText: '穆西亚拉' }).click();
  await expect(page.locator('#slot_defender')).toHaveValue('穆西亚拉');
  await page.locator('#behaviorGroups .behavior-button').filter({ hasText: '黄牌' }).click();
  await expect(page.locator('#action')).toHaveValue('complain');
  await expect(page.locator('#slots [data-role="scorer"]')).toHaveCount(0);
  await expect(page.locator('#slots [data-role="assist"]')).toHaveCount(0);
  await page.locator('#awayChips .player-chip').filter({ hasText: '穆西亚拉' }).click();
  await expect(page.locator('#slot_offender')).toHaveValue('穆西亚拉');
});

test('助攻和防守可分别选择多名球员', async ({ page }) => {
  await page.goto(operatorURL('live'));
  await page.locator('#homeChips .player-chip').filter({ hasText: '莫拉塔' }).click();
  await page.locator('#behaviorGroups .behavior-button[data-event-type="goal"]').click();
  await page.locator('#slots .slot[data-role="assist"]').click();
  await page.locator('#homeChips .player-chip').filter({ hasText: '亚马尔' }).click();
  await page.locator('#homeChips .player-chip').filter({ hasText: '佩德里' }).click();
  await expect(page.locator('#slot_assist')).toHaveValue('亚马尔、佩德里');

  await page.locator('#awaySideButton').click();
  await page.locator('#slots .slot[data-role="defender"]').click();
  await page.locator('#awayChips .player-chip').filter({ hasText: '吕迪格' }).click();
  await page.locator('#awayChips .player-chip').filter({ hasText: '基米希' }).click();
  await expect(page.locator('#slot_defender')).toHaveValue('吕迪格、基米希');
  await page.locator('#awayChips .player-chip').filter({ hasText: '吕迪格' }).click();
  await expect(page.locator('#slot_defender')).toHaveValue('基米希');
});

test('主客队切换只显示当前球队名单', async ({ page }) => {
  await page.goto(operatorURL('live'));

  await expect(page.locator('#homeChips')).toBeVisible();
  await expect(page.locator('#awayChips')).toBeHidden();
  await expect(page.locator('#homeChips')).toContainText('佩德里');
  await expect(page.locator('#awayChips')).toContainText('穆西亚拉');

  await page.locator('#awaySideButton').click();
  await expect(page.locator('#homeChips')).toBeHidden();
  await expect(page.locator('#awayChips')).toBeVisible();
  await expect(page.locator('#awayChips')).toContainText('穆西亚拉');
});

test('确认换人后才交换场上和替补席，并写入公开事实', async ({ page, request }) => {
  await page.goto(operatorURL('live'));
  await page.locator('#behaviorGroups .behavior-button[data-event-type="substitution"]').click();

  await expect(page.locator('#homeChips')).toContainText('费兰·托雷斯');
  await expect(page.locator('#homeChips')).not.toContainText('佩德里');
  await page.locator('#homeChips .player-chip').filter({ hasText: '费兰·托雷斯' }).click();
  await expect(page.locator('#homeChips')).toContainText('佩德里');
  await page.locator('#homeChips .player-chip').filter({ hasText: '佩德里' }).click();
  await page.locator('#mode').selectOption('quiet');
  await page.locator('#draftSubmit').click();
  await expect(page.locator('#toast')).toContainText('已确认并发送：换人');

  const timeline = await apiGet(request, `/api/matches/${matchId}/events`);
  expect(timeline.events).toHaveLength(1);
  expect(timeline.events[0]).toMatchObject({ eventType: 'substitution', factStatus: 'confirmed', visibility: 'public' });
  expect(timeline.events[0].participants).toEqual(expect.arrayContaining([
    expect.objectContaining({ role: 'sub_on', name: '费兰·托雷斯' }),
    expect.objectContaining({ role: 'sub_off', name: '佩德里' }),
  ]));

  await page.locator('#behaviorGroups .behavior-button[data-event-type="substitution"]').click();
  await expect(page.locator('#homeChips')).toContainText('佩德里');
  await expect(page.locator('#homeChips')).not.toContainText('费兰·托雷斯');
});

test('候选进球不改公开比分，确认后只增加一次', async ({ page, request }) => {
  await page.goto(operatorURL('live'));
  await page.locator('#homeChips .player-chip').filter({ hasText: '佩德里' }).click();
  await page.locator('#behaviorGroups .behavior-button[data-event-type="goal"]').click();
  await page.locator('#mode').selectOption('quiet');
  await page.locator('#saveCandidate').click();
  await expect(page.locator('#toast')).toContainText('已暂存候选');
  await expect(page.locator('#homeScore')).toHaveValue('0');

  let timeline = await apiGet(request, `/api/matches/${matchId}/events`);
  expect(timeline.events).toHaveLength(1);
  expect(timeline.events[0]).toMatchObject({ eventType: 'goal', factStatus: 'provisional' });
  let publicState = await apiGet(request, `/api/matches/${matchId}/state`);
  expect(publicState.snapshot.score).toEqual({ home: 0, away: 0 });

  await page.locator('#timeline .event-card').filter({ hasText: '进球' }).getByRole('button', { name: '确认', exact: true }).click();
  await expect(page.locator('#toast')).toContainText('事实已确认');
  await expect(page.locator('#homeScore')).toHaveValue('1');
  await expect(page.locator('#awayScore')).toHaveValue('0');
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

  await page.goto(operatorURL('live'));
  const card = page.locator('.event-card').filter({ hasText: '外部源识别为佩德里射门' });
  await card.getByRole('button', { name: '采用到草稿' }).click();
  await expect(page.locator('#correctionReasonField')).toBeVisible();
  await expect(page.locator('#draftSubmit')).toBeDisabled();
  await page.locator('#correctionReason').fill('已对照直播画面，球员和事件时间无误。');
  await page.locator('#mode').selectOption('quiet');
  await page.locator('#draftSubmit').click();
  await expect(page.locator('#toast')).toContainText('已确认并发送：射门');

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

  await page.goto(operatorURL('live'));
  await expect(page.locator('.conflict-resolution-card')).toContainText('多条赛况存在冲突');
  await expect(page.locator('.conflict-resolution-card').getByRole('checkbox')).toHaveCount(2);
  await expect(page.locator('.conflict-resolution-card').getByRole('button', { name: '确认事实选择' })).toBeVisible();
  const card = page.locator('.event-card:not(.conflict-resolution-card)').filter({ hasText: '外部源记录穆西亚拉进球' });
	await expect(card).toContainText('上报 0-1');
	await expect(card).toContainText('尚未生效');
  await card.getByRole('button', { name: '采用到草稿' }).click();
  await page.locator('#correctionReason').fill('已对照官方数据，采用外部源记录。');
  await page.locator('#mode').selectOption('quiet');
  await page.locator('#draftSubmit').click();
  await expect(page.locator('#toast')).toContainText('已确认并发送：进球');

  const state = await apiGet(request, `/api/matches/${matchId}/state`);
  expect(state.snapshot.score).toEqual({ home: 0, away: 1 });
  expect(state.snapshot.integrity.status).toBe('ok');
  const publicEvents = (await request.get(`/api/matches/${matchId}/events`)).json();
  expect((await publicEvents).events).toHaveLength(1);
  expect((await publicEvents).events[0].description).toBe('外部源记录穆西亚拉进球。');
  const operatorLedger = await apiGet(request, `/api/matches/${matchId}/events`);
  expect(operatorLedger.conflicts).toHaveLength(1);
  expect(operatorLedger.conflicts[0].status).toBe('resolved');
	const adoptedCard = page.locator('.event-card:not(.conflict-resolution-card)').filter({ hasText: '外部源记录穆西亚拉进球' }).filter({ hasText: '事实已确认' });
	await expect(adoptedCard).toContainText('上报 1-1');
	await expect(adoptedCard).toContainText('生效 0-1');
	await expect(page.locator('.event-card:not(.conflict-resolution-card)').filter({ hasText: '外部源记录穆西亚拉进球' }).filter({ hasText: '历史版本' })).toHaveCount(1);
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

  await page.goto(operatorURL('live'));
  const conflictCard = page.locator('.conflict-resolution-card');
  await expect(conflictCard.getByRole('checkbox', { name: '选择 人工确认佩德里进球。' })).toBeChecked();
  await conflictCard.getByRole('button', { name: '确认事实选择' }).click();
  await expect(page.locator('#toast')).toContainText('事实选择已生效');
  await expect(conflictCard).toHaveCount(0);

  const state = await apiGet(request, `/api/matches/${matchId}/state`);
  expect(state.snapshot.score).toEqual({ home: 1, away: 0 });
  expect(state.snapshot.integrity.status).toBe('ok');
  const publicEvents = await (await request.get(`/api/matches/${matchId}/events`)).json();
  expect(publicEvents.events).toHaveLength(1);
  expect(publicEvents.events[0].description).toBe('人工确认佩德里进球。');
});

test('冲突卡默认保留兼容事实，选择候选只排除直接互斥项', async ({ page }) => {
  const events = [
    {
      id: 'accepted-goal', factId: 'accepted-goal', factRevision: 1, factStatus: 'confirmed', status: 'active', confirmed: true,
      eventType: 'goal', period: 'first_half', clock: '12:00', teamId: 'home', teamName: '西班牙', score: { home: 1, away: 0 },
      description: '已确认主队进球。', visibility: 'public',
    },
    {
      id: 'accepted-card', factId: 'accepted-card', factRevision: 1, factStatus: 'confirmed', status: 'active', confirmed: true,
      eventType: 'yellow_card', period: 'first_half', clock: '12:20', teamId: 'away', teamName: '德国', score: { home: 1, away: 0 },
      description: '已确认客队黄牌。', visibility: 'public',
    },
    {
      id: 'candidate-goal', factId: 'candidate-goal', factRevision: 1, factStatus: 'provisional', status: 'active', confirmed: false,
      eventType: 'goal', period: 'first_half', clock: '12:10', teamId: 'away', teamName: '德国', score: { home: 0, away: 1 },
      description: '候选客队进球。', visibility: 'operator',
    },
  ];
  await page.route(`**/api/matches/${matchId}/events`, async (route) => {
    if (route.request().method() !== 'GET') return route.continue();
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        events,
        conflicts: [{
          id: 'conflict-compatible-selection', matchId, status: 'open',
          members: [
            { factId: 'accepted-goal', role: 'accepted' },
            { factId: 'accepted-card', role: 'accepted' },
            { factId: 'candidate-goal', role: 'candidate' },
          ],
          edges: [{ leftFactId: 'accepted-goal', rightFactId: 'candidate-goal', reason: '比分与进球队冲突' }],
        }],
      }),
    });
  });

  await page.goto(operatorURL('live'));
  const card = page.locator('.conflict-resolution-card');
  const acceptedGoal = card.getByRole('checkbox', { name: '选择 已确认主队进球。' });
  const acceptedCard = card.getByRole('checkbox', { name: '选择 已确认客队黄牌。' });
  const candidateGoal = card.getByRole('checkbox', { name: '选择 候选客队进球。' });
  await expect(acceptedGoal).toBeChecked();
  await expect(acceptedCard).toBeChecked();
  await expect(candidateGoal).not.toBeChecked();
  await expect(card).toContainText('保留 2 条 · 排除 1 条');

  await candidateGoal.check();
  await expect(acceptedGoal).not.toBeChecked();
  await expect(acceptedCard).toBeChecked();
  await expect(candidateGoal).toBeChecked();
  await expect(card).toContainText('保留 2 条 · 排除 1 条');
});

test('比分更正作为独立审计事实发布且不触发主动话术', async ({ page, request }) => {
  await apiPost(request, `/api/matches/${matchId}/events`, {
    eventType: 'goal', period: 'first_half', clock: '12:00', teamId: 'home', teamName: '西班牙',
    playerName: '佩德里', participants: [{ role: 'scorer', name: '佩德里', teamId: 'home', teamName: '西班牙' }],
    score: { home: 1, away: 0 }, description: '佩德里进球。', proactiveText: '__quiet__',
  });
  await page.goto(operatorURL('live'));
  await page.getByRole('button', { name: '人工校准比分' }).click();
  await expect(page.locator('#scoreCorrectionFields')).toBeVisible();
  await expect(page.locator('#draftSubmit')).toBeDisabled();
  await page.locator('#scoreCorrectionHome').fill('0');
  await page.locator('#scoreCorrectionAway').fill('0');
  await page.locator('#correctionReason').fill('现场记分牌回退，原进球无效。');
  await page.locator('#draftSubmit').click();
  await expect(page.locator('#toast')).toContainText('已确认并发送：比分更正');

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

  await page.goto(operatorURL('live'));
  const goalCard = page.locator('#timeline .event-card').filter({ hasText: '佩德里推射破门' });
  await goalCard.getByRole('button', { name: '拉回更正' }).click();
  await expect(page.locator('#mode')).toHaveValue('quiet');
  await page.locator('#behaviorGroups .behavior-button').filter({ hasText: '进球取消' }).click();
  await expect(page.locator('#correctionReasonField')).toBeVisible();
  await expect(page.locator('#draftSummary')).toContainText('确认后 0-0');
  await page.locator('#correctionReason').fill('VAR 回放确认越位，原进球无效。');
  await page.locator('#draftSubmit').click();
  await expect(page.locator('#toast')).toContainText('已确认并发送：进球取消');

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

  await page.goto(operatorURL('live'));
  await page.locator('#behaviorGroups .behavior-button[data-event-type="var_result"]').click();
  await expect(page.locator('#draftSubmit')).toBeDisabled();
  await expect(page.locator('#draftErrors')).toContainText('请选择 VAR 正在审查的原事实');
  await page.locator('#clearDraft').click();

  const checkCard = page.locator('#timeline .event-card').filter({ hasText: 'VAR 正在检查禁区内接触' });
  await checkCard.getByRole('button', { name: '拉回更正' }).click();
  await page.locator('#behaviorGroups .behavior-button[data-event-type="var_result"]').click();
  await page.locator('#correctionReason').fill('视频回放核对完成。');
  await page.locator('#mode').selectOption('quiet');
  await page.locator('#draftSubmit').click();
  await expect(page.locator('#toast')).toContainText('已确认并发送：VAR 结果');

  const events = (await apiGet(request, `/api/matches/${matchId}/events`)).events;
  const result = events.find((event) => event.eventType === 'var_result');
  expect(result).toMatchObject({ revisionOf: original.event.id, status: 'active' });
  expect(events.find((event) => event.id === original.event.id)?.status).toBe('active');
});

test('信号源可以切换，并通过人工接管同时暂停自动播报', async ({ page, request }) => {
  await page.goto(operatorURL('sources'));
  await expect(page.locator('[data-view-panel="sources"]')).toBeVisible();
  await expect(page.locator('[data-view-link="sources"]')).toHaveCount(2);
  await expect(page.locator('#activeSourceBadge')).not.toHaveText('读取中');
  await expect(page.locator('#sourceFreshness')).toHaveText('等待同步基准');
  await expect(page.locator('#sourceLeadHint')).toContainText('可能领先系统');
  await expect(page.locator('#manualExpectedDelay')).toHaveValue('normal');

  await page.locator('#useReplaySource').click();
  await expect(page.locator('#activeSourceBadge')).toContainText('回放数据');

  await page.locator('#manualTakeoverSource').click();
  await expect(page.locator('#activeSourceBadge')).toContainText('人工导演');
  await expect(page.locator('#toast')).toContainText('暂停自动播报');

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
  await page.goto(operatorURL('live'));
  const layout = await page.evaluate(() => {
    const participant = document.querySelector('.participant-panel').getBoundingClientRect();
    const behavior = document.querySelector('.behavior-panel').getBoundingClientRect();
    const current = document.querySelector('.current-event-panel').getBoundingClientRect();
    const timeline = document.querySelector('.live-timeline').getBoundingClientRect();
    return {
      documentFits: document.documentElement.scrollWidth <= document.documentElement.clientWidth,
      sameRow: Math.abs(participant.top - behavior.top) < 2 && Math.abs(behavior.top - current.top) < 2,
      ordered: participant.right <= behavior.left + 1 && behavior.right <= current.left + 1,
      timelineBelow: timeline.top >= Math.max(participant.bottom, behavior.bottom, current.bottom) - 1,
    };
  });
  expect(layout).toEqual({ documentFits: true, sameRow: true, ordered: true, timelineBelow: true });
});

test('比赛主时钟由后端推进，事件发生时间不会把主时钟跳回', async ({ page, request }) => {
  await page.goto(operatorURL('live'));
  await expect(page.locator('#clock')).toHaveValue('12:00');
  await expect(page.locator('#occurredClock')).toHaveValue('12:00');
  await page.locator('#clockStart').click();
  await expect(page.locator('#clockVersion')).toHaveText('版本 2');
  await page.waitForTimeout(1200);
  await expect(page.locator('#clock')).not.toHaveValue('12:00');

  await page.locator('#homeChips .player-chip').filter({ hasText: '佩德里' }).click();
  await page.locator('#behaviorGroups .behavior-button').filter({ hasText: '射门' }).click();
  const occurredAt = await page.locator('#occurredClock').inputValue();
  await page.locator('#clockPlus').click();
  await page.locator('#mode').selectOption('quiet');
  await page.locator('#draftSubmit').click();
  await expect(page.locator('#toast')).toContainText('已确认并发送：射门');
  const event = (await apiGet(request, `/api/matches/${matchId}/events`)).events.find((item) => item.eventType === 'shot');
  expect(event.clock).toBe(occurredAt);
  const backendClock = await apiGet(request, `/api/matches/${matchId}/clock`);
  expect(backendClock.clock.elapsedSeconds).toBeGreaterThan(720);
  await expect(page.locator('#clock')).not.toHaveValue(occurredAt);
});

test('切换比赛阶段会将主时钟重置为零', async ({ page, request }) => {
  await page.goto(operatorURL('live'));
  await expect(page.locator('#clock')).toHaveValue('12:00');

  await page.locator('#period').selectOption('second_half');
  await expect(page.locator('#clock')).toHaveValue('00:00');
  await expect(page.locator('#period')).toHaveValue('second_half');
  const clock = await apiGet(request, `/api/matches/${matchId}/clock`);
  expect(clock.clock).toMatchObject({ period: 'second_half', elapsedSeconds: 0 });

  await page.locator('#period').selectOption('extra_time');
  await expect(page.locator('#clock')).toHaveValue('00:00');
  await expect(page.locator('#period')).toHaveValue('extra_time');
});

test('导演语音只补全草稿，不创建比赛事实', async ({ page, request }) => {
  await page.addInitScript(() => {
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
  await page.goto(operatorURL('live'));
  await page.locator('#voiceInput').click();
  await expect(page.locator('#voiceInput')).toContainText('停止录音');
  await page.locator('#voiceInput').click();
  await expect(page.locator('#voiceStatus')).toContainText('语音已转成文字');
  await expect(page.locator('#draftSummary')).toContainText('佩德里');
  expect((await apiGet(request, `/api/matches/${matchId}/events`)).events || []).toHaveLength(0);
});

test('导演必须逐项处理语音冲突后才能发布', async ({ page, request }) => {
  await page.addInitScript(() => {
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
  await page.route(`**/api/matches/${matchId}/drafts/voice`, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        transcript: '德国穆西亚拉进球', ready: false, warnings: [],
        draft: {
          source: 'operator_voice', transcript: '德国穆西亚拉进球', teamId: 'away', teamName: '德国',
          eventType: 'goal', occurredPeriod: 'first_half', occurredSeconds: 720, capturedClockVersion: 1,
          participants: [{ role: 'scorer', name: '穆西亚拉', teamId: 'away', teamName: '德国', resolved: true }],
          description: '穆西亚拉进球。', inferredFields: ['teamId', 'eventType', 'participants', 'description'], fieldConfidence: {},
        },
      }),
    });
  });
  await page.goto(operatorURL('live'));
  await page.locator('#homeChips .player-chip').filter({ hasText: '佩德里' }).click();
  await page.locator('#behaviorGroups .behavior-button').filter({ hasText: '射门' }).click();
  await page.locator('#voiceInput').click();
  await page.locator('#voiceInput').click();

  await expect(page.locator('.voice-conflict-row')).toHaveCount(3);
  await expect(page.locator('#draftSubmit')).toBeDisabled();
  while (await page.locator('[data-voice-choice="apply"]').count()) {
    await page.locator('[data-voice-choice="apply"]').first().click();
  }
  await expect(page.locator('#draftSubmit')).toBeEnabled();
  await expect(page.locator('#draftSummary')).toContainText('德国 · 进球');
  await expect(page.locator('#draftSummary')).toContainText('穆西亚拉');
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

  const operator = await context.newPage();
  await operator.goto(operatorURL('live', userMatchId));
  await operator.locator('#homeChips .player-chip').filter({ hasText: '佩德里' }).click();
  await operator.locator('#behaviorGroups .behavior-button[data-event-type="goal"]').click();
  const occurredAt = await operator.locator('#occurredClock').inputValue();
  await operator.locator('#mode').selectOption('quiet');
  await operator.locator('#saveCandidate').click();
  await expect(operator.locator('#toast')).toContainText('已暂存候选');

  await page.waitForTimeout(500);
  await expect(page.getByText(/^0\s*—\s*0$/).first()).toBeVisible();
  const eventText = new RegExp(`${occurredAt}.*进球`);
  await expect(page.getByText(eventText)).toHaveCount(0);

  await operator.locator('#timeline .event-card').filter({ hasText: '进球' })
    .getByRole('button', { name: '确认', exact: true }).click();
  await expect(operator.locator('#toast')).toContainText('事实已确认');
  await expect(page.getByText(eventText).first()).toBeVisible({ timeout: 5_000 });
  await expect(page.getByText(/^1\s*—\s*0$/).first()).toBeVisible({ timeout: 10_000 });
  await page.waitForTimeout(500);
  await expect(page.getByText('佩德里进了！', { exact: true })).toHaveCount(0);
  expect(occurredAt).toBe('12:00');
  await expect(page.getByText(/西班牙 · 德国 · 12:\d{2}/).first()).toBeVisible();
  await operator.close();
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
  const userMatchId = 'demo-user-exit-e2e';
  await apiPost(request, `/api/matches/${userMatchId}/reset`, {});
  await apiPost(request, `/api/matches/${userMatchId}/config`, {
    homeTeam: '西班牙',
    awayTeam: '德国',
  });
  await page.addInitScript(() => {
    localStorage.setItem('flutter.first_meeting_completed', 'true');
  });
  await page.goto(`/?matchId=${encodeURIComponent(userMatchId)}`);
  await enableFlutterAccessibility(page);

  await page.getByRole('button', { name: '比赛选项' }).click();
  await page.getByRole('menuitem', { name: '退出本场' }).click();
  await expect(page.getByText('退出这场比赛？', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: '退出本场' }).click();

  await expect.poll(() => new URL(page.url()).searchParams.has('matchId')).toBe(false);
  await expect(page.getByText('选择一场比赛', { exact: true })).toBeVisible();
  await page.reload();
  await enableFlutterAccessibility(page);
  await expect(page.getByText('选择一场比赛', { exact: true })).toBeVisible();
  expect(new URL(page.url()).searchParams.has('matchId')).toBe(false);
});

test('自动化策略页面保存事件范围和冷却时间', async ({ page, request }) => {
  await page.goto(operatorURL('automation'));
  await expect(page.locator('[data-view-panel="automation"]')).toBeVisible();
  await expect(page.locator('#automationBadge')).not.toHaveText('读取中');
  for (const checkbox of await page.locator('#automationEventTypes input').all()) {
    await checkbox.uncheck({ force: true });
  }
  await page.locator('#automationEventTypes input[value="goal"]').check({ force: true });
  await page.locator('input[name="automationMode"][value="active"]').check({ force: true });
  await page.locator('#automationCooldown').fill('11');
  await page.locator('#saveAutomation').click();
  await expect(page.locator('#toast')).toContainText('自动化策略已保存');

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

  await page.goto(operatorURL('monitor'));
  await expect(page.locator('#monitorHealthValue')).toHaveText('可用');
  await expect(page.locator('#monitorMatchValue')).toHaveText('1-0');
  await expect(page.locator('#monitorEvents')).toContainText('佩德里推射破门');
  await expect(page.locator('#monitorHealthMeta')).toContainText('健康检查');
});

test('operator write buttons expose a busy state while submission is pending', async ({ page }) => {
  await page.route(`**/api/matches/${matchId}/config`, async (route) => {
    if (route.request().method() === 'POST') {
      await new Promise((resolve) => setTimeout(resolve, 400));
    }
    await route.continue();
  });
  await page.goto(operatorURL('live'));
  const saveButton = page.locator('#saveConfig');
  await saveButton.click();
  await expect(saveButton).toBeDisabled();
  await expect(saveButton).toHaveAttribute('aria-busy', 'true');
  await expect(saveButton).toContainText('保存中');
  await expect(saveButton).toBeEnabled();
  await expect(saveButton).not.toHaveAttribute('aria-busy', 'true');
  await expect(saveButton.locator('.material-symbols-outlined')).toHaveText('save');
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
