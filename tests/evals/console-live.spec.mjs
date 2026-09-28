import { expect, test } from '@playwright/test';

// console-live.spec.mjs —— 直播监听页真 WS E2E（operations-live-stream）。
//
// 直连真后端（QIUQIU_BASE_URL，后端自伺服 console/dist），鉴权走 legacy
// APP_TOKEN 形态（与 console-auth 同一 localStorage 存储惯用法：
// qiuqiu.console.token）。断言面：
//   1. /ws/ops 子协议握手成功 → 连接徽标「实时流已连接」；
//   2. 页面内开一条 /ws/match 用户通道播种一个真实回合（save 等事实事件
//      不进 interaction 旁路，用户回合才产生 signal/turn_planned 账本行）→
//      /ws/ops 把 ops_event 推到页面 → 事件流列表出现 kind 徽标行；
//   3. 全程无未捕获页内异常（pageerror）。
// ops 流不重放历史：播种前先等徽标就绪，避免事件落在订阅建立之前。

const token = process.env.APP_TOKEN || 'qiuqiu-dev-token';
const fan = 'console-live-fan';

test.describe.configure({ timeout: 60_000 });

test('直播监听页真 WS 连接，播种事件实时上屏', async ({ page }) => {
  const failures = [];
  page.on('pageerror', (error) => failures.push(`pageerror: ${error.message}`));
  await page.addInitScript((value) => {
    localStorage.setItem('qiuqiu.console.token', value);
  }, token);
  await page.goto('/console/#/console/live');
  await page.waitForLoadState('networkidle');

  // /ws/ops 握手 + welcome：徽标翻绿。
  const badge = page.locator('.ant-badge').filter({ hasText: '实时流已连接' });
  await expect(badge).toBeVisible({ timeout: 10_000 });

  // 页面内开 /ws/match 用户通道播种一个真实回合（等 qiuqiu_reply 落地并
  // 回报 displayed，防止占住投递管线），/ws/ops 旁路应实时推上屏。
  await page.evaluate(({ token, fan }) => new Promise((resolve, reject) => {
    const encoded = btoa(token).replaceAll('+', '-').replaceAll('/', '_').replaceAll('=', '');
    const ws = new WebSocket(
      `${location.protocol === 'https:' ? 'wss:' : 'ws:'}//${location.host}/ws/match/test`,
      [`qiuqiu-auth.${encoded}`],
    );
    const timer = setTimeout(() => reject(new Error('turn timeout')), 30_000);
    ws.addEventListener('message', (event) => {
      if (typeof event.data !== 'string') return;
      let message;
      try { message = JSON.parse(event.data); } catch { return; }
      if (message.type === 'event' && message.event === 'qiuqiu_reply') {
        clearTimeout(timer);
        if (message.data?.traceId) ws.send(JSON.stringify({ type: 'reply_displayed', traceId: message.data.traceId }));
        resolve(message.data?.text || '');
        ws.close();
      }
    });
    ws.addEventListener('open', () => {
      ws.send(JSON.stringify({ type: 'user_speech', userId: fan, text: '现在比分多少？', talkativeness: 'normal' }));
    });
    ws.addEventListener('error', () => { clearTimeout(timer); reject(new Error('ws failed')); });
  }), { token, fan });

  // /ws/ops 旁路：回合账本行（signal/turn_planned/…）实时进列表。
  const list = page.locator('.ant-list');
  await expect(list).toContainText(fan, { timeout: 15_000 });
  await expect(list).toContainText('test');
  await expect(list).toContainText('回合规划');

  await expect(page.locator('.ant-alert-error')).toHaveCount(0);
  expect(failures).toEqual([]);
});
