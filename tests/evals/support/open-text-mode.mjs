import { expect } from '@playwright/test';

export async function openTextMode(page) {
  const textbox = page.getByLabel('直接和球球说…');
  const catalogIndex = await configuredMatchIndex(page);
  const catalogEntries = page.getByRole('button', { name: /进入陪看|查看赛前|回看陪聊/ });
  const catalogEntry = catalogEntries.nth(catalogIndex ?? 0);
  const enterMatchButton = page.getByRole('button', { name: '进入球球的看台' });
  const moreButton = page.getByRole('button', { name: '更多陪看方式' });
  const textModeItem = page.getByRole('menuitem', { name: '改用文字说' });

  await expect.poll(async () => {
    if (await textbox.isVisible()) return true;

    for (const control of [catalogEntry, enterMatchButton, textModeItem, moreButton]) {
      if (!await control.isVisible()) continue;
      try {
        await control.evaluate((element) => element.click(), { timeout: 500 });
      } catch (error) {
        if (page.isClosed()) throw error;
      }
    }

    return textbox.isVisible();
  }, { timeout: 15_000, intervals: [100, 250, 500] }).toBe(true);
}

async function configuredMatchIndex(page) {
  try {
    const response = await page.request.get(new URL('/api/matches/catalog', page.url()).toString());
    if (!response.ok()) return null;
    const body = await response.json();
    const matchId = process.env.QIUQIU_MATCH_ID || 'test';
    if (!Array.isArray(body.matches)) return null;
    const index = body.matches.findIndex((item) => item?.matchId === matchId);
    return index >= 0 ? index : null;
  } catch {
    return null;
  }
}
