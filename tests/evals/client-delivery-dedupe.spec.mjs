import { expect, test } from '@playwright/test';

test('HTML demo client deduplicates exact deliveries but preserves fact revisions', async ({ page }) => {
  await page.addInitScript(() => {
    window.__audioPlayCount = 0;
    window.WebSocket = class MockWebSocket {
      static OPEN = 1;

      constructor() {
        this.readyState = MockWebSocket.OPEN;
        window.__testSocket = this;
        queueMicrotask(() => this.onopen?.());
      }

      send() {}

      close() {
        this.readyState = 3;
        this.onclose?.();
      }
    };
    window.Audio = class MockAudio {
      play() {
        window.__audioPlayCount += 1;
        return Promise.resolve();
      }
    };
  });
  await page.route('**/live2d-assets/live2d.html', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'text/html',
      body: '<!doctype html><script>window.setMood=()=>{};window.performAction=()=>{};<\/script>',
    });
  });
  await page.goto('/live2d-assets/app.html');
  await expect.poll(() => page.evaluate(() => Boolean(window.__testSocket?.onmessage))).toBe(true);

  await page.evaluate(() => {
    const ws = window.__testSocket;
    window.__presentationActions = [];
    live2d.contentWindow.setMood = () => {};
    live2d.contentWindow.performAction = (action) => {
      window.__presentationActions.push(action);
    };
    const matchEvent = JSON.stringify({
      type: 'match_event',
      data: {
        id: 'evt-dedupe-test',
        factRevision: 1,
        factStatus: 'confirmed',
        eventType: 'shot',
        clock: '12:34',
        description: 'dedupe match event',
        recommendedAction: 'focus',
      },
      snapshot: {
        homeTeam: 'Home',
        awayTeam: 'Away',
        score: { home: 0, away: 0 },
        clock: '12:34',
        period: 'first_half',
      },
    });
    ws.onmessage({ data: matchEvent });
    ws.onmessage({ data: matchEvent });
    ws.onmessage({ data: JSON.stringify({
      ...JSON.parse(matchEvent),
      data: {
        ...JSON.parse(matchEvent).data,
        factRevision: 2,
        factStatus: 'reconciled',
        description: 'reconciled match event',
      },
    }) });

    const reaction = JSON.stringify({
      type: 'event',
      event: 'qiuqiu_reply',
      data: {
        source: 'match_reaction',
        eventId: 'evt-dedupe-test',
        deliveryKey: 'evt-dedupe-test:1:confirmed',
        text: 'dedupe companion reaction',
      },
    });
    ws.onmessage({ data: reaction });
    ws.onmessage({ data: reaction });
    ws.onmessage({ data: JSON.stringify({
      ...JSON.parse(reaction),
      data: {
        ...JSON.parse(reaction).data,
        deliveryKey: 'evt-dedupe-test:2:reconciled',
        text: 'reconciled companion reaction',
      },
    }) });

    const presentation = JSON.stringify({
      type: 'presentation',
      source: 'match_reaction',
      eventId: 'evt-dedupe-test',
      deliveryKey: 'evt-dedupe-test:1:confirmed',
      data: { expression: 'excited', motion: 'cheer' },
    });
    ws.onmessage({ data: presentation });
    ws.onmessage({ data: presentation });
    ws.onmessage({ data: JSON.stringify({
      ...JSON.parse(presentation),
      deliveryKey: 'evt-dedupe-test:2:reconciled',
      data: { expression: 'focused', motion: 'settle' },
    }) });

    const audio = (deliveryKey) => {
      ws.onmessage({ data: JSON.stringify({
        type: 'voice_audio',
        source: 'match_reaction',
        eventId: 'evt-dedupe-test',
        deliveryKey,
        traceId: `trace-${deliveryKey}`,
        mime: 'audio/mpeg',
        byteLength: 1,
      }) });
      ws.onmessage({ data: new Uint8Array([1]).buffer });
    };
    audio('evt-dedupe-test:1:confirmed');
    audio('evt-dedupe-test:1:confirmed');
    audio('evt-dedupe-test:2:reconciled');
  });

  await expect(page.locator('.msg.match-event').filter({ hasText: 'dedupe match event' })).toHaveCount(1);
  await expect(page.locator('.msg.match-event').filter({ hasText: 'reconciled match event' })).toHaveCount(1);
  await expect(page.locator('.msg.bot').filter({ hasText: 'dedupe companion reaction' })).toHaveCount(1);
  await expect(page.locator('.msg.bot').filter({ hasText: 'reconciled companion reaction' })).toHaveCount(1);
  await expect.poll(() => page.evaluate(() => window.__presentationActions.filter((action) => ['cheer', 'settle'].includes(action)).length)).toBe(2);
  await expect.poll(() => page.evaluate(() => window.__audioPlayCount)).toBe(2);
});
