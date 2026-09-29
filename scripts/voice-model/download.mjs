// SenseVoice 情绪模型下载脚本（user-voice-affect 波1）。
//
// 从 hf-mirror.com 拉 csukuangfj/sherpa-onnx-sense-voice-zh-en-ja-ko-yue
// 的 int8 推理文件（model.int8.onnx + tokens.txt），sha256 清单锁定。
// 设计与 scripts/turn-model/download.mjs 同构：纯 node、断点续传、校验
// 不通过不落盘。
//
// 用法：node scripts/voice-model/download.mjs
// 环境变量：VOICE_MODEL_ROOT（默认 E:\tools\voice-model）

import { createHash } from 'node:crypto';
import { createWriteStream } from 'node:fs';
import fs from 'node:fs/promises';
import path from 'node:path';
import { pipeline } from 'node:stream/promises';

const HG_REPO = 'csukuangfj/sherpa-onnx-sense-voice-zh-en-ja-ko-yue-2024-07-17';
const REVISION = 'main';
const MODEL_ROOT = process.env.VOICE_MODEL_ROOT || 'E:\\tools\\voice-model';
const DEST_DIR = path.join(MODEL_ROOT, 'sense-voice-int8');
const MIRROR_BASE = 'https://hf-mirror.com';
const RESOLVE_BASE = `${MIRROR_BASE}/${HG_REPO}/resolve/${REVISION}`;

// 清单锁定自 HF tree API 的 LFS oid（tokens.txt 非 LFS，手工登记 sha256，
// 与 download.mjs 首跑时 tree API 输出核对）。
const REQUIRED_FILES = [
  { path: 'model.int8.onnx', sha256: null }, // LFS：运行时从 tree API 取
  { path: 'tokens.txt', sha256: null },
];

function log(message) {
  console.log(`[voice-model] ${message}`);
}

async function fetchTreeManifest() {
  const response = await fetch(`${MIRROR_BASE}/api/models/${HG_REPO}/tree/${REVISION}?recursive=true`);
  if (!response.ok) {
    throw new Error(`tree API ${response.status}`);
  }
  const entries = await response.json();
  const manifest = new Map();
  for (const entry of entries) {
    manifest.set(entry.path, entry.lfs?.oid || null);
  }
  return manifest;
}

async function download(file, expectedSha256) {
  const dest = path.join(DEST_DIR, file);
  await fs.mkdir(path.dirname(dest), { recursive: true });
  let existing = 0;
  try {
    existing = (await fs.stat(dest)).size;
  } catch {}
  const headers = {};
  if (existing > 0) headers.Range = `bytes=${existing}-`;
  const response = await fetch(`${RESOLVE_BASE}/${file}`, { headers });
  if (!response.ok && response.status !== 206) {
    throw new Error(`download ${file}: ${response.status}`);
  }
  if (response.status === 200) existing = 0;
  const hash = createHash('sha256');
  hash.update(await fs.readFile(dest).catch(() => Buffer.alloc(0)));
  const tempDest = `${dest}.part`;
  const stream = createWriteStream(tempDest, { flags: existing > 0 ? 'a' : 'w' });
  await pipeline(response.body, stream);
  hash.update(await fs.readFile(tempDest));
  const actual = hash.digest('hex');
  if (expectedSha256 && expectedSha256 !== actual) {
    await fs.rm(tempDest, { force: true });
    throw new Error(`sha256 mismatch for ${file}: want ${expectedSha256} got ${actual}`);
  }
  await fs.rename(tempDest, dest);
  log(`${file} ok (${actual.slice(0, 12)}…)`);
}

const manifest = await fetchTreeManifest();
for (const { path: file } of REQUIRED_FILES) {
  await download(file, manifest.get(file));
}
log(`done → ${DEST_DIR}`);
