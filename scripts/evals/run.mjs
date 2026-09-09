import { readFile } from 'node:fs/promises';
import { join } from 'node:path';
import { backendDir, repoRoot, runCommand, startEvalBackend } from './backend.mjs';

const args = new Set(process.argv.slice(2));
const tier = valueAfter('--tier') || 'pr';
const skipBrowser = args.has('--skip-browser');

if (tier === 'release') await loadMiMoKeyFromProjectEnv();
const evalEnvironment = tier === 'release'
  ? { ...process.env }
  : {
      ...process.env,
      DEEPSEEK_API_KEY: '',
      MIMO_API_KEY: '',
      ELEVENLABS_API_KEY: '',
      APISPORTS_API_KEY: '',
    };

await runCommand('go', ['test', './...'], { cwd: backendDir, env: evalEnvironment });
await runCommand('go', ['run', './cmd/evals', '-suite', 'all', '-out', '../artifacts/evals/offline.json'], { cwd: backendDir, env: evalEnvironment });
await runCommand('node', [join('scripts', 'voice-ui-smoke.mjs')], { cwd: repoRoot, env: evalEnvironment });

if (tier !== 'offline') {
  await runCommand('node', [join('scripts', 'evals', 'session-isolation-e2e.mjs')], { cwd: repoRoot, env: evalEnvironment });
  const backend = await startEvalBackend({ environment: { QIUQIU_RUNTIME_TTS: '1' } });
  try {
    const env = {
      ...evalEnvironment,
      QIUQIU_BASE_URL: backend.baseUrl,
      QIUQIU_RUNTIME_TTS: '0',
      APP_TOKEN: backend.token,
    };
    await runCommand('node', [join('scripts', 'evals', 'runtime-e2e.mjs')], { cwd: repoRoot, env });
    await runCommand('go', ['run', './cmd/eval-audit', '-base-url', backend.baseUrl, '-match-id', 'test', '-out', '../artifacts/evals/trace-audit.json'], { cwd: backendDir, env });
    await runCommand('node', [join('scripts', 'evals', 'interaction-audit.mjs'), '--base-url', backend.baseUrl, '--match-id', 'test', '--user-id', 'runtime-fan', '--out', 'artifacts/evals/interaction-audit.json'], { cwd: repoRoot, env });
  } finally {
    await backend.stop();
  }

  if (!skipBrowser) {
    const browserBackend = await startEvalBackend();
    try {
      const browserEnv = {
        ...evalEnvironment,
        QIUQIU_BASE_URL: browserBackend.baseUrl,
        QIUQIU_RUNTIME_TTS: '0',
        APP_TOKEN: browserBackend.token,
      };
      await runCommand(process.execPath, [join('node_modules', '@playwright', 'test', 'cli.js'), 'test', '--config=playwright.config.mjs'], { cwd: repoRoot, env: browserEnv });
    } finally {
      await browserBackend.stop();
    }
  }
}

if (tier === 'release') {
  if (!process.env.MIMO_API_KEY) throw new Error('release evals require MIMO_API_KEY in the environment');
  await runCommand('node', [join('scripts', 'mimo-voice-smoke.mjs')], { cwd: repoRoot, env: evalEnvironment });
}

if (tier === 'nightly') {
  await runCommand('go', ['test', './internal/companion', '-run', '^$', '-bench', '^BenchmarkEvalCompanionRecentEventAnswer$', '-benchmem', '-count=3'], { cwd: backendDir, env: evalEnvironment });
  if (process.env.DATABASE_URL) {
    await runCommand('go', ['test', './internal/matchstate', './internal/companion', '-run', 'Postgres', '-count=1'], { cwd: backendDir, env: evalEnvironment });
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
