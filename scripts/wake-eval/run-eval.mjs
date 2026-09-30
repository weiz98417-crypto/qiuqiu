// 唤醒词误触/唤醒率评估 runner（wake-word-kws 10.1 评估门）。
//
// 引擎路径说明：本机无可用 python（Store stub），评估走 sherpa-onnx 的
// node 绑定（sherpa-onnx-node，与 python 包同一套 C++ 核心/同一模型/同一
// KWS 解码语义），统计口径与 python 路一致；真机（Android 端 sherpa_onnx
// flutter 插件）人耳确认留尾，见 wake-word-kws 报告。
//
// 流程：读 dataset/manifest.json → 每条 wav 以 100ms 帧喂 KeywordSpotter
// （流内重采样 24k→16k，与端上 16k 直喂同语义）→ 统计：
//   - 安静唤醒率：词表所含短语的正样本 ≥1 次唤醒占比（判据 >95%）；
//   - 误触：负样本命中按 3600s/负样本总时长 折算次/小时（判据 <1 次/小时）；
//   - cooldown 后二次唤醒：唤醒 + 1.6s 静默 + 唤醒拼接流应命中 2 次
//     （>cooldown 间隔，服务层 1.5s 抑制不吞二次唤醒），按词表所含短语
//     各测一条。
//
// 用法：
//   node scripts/wake-eval/run-eval.mjs [--keywords <file>] [--score 1.8]
//        [--threshold 0.1] [--trailing-blanks 1] [--label <name>]
// 产物：scripts/wake-eval/results/<label>.json（入库，评估沉淀）。

import fs from 'node:fs';
import fsp from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { createRequire } from 'node:module';

const require = createRequire(import.meta.url);

const HERE = path.dirname(fileURLToPath(import.meta.url));
const DATASET_DIR = path.join(HERE, 'dataset');
const RESULTS_DIR = path.join(HERE, 'results');
const DEFAULT_MODEL_DIR = 'E:/tools/wake-model/sherpa-onnx-kws-zipformer-wenetspeech-3.3M-2024-01-01';

// 服务层冷却（proposal 起步参数）：窗口内的引擎二次命中不计为唤醒。
const COOLDOWN_MS = 1500;
const CHUNK_SAMPLES = 1600; // 0.1s @16kHz

// 词表 @别名 → 数据集正样本 phrase 字段：唤醒率/二次唤醒按词表实际包含的
// 短语评估（单短语词表不对其他短语的正样本计分）。
const PHRASE_BY_ALIAS = { '嘿球球': 'hei', '嗨球球': 'hai', '你好球球': 'nihao' };

function activePhrases(keywordsText) {
  const phrases = new Set();
  for (const match of keywordsText.matchAll(/@(\S+)/g)) {
    const phrase = PHRASE_BY_ALIAS[match[1]];
    if (phrase) phrases.add(phrase);
  }
  return phrases;
}

function parseArgs(argv) {
  const args = {
    keywords: path.join(HERE, 'keywords', 'hei-qiu-qiu.txt'),
    score: 1.8,
    threshold: 0.1,
    trailingBlanks: 1,
    label: null,
  };
  for (let i = 0; i < argv.length; i++) {
    const flag = argv[i];
    if (flag === '--keywords') args.keywords = argv[++i];
    else if (flag === '--score') args.score = Number(argv[++i]);
    else if (flag === '--threshold') args.threshold = Number(argv[++i]);
    else if (flag === '--trailing-blanks') args.trailingBlanks = Number(argv[++i]);
    else if (flag === '--label') args.label = argv[++i];
  }
  return args;
}

function loadSpotter(modelDir, args) {
  const sherpa = require('sherpa-onnx-node');
  return new sherpa.KeywordSpotter({
    modelConfig: {
      transducer: {
        encoder: `${modelDir}/encoder-epoch-12-avg-2-chunk-16-left-64.int8.onnx`,
        decoder: `${modelDir}/decoder-epoch-12-avg-2-chunk-16-left-64.int8.onnx`,
        joiner: `${modelDir}/joiner-epoch-12-avg-2-chunk-16-left-64.int8.onnx`,
      },
      tokens: `${modelDir}/tokens.txt`,
      numThreads: 1,
      debug: 0,
    },
    keywordsFile: args.keywords,
    keywordsScore: args.score,
    keywordsThreshold: args.threshold,
    numTrailingBlanks: args.trailingBlanks,
  });
}

