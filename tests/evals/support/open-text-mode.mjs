import { expect } from '@playwright/test';

export async function openTextMode(page) {
  const textbox = page.getByLabel('直接和球球说…');
  const moreButton = page.getByRole('button', { name: '更多陪看方式' });
  const textModeItem = page.getByRole('menuitem', { name: '改用文字说' });

  await expect.poll(async () => {
    if (await textbox.isVisible()) return true;

    for (const control of [textModeItem, moreButton]) {
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
