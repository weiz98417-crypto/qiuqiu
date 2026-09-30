// 唤醒词评估集生成（wake-word-kws 10.1 评估门）。
//
// 真实解说音频不可得（评估资产纪律：代理评估如实记），用 MiMo TTS 合成：
// - 正样本：「嘿球球」三音节唤醒词，双音色 × 多语气（平静/看台呐喊/隔房
//   喊人/轻声试探/疑问上扬）；
// - 负样本：足球语境「球球」高频句 + 近失混淆（hēi qiú 开头、qiú qiú
//   同音连用、「黑球」「求球」等）——误触门的主考题；
// - 二次唤醒样本：唤醒 → 1.5s 静默 → 再唤醒（cooldown 判据），由 runner
//   在解码侧拼接，不单独走 TTS。
//
// 用法：node scripts/wake-eval/generate-dataset.mjs
// 凭据：MIMO_API_KEY 环境变量优先，其次读取未入库的 backend/.env（与
// release runner 同纪律，绝不打印密钥）。产物 dataset/*.wav 与
// dataset/manifest.json；wav 可重生成，manifest 连同 runner 结果入库。
// TTS 非逐比特确定：重生成会换音频，但文本口径由 manifest 锁定。

import { createHash } from 'node:crypto';
import fs from 'node:fs';
import fsp from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const DATASET_DIR = path.join(HERE, 'dataset');
const BACKEND_ENV = path.join(HERE, '..', '..', 'backend', '.env');
const BASE_URL = process.env.MIMO_BASE_URL || 'https://api.xiaomimimo.com/v1';
const MODEL = 'mimo-v2.5-tts';

// ---- 正样本：候选唤醒词 × 双音色 × 五语气 ------------------------------
// 词表定稿是评估的输出（tasks 10.1：三音节起步，不过门换词重测）：多个
// 候选短语的音频都进数据集，keywords/ 下各有词表文件与 combo 文件，由
// run-eval.mjs 按词表评估。「嘿」被 TTS 渲染得不稳定（模型把一段正样本
// 听成 bēi jiù qiú qiú），嗨/你好是声学上更稳的候选，留作迭代。
const WAKE_PHRASES = [
  {
    phrase: 'hei',
    texts: [
      { text: '嘿球球', instruction: '平静自然地呼唤身边的人。' },
      { text: '嘿，球球！', instruction: '兴奋地大声喊出来，像在球场看台上喊同伴。' },
      { text: '嘿球球', instruction: '隔着一个房间喊人，声音洪亮，尾音拖长。' },
      { text: '嘿球球', instruction: '轻声试探地喊，像怕打扰别人。' },
      { text: '嘿球球，你看这个球', instruction: '日常自然的语气，喊人后接一句话。' },
    ],
  },
  {
    phrase: 'hai',
    texts: [
      { text: '嗨球球', instruction: '平静自然地呼唤身边的人。' },
      { text: '嗨，球球！', instruction: '兴奋地大声喊出来，像在球场看台上喊同伴。' },
      { text: '嗨球球', instruction: '隔着一个房间喊人，声音洪亮，尾音拖长。' },
      { text: '嗨球球', instruction: '轻声试探地喊，像怕打扰别人。' },
      { text: '嗨球球，你看这个球', instruction: '日常自然的语气，喊人后接一句话。' },
    ],
  },
  {
    phrase: 'nihao',
    texts: [
      { text: '你好球球', instruction: '平静自然地打招呼。' },
      { text: '你好，球球！', instruction: '兴奋地大声喊出来，像在球场看台上喊同伴。' },
      { text: '你好球球', instruction: '隔着一个房间喊人，声音洪亮，尾音拖长。' },
      { text: '你好球球', instruction: '轻声试探地喊，像怕打扰别人。' },
      { text: '你好球球，你看这个球', instruction: '日常自然的语气，喊人后接一句话。' },
    ],
  },
];
const POSITIVE_VOICES = ['冰糖', 'Chloe'];

// ---- 负样本：足球语境「球球」+ 近失混淆 --------------------------------
const NEGATIVES = [
  '球球你说话呀',
  '这个球球进得漂亮',
  '球球落地反弹',
  '嘿，球进了！',
  '嘿，球没进，可惜了',
  '嘿嘿，这球太棒了',
  '黑球白球，能进球的就是好球',
  '球球在禁区里混战',
  '我求求你了，别再传中了',
  '球进了！球进了！',
  '看这个球的弧线，太诡异了',
  '球球出底线了，门球',
  '嘿，你看这个任意球',
  '这个球球停得真好',
  '求球稳住，别慌',
  '足球就是这样，一秒定生死',
  '球球传中，门前包抄',
  '他穿了黑色球衣',
  '罚球点球，全场安静',
  '这球裁判判得有争议',
  '球迷都在唱队歌',
  '角球开出，头球攻门',
  '球球被断掉了',
  '嘿嘿，居然平了',
  '这记倒钩太帅了',
  '球权又丢了，危险啊',
];

