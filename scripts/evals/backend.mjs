import { createServer } from 'node:net';
import { mkdir } from 'node:fs/promises';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { spawn } from 'node:child_process';

const scriptsDir = dirname(fileURLToPath(import.meta.url));
export const repoRoot = resolve(scriptsDir, '..', '..');
export const backendDir = join(repoRoot, 'backend');

export async function startEvalBackend() {
  const port = await reservePort();
  const binary = join(repoRoot, 'artifacts', 'evals', process.platform === 'win32' ? 'qiuqiu-eval-server.exe' : 'qiuqiu-eval-server');
  await mkdir(dirname(binary), { recursive: true });
  await runCommand('go', ['build', '-o', binary, './cmd/server'], { cwd: backendDir });

  const child = spawn(binary, [], {
    cwd: backendDir,
    env: {
      ...process.env,
      PORT: String(port),
      APP_TOKEN: 'qiuqiu-dev-token',
      DATABASE_URL: '',
      DEEPSEEK_API_KEY: '',
      MIMO_API_KEY: '',
      ELEVENLABS_API_KEY: '',
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
    child.kill();
    throw new Error(`${error.message}\nbackend logs:\n${logs.slice(-2000)}`);
  }

  return {
    baseUrl,
    token: 'qiuqiu-dev-token',
    async stop() {
      if (child.exitCode !== null) return;
      child.kill();
      await new Promise((resolveStop) => {
        const timer = setTimeout(resolveStop, 5_000);
        child.once('exit', () => {
          clearTimeout(timer);
          resolveStop();
        });
      });
    },
  };
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
  return new Promise((resolvePort, rejectPort) => {
    const server = createServer();
    server.unref();
    server.once('error', rejectPort);
    server.listen(0, '127.0.0.1', () => {
      const address = server.address();
      server.close((error) => {
        if (error) rejectPort(error);
        else resolvePort(address.port);
      });
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
