// 四判据汇总(asr-selfhost-eval 9.1→9.2 输入)。
// 读 results/{mimo,funasr,cpu-funasr}.json,产出 results/summary.json + 控制台
// markdown 表(直接进 docs/design/asr-selfhost-eval.md)。
// 用法:node scripts/asr-eval/summarize.mjs

import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { cer, hotwordHits, normalize, summarize } from './lib/textnorm.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const resultsDir = path.join(here, 'results');
const read = (p) => JSON.parse(fs.readFileSync(path.join(resultsDir, p), 'utf8'));

const cases = JSON.parse(fs.readFileSync(path.join(here, 'cases.json'), 'utf8'));
const caseById = new Map(cases.cases.map((c) => [c.id, c]));
const mimo = read('mimo.json');
const funasr = read('funasr.json');
const cpu = fs.existsSync(path.join(resultsDir, 'cpu-funasr.json')) ? read('cpu-funasr.json') : null;

const funasrById = new Map(funasr.records.map((r) => [r.id, r]));
const mimoById = new Map(mimo.records.map((r) => [r.id, r]));

// ---------- 判据① partial 首 token ----------
const ftFunasr = funasr.records.filter((r) => r.partial?.firstToken).map((r) => r.partial.firstToken);
const ftMimo = mimo.records.filter((r) => r.firstPartial).map((r) => r.firstPartial);
const c1 = {
  funasr: {
    n: ftFunasr.length,
    audioLeadMs: summarize(ftFunasr.map((x) => x.audioLeadMs)),
    computeMs: summarize(ftFunasr.map((x) => x.computeMs)),
    latencyMs: summarize(ftFunasr.map((x) => x.latencyMs)),
  },
  mimo: {
    n: ftMimo.length,
    audioLeadMs: { n: ftMimo.length, avg: 1500, min: 1500, max: 1500 },
    rttMs: summarize(ftMimo.map((x) => x.rttMs)),
    latencyMs: summarize(ftMimo.map((x) => x.latencyMs)),
  },
  noWindowCases: mimo.records.filter((r) => !r.firstPartial).map((r) => r.id),
};
const funasrLatencyAvg = c1.funasr.latencyMs.avg ?? Infinity;
const c1Pass = Number.isFinite(funasrLatencyAvg) && funasrLatencyAvg < 800;

// ---------- 判据② final CER ----------
const perCase = [];
for (const c of cases.cases) {
  const f = funasrById.get(c.id);
  const m = mimoById.get(c.id);
  if (!f?.final || !m?.final) continue;
  const cerPlain = cer(c.text, f.final.plainText);
  const cerHot = cer(c.text, f.final.hotText);
  const cerMimo = cer(c.text, m.final.text);
  perCase.push({
    id: c.id,
    category: c.category,
    ref: normalize(c.text),
    funasrPlain: cerPlain,
    funasrHot: cerHot,
    mimo: cerMimo,
    funasrPlainCER: +cerPlain.cer.toFixed(4),
    funasrHotCER: +cerHot.cer.toFixed(4),
    mimoCER: +cerMimo.cer.toFixed(4),
  });
}
const avgCER = (key) => summarize(perCase.map((p) => p[key]));
const c2 = {
  funasrPlainCER: avgCER('funasrPlainCER'),
  funasrHotCER: avgCER('funasrHotCER'),
  mimoCER: avgCER('mimoCER'),
  perCategory: {},
  perCase,
};
for (const cat of new Set(perCase.map((p) => p.category))) {
  c2.perCategory[cat] = {
    funasrPlainCER: summarize(perCase.filter((p) => p.category === cat).map((p) => p.funasrPlainCER)).avg,
    funasrHotCER: summarize(perCase.filter((p) => p.category === cat).map((p) => p.funasrHotCER)).avg,
    mimoCER: summarize(perCase.filter((p) => p.category === cat).map((p) => p.mimoCER)).avg,
  };
}
const c2Pass = (c2.funasrHotCER.avg ?? 1) <= (c2.mimoCER.avg ?? 0);

