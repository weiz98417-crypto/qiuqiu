// MiMo 云侧对照 runner(asr-selfhost-eval 9.1)。
// 与 FunASR runner 同批音频(16k wav manifest),两件事:
// 1) final:整段转写(生产 transcribeFinal 同形,带全表 hints)。
// 2) partial 模拟:严格复刻 backend/internal/asr/session.go 的假流式窗口——
//    1.5s 窗(48000B)/0.25s 重叠(4000B),窗齐才发,Finish 不补发尾部;
//    每 RTT 一次云调用,mergeTranscript 合并。首响口径 = audioLeadMs(1500)+ RTT。
//
// 用法:MIMO_API_KEY=... node scripts/asr-eval/run-mimo.mjs --audio-dir DIR --cases cases.json --out results/mimo.json

import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { mergeTranscript } from './lib/textnorm.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const args = process.argv.slice(2);
const argOf = (name, def) => {
  const i = args.indexOf(name);
  return i >= 0 && args[i + 1] ? args[i + 1] : def;
};
const audioDir = argOf('--audio-dir');
const casesPath = argOf('--cases', path.join(here, 'cases.json'));
const outPath = argOf('--out', path.join(here, 'results', 'mimo.json'));

const PARTIAL_BYTES = 48000; // session.go defaultPartialBytes = 16000*2*1.5
const OVERLAP_BYTES = 4000; // session.go defaultOverlapBytes = 16000*2/4

function loadMimoKey() {
  if (process.env.MIMO_API_KEY) return process.env.MIMO_API_KEY;
  const envPath = path.join(here, '..', '..', 'backend', '.env');
  if (fs.existsSync(envPath)) {
    for (const line of fs.readFileSync(envPath, 'utf8').split(/\r?\n/)) {
      const m = line.match(/^\s*MIMO_API_KEY\s*=\s*(.+?)\s*$/);
      if (m) return m[1];
    }
  }
  return '';
}

async function transcribe(key, wavBuffer, hints, tag) {
  const t0 = Date.now();
  const payload = {
    model: 'mimo-v2.5-asr',
    messages: [
      {
        role: 'user',
        content: [
          { type: 'input_audio', input_audio: { data: 'data:audio/wav;base64,' + wavBuffer.toString('base64') } },
        ],
      },
    ],
    asr_options: { language: 'zh' },
  };
  if (hints?.length) payload.hints = hints;
  const res = await fetch('https://api.xiaomimimo.com/v1/chat/completions', {
    method: 'POST',
    headers: { 'api-key': key, 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  });
  const body = await res.text();
  const ms = Date.now() - t0;
  if (!res.ok) throw new Error(`[${tag}] asr http ${res.status}: ${body.slice(0, 200)}`);
  const json = JSON.parse(body);
  const text = (json?.choices?.[0]?.message?.content || '').trim();
  return { text, ms };
}

function pcmWindows(pcmLen) {
  // 复刻 session.go:cursor 步进 PartialBytes,窗起点回退 OverlapBytes;
  // 剩余不足一个窗不再发(与 startPartialLocked 判据一致)。
  const windows = [];
  let cursor = 0;
  while (pcmLen - cursor >= PARTIAL_BYTES) {
    const end = cursor + PARTIAL_BYTES;
    const start = Math.max(0, cursor - OVERLAP_BYTES);
    windows.push({ start, end });
    cursor = end;
  }
  return windows;
}

const key = loadMimoKey();
if (!key) {
  console.error('[mimo] MIMO_API_KEY 不可得。云端侧按卡片要求将记为「云端侧未实测」。');
  process.exit(2);
}

const manifest = JSON.parse(fs.readFileSync(casesPath, 'utf8'));
const hints = manifest.hints || [];
const records = [];
let failures = 0;

for (const c of manifest.cases) {
  const wavPath = path.join(audioDir, `${c.id}.wav`);
  if (!fs.existsSync(wavPath)) {
    console.error(`[mimo] missing ${wavPath}, skip`);
    continue;
  }
  const wav = fs.readFileSync(wavPath);
  const pcm = wav.subarray(44); // 本批音频是本库 encodeWav 输出,44B 标准头
  const rec = { id: c.id, category: c.category };
  try {
    // final(整段,生产同形带 hints)
    rec.final = await transcribe(key, wav, hints, c.id);

    // partial 模拟
    const windows = pcmWindows(pcm.length);
    let partialText = '';
    const partials = [];
    for (let i = 0; i < windows.length; i++) {
      const { start, end } = windows[i];
      const windowWav = fs.readFileSync(wavPath);
      // 重建窗口 wav:44B 头 + 截取 PCM
      const head = windowWav.subarray(0, 44);
      const seg = pcm.subarray(start, end);
      const windowBuf = Buffer.concat([head, seg]);
      // data 长度字段修正
      windowBuf.writeUInt32LE(seg.length, 40);
      windowBuf.writeUInt32LE(36 + seg.length, 4);
      const r = await transcribe(key, windowBuf, hints, `${c.id}#w${i}`);
      const before = partialText;
      partialText = mergeTranscript(partialText, r.text);
      partials.push({
        windowIndex: i,
        audioLeadMs: Math.round((end - start) / 32),
        rttMs: r.ms,
        text: r.text,
        merged: partialText,
        changed: partialText !== before,
      });
    }
    rec.partials = partials;
    rec.firstPartial =
      partials.length > 0
        ? { audioLeadMs: 1500, rttMs: partials[0].rttMs, latencyMs: 1500 + partials[0].rttMs, text: partials[0].merged }
        : null; // 音频不足 1.5s 时生产根本不会出 partial(如实记录)
    rec.windowCount = windows.length;
  } catch (err) {
    failures++;
    rec.error = String(err.message).slice(0, 300);
    console.error(`[mimo] ${c.id} error: ${rec.error}`);
  }
  records.push(rec);
  const ft = rec.firstPartial ? `firstRTT=${rec.firstPartial.rttMs}ms` : 'no-window';
  console.log(`[mimo] ${c.id} final=${rec.final?.ms ?? 'ERR'}ms ${ft} windows=${rec.windowCount ?? 0}`);
}

fs.mkdirSync(path.dirname(outPath), { recursive: true });
fs.writeFileSync(
  outPath,
  JSON.stringify(
    {
      engine: 'mimo-cloud',
      model: 'mimo-v2.5-asr',
      partialScheme: 'fake-streaming: 1.5s window / 0.25s overlap (session.go defaultPartialBytes/OverlapBytes)',
      records,
      failures,
    },
    null,
    2,
  ),
);
console.log(`[mimo] wrote ${outPath} (${records.filter((r) => !r.error).length} ok, ${failures} failed)`);
if (failures >= manifest.cases.length) process.exit(3);