// 喂一段音频，返回每次命中 {offsetMs, keyword}；每次命中后 reset 流
// （sherpa KWS 契约：检出即 reset，才能连检下一次）。
function decodeWav(sherpa, spotter, wavPath) {
  const wave = sherpa.readWave(wavPath);
  const stream = spotter.createStream();
  const hits = [];
  const samples = wave.samples;
  for (let start = 0; start < samples.length; start += CHUNK_SAMPLES) {
    const end = Math.min(start + CHUNK_SAMPLES, samples.length);
    stream.acceptWaveform({
      sampleRate: wave.sampleRate,
      samples: samples.subarray(start, end),
    });
    drain(spotter, stream, hits, start, wave.sampleRate);
  }
  // 0.5s 尾静默，把词尾帧推完（与官方 dart/node 示例同口径）。
  const tail = new Float32Array(wave.sampleRate / 2);
  stream.acceptWaveform({ sampleRate: wave.sampleRate, samples: tail });
  drain(spotter, stream, hits, samples.length, wave.sampleRate);

  return hits;
}

function drain(spotter, stream, hits, sampleOffset, sampleRate) {
  while (spotter.isReady(stream)) {
    spotter.decode(stream);
    const result = spotter.getResult(stream);
    if (result.keyword !== '') {
      hits.push({
        offsetMs: Math.round((sampleOffset / sampleRate) * 1000),
        keyword: result.keyword,
        tokens: result.tokens,
      });
      spotter.reset(stream);
    }
  }
}

// 服务层冷却口径：窗口内（1.5s）的后续命中不算新唤醒。
function countAwakenings(hits) {
  let count = 0;
  let lastMs = -Infinity;
  for (const hit of hits) {
    if (hit.offsetMs - lastMs > COOLDOWN_MS) count += 1;
    lastMs = hit.offsetMs;
  }
  return count;
}

