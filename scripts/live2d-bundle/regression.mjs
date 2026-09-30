// 17-motion full-trigger regression for the swapped Live2D engine
// (live2d-engine-swap 6.2). Serves client/assets/live2d statically under the
// same /live2d-assets URL contract the backend uses, opens live2d.html in
// Chrome (project pixel-verification precedent), then drives every motion
// name of presentation-map.json's motions table through the page's
// playMotion entry point — capturing console/page errors and a screenshot
// per motion. A motion run fails the script when: the model never becomes
// ready, any console/page error fires, or the screenshot collapses to the
// transparent-canvas floor (model not drawn).
//
// Usage: node scripts/live2d-bundle/regression.mjs [--out artifacts/live2d-regression]
import { createServer as createHTTPServer } from 'node:http';
import { readFile, mkdir } from 'node:fs/promises';
import { join, dirname, resolve, extname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright-core';

const scriptsDir = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(scriptsDir, '..', '..');
const assetsDir = join(repoRoot, 'client', 'assets', 'live2d');
const outDir = resolve(repoRoot, process.argv.includes('--out')
  ? process.argv[process.argv.indexOf('--out') + 1]
  : 'artifacts/live2d-regression');

const mime = {
  '.html': 'text/html', '.js': 'application/javascript', '.mjs': 'application/javascript',
  '.json': 'application/json', '.png': 'image/png', '.bin': 'application/octet-stream',
  '.wasm': 'application/wasm', '.moc3': 'application/octet-stream',
};

const server = createHTTPServer(async (req, resp) => {
  const path = decodeURIComponent(new URL(req.url, 'http://x').pathname);
  if (path === '/favicon.ico') { resp.writeHead(204); resp.end(); return; }
  const file = path === '/live2d.html'
    ? join(assetsDir, 'live2d.html')
    : path.startsWith('/live2d-assets/')
      ? join(assetsDir, path.slice('/live2d-assets/'.length))
      : null;
  if (!file || !file.startsWith(assetsDir)) {
    console.warn(`[regression] unhandled path: ${path}`);
    resp.writeHead(404); resp.end(); return;
  }
  try {
    const body = await readFile(file);
    resp.writeHead(200, { 'content-type': mime[extname(file)] ?? 'application/octet-stream' });
    resp.end(body);
  } catch {
    console.warn(`[regression] missing asset: ${path}`);
    resp.writeHead(404); resp.end();
  }
});
await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
const baseUrl = `http://127.0.0.1:${server.address().port}`;

// The canonical 17 motions of presentation-map.json's motions table.
const motions = [
  'hello', 'idle_01', 'idle_02', 'idle_03', 'listen_01', 'listen_02',
  'speak_01', 'speak_02', 'think', 'celebrate', 'celebrate_02', 'miss',
  'complain', 'analysis', 'tense', 'agree', 'wave',
];

await mkdir(outDir, { recursive: true });
const errors = [];
const browser = await chromium.launch({
  executablePath: 'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe',
  args: ['--use-gl=angle', '--enable-webgl', '--ignore-gpu-blocklist'],
});
try {
  const page = await browser.newPage({ viewport: { width: 480, height: 720 } });
  page.on('console', (msg) => { if (msg.type() === 'error') errors.push(`console: ${msg.text()}`); });
  page.on('pageerror', (err) => errors.push(`pageerror: ${err.message}`));
  await page.goto(`${baseUrl}/live2d.html`);
  await page.waitForFunction('window.modelReady === true', null, { timeout: 30000 });
  const api = await page.evaluate(() => ({
    hasPixi: typeof window.PIXI === 'object',
    hasModel: typeof window.Live2DModel === 'function',
    hasMotionLastFrame: !!window.model && typeof window.model.motionLastFrame === 'function',
    lipSyncPinnedOff: window.model && window.model.internalModel
      ? window.model.internalModel.lipSync === false
      : null,
  }));
  if (!api.hasPixi || !api.hasModel) throw new Error(`bundle globals missing: ${JSON.stringify(api)}`);
  if (!api.hasMotionLastFrame) throw new Error('advanced fork API missing: model.motionLastFrame');
  if (api.lipSyncPinnedOff !== true) throw new Error('advanced built-in lipsync not pinned off (internalModel.lipSync)');

  let index = 0;
  for (const motion of motions) {
    await page.evaluate((name) => window.playMotion(name), motion);
    await page.waitForTimeout(index === 0 ? 1600 : 900);
    const shot = join(outDir, `${String(index).padStart(2, '0')}-${motion}.png`);
    const buffer = await page.screenshot({ path: shot });
    if (buffer.length < 24_000) {
      throw new Error(`motion ${motion}: screenshot collapsed to ${buffer.length} bytes — model likely not drawn`);
    }
    index += 1;
  }

  // 6.3 smoke on the web surface: the holdLastFrame apply must resolve
  // without errors (the server floors the hold window; the page freezes the
  // final frame via motionLastFrame after one pass).
  await page.evaluate(() => {
    window.parent === window; // page is top-level here
    window.dispatchEvent(new MessageEvent('message', {
      source: window, data: { type: 'qiuqiu-live2d-state', expression: 'excited', motion: 'celebrate', speaking: false, holdLastFrame: true, holdMs: 7400 },
    }));
  });
  await page.waitForTimeout(4200);
  const held = await page.evaluate(() => document.body.dataset.motion || '');
  await page.screenshot({ path: join(outDir, 'hold-last-frame-celebrate.png') });
  if (!errors.length) {
    console.log(`17-motion regression OK: ${motions.length} motions, holdLastFrame apply on ${held || 'celebrate'} resolved clean`);
    console.log(`screenshots: ${outDir}`);
  } else {
    throw new Error(`surface errors:\n  - ${errors.join('\n  - ')}`);
  }
} finally {
  await browser.close();
  server.close();
}
if (errors.length) process.exit(1);
