// promptfoo 门禁层驱动（eval-tooling 9.2）：起 eval backend
// （startEvalBackend 同形环境，与 pr tier 同源）→ 等健康 → 跑
// promptfoo eval -c tests/promptfoo/promptfooconfig.gate.yaml → 以
// promptfoo 退出码为门禁结论（0=绿，非 0=阻断）。
//
// 用法：node scripts/promptfoo/run-gate.mjs
//   （本地与 CI 同一入口；QIUQIU_EVAL_KEEP=1 保留后端进程供调试）
//
// 端口占用自检：默认 18090 若已有服务在场则直接复用（demo 场景自测用），
// CI 环境隔离无此歧义。

import { pathToFileURL } from 'node:url';
import { join } from 'node:path';
import { spawnSync } from 'node:child_process';

const { startEvalBackend, repoRoot } = await import(
  pathToFileURL(join(process.cwd(), 'scripts', 'evals', 'backend.mjs')).href
);

let backend;
try {
  backend = await startEvalBackend();
  process.env.QIUQIU_BASE_URL = backend.baseUrl;
  process.env.APP_TOKEN = process.env.APP_TOKEN || backend.token;
  console.log(`eval backend ready at ${backend.baseUrl}`);

  const npxBin = process.platform === 'win32' ? 'npx.cmd' : 'npx';
  const result = spawnSync(
    npxBin,
    ['promptfoo', 'eval', '-c', 'tests/promptfoo/promptfooconfig.gate.yaml', '--no-progress-bar'],
    {
      cwd: repoRoot,
      stdio: 'inherit',
      env: process.env,
      shell: process.platform === 'win32',
    },
  );
  if (result.error) {
    throw result.error;
  }
  process.exitCode = result.status ?? 1;
  if (process.exitCode !== 0) {
    console.error('promptfoo gate FAILED — see assertions above.');
  } else {
    console.log('promptfoo gate PASSED.');
  }
} finally {
  if (backend && process.env.QIUQIU_EVAL_KEEP !== '1') {
    await backend.stop();
  }
}
