// Build the live2d display bundle deterministically.
// Output: ../../client/assets/live2d/live2d-display-bundle.js (single IIFE).
// Deterministic: esbuild output depends only on inputs; re-running with the
// locked dependency set must reproduce the same sha256 (recorded below).
import esbuild from 'esbuild';
import crypto from 'node:crypto';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const outFile = path.resolve(here, '../../client/assets/live2d/live2d-display-bundle.js');

await esbuild.build({
  entryPoints: [path.join(here, 'src/index.js')],
  bundle: true,
  format: 'iife',
  target: 'es2018',
  minify: true,
  outfile: outFile,
  logLevel: 'info',
});

const buf = fs.readFileSync(outFile);
const sha = crypto.createHash('sha256').update(buf).digest('hex');
console.log(`bundle: ${outFile}`);
console.log(`bytes:  ${buf.length}`);
console.log(`sha256: ${sha}`);