async function main() {
  const args = parseArgs(process.argv.slice(2));
  if (!fs.existsSync(args.keywords)) {
    console.error(`keywords 文件不存在: ${args.keywords}`);
    process.exit(1);
  }
  const keywordsText = fs.readFileSync(args.keywords, 'utf8');
  const phrases = activePhrases(keywordsText);
  if (phrases.size === 0) {
    console.error(`词表里没有可识别的唤醒短语别名（需 ∈ ${Object.keys(PHRASE_BY_ALIAS).join('/')}）`);
    process.exit(1);
  }
  const manifest = JSON.parse(
    await fsp.readFile(path.join(DATASET_DIR, 'manifest.json'), 'utf8'),
  );
  const sherpa = require('sherpa-onnx-node');
  const modelDir = process.env.WAKE_MODEL_DIR || DEFAULT_MODEL_DIR;
  const spotter = loadSpotter(modelDir, args);

  const perCase = [];
  for (const entry of manifest) {
    // 负样本永远解码（误触门的主考题）；正样本只解码词表所含短语。
    const scored = entry.kind === 'positive' && phrases.has(entry.phrase);
    const hits = entry.kind === 'negative' || scored
      ? decodeWav(sherpa, spotter, path.join(HERE, entry.wav))
      : [];
    perCase.push({
      id: entry.id,
      kind: entry.kind,
      phrase: entry.phrase ?? null,
      text: entry.text,
      voice: entry.voice,
      seconds: entry.seconds,
      hits,
      awakenings: countAwakenings(hits),
      skipped: entry.kind === 'positive' && !phrases.has(entry.phrase),
    });
    if (scored) {
      const mark = hits.length > 0 ? '✓' : '✗';
      console.log(
        `${mark} ${entry.id.padEnd(18)} hits=${hits.length}${hits.length ? ` @${hits.map((h) => h.offsetMs).join(',')}` : ''}  ${entry.text}`,
      );
    }
  }

  // 二次唤醒（cooldown 判据）：词表所含短语各取第一条正样本做
  // 唤醒+1.6s 静默+唤醒 拼接流。
  const doubleWake = [];
  for (const phrase of phrases) {
    const positive = manifest.find((c) => c.kind === 'positive' && c.phrase === phrase);
    doubleWake.push({
      phrase,
      ...(await runDoubleWake(sherpa, spotter, positive)),
    });
  }

  // 唤醒率分母只计词表所含短语的已评正样本（skipped 的其他短语不计）。
  const positives = perCase.filter((c) => c.kind === 'positive' && !c.skipped);
  const negatives = perCase.filter((c) => c.kind === 'negative');
  const positiveHits = positives.filter((c) => c.awakenings > 0);
  const negativeHitCases = negatives.filter((c) => c.awakenings > 0);
  const negativeSeconds = negatives.reduce((sum, c) => sum + c.seconds, 0);
  const falseTriggers = negativeHitCases.reduce((sum, c) => sum + c.awakenings, 0);
  const triggersPerHour = negativeSeconds > 0
    ? (falseTriggers * 3600) / negativeSeconds
    : 0;

  const summary = {
    label: args.label ?? path.basename(args.keywords),
    keywords: keywordsText.trim(),
    phrases: [...phrases],
    params: {
      keywordsScore: args.score,
      keywordsThreshold: args.threshold,
      numTrailingBlanks: args.trailingBlanks,
      cooldownMs: COOLDOWN_MS,
    },
    model: path.basename(modelDir),
    wakeRate: positiveHits.length / positives.length,
    wakeHits: `${positiveHits.length}/${positives.length}`,
    positiveMisses: positives
      .filter((c) => c.awakenings === 0)
      .map((c) => ({ id: c.id, voice: c.voice, text: c.text })),
    falseTriggerCases: negativeHitCases.map((c) => ({
      id: c.id,
      text: c.text,
      awakenings: c.awakenings,
    })),
    falseTriggers,
    falseTriggersPerHour: Number(triggersPerHour.toFixed(1)),
    negativeAudioSeconds: Number(negativeSeconds.toFixed(1)),
    doubleWake,
    gate: {
      wakeRatePass: positiveHits.length / positives.length > 0.95,
      falseTriggerPass: triggersPerHour < 1,
      doubleWakePass: doubleWake.every((d) => d.awakenings >= 2),
    },
    perCase,
  };

  await fsp.mkdir(RESULTS_DIR, { recursive: true });
  const outPath = path.join(RESULTS_DIR, `${summary.label.replace(/[^\w.-]+/g, '_')}.json`);
  await fsp.writeFile(outPath, `${JSON.stringify(summary, null, 2)}\n`);

  console.log('--- 评估结论 ---');
  console.log(`唤醒率: ${(summary.wakeRate * 100).toFixed(1)}%（${summary.wakeHits}，判据 >95%）`);
  console.log(`误触: ${falseTriggers} 次 / ${negatives.length} 条 → ${summary.falseTriggersPerHour} 次/小时折算（判据 <1）`);
  console.log(`二次唤醒: ${doubleWake.map((d) => `${d.phrase}=${d.awakenings}`).join(' ')}（判据 各≥2）`);
  console.log(`门禁: ${Object.values(summary.gate).every(Boolean) ? 'PASS' : 'FAIL'} → ${outPath}`);
}

async function runDoubleWake(sherpa, spotter, positive) {
  const wave = sherpa.readWave(path.join(HERE, positive.wav));
  const gap = new Float32Array(Math.round(wave.sampleRate * 1.6));
  const merged = new Float32Array(wave.samples.length * 2 + gap.length);
  merged.set(wave.samples, 0);
  merged.set(gap, wave.samples.length);
  merged.set(wave.samples, wave.samples.length + gap.length);

  const stream = spotter.createStream();
  const hits = [];
  for (let start = 0; start < merged.length; start += CHUNK_SAMPLES) {
    const end = Math.min(start + CHUNK_SAMPLES, merged.length);
    stream.acceptWaveform({ sampleRate: wave.sampleRate, samples: merged.subarray(start, end) });
    drain(spotter, stream, hits, start, wave.sampleRate);
  }
  stream.acceptWaveform({ sampleRate: wave.sampleRate, samples: new Float32Array(wave.sampleRate / 2) });
  drain(spotter, stream, hits, merged.length, wave.sampleRate);

  return {
    caseId: `${positive.id}+gap1.6s+${positive.id}`,
    hits: hits.length,
    awakenings: countAwakenings(hits),
  };
}

main().catch((error) => {
  console.error(`评估失败: ${error.message}`);
  process.exit(1);
});