async function loadApiKey() {
  if (process.env.MIMO_API_KEY) return process.env.MIMO_API_KEY;
  try {
    const env = await fsp.readFile(BACKEND_ENV, 'utf8');
    const match = env.match(/^MIMO_API_KEY=(.+)$/m);
    if (match) return match[1].trim();
  } catch {
    // backend/.env 不存在：走报错分支。
  }
  console.error('MIMO_API_KEY 缺失：设环境变量或配置 backend/.env');
  process.exit(1);
}

async function synthesize(key, { text, instruction, voice }) {
  const messages = [];
  if (instruction) {
    messages.push({ role: 'user', content: instruction });
  }
  messages.push({ role: 'assistant', content: text });
  const response = await fetch(`${BASE_URL}/chat/completions`, {
    method: 'POST',
    headers: { 'api-key': key, 'Content-Type': 'application/json' },
    body: JSON.stringify({
      model: MODEL,
      messages,
      audio: { format: 'wav', voice },
    }),
  });
  const body = await response.json().catch(() => ({}));
  const data = body?.choices?.[0]?.message?.audio?.data;
  if (!response.ok || !data) {
    throw new Error(`TTS 失败 HTTP ${response.status}: ${JSON.stringify(body).slice(0, 200)}`);
  }
  return Buffer.from(data.includes(',') ? data.split(',').at(-1) : data, 'base64');
}

// 解析 WAV 头拿时长（MiMo 出 24kHz pcm16 单声道；runner 喂 KWS 时由
// sherpa-onnx 流内重采样，这里只做记账）。
function wavInfo(buffer) {
  // RIFF....WAVEfmt ....data....: 找 data 块。
  const view = buffer;
  if (view.toString('ascii', 0, 4) !== 'RIFF') return { seconds: 0, sampleRate: 0 };
  let offset = 12;
  let sampleRate = 0;
  while (offset + 8 <= view.length) {
    const id = view.toString('ascii', offset, offset + 4);
    const size = view.readUInt32LE(offset + 4);
    if (id === 'fmt ') {
      sampleRate = view.readUInt32LE(offset + 12);
    }
    if (id === 'data') {
      const bytesPerSample = 2; // pcm16
      return { seconds: size / (sampleRate * bytesPerSample), sampleRate };
    }
    offset += 8 + size + (size % 2);
  }
  return { seconds: 0, sampleRate };
}

async function main() {
  const key = await loadApiKey();
  await fsp.mkdir(DATASET_DIR, { recursive: true });

  const cases = [];
  let positiveIndex = 0;
  for (const { phrase, texts } of WAKE_PHRASES) {
    for (const voice of POSITIVE_VOICES) {
      for (const { text, instruction } of texts) {
        positiveIndex += 1;
        cases.push({
          id: `pos_${phrase}_${voice}_${positiveIndex}`,
          kind: 'positive',
          phrase,
          text,
          instruction,
          voice,
        });
      }
    }
  }
  NEGATIVES.forEach((text, index) => {
    // 负样本也双音色轮换：同一个足球语境句，音色不该改变结论。
    const voice = POSITIVE_VOICES[index % POSITIVE_VOICES.length];
    cases.push({
      id: `neg_${String(index + 1).padStart(2, '0')}`,
      kind: 'negative',
      text,
      instruction: '足球解说或看球时的日常口语。',
      voice,
    });
  });

  const manifest = [];
  for (const entry of cases) {
    const wavPath = path.join(DATASET_DIR, `${entry.id}.wav`);
    const cached = fs.existsSync(wavPath);
    const buffer = cached
      ? await fsp.readFile(wavPath)
      : await synthesize(key, entry);
    if (!cached) await fsp.writeFile(wavPath, buffer);
    const { seconds, sampleRate } = wavInfo(buffer);
    manifest.push({
      ...entry,
      wav: `dataset/${entry.id}.wav`,
      seconds: Number(seconds.toFixed(2)),
      sampleRate,
      sha256: createHash('sha256').update(buffer).digest('hex'),
    });
    console.log(`${entry.id}  ${seconds.toFixed(1)}s  ${entry.voice}  ${entry.text}`);
  }

  const manifestPath = path.join(DATASET_DIR, 'manifest.json');
  await fsp.writeFile(manifestPath, `${JSON.stringify(manifest, null, 2)}\n`);
  const positives = manifest.filter((c) => c.kind === 'positive').length;
  const negatives = manifest.filter((c) => c.kind === 'negative').length;
  const negativeSeconds = manifest
    .filter((c) => c.kind === 'negative')
    .reduce((sum, c) => sum + c.seconds, 0);
  const byPhrase = {};
  for (const entry of manifest) {
    if (entry.kind === 'positive') {
      byPhrase[entry.phrase] = (byPhrase[entry.phrase] ?? 0) + 1;
    }
  }
  console.log(
    `manifest 就绪：正样本 ${positives}（${JSON.stringify(byPhrase)}）/ 负样本 ${negatives}，负样本总时长 ${negativeSeconds.toFixed(1)}s → 1 次误触 ≈ ${(3600 / negativeSeconds).toFixed(0)}/h 折算基数`,
  );
}

main().catch((error) => {
  console.error(`生成失败: ${error.message}`);
  process.exit(1);
});
