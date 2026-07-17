import { expect, test } from '@playwright/test';

const token = process.env.APP_TOKEN || 'qiuqiu-dev-token';
const matchId = 'test';

test.beforeEach(async ({ request }) => {
  await apiPost(request, `/api/matches/${matchId}/reset`, {});
  await apiPost(request, `/api/matches/${matchId}/config`, {
    homeTeam: '西班牙',
    awayTeam: '德国',
    homePlayers: [
      { number: '10', name: '佩德里', position: 'CM' },
      { number: '19', name: '亚马尔', position: 'RW' },
    ],
  });
});

test('快捷事件只使用当前球员事实，不复用详细草稿', async ({ page, request }) => {
  await page.goto(`/operator.html?token=${encodeURIComponent(token)}#live`);
  await page.locator('#defaultPublishMode').evaluate((select) => {
    select.value = 'quiet';
  });
  await page.locator('#description').fill('佩德里推射破门。');
  await page.locator('#homeChips .player-chip').filter({ hasText: '亚马尔' }).click();
  await page.locator('#homeEvents .event-btn').filter({ hasText: '进球' }).click();
  await expect(page.locator('#toast')).toContainText('已发布：西班牙 进球');

  const timeline = await apiGet(request, `/api/matches/${matchId}/events`);
  const goal = timeline.events.find((event) => event.eventType === 'goal');
  expect(goal).toMatchObject({
    playerName: '亚马尔',
    description: '亚马尔：进球了！',
    confirmed: false,
  });
  expect(goal.description).not.toContain('佩德里');
  await expect(page.locator('#description')).toHaveValue('佩德里推射破门。');
});

test('快捷事件双击只提交一次并携带幂等键', async ({ page, request }) => {
  const eventKeys = [];
  page.on('request', (outgoing) => {
    if (outgoing.method() === 'POST' && /\/api\/matches\/test\/events$/.test(new URL(outgoing.url()).pathname)) {
      eventKeys.push(outgoing.headers()['idempotency-key'] || '');
    }
  });
  await page.goto(`/operator.html?token=${encodeURIComponent(token)}#live`);
  await page.locator('#homeChips .player-chip').filter({ hasText: '佩德里' }).click();
  const goal = page.locator('#homeEvents .event-btn').filter({ hasText: '进球' });
  await goal.evaluate((button) => {
    button.click();
    button.click();
  });
  await expect(page.locator('#toast')).toContainText('已发布：西班牙 进球');
  const timeline = await apiGet(request, `/api/matches/${matchId}/events`);
  expect(timeline.events.filter((event) => event.eventType === 'goal')).toHaveLength(1);
  expect(eventKeys).toHaveLength(1);
  expect(eventKeys[0]).not.toBe('');
});

test('详细进球事件按当前比分自动增加一分', async ({ page, request }) => {
  await page.goto(`/operator.html?token=${encodeURIComponent(token)}#live`);
  await page.locator('#eventType').selectOption('goal');
  await page.locator('#sideSelect').selectOption('home');
  await page.locator('#mainPlayer').fill('佩德里');
  await page.locator('#period').selectOption('first_half');
  await page.locator('#clock').fill('12:00');
  await page.locator('#description').fill('佩德里禁区内推射破门。');
  await page.locator('#confirmation').selectOption('confirmed');
  await page.locator('#mode').selectOption('quiet');
  await page.locator('#draft button[type="submit"]').click();
  await expect(page.locator('#timeline')).toContainText('佩德里禁区内推射破门');

  const timeline = await apiGet(request, `/api/matches/${matchId}/events`);
  const goal = timeline.events.find((event) => event.eventType === 'goal');
  expect(goal.score).toEqual({ home: 1, away: 0 });
  expect(goal.confirmed).toBe(true);
});

