// 唤醒词 KWS 模型下载脚本（wake-word-kws 10.1 评估门）。
//
// 模型：sherpa-onnx-kws-zipformer-wenetspeech-3.3M-2024-01-01 的 int8 三件套
// （encoder/decoder/joiner chunk-16-left-64）+ tokens.txt。源头锁定
// ModelScope 官方镜像 pkufool/sherpa-onnx-kws-zipformer-wenetspeech-3.3M-
// 2024-01-01（pkufool = k2-fsa 核心维护者；k2-fsa 官方发布物是 GitHub
// release tar.bz2，国内直连不稳，ModelScope 每文件带内容 sha256，正好落锁）。
//
// 设计要点（照 scripts/turn-model/download.mjs 先例）：
// - registry 无关：纯 node（fetch + crypto + fs），不依赖 pip/hf_hub/git-lfs；
// - sha256 锁：清单取自 ModelScope repo files API 的 Sha256 字段（内容哈希，
//   与本地下载逐一比对），不匹配即报错退出，不落半校验文件；
// - 断点续传：按已有字节数发 Range 请求续拉；
// - 默认落盘仓库外（E:\tools\wake-model\<revision>\），模型不入 git；
//   WAKE_ASSETS=1 时同步复制进 client/assets/wake/model/（该目录 gitignore，
//   Flutter 侧打包资产从这里来，见 client/README.md 落盘约定）。
//
// 用法：node scripts/wake-model/download.mjs
// 环境变量：WAKE_MODEL_ROOT（默认 E:\tools\wake-model）
//           WAKE_MODEL_REVISION（默认 master，仅作下载路径参数；内容一致性
//           由逐文件 sha256 锁保证，与 revision 名无关）
//           WAKE_ASSETS=1（额外同步到 client/assets/wake/model/）

import { createHash } from 'node:crypto';
import { createReadStream, createWriteStream } from 'node:fs';
import fs from 'node:fs/promises';
import path from 'node:path';
import { pipeline } from 'node:stream/promises';
import { fileURLToPath } from 'node:url';

// 模型文件清单（int8 推理集；float 版不拉—— wake 目标平台是 Android 端侧，
// int8 是 proposal 定稿）。keywords.txt 是上游示例词表（参考用），实际词表
// 由 client/assets/wake/keywords.txt 与评估 harness 自管。
const FILES = [
  'encoder-epoch-12-avg-2-chunk-16-left-64.int8.onnx',
  'decoder-epoch-12-avg-2-chunk-16-left-64.int8.onnx',
  'joiner-epoch-12-avg-2-chunk-16-left-64.int8.onnx',
  'tokens.txt',
  'keywords.txt',
];

const REPO = 'pkufool/sherpa-onnx-kws-zipformer-wenetspeech-3.3M-2024-01-01';
const REVISION = process.env.WAKE_MODEL_REVISION || 'master';
const MODEL_ROOT = process.env.WAKE_MODEL_ROOT || 'E:\\tools\\wake-model';
const DEST_DIR = path.join(MODEL_ROOT, 'sherpa-onnx-kws-zipformer-wenetspeech-3.3M-2024-01-01');
const ASSETS_DIR = path.join(path.dirname(fileURLToPath(import.meta.url)), '..', '..', 'client', 'assets', 'wake', 'model');
// ModelScope 的国内直追数据面；API 与文件下载同源。
const API_BASE = `https://www.modelscope.cn/api/v1/models/${REPO}/repo`;

function log(message) {
  console.log(`[wake-model] ${message}`);
}

async function fetchManifest() {
  const response = await fetch(`${API_BASE}/files?Recursive=true`);
  if (!response.ok) {
    throw new Error(`files API ${response.status}`);
  }
  const body = await response.json();
  if (body.Code !== 200 || !body.Data?.Files) {
    throw new Error(`files API 响应异常: ${JSON.stringify(body).slice(0, 200)}`);
  }
  const manifest = new Map();
  for (const entry of body.Data.Files) {
    if (entry.Type !== 'blob') continue;
    manifest.set(entry.Path, { size: entry.Size, sha256: entry.Sha256 });
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

// 下载单文件：按已有字节数续传；完成后 sha256 校验，失败删掉重下一次，
// 两次失败即退出（防止续传基线本身损坏）。
async function downloadFile(relativePath, meta, attempt = 0) {
  const destPath = path.join(DEST_DIR, ...relativePath.split('/'));
  await fs.mkdir(path.dirname(destPath), { recursive: true });
  const existingBytes = attempt === 0 ? await fileSize(destPath) : 0;
  if (existingBytes > 0 && existingBytes === meta.size &&
      (await hashFile(destPath)) === meta.sha256) {
    log(`已就绪（校验通过）: ${relativePath}`);
    return;
  }
  if (existingBytes > 0 && existingBytes !== meta.size) {
    await fs.rm(destPath, { force: true });
  }

  const url = `${API_BASE}?Revision=${encodeURIComponent(REVISION)}&FilePath=${encodeURIComponent(relativePath)}`;
  const headers = {};
  if (existingBytes > 0 && existingBytes < meta.size) {
    headers.Range = `bytes=${existingBytes}-`;
    log(`续传 ${relativePath}：已有 ${existingBytes}/${meta.size} 字节`);
  }
  const response = await fetch(url, { headers });
  if (!response.ok && response.status !== 206) {
    throw new Error(`download ${relativePath} → HTTP ${response.status}`);
  }
  if (response.status === 200 && existingBytes > 0) {
    // 服务端不支持 Range：从头写。
    await pipeline(response.body, createWriteStream(destPath));
  } else {
    await pipeline(response.body, createWriteStream(destPath, { flags: 'a' }));
  }

  const digest = await hashFile(destPath);
  if (digest !== meta.sha256) {
    if (attempt >= 1) {
      throw new Error(`sha256 校验两次失败: ${relativePath}（got ${digest}, want ${meta.sha256}）`);
    }
    log(`校验失败，重下 ${relativePath}`);
    await fs.rm(destPath, { force: true });
    return downloadFile(relativePath, meta, attempt + 1);
  }
  log(`完成 ${relativePath}（${meta.size} 字节，sha256 通过）`);
}

async function main() {
  const startedAt = Date.now();
  log(`revision=${REVISION} dest=${DEST_DIR}`);
  const manifest = await fetchManifest();
  const missing = FILES.filter((file) => !manifest.has(file));
  if (missing.length > 0) {
    throw new Error(`files API 缺少所需文件: ${missing.join(', ')}`);
  }
  for (const file of FILES) {
    await downloadFile(file, manifest.get(file));
  }
  if (process.env.WAKE_ASSETS === '1') {
    await fs.mkdir(ASSETS_DIR, { recursive: true });
    for (const file of FILES) {
      await fs.copyFile(
        path.join(DEST_DIR, ...file.split('/')),
        path.join(ASSETS_DIR, ...file.split('/')),
      );
    }
    log(`已同步 ${FILES.length} 个文件到 ${ASSETS_DIR}`);
  }
  const seconds = ((Date.now() - startedAt) / 1000).toFixed(1);
  log(`全部文件就绪，耗时 ${seconds}s`);
}

main().catch((error) => {
  log(`下载失败: ${error.message}`);
  process.exit(1);
});
