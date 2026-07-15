import { readFile } from 'node:fs/promises';
import { join } from 'node:path';
import { backendDir, repoRoot, runCommand, startEvalBackend } from './backend.mjs';

const args = new Set(process.argv.slice(2));
const tier = valueAfter('--tier') || 'pr';
const skipBrowser = args.has('--skip-browser');

await loadMiMoKeyFromProjectEnv();

await runCommand('go', ['test', './...'], { cwd: backendDir });
await runCommand('go', ['run', './cmd/evals', '-suite', 'all', '-out', '../artifacts/evals/offline.json'], { cwd: backendDir });
await runCommand('node', [join('scripts', 'voice-ui-smoke.mjs')], { cwd: repoRoot });

if (tier !== 'offline') {
  const backend = await startEvalBackend();
  try {
    const env = {
      ...process.env,
      QIUQIU_BASE_URL: backend.baseUrl,
      QIUQIU_RUNTIME_TTS: '0',
      APP_TOKEN: backend.token,
    };
    await runCommand('node', [join('scripts', 'evals', 'runtime-e2e.mjs')], { cwd: repoRoot, env });
    await runCommand('go', ['run', './cmd/eval-audit', '-base-url', backend.baseUrl, '-match-id', 'test', '-out', '../artifacts/evals/trace-audit.json'], { cwd: backendDir, env });
    if (!skipBrowser) {
      await runCommand(process.execPath, [join('node_modules', '@playwright', 'test', 'cli.js'), 'test', '--config=playwright.config.mjs'], { cwd: repoRoot, env });
    }
  } finally {
    await backend.stop();
  }
}

if (tier === 'release') {
  if (!process.env.MIMO_API_KEY) throw new Error('release evals require MIMO_API_KEY in the environment');
  await runCommand('node', [join('scripts', 'mimo-voice-smoke.mjs')], { cwd: repoRoot });
}

if (tier === 'nightly') {
  await runCommand('go', ['test', './internal/companion', '-run', '^$', '-bench', '^BenchmarkEvalCompanionRecentEventAnswer$', '-benchmem', '-count=3'], { cwd: backendDir });
  if (process.env.DATABASE_URL) {
    await runCommand('go', ['test', './internal/matchstate', './internal/companion', '-run', 'Postgres', '-count=1'], { cwd: backendDir });
  }
}

function valueAfter(name) {
  const index = process.argv.indexOf(name);
  return index >= 0 ? process.argv[index + 1] : '';
}

async function loadMiMoKeyFromProjectEnv() {
  if (process.env.MIMO_API_KEY) return;
  try {
    const envFile = await readFile(join(backendDir, '.env'), 'utf8');
    const match = envFile.match(/^MIMO_API_KEY\s*=\s*(.+)$/m);
    const value = match?.[1]?.trim().replace(/^['"]|['"]$/g, '');
    if (value) process.env.MIMO_API_KEY = value;
  } catch (error) {
    if (error.code !== 'ENOENT') throw error;
  }
}
