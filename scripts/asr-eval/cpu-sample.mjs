// CPU 占用采样包装器(asr-selfhost-eval 判据④)。
// 用法:node scripts/asr-eval/cpu-sample.mjs --out results/cpu-funasr.json -- <child cmd...>
// 每 1s 采样:系统负载(Win32_Processor.LoadPercentage)+ 子进程 CPU 时间增量
// (Get-Process TotalProcessorTime)。中文 Windows 计数器名问题用 CIM 规避。

import { spawn, spawnSync } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';

const argv = process.argv.slice(2);
const outIdx = argv.indexOf('--out');
const outPath = outIdx >= 0 ? argv[outIdx + 1] : null;
const cmd = argv.slice(outIdx >= 0 ? outIdx + 2 : 0).filter((a, i) => !(a === '--' && i === 0));
const logicalCores = Number(process.env.NUMBER_OF_PROCESSORS || 1);

function ps(command) {
  const r = spawnSync('powershell', ['-NoProfile', '-Command', command], { encoding: 'utf8', timeout: 15000 });
  if (r.status !== 0) return null;
  const v = parseFloat((r.stdout || '').trim());
  return Number.isFinite(v) ? v : null;
}

const child = spawn(cmd[0], cmd.slice(1), { stdio: 'inherit' });
console.log(`[cpu-sample] child pid=${child.pid} cores=${logicalCores} cmd=${cmd.join(' ')}`);

const systemPct = [];
const procCores = [];
let lastCpuMs = null;
let lastAt = null;
let dead = false;

const timer = setInterval(() => {
  if (dead) return;
  const sys = ps('(Get-CimInstance Win32_Processor | Measure-Object -Property LoadPercentage -Average).Average');
  const cpuMs = ps(`(Get-Process -Id ${child.pid} -ErrorAction SilentlyContinue).TotalProcessorTime.TotalMilliseconds`);
  if (Number.isFinite(sys)) systemPct.push(sys);
  if (Number.isFinite(cpuMs)) {
    if (lastCpuMs !== null && lastAt !== null) {
      const elapsed = Date.now() - lastAt;
      if (elapsed > 0) procCores.push((cpuMs - lastCpuMs) / elapsed); // 核数(0..N)
    }
    lastCpuMs = cpuMs;
    lastAt = Date.now();
  }
}, 1000);

child.on('exit', (code) => {
  dead = true;
  clearInterval(timer);
  const avg = (xs) => (xs.length ? xs.reduce((a, b) => a + b, 0) / xs.length : null);
  const mx = (xs) => (xs.length ? Math.max(...xs) : null);
  const summary = {
    logicalCores,
    samples: systemPct.length,
    sysAvgPct: avg(systemPct),
    sysMaxPct: mx(systemPct),
    procCoresAvg: avg(procCores),
    procCoresMax: mx(procCores),
    procShareOfMachineAvgPct: avg(procCores) !== null ? (avg(procCores) / logicalCores) * 100 : null,
    childExitCode: code,
  };
  const result = { cmd: cmd.join(' '), logicalCores, systemPct, procCores, summary };
  if (outPath) {
    fs.mkdirSync(path.dirname(outPath), { recursive: true });
    fs.writeFileSync(outPath, JSON.stringify(result, null, 2));
  }
  console.log(`[cpu-sample] ${JSON.stringify(summary)}`);
  process.exit(code ?? 0);
});
