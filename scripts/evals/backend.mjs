import { createServer as createHTTPServer } from 'node:http';
import { createServer as createNetServer } from 'node:net';
import { mkdir } from 'node:fs/promises';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { spawn } from 'node:child_process';

const scriptsDir = dirname(fileURLToPath(import.meta.url));
export const repoRoot = resolve(scriptsDir, '..', '..');
export const backendDir = join(repoRoot, 'backend');

export async function startEvalBackend({ environment = {} } = {}) {
  const allowedOverrides = ['APP_ENV', 'AUTH_MODE', 'SESSION_SIGNING_KEY'];
  const environmentOverrides = Object.fromEntries(
    allowedOverrides
      .filter((name) => Object.hasOwn(environment, name))
      .map((name) => [name, environment[name]]),
  );
  const port = await reservePort();
  const binary = join(repoRoot, 'artifacts', 'evals', process.platform === 'win32' ? 'qiuqiu-eval-server.exe' : 'qiuqiu-eval-server');
  await mkdir(dirname(binary), { recursive: true });
  await runCommand('go', ['build', '-o', binary, './cmd/server'], { cwd: backendDir });
  const sportsAPI = await startEvalSportsAPI();

  const child = spawn(binary, [], {
    cwd: backendDir,
    env: {
      ...process.env,
      APP_ENV: 'development',
      AUTH_MODE: 'dual',
      SESSION_SIGNING_KEY: 'eval-session-signing-key-0123456789',
      PORT: String(port),
      APP_TOKEN: 'qiuqiu-dev-token',
      DATABASE_URL: '',
      DEEPSEEK_API_KEY: '',
      MIMO_API_KEY: '',
      ELEVENLABS_API_KEY: '',
      APISPORTS_API_KEY: 'eval-api-sports-key',
      APISPORTS_BASE_URL: sportsAPI.baseUrl,
      ...environmentOverrides,
    },
    stdio: ['ignore', 'pipe', 'pipe'],
  });
  let logs = '';
  child.stdout.on('data', (chunk) => { logs += chunk; });
  child.stderr.on('data', (chunk) => { logs += chunk; });
  const baseUrl = `http://127.0.0.1:${port}`;

  try {
    await waitForHealth(baseUrl, 20_000);
  } catch (error) {
    await stopChild(child);
    await sportsAPI.stop();
    throw new Error(`${error.message}\nbackend logs:\n${logs.slice(-2000)}`);
  }

  return {
    baseUrl,
    token: 'qiuqiu-dev-token',
    async stop() {
      await stopChild(child);
      await sportsAPI.stop();
    },
  };
}

async function startEvalSportsAPI() {
  const server = createHTTPServer((request, response) => {
    const url = new URL(request.url || '/', 'http://127.0.0.1');
    if (request.headers['x-apisports-key'] !== 'eval-api-sports-key') {
      response.writeHead(401, { 'Content-Type': 'application/json' });
      response.end(JSON.stringify({ errors: { token: 'invalid eval API key' }, results: 0, response: [] }));
      return;
    }
    if (url.pathname !== '/fixtures') {
      response.writeHead(404, { 'Content-Type': 'application/json' });
      response.end(JSON.stringify({ errors: { path: 'not found' }, results: 0, response: [] }));
      return;
    }

    if (url.searchParams.has('live')) {
      sendJSON(response, { get: 'fixtures', errors: [], results: 0, response: [] });
      return;
    }

    const date = url.searchParams.get('date') || new Date().toISOString().slice(0, 10);
    const payload = {
      get: 'fixtures',
      parameters: { date },
      errors: [],
      results: 1,
      response: [{
        fixture: {
          id: 424242,
          date: `${date}T19:30:00+08:00`,
          status: { long: 'Not Started', short: 'NS', elapsed: 0 },
        },
        league: { id: 10, name: '国际友谊赛' },
        teams: {
          home: { id: 1, name: '西班牙', logo: '' },
          away: { id: 2, name: '德国', logo: '' },
        },
        goals: { home: null, away: null },
      }],
    };
    const timer = setTimeout(() => sendJSON(response, payload), 250);
    response.once('close', () => clearTimeout(timer));
  });

  const port = await listenOnRandomPort(server);
  server.unref();
  return {
    baseUrl: `http://127.0.0.1:${port}`,
    async stop() {
      if (!server.listening) return;
      await new Promise((resolveStop, rejectStop) => {
        server.close((error) => {
          if (error) rejectStop(error);
          else resolveStop();
        });
      });
    },
  };
}

function sendJSON(response, payload) {
  if (response.destroyed || response.writableEnded) return;
  response.writeHead(200, { 'Content-Type': 'application/json; charset=utf-8' });
  response.end(JSON.stringify(payload));
}

async function stopChild(child) {
  if (child.exitCode !== null) return;
  child.kill();
  await new Promise((resolveStop) => {
    const timer = setTimeout(resolveStop, 5_000);
    child.once('exit', () => {
      clearTimeout(timer);
      resolveStop();
    });
  });
}

export async function runCommand(command, args, options = {}) {
  await new Promise((resolveRun, rejectRun) => {
    const child = spawn(command, args, { ...options, stdio: 'inherit' });
    child.once('error', rejectRun);
    child.once('exit', (code) => {
      if (code === 0) resolveRun();
      else rejectRun(new Error(`${command} ${args.join(' ')} exited with ${code}`));
    });
  });
}

async function reservePort() {
  const server = createNetServer();
  const port = await listenOnRandomPort(server);
  await new Promise((resolveClose, rejectClose) => {
    server.close((error) => {
      if (error) rejectClose(error);
      else resolveClose();
    });
  });
  return port;
}

async function listenOnRandomPort(server) {
  return new Promise((resolvePort, rejectPort) => {
    server.once('error', rejectPort);
    server.listen(0, '127.0.0.1', () => {
      const address = server.address();
      resolvePort(address.port);
    });
  });
}

async function waitForHealth(baseUrl, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  let lastError = 'not started';
  while (Date.now() < deadline) {
    try {
      const response = await fetch(`${baseUrl}/health`);
      if (response.ok && (await response.text()).trim() === 'ok') return;
      lastError = `health returned ${response.status}`;
    } catch (error) {
      lastError = error.message;
    }
    await new Promise((resolveWait) => setTimeout(resolveWait, 200));
  }
  throw new Error(`backend health check timed out: ${lastError}`);
}
