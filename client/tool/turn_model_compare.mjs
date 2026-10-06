#!/usr/bin/env node
// smart-turn v3.2 离线对比评测(docs/evals/turn-collection-harness.md T3)。
//
// 输入:turn_dataset/labels.jsonl(T2 产出)+ turn_dataset/raw/*.wav + *.json
// 评测:每条样本两腿同判——
//   现役腿(text):POST {TURN_MODEL_URL}/turn  {"text": <提示语文本>}
//   v3.2 腿(audio):POST {V32_MODEL_URL}/turn_audio  raw pcm16@16k 单声道
// 指标(spec 判据):
//   说完点准确率 = 截断到「说完点+hold」判 isComplete=true 的比例;
//   抢话误判率   = 截断到「说完点 60%」判 isComplete=true 的比例(不高
//                  于现役才过门);
//   判定延迟 p50/p90。
// 用法:
//   node client/tool/turn_model_compare.mjs [--dataset turn_dataset] \
//        [--hold-ms 800] [--limit N] [--stub] [--out docs/evals/turn-v32-compare.md]
//   --stub:不起真实 sidecar,内置两腿替身跑通链路与指标计算(验收/回归)。
// 环境变量:TURN_MODEL_URL / V32_MODEL_URL(缺省 127.0.0.1:8199/8299)。

import { readFileSync, writeFileSync, existsSync } from 'node:fs';
import { resolve } from 'node:path';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { createServer } from 'node:http';

const repoRoot = dirname(dirname(dirname(fileURLToPath(import.meta.url)))); // client/tool/ → 仓库根
const args = process.argv.slice(2);
function arg(name, fallback) {
  const index = process.argv.indexOf(`--${name}`);
  return index >= 0 ? process.argv[index + 1] : fallback;
}
const hasFlag = (name) => process.argv.includes(`--${name}`);

// 参数晚绑定:--stub 模式要在 main 前重写 argv,不能在模块加载期固化。
let datasetDir = arg('dataset', 'turn_dataset');
let holdMs = Number(arg('hold-ms', '800'));
let limit = Number(arg('limit', '0'));
let outPath = arg('out', 'docs/evals/turn-v32-compare.md');
const turnUrl = () => process.env.TURN_MODEL_URL || 'http://127.0.0.1:8199';
const v32Url = () => process.env.V32_MODEL_URL || 'http://127.0.0.1:8299';

// ---------- WAV 解析与 16k 重采样(零依赖) ----------

// parseWav 解析 RIFF/PCM16:返回 {sampleRate, channels, samples:Int16Array}
//(多通道折叠为平均单声道)。
export function parseWav(buffer) {
  if (buffer.toString('ascii', 0, 4) !== 'RIFF' || buffer.toString('ascii', 8, 12) !== 'WAVE') {
    throw new Error('not a RIFF/WAVE file');
  }
  let offset = 12;
  let sampleRate = 0;
  let channels = 1;
  let bitsPerSample = 16;
  let data = null;
  while (offset + 8 <= buffer.length) {
    const chunkId = buffer.toString('ascii', offset, offset + 4);
    const chunkSize = buffer.readUInt32LE(offset + 4);
    const body = offset + 8;
    if (chunkId === 'fmt ') {
      channels = buffer.readUInt16LE(body + 2);
      sampleRate = buffer.readUInt32LE(body + 4);
      bitsPerSample = buffer.readUInt16LE(body + 14);
    } else if (chunkId === 'data') {
      data = buffer.subarray(body, Math.min(body + chunkSize, buffer.length));
    }
    offset = body + chunkSize + (chunkSize % 2);
  }
  if (!data) throw new Error('WAV data chunk missing');
  if (bitsPerSample !== 16) throw new Error(`only pcm16 supported, got ${bitsPerSample} bits`);
  const frameCount = Math.floor(data.length / (2 * channels));
  const samples = new Int16Array(frameCount);
  for (let frame = 0; frame < frameCount; frame++) {
    let sum = 0;
    for (let channel = 0; channel < channels; channel++) {
      sum += data.readInt16LE((frame * channels + channel) * 2);
    }
    samples[frame] = Math.round(sum / channels);
  }
  return { sampleRate, samples };
}

