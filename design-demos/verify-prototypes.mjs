import { mkdir } from 'node:fs/promises';
import path from 'node:path';
import { pathToFileURL } from 'node:url';
import { chromium } from 'playwright';

const root = path.resolve('design-demos');
const output = path.join(root, 'screenshots');
await mkdir(output, { recursive: true });

const cases = [
  {
    name: 'prototype-a-broadcast',
    file: 'prototype-a-broadcast.html',
    steps: [
      ['[data-go="voice"]', '[data-view="voice"].active'],
      ['[data-go="live"]', '[data-view="live"].active'],
      ['[data-go="settings"]', '[data-view="settings"].active'],
    ],
  },
  {
    name: 'prototype-b-sofa',
    file: 'prototype-b-sofa.html',
    steps: [
      ['[data-go="talk"]', '[data-scene="talk"].active'],
      ['[data-go="watch"]', '[data-scene="watch"].active'],
      ['[data-go="settings"]', '[data-scene="settings"].active'],
    ],
  },
  {
    name: 'prototype-c-terrace',
    file: 'prototype-c-terrace.html',
    steps: [
      ['[data-go="live"]', '[data-page="live"].active'],
      ['#voiceOrb', '#voiceOrb.listening'],
      ['[data-go="settings"]', '[data-page="settings"].active'],
    ],
  },
];

const browser = await chromium.launch({ headless: true });
let failed = false;

for (const testCase of cases) {
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 }, deviceScaleFactor: 1 });
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  page.on('console', message => {
    if (message.type() === 'error') errors.push(message.text());
  });

  await page.goto(pathToFileURL(path.join(root, testCase.file)).href, { waitUntil: 'networkidle' });
  await page.screenshot({ path: path.join(output, `${testCase.name}.png`), fullPage: true });

  for (const [trigger, expected] of testCase.steps) {
    await page.locator(`${trigger}:visible`).first().click();
    await page.locator(expected).waitFor({ state: 'visible' });
  }

  const mobile = await browser.newPage({ viewport: { width: 390, height: 844 }, deviceScaleFactor: 1 });
  mobile.on('pageerror', error => errors.push(`mobile: ${error.message}`));
  await mobile.goto(pathToFileURL(path.join(root, testCase.file)).href, { waitUntil: 'networkidle' });
  await mobile.screenshot({ path: path.join(output, `${testCase.name}-mobile.png`) });
  await mobile.close();

  if (errors.length > 0) {
    failed = true;
    console.error(`${testCase.name}: ${errors.join(' | ')}`);
  } else {
    console.log(`${testCase.name}: ok`);
  }
  await page.close();
}

await browser.close();
if (failed) process.exit(1);