test('rejected detailed goal restores the latest server-confirmed score', async ({ page, request }) => {
  await page.goto(`/operator.html?token=${encodeURIComponent(token)}#live`);
  await expect(page.locator('#homeScore')).toHaveValue('0');
  await apiPost(request, `/api/matches/${matchId}/events`, {
    eventType: 'goal',
    period: 'first_half',
    clock: '03:00',
    teamId: 'home',
    teamName: '西班牙',
    playerName: '佩德里',
    score: { home: 1, away: 0 },
    description: 'Externally confirmed goal',
    confirmed: true,
    proactiveText: '__quiet__',
  });
  await page.locator('#eventType').selectOption('goal');
  await page.locator('#sideSelect').selectOption('home');
  await page.locator('#mainPlayer').fill('Pedri');
  await page.locator('#period').selectOption('pre_match');
  await page.locator('#clock').fill('00:00');
  await page.locator('#description').fill('Goal before kickoff');
  await page.locator('#mode').selectOption('quiet');
  await page.locator('#draft button[type="submit"]').click();

  await expect(page.locator('#toast')).toContainText('goal cannot occur before kickoff');
  await expect(page.locator('#homeScore')).toHaveValue('1');
  await expect(page.locator('#awayScore')).toHaveValue('0');
  await expect(page.locator('#contextScore')).toHaveText('1-0');
});

test('信号源可以切换，并通过人工接管同时暂停自动播报', async ({ page, request }) => {
  await page.goto(`/operator.html?token=${encodeURIComponent(token)}#sources`);
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

test('导播台在窄桌面和手机上不裁切或重叠', async ({ page }) => {
  for (const viewport of [
    { width: 1256, height: 900 },
    { width: 900, height: 900 },
    { width: 390, height: 844 },
  ]) {
    await page.setViewportSize(viewport);
    await page.goto(`/operator.html?token=${encodeURIComponent(token)}#live`);
    const layout = await page.evaluate(() => {
      const workspace = document.querySelector('.workspace.view.active');
      const boxes = Array.from(workspace?.children || [], (element) => element.getBoundingClientRect());
      const center = workspace?.querySelector('.center');
      const centerBox = center?.getBoundingClientRect();
      const centerChildren = Array.from(center?.children || [], (element) => element.getBoundingClientRect());
      return {
        documentFits: document.documentElement.scrollWidth <= document.documentElement.clientWidth,
        workspaceFits: workspace.scrollWidth <= workspace.clientWidth,
        panelsDoNotOverlap: boxes.every((box, index) => index === boxes.length - 1 || box.bottom <= boxes[index + 1].top + 1),
        centerContainsPanels: centerChildren.every((box) => box.top >= centerBox.top - 1 && box.bottom <= centerBox.bottom + 1),
      };
    });
    expect(layout, `${viewport.width}px live layout`).toEqual({
      documentFits: true,
      workspaceFits: true,
      panelsDoNotOverlap: true,
      centerContainsPanels: true,
    });
    await page.locator('#draftSubmit').scrollIntoViewIfNeeded();
    await page.locator('#draftSubmit').click({ trial: true });
  }

  for (const viewport of [{ width: 900, height: 900 }, { width: 390, height: 844 }]) {
    await page.setViewportSize(viewport);
    await page.goto(`/operator.html?token=${encodeURIComponent(token)}#sources`);
    const pageFits = await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth);
    expect(pageFits, `${viewport.width}px sources layout`).toBe(true);
  }
});

test('自动化策略页面保存事件范围和冷却时间', async ({ page, request }) => {
  await page.goto(`/operator.html?token=${encodeURIComponent(token)}#automation`);
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

  await page.goto(`/operator.html?token=${encodeURIComponent(token)}#monitor`);
  await expect(page.locator('#monitorHealthValue')).toHaveText('可用');
  await expect(page.locator('#monitorMatchValue')).toHaveText('1-0');
  await expect(page.locator('#monitorEvents')).toContainText('佩德里推射破门');
  await expect(page.locator('#monitorHealthMeta')).toContainText('健康检查');
});

test('operator write buttons expose a busy state while submission is pending', async ({ page }) => {
  await page.route('**/api/matches/test/config', async (route) => {
    if (route.request().method() === 'POST') {
      await new Promise((resolve) => setTimeout(resolve, 400));
    }
    await route.continue();
  });
  await page.goto(`/operator.html?token=${encodeURIComponent(token)}#live`);
  const saveButton = page.locator('#saveConfig');
  await saveButton.click();
  await expect(saveButton).toBeDisabled();
  await expect(saveButton).toHaveAttribute('aria-busy', 'true');
  await expect(saveButton).toContainText('提交中');
  await expect(saveButton).toBeEnabled();
  await expect(saveButton).not.toHaveAttribute('aria-busy', 'true');
  await expect(saveButton).toContainText('保存配置');
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

function testIdempotencyKey() {
  return `test-${Date.now()}-${Math.random().toString(16).slice(2)}`;
}