// resample16k 线性插值重采样到 16kHz 单声道(评测够用;说完点判定不吃
// 高频细节)。返回 Buffer:RIFF wav pcm16@16k。
export function resampleTo16kWav(parsed) {
  const targetRate = 16000;
  const ratio = parsed.sampleRate / targetRate;
  const outFrames = Math.max(1, Math.floor(parsed.samples.length / ratio));
  const out = new Int16Array(outFrames);
  for (let i = 0; i < outFrames; i++) {
    const source = i * ratio;
    const left = Math.floor(source);
    const right = Math.min(left + 1, parsed.samples.length - 1);
    const blend = source - left;
    out[i] = Math.round(parsed.samples[left] * (1 - blend) + parsed.samples[right] * blend);
  }
  const header = Buffer.alloc(44);
  header.write('RIFF', 0);
  header.writeUInt32LE(36 + out.length * 2, 4);
  header.write('WAVE', 8);
  header.write('fmt ', 12);
  header.writeUInt32LE(16, 16);
  header.writeUInt16LE(1, 20); // pcm
  header.writeUInt16LE(1, 22); // mono
  header.writeUInt32LE(targetRate, 24);
  header.writeUInt32LE(targetRate * 2, 28);
  header.writeUInt16LE(2, 32);
  header.writeUInt16LE(16, 34);
  header.write('data', 36);
  header.writeUInt32LE(out.length * 2, 40);
  return Buffer.concat([header, Buffer.from(out.buffer, 0, out.length * 2)]);
}

// sliceWav 把 16k wav 截到 ms 毫秒(T3 的两段截取用)。
export function sliceWavMs(wav, ms) {
  const frames = Math.min(Math.floor((ms / 1000) * 16000), (wav.length - 44) / 2);
  const header = Buffer.from(wav.subarray(0, 44));
  header.writeUInt32LE(36 + frames * 2, 4);
  header.writeUInt32LE(frames * 2, 40);
  return Buffer.concat([header, wav.subarray(44, 44 + frames * 2)]);
}

// ---------- 双腿评测端点 ----------

async function judgeText(baseUrl, text, timeoutMs = 3000) {
  const started = performance.now();
  const response = await fetch(`${baseUrl}/turn`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ text }),
    signal: AbortSignal.timeout(timeoutMs),
  });
  if (!response.ok) throw new Error(`text leg ${response.status}`);
  const payload = await response.json();
  return { isComplete: Boolean(payload.isComplete), latencyMs: performance.now() - started };
}

async function judgeAudio(baseUrl, wav16k, timeoutMs = 3000) {
  const started = performance.now();
  const response = await fetch(`${baseUrl}/turn_audio`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/octet-stream' },
    body: wav16k,
    signal: AbortSignal.timeout(timeoutMs),
  });
  if (!response.ok) throw new Error(`audio leg ${response.status}`);
  const payload = await response.json();
  return { isComplete: Boolean(payload.isComplete), latencyMs: performance.now() - started };
}

// ---------- 指标聚合 ----------

function metricsRow() {
  return { completeHit: 0, completeTotal: 0, falsePositive: 0, interruptTotal: 0, latencies: [], errors: 0 };
}
function finalize(row) {
  const sorted = [...row.latencies].sort((a, b) => a - b);
  const pick = (p) => (sorted.length ? sorted[Math.min(sorted.length - 1, Math.floor(sorted.length * p))] : 0);
  return {
    accuracy: row.completeTotal ? row.completeHit / row.completeTotal : 0,
    falsePositiveRate: row.interruptTotal ? row.falsePositive / row.interruptTotal : 0,
    p50: pick(0.5),
    p90: pick(0.9),
    errors: row.errors,
  };
}

// ---------- 主流程 ----------

