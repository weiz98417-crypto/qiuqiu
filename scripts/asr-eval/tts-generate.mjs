// 用例音频生成(asr-selfhost-eval 9.1)。
// 首选 MiMo TTS(mimo-v2.5-tts,同批喂两家引擎);MIMO_API_KEY 缺失或调用失败
// 逐例降级 Windows SAPI(System.Speech,Huihui zh-CN)。统一后处理:
// 单声道 → 16k 线性重采样 → 噪声例(语音增益 0.35 + 白噪声 SNR 8dB)。
//
// 用法:node scripts/asr-eval/tts-generate.mjs [--engine mimo|sapi] [--outdir DIR]
// 默认 outdir=%TEMP%\asr-eval\audio16k(不入 git);MiMo 原始返回缓存在同目录 raw/。

import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { decodeWav, durationMs, encodeWav, mixNoise, resampleLinear, scale } from './lib/wav.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const tmpRoot = path.join(process.env.TEMP_WINDOWS || 'C:\\Users\\win10\\AppData\\Local\\Temp', 'asr-eval');

const args = process.argv.slice(2);
const argOf = (name, def) => {
  const i = args.indexOf(name);
  return i >= 0 && args[i + 1] ? args[i + 1] : def;
};
const engine = argOf('--engine', 'mimo');
const outDir = argOf('--outdir', path.join(tmpRoot, 'audio16k'));

fs.mkdirSync(outDir, { recursive: true });
const rawDir = path.join(outDir, 'raw', engine);
fs.mkdirSync(rawDir, { recursive: true });

const cases = JSON.parse(fs.readFileSync(path.join(here, 'cases.json'), 'utf8'));

/** backend/.env 兜底读取 MIMO_API_KEY(主路同源)。 */
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

async function mimoTts(text, key) {
  const res = await fetch('https://api.xiaomimimo.com/v1/chat/completions', {
    method: 'POST',
    headers: { 'api-key': key, 'Content-Type': 'application/json' },
    body: JSON.stringify({
      model: 'mimo-v2.5-tts',
      messages: [{ role: 'assistant', content: text }],
      audio: { format: 'wav', voice: process.env.MIMO_VOICE || 'Chloe' },
    }),
  });
  const body = await res.text();
  if (!res.ok) throw new Error(`tts http ${res.status}: ${body.slice(0, 200)}`);
  const json = JSON.parse(body);
  const b64 = json?.choices?.[0]?.message?.audio?.data || '';
  if (!b64) throw new Error(`tts no audio: ${body.slice(0, 200)}`);
  return Buffer.from(b64.includes(',') ? b64.split(',').at(-1) : b64, 'base64');
}

function sapiTts(text, id) {
  // 16kHz/16bit/mono 直接由 SpeechAudioFormatInfo 保证;中文声音 Huihui。
  const outPath = path.join(rawDir, `${id}.wav`);
  const escapedText = text.replace(/'/g, "''");
  const ps = `
Add-Type -AssemblyName System.Speech
$s = New-Object System.Speech.Synthesis.SpeechSynthesizer
$s.SetOutputToWaveFile('${outPath.replace(/\\/g, '\\\\')}', (New-Object System.Speech.AudioFormat.SpeechAudioFormatInfo(16000,[System.Speech.AudioFormat.AudioBitsPerSample]::Sixteen,[System.Speech.AudioFormat.AudioChannel]::Mono)))
$s.SelectVoice('Microsoft Huihui Desktop')
$s.Speak('${escapedText}')
$s.Dispose()
`;
  execFileSync('powershell', ['-NoProfile', '-ExecutionPolicy', 'Bypass', '-Command', ps], { stdio: 'pipe' });
  if (!fs.existsSync(outPath)) throw new Error('sapi produced no file');
  return fs.readFileSync(outPath);
}

let key = engine === 'mimo' ? loadMimoKey() : '';
if (engine === 'mimo' && !key) {
  console.error('[tts] MIMO_API_KEY 不可得,自动降级 SAPI 引擎(报告将标注)。');
  engineFallback();
} else {
  await main();
}

async function engineFallback() {
  // 直接以 SAPI 跑完全部用例(递归调用本脚本带 --engine sapi)。
  const { spawnSync } = await import('node:child_process');
  const r = spawnSync(process.execPath, [fileURLToPath(import.meta.url), '--engine', 'sapi', '--outdir', outDir], { stdio: 'inherit' });
  process.exit(r.status ?? 1);
}

async function main() {
  const manifest = [];
  const failures = [];
  for (const c of cases.cases) {
    const rawPath = path.join(rawDir, `${c.id}.wav`);
    let rawBuf;
    let usedEngine = engine;
    try {
      if (engine === 'mimo') {
        rawBuf = await mimoTts(c.text, key);
        fs.writeFileSync(rawPath, rawBuf);
      } else {
        rawBuf = sapiTts(c.text, c.id);
      }
    } catch (err) {
      console.error(`[tts] ${c.id} ${engine} 失败: ${err.message}`);
      failures.push({ id: c.id, engine, error: String(err.message).slice(0, 200) });
      if (engine === 'mimo') {
        try {
          rawBuf = sapiTts(c.text, c.id);
          usedEngine = 'sapi-fallback';
          console.error(`[tts] ${c.id} 已用 SAPI 兜底。`);
        } catch (err2) {
          console.error(`[tts] ${c.id} SAPI 也失败: ${err2.message}`);
          continue;
        }
      } else {
        continue;
      }
    }
    const decoded = decodeWav(rawBuf);
    let samples = resampleLinear(decoded.samples, decoded.sampleRate, 16000);
    if (c.category === 'noise') {
      samples = scale(samples, 0.35);
      samples = mixNoise(samples, 8, 42);
    }
    const outPath = path.join(outDir, `${c.id}.wav`);
    fs.writeFileSync(outPath, encodeWav(samples, 16000));
    manifest.push({
      id: c.id,
      category: c.category,
      text: c.text,
      hotwords: c.hotwords,
      engine: usedEngine,
      durationMs: durationMs(samples, 16000),
    });
    console.log(`[tts] ${c.id} ok engine=${usedEngine} ${manifest.at(-1).durationMs}ms`);
  }
  const manifestPath = path.join(outDir, 'manifest.json');
  fs.writeFileSync(manifestPath, JSON.stringify({ sourceEngine: engine, failures, cases: manifest }, null, 2));
  console.log(`[tts] manifest → ${manifestPath} (${manifest.length}/${cases.cases.length} 例)`);
  if (!manifest.length) process.exit(2);
}
