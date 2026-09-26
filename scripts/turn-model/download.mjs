// LiveKit EOU 轮次检测模型下载脚本（voice-turn-detection 决策 c）。
//
// 从 hf-mirror.com（huggingface.co 的国内可达镜像）拉取 livekit/turn-detector
// 指定 revision 的推理所需文件，默认 multilingual 版 v0.4.1-intl（中文覆盖）；
// en 版 v1.2.2-en 作备选（TURN_MODEL_REVISION=v1.2.2-en）。
//
// 设计要点：
// - registry 无关：纯 node（fetch + crypto + fs），不依赖 pip/hf_hub；
// - 断点续传：已有部分文件按字节数发 Range 请求续拉；
// - sha256 校验：清单锁定自 HF tree API 的 LFS oid（lfs.oid 即 sha256），
//   不匹配即报错退出，不落半校验文件；
// - 落盘目录默认在仓库外（E:\tools\turn-model\<revision>\），gitignored；
//   TURN_MODEL_ROOT 可覆盖。
//
// 用法：node scripts/turn-model/download.mjs
// 环境变量：TURN_MODEL_ROOT（默认 E:\tools\turn-model）
//           TURN_MODEL_REVISION（默认 v0.4.1-intl）

import { createHash } from 'node:crypto';
import { createReadStream, createWriteStream } from 'node:fs';
import fs from 'node:fs/promises';
import path from 'node:path';
import { pipeline } from 'node:stream/promises';

const HG_REPO = 'livekit/turn-detector';
const REVISION = process.env.TURN_MODEL_REVISION || 'v0.4.1-intl';
const MODEL_ROOT = process.env.TURN_MODEL_ROOT || 'E:\\tools\\turn-model';
const DEST_DIR = path.join(MODEL_ROOT, REVISION);
// 镜像与 API 同源：resolve 走 LFS 重定向，tree API 出 sha256 清单。
const MIRROR_BASE = 'https://hf-mirror.com';
const RESOLVE_BASE = `${MIRROR_BASE}/${HG_REPO}/resolve/${REVISION}`;
const TREE_API = `${MIRROR_BASE}/api/models/${HG_REPO}/tree/${REVISION}?recursive=true`;

// 推理所需文件清单（与 LiveKit agents 的 download-files 对齐，另含
// ort_config.json）：onnx 权重 + tokenizer 全家桶 + 语言阈值表。
const REQUIRED_FILES = [
  'onnx/model_q8.onnx',
  'tokenizer.json',
  'tokenizer_config.json',
  'special_tokens_map.json',
  'added_tokens.json',
  'vocab.json',
  'merges.txt',
  'config.json',
  'languages.json',
  'ort_config.json',
];

function log(message) {
  console.log(`[turn-model] ${message}`);
}

async function fetchTreeManifest() {
  const response = await fetch(TREE_API);
  if (!response.ok) {
    throw new Error(`tree API ${response.status}: ${TREE_API}`);
  }
  const entries = await response.json();
  const manifest = new Map();
  for (const entry of entries) {
    if (entry.type !== 'file') continue;
    manifest.set(entry.path, {
      size: entry.size,
      // LFS 文件的 oid 即内容 sha256；非 LFS 小文件校验走长度兜底。
      sha256: entry.lfs?.oid ?? null,
    });
  }
  return manifest;
}

function hashFile(filePath) {
  return new Promise((resolve, reject) => {
    const hash = createHash('sha256');
    const stream = createReadStream(filePath);
    stream.on('error', reject);
    stream.on('data', (chunk) => hash.update(chunk));
    stream.on('end', () => resolve(hash.digest('hex')));
  });
}

async function fileSize(filePath) {
  try {
    const stat = await fs.stat(filePath);
    return stat.isFile() ? stat.size : 0;
  } catch {
    return 0;
  }
}

// 下载单个文件：支持按已有字节数续传；完成后校验 sha256（LFS 文件）或
// 字节数（非 LFS），校验失败删掉重来一次（防止续传基线本身损坏）。
async function downloadFile(relativePath, meta, attempt = 0) {
  const destPath = path.join(DEST_DIR, ...relativePath.split('/'));
  await fs.mkdir(path.dirname(destPath), { recursive: true });
  const existingBytes = attempt === 0 ? await fileSize(destPath) : 0;
  const complete = existingBytes > 0 && existingBytes === meta.size &&
    (await fileMatches(destPath, meta));
  if (complete) {
    log(`已就绪（校验通过）: ${relativePath}`);
    return;
  }

  const headers = {};
  if (existingBytes > 0 && existingBytes < meta.size) {
    headers.Range = `bytes=${existingBytes}-`;
    log(`续传 ${relativePath}：已有 ${existingBytes}/${meta.size} 字节`);
  } else if (existingBytes > 0) {
    // 长度不符（含超下载）：清掉重来。
    await fs.rm(destPath, { force: true });
  }
  const response = await fetch(`${RESOLVE_BASE}/${relativePath}`, { headers });
  if (!response.ok && response.status !== 206) {
    throw new Error(`download ${relativePath} → HTTP ${response.status}`);
  }
  if (response.status === 200 && existingBytes > 0) {
    // 服务端不支持 Range：从头写。
    await pipeline(response.body, createWriteStream(destPath));
  } else {
    await pipeline(response.body, createWriteStream(destPath, { flags: 'a' }));
  }

  if (!(await fileMatches(destPath, meta))) {
    if (attempt >= 1) {
      throw new Error(`sha256 校验两次失败: ${relativePath}`);
    }
    log(`校验失败，重下 ${relativePath}`);
    await fs.rm(destPath, { force: true });
    return downloadFile(relativePath, meta, attempt + 1);
  }
  log(`完成 ${relativePath}（${meta.size} 字节，校验通过）`);
}

async function fileMatches(filePath, meta) {
  const size = await fileSize(filePath);
  if (size !== meta.size) return false;
  if (!meta.sha256) return true; // 非 LFS 小文件：长度兜底。
  const digest = await hashFile(filePath);
  return digest === meta.sha256;
}

async function main() {
  const startedAt = Date.now();
  log(`revision=${REVISION} dest=${DEST_DIR}`);
  const manifest = await fetchTreeManifest();
  const missing = REQUIRED_FILES.filter((file) => !manifest.has(file));
  if (missing.length > 0) {
    throw new Error(`tree API 缺少所需文件清单: ${missing.join(', ')}`);
  }
  for (const file of REQUIRED_FILES) {
    await downloadFile(file, manifest.get(file));
  }
  const seconds = ((Date.now() - startedAt) / 1000).toFixed(1);
  log(`全部文件就绪，耗时 ${seconds}s`);
}

main().catch((error) => {
  log(`下载失败: ${error.message}`);
  process.exit(1);
});