// ---------- 判据③ 热词命中 ----------
const hitRows = [];
for (const c of cases.cases) {
  if (!c.hotwords?.length) continue;
  const f = funasrById.get(c.id);
  const m = mimoById.get(c.id);
  if (!f?.final || !m?.final) continue;
  hitRows.push({
    id: c.id,
    hotwords: c.hotwords,
    funasrHot: hotwordHits(f.final.hotText, c.hotwords),
    funasrPlain: hotwordHits(f.final.plainText, c.hotwords),
    mimo: hotwordHits(m.final.text, c.hotwords),
    funasrHotText: f.final.hotText,
    funasrPlainText: f.final.plainText,
    mimoText: m.final.text,
  });
}
const rate = (rows, key) => {
  const total = rows.reduce((a, r) => a + r[key].total, 0);
  const exact = rows.reduce((a, r) => a + r[key].exactCount, 0);
  const fuzzy = rows.reduce((a, r) => a + r[key].fuzzyCount, 0);
  return { total, exact, fuzzy, exactRate: total ? +(exact / total).toFixed(3) : null, fuzzyRate: total ? +(fuzzy / total).toFixed(3) : null };
};
const c3 = {
  funasrHot: rate(hitRows, 'funasrHot'),
  funasrPlain: rate(hitRows, 'funasrPlain'),
  mimo: rate(hitRows, 'mimo'),
  rows: hitRows,
};
const c3Pass = (c3.funasrHot.exactRate ?? 0) > (c3.funasrPlain.exactRate ?? 0);

// ---------- 判据④ CPU ----------
const c4 = cpu
  ? {
      cmd: cpu.cmd,
      logicalCores: cpu.logicalCores,
      sysAvgPct: cpu.summary.sysAvgPct,
      sysMaxPct: cpu.summary.sysMaxPct,
      procCoresAvg: cpu.summary.procCoresAvg,
      procCoresMax: cpu.summary.procCoresMax,
      procShareOfMachineAvgPct: cpu.summary.procShareOfMachineAvgPct,
      note: '观测值:批处理单路顺序解码,含模型加载与探针循环;非长期服务稳态。',
    }
  : null;
const c4Pass = c4 ? (c4.procCoresMax ?? 99) <= 4 : null; // 单机 4c8G 参照:峰值不超过 4 核

// ---------- 输出 ----------
const summary = {
  generatedAt: new Date().toISOString(),
  criteria: {
    c1_partialFirstToken: { data: c1, pass: c1Pass, threshold: 'funasr partial 首响(audioLead+compute)< 800ms' },
    c2_finalCER: { data: c2, pass: c2Pass, threshold: 'funasr(热词)平均 CER ≤ mimo 同批 CER' },
    c3_hotwordHit: { data: c3, pass: c3Pass, threshold: 'seaco 热词组命中率 > 无热词组(显著)' },
    c4_cpu: { data: c4, pass: c4Pass, threshold: '解码峰值 ≤ 4 逻辑核(单机 4c8G 参照)' },
  },
  verdict: null,
};
summary.verdict =
  c1Pass && c2Pass && c3Pass && c4Pass
    ? 'pass-all'
    : c1Pass && c2Pass && c4Pass
      ? 'pass-partial(c3 不显著或数据不足)'
      : c2Pass && c4Pass
        ? 'pass-partial(partial 延迟不达标)'
        : 'fail';

fs.writeFileSync(path.join(resultsDir, 'summary.json'), JSON.stringify(summary, null, 2));

