// 唤醒词参数扫描（wake-word-kws 10.1）：在评估集上扫 keywordsScore ×
// keywordsThreshold（× numTrailingBlanks），汇总各档唤醒率/误触率。
// 用法：node scripts/wake-eval/sweep.mjs [--keywords <file>] [--label <prefix>]
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import fsp from 'node:fs/promises';
import { execFileSync } from 'node:child_process';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const RUNNER = path.join(HERE, 'run-eval.mjs');

const scores = [1.0, 1.8, 2.5];
const thresholds = [0.05, 0.1, 0.2, 0.3];
const blanks = [1];

const args = process.argv.slice(2);
const flag = (name) => {
  const i = args.indexOf(name);
  return i >= 0 ? args[i + 1] : undefined;
};
const keywords = flag('--keywords') ?? path.join(HERE, 'keywords', 'hei-qiu-qiu.txt');
const labelPrefix = flag('--label') ?? 'sweep';

const rows = [];
for (const score of scores) {
  for (const threshold of thresholds) {
    for (const trailingBlanks of blanks) {
      const label = `${labelPrefix}-s${score}-t${threshold}-b${trailingBlanks}`;
      execFileSync(
        process.execPath,
        [RUNNER, '--keywords', keywords, '--score', String(score),
         '--threshold', String(threshold), '--trailing-blanks', String(trailingBlanks),
         '--label', label],
        { stdio: ['ignore', 'ignore', 'inherit'] },
      );
      const result = JSON.parse(
        await fsp.readFile(path.join(HERE, 'results', `${label}.json`), 'utf8'),
      );
      rows.push({
        label,
        score,
        threshold,
        trailingBlanks,
        wakeRate: Number((result.wakeRate * 100).toFixed(0)),
        falseTriggers: result.falseTriggers,
        perHour: result.falseTriggersPerHour,
        doubleWake: result.doubleWake.awakenings,
        pass: Object.values(result.gate).every(Boolean),
        falseCases: result.falseTriggerCases.map((c) => c.text).join('|'),
      });
    }
  }
}

console.log('\n=== 扫描汇总（唤醒率% / 误触次 / 折算每小时 / 二次唤醒） ===');
for (const row of rows) {
  console.log(
    `s=${row.score} t=${row.threshold} b=${row.trailingBlanks} → 唤醒 ${row.wakeRate}% 误触 ${row.falseTriggers}(${row.perHour}/h) 二次 ${row.doubleWake} ${row.pass ? 'PASS' : 'fail'} ${row.falseCases}`,
  );
}
