#!/usr/bin/env node
// Re-downloads the pinned lip-sync libraries vendored into
// client/assets/live2d/vendor/wlipsync/. Run from the repo root:
//   node scripts/fetch-lipsync-libs.mjs
//
// - wLipSync 1.3.1 (npm "wlipsync", MIT) — single-file build with the WASM
//   binary and audio-worklet processor inlined.
// - profile.bin — the example viseme calibration profile from the wLipSync
//   repository (5-vowel A/I/U/E/O + S), parsed at runtime with
//   parseBinaryProfile.
import { mkdir, writeFile } from 'node:fs/promises';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const WLIPSYNC_VERSION = '1.3.1';
const WLIPSYNC_PROFILE_REF = 'v1.3.1';

const vendorDir = join(
  dirname(fileURLToPath(import.meta.url)),
  '..',
  'client',
  'assets',
  'live2d',
  'vendor',
  'wlipsync',
);

const files = [
  {
    url: `https://cdn.jsdelivr.net/npm/wlipsync@${WLIPSYNC_VERSION}/dist/wlipsync-single.js`,
    out: 'wlipsync-single.js',
  },
  {
    url: `https://raw.githubusercontent.com/mrxz/wLipSync/${WLIPSYNC_PROFILE_REF}/example/profile.bin`,
    out: 'profile.bin',
  },
  {
    url: `https://raw.githubusercontent.com/mrxz/wLipSync/${WLIPSYNC_PROFILE_REF}/LICENSE`,
    out: 'LICENSE',
  },
];

await mkdir(vendorDir, { recursive: true });
for (const file of files) {
  let lastError;
  for (let attempt = 1; attempt <= 3; attempt++) {
    try {
      const response = await fetch(file.url);
      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
      }
      const bytes = Buffer.from(await response.arrayBuffer());
      await writeFile(join(vendorDir, file.out), bytes);
      console.log(`vendored ${file.out} (${bytes.length} bytes)`);
      lastError = undefined;
      break;
    } catch (error) {
      lastError = error;
      console.warn(`attempt ${attempt} for ${file.out} failed: ${error}`);
    }
  }
  if (lastError) {
    throw new Error(`failed to fetch ${file.url}: ${lastError}`);
  }
}