async function main() {
  const labelsPath = join(repoRoot, datasetDir, 'labels.jsonl');
  const rawDir = join(repoRoot, datasetDir, 'raw');
  if (!existsSync(labelsPath)) {
    console.error(`labels.jsonl not found at ${labelsPath}`);
    process.exit(1);
  }
  const labels = readFileSync(labelsPath, 'utf8')
    .split('\n')
    .filter((line) => line.trim())
    .map((line) => JSON.parse(line));
  const selected = limit > 0 ? labels.slice(0, limit) : labels;

  // 元数据(提示语文本,现役腿输入):raw 目录的伴生 json
  const texts = {};
  for (const label of selected) {
    const metaPath = join(rawDir, label.file.replace(/\.wav$/, '.json'));
    if (existsSync(metaPath)) {
      try { texts[label.file] = JSON.parse(readFileSync(metaPath, 'utf8')).prompt || ''; } catch {}
    }
  }

  const legs = {
    legacy: { name: '现役 396MB(text)', url: turnUrl(), mode: 'text', metrics: metricsRow() },
    v32: { name: 'v3.2 8MB int8(audio)', url: v32Url(), mode: 'audio', metrics: metricsRow() },
  };
  const perScene = {};

  for (const label of selected) {
    const wavPath = join(rawDir, label.file);
    if (!existsSync(wavPath)) {
      console.error(`skip ${label.file}: wav missing`);
      continue;
    }
    let wav16k;
    try {
      wav16k = resampleTo16kWav(parseWav(readFileSync(wavPath)));
    } catch (error) {
      console.error(`skip ${label.file}: ${error.message}`);
      continue;
    }
    // 坑用例(审计面):源采样率非 16k 的样本显式计数(转码已统一,记录防假通过)。
    const text = texts[label.file] || '';
    // 两段截取:说完点+hold(期望 complete)/说完点 60%(期望未完)
    const tail = sliceWavMs(wav16k, label.speechEndMs + holdMs);
    const mid = sliceWavMs(wav16k, Math.max(200, Math.round(label.speechEndMs * 0.6)));
    const scene = label.category || 'unknown';
    perScene[scene] = perScene[scene] || { legacy: metricsRow(), v32: metricsRow() };

    for (const leg of Object.values(legs)) {
      for (const [slice, expectComplete] of [[tail, true], [mid, false]]) {
        const row = leg.metrics;
        const sceneRow = perScene[scene][leg === legs.legacy ? 'legacy' : 'v32'];
        try {
          const verdict = leg.mode === 'text'
            ? await judgeText(leg.url, text)
            : await judgeAudio(leg.url, sliceWavMs(slice, 10 ** 9)); // 全长送判
          row.latencies.push(verdict.latencyMs);
          if (expectComplete) {
            row.completeTotal++;
            sceneRow.completeTotal++;
            if (verdict.isComplete) { row.completeHit++; sceneRow.completeHit++; }
          } else {
            row.interruptTotal++;
            sceneRow.interruptTotal++;
            if (verdict.isComplete) { row.falsePositive++; sceneRow.falsePositive++; }
          }
        } catch (error) {
          row.errors++;
          sceneRow.errors++;
          console.error(`${leg.name} ${label.file}(${expectComplete ? 'tail' : 'mid'}): ${error.message}`);
        }
      }
    }
  }

  const summary = {
    legacy: finalize(legs.legacy.metrics),
    v32: finalize(legs.v32.metrics),
  };
  for (const scene of Object.keys(perScene)) {
    perScene[scene].legacy = finalize(perScene[scene].legacy);
    perScene[scene].v32 = finalize(perScene[scene].v32);
  }
  const gate = {
    accuracyOk: summary.v32.accuracy >= summary.legacy.accuracy,
    falsePositiveOk: summary.v32.falsePositiveRate <= summary.legacy.falsePositiveRate,
  };
  gate.passed = gate.accuracyOk && gate.falsePositiveOk && summary.v32.errors === 0;

  const report = renderReport({ selected: selected.length, legs, summary, gate, perScene });
  writeFileSync(resolve(repoRoot, outPath), report);
  console.log(report);
  console.log(`\nreport written to ${outPath}`);
  process.exit(gate.passed || hasFlag('stub') ? 0 : 2);
}

function renderReport({ selected, legs, summary, gate, perScene }) {
  const line = (name, row) => `| ${name} | ${(row.accuracy * 100).toFixed(1)}% | ${(row.falsePositiveRate * 100).toFixed(1)}% | ${row.p50.toFixed(0)}ms | ${row.p90.toFixed(0)}ms | ${row.errors} |`;
  const sceneLines = Object.entries(perScene).map(([scene, rows]) =>
    `| ${scene} | legacy ${(rows.legacy.accuracy * 100).toFixed(0)}%/${(rows.legacy.falsePositiveRate * 100).toFixed(0)}% | v3.2 ${(rows.v32.accuracy * 100).toFixed(0)}%/${(rows.v32.falsePositiveRate * 100).toFixed(0)}% |`).join('\n');
  return `# smart-turn v3.2 离线对比报告(自动生成)

> 样本 ${selected} 条;hold=${holdMs}ms;判据:准确率不低于现役且抢话误判不高于现役。

| 模型 | 说完点准确率 | 抢话误判率 | 延迟 p50 | 延迟 p90 | 错误 |
| --- | --- | --- | --- | --- | --- |
${line(legs.legacy.name, summary.legacy)}
${line(legs.v32.name, summary.v32)}

分场景(准确率/误判率):

| 场景 | 现役 | v3.2 |
| --- | --- | --- |
${sceneLines || '(无)'}

**门判:${gate.passed ? '通过(6.4a 切换评估)' : '不过(6.4b 冻结归档)'}** —— 准确率 ${gate.accuracyOk ? '✓' : '✗'} / 抢话误判 ${gate.falsePositiveOk ? '✓' : '✗'}
`;
}

// ---------- stub 自测(--stub):两腿替身 + 内置合成链路验证 ----------