// markdown(贴决策文档用)
const md = [];
md.push('## 四判据实测数据(自动汇总 summarize.mjs)\n');
md.push(`### 判据① partial 首 token(阈值 < 800ms)→ ${c1Pass ? '过' : '不过'}\n`);
md.push('| 引擎 | 首响口径 | avg | min | max | n |');
md.push('|---|---|---|---|---|---|');
md.push(`| FunASR streaming | audioLead+compute | ${funasrLatencyAvg?.toFixed(0) ?? '-'}ms | ${c1.funasr.latencyMs.min ?? '-'}ms | ${c1.funasr.latencyMs.max ?? '-'}ms | ${c1.funasr.latencyMs.n} |`);
md.push(`| — 其中 audioLead(最短可出字音频) | | ${c1.funasr.audioLeadMs.avg?.toFixed(0) ?? '-'}ms | ${c1.funasr.audioLeadMs.min ?? '-'} | ${c1.funasr.audioLeadMs.max ?? '-'} | |`);
md.push(`| — 其中 compute | | ${c1.funasr.computeMs.avg?.toFixed(0) ?? '-'}ms | ${c1.funasr.computeMs.min ?? '-'} | ${c1.funasr.computeMs.max ?? '-'} | |`);
md.push(`| MiMo 云(假流式 1.5s 窗) | 1500ms 窗 + RTT | ${c1.mimo.latencyMs.avg?.toFixed(0) ?? '-'}ms | ${c1.mimo.latencyMs.min ?? '-'}ms | ${c1.mimo.latencyMs.max ?? '-'}ms | ${c1.mimo.latencyMs.n} |\n`);
md.push(`### 判据② final 准确率(平均 CER,越低越好)→ ${c2Pass ? '不劣于 MiMo:过' : '不过'}\n`);
md.push('| 组 | 平均 CER | n |');
md.push('|---|---|---|');
md.push(`| FunASR seaco 无热词 | ${c2.funasrPlainCER.avg?.toFixed(3)} | ${c2.funasrPlainCER.n} |`);
md.push(`| FunASR seaco 热词 | ${c2.funasrHotCER.avg?.toFixed(3)} | ${c2.funasrHotCER.n} |`);
md.push(`| MiMo 云(hints) | ${c2.mimoCER.avg?.toFixed(3)} | ${c2.mimoCER.n} |\n`);
md.push('| 类别 | FunASR 无热词 | FunASR 热词 | MiMo |');
md.push('|---|---|---|---|');
for (const [cat, v] of Object.entries(c2.perCategory)) {
  md.push(`| ${cat} | ${v.funasrPlainCER?.toFixed(3)} | ${v.funasrHotCER?.toFixed(3)} | ${v.mimoCER?.toFixed(3)} |`);
}
md.push('');
md.push(`### 判据③ 热词命中(${hitRows.length} 例含热词)→ ${c3Pass ? '过' : '不过'}\n`);
md.push('| 组 | 精确命中/总 | 命中率 | 含容错窗(±20%) |');
md.push('|---|---|---|---|');
md.push(`| seaco 热词 | ${c3.funasrHot.exact}/${c3.funasrHot.total} | ${c3.funasrHot.exactRate} | ${c3.funasrHot.fuzzyRate} |`);
md.push(`| seaco 无热词 | ${c3.funasrPlain.exact}/${c3.funasrPlain.total} | ${c3.funasrPlain.exactRate} | ${c3.funasrPlain.fuzzyRate} |`);
md.push(`| MiMo(hints) | ${c3.mimo.exact}/${c3.mimo.total} | ${c3.mimo.exactRate} | ${c3.mimo.fuzzyRate} |\n`);
if (c4) {
  md.push(`### 判据④ CPU 占用(观测)→ ${c4Pass ? '可接受' : '不可接受'}\n`);
  md.push(`- 解码进程峰值 ${c4.procCoresMax?.toFixed(2)} 逻辑核,平均 ${c4.procCoresAvg?.toFixed(2)} 核(整机 ${c4.logicalCores} 核);系统负载峰值 ${c4.sysMaxPct}%,均值 ${c4.sysAvgPct}%。`);
  md.push(`- ${c4.note}\n`);
}
md.push('### 逐例 CER 明细\n');
md.push('| id | 类别 | ref(归一) | FunASR热词/hyp | FunASR无热词 CER | FunASR热词 CER | MiMo CER |');
md.push('|---|---|---|---|---|---|---|');
for (const p of perCase) {
  md.push(`| ${p.id} | ${p.category} | ${p.ref} | ${p.funasrHot.hyp} | ${p.funasrPlainCER} | ${p.funasrHotCER} | ${p.mimoCER} |`);
}
const mdText = md.join('\n');
fs.writeFileSync(path.join(resultsDir, 'summary.md'), mdText + '\n');
console.log(mdText);
console.log(`\nverdict = ${summary.verdict}`);