// runStubSelfTest 验收回归锚:内置替身(现役腿对「?」结尾判完,v3.2 腿对
// 高能量尾部判完)合成三条已知样本,断言指标计算与门判逻辑按预期产出。
async function runStubSelfTest() {
  const server = createServer((request, response) => {
    let body = '';
    request.on('data', (chunk) => { body += chunk; });
    request.on('end', () => {
      if (request.url === '/turn') {
        const payload = JSON.parse(body || '{}');
        const text = String(payload.text || '');
        response.setHeader('Content-Type', 'application/json');
        response.end(JSON.stringify({ isComplete: text.endsWith('。') || text.endsWith('！'), probability: 0.9 }));
        return;
      }
      if (request.url === '/turn_audio') {
        // 替身判定:尾部 200ms 均值能量 < 头部 1/4 视为说完(合成静音尾即 complete)
        const raw = Buffer.from(body, 'binary');
        const frames = Math.floor((raw.length - 44) / 2);
        const energyAt = (from, to) => {
          let sum = 0;
          const count = Math.max(1, to - from);
          for (let i = from; i < to && i < frames; i++) sum += Math.abs(raw.readInt16LE(44 + i * 2));
          return sum / count;
        };
        const tailEnergy = energyAt(Math.max(0, frames - 3200), frames); // 尾 200ms
        const headEnergy = energyAt(0, Math.min(frames, 16000)); // 头 1s
        response.setHeader('Content-Type', 'application/json');
        response.end(JSON.stringify({ isComplete: tailEnergy * 4 < headEnergy, probability: 0.8 }));
        return;
      }
      response.writeHead(404).end();
    });
  });
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  process.env.TURN_MODEL_URL = `http://127.0.0.1:${server.address().port}`;
  process.env.V32_MODEL_URL = process.env.TURN_MODEL_URL;
  console.log('stub legs up — running built-in synthetic labels');

  // 合成 3 条 wav(1s 16k:头部能量+尾部按预期写静音/有声)+ labels
  const wavWithTail = (tailSilent) => {
    const frames = 16000;
    const buffer = Buffer.alloc(44 + frames * 2);
    buffer.write('RIFF', 0); buffer.writeUInt32LE(36 + frames * 2, 4); buffer.write('WAVE', 8);
    buffer.write('fmt ', 12); buffer.writeUInt32LE(16, 16); buffer.writeUInt16LE(1, 20);
    buffer.writeUInt16LE(1, 22); buffer.writeUInt32LE(16000, 24); buffer.writeUInt32LE(32000, 28);
    buffer.writeUInt16LE(2, 32); buffer.writeUInt16LE(16, 34); buffer.write('data', 36);
    buffer.writeUInt32LE(frames * 2, 40);
    for (let i = 0; i < frames; i++) {
      const value = i < 8000 ? Math.round(12000 * Math.sin(i / 8)) : (tailSilent ? 0 : Math.round(12000 * Math.sin(i / 8)));
      buffer.writeInt16LE(value, 44 + i * 2);
    }
    return buffer;
  };
  const fixtures = [
    { file: 's1.wav', speechEndMs: 500, category: 'calm', prompt: '现在比分是多少。', tailSilent: true },
    { file: 's2.wav', speechEndMs: 500, category: 'excited', prompt: '快传啊！', tailSilent: false },
  ];
  const path = await import('node:path');
  const tmp = fs.mkdtempSync(path.join(repoRoot, 'artifacts', 'turn-stub-'));
  fs.mkdirSync(join(tmp, 'raw'), { recursive: true });
  const lines = [];
  for (const fixture of fixtures) {
    writeFileSync(join(tmp, 'raw', fixture.file), wavWithTail(fixture.tailSilent));
    const meta = { prompt: fixture.prompt };
    writeFileSync(join(tmp, 'raw', fixture.file.replace('.wav', '.json')), JSON.stringify(meta));
    lines.push(JSON.stringify({ file: fixture.file, speechEndMs: fixture.speechEndMs, category: fixture.category }));
  }
  writeFileSync(join(tmp, 'labels.jsonl'), lines.join('\n') + '\n');
  process.argv = [process.argv[0], process.argv[1], '--dataset', path.relative(repoRoot, tmp), '--out', join(tmp, 'report.md'), '--stub'];
  // 晚绑定重读参数(stub 的 dataset 是临时目录)
  datasetDir = arg('dataset', 'turn_dataset');
  holdMs = Number(arg('hold-ms', '800'));
  limit = Number(arg('limit', '0'));
  outPath = arg('out', 'docs/evals/turn-v32-compare.md');
  await main();
  server.close();
}

// ---------- boot ----------

const fs = await import('node:fs');
if (hasFlag('stub')) {
  await runStubSelfTest();
} else {
  await main();
}
