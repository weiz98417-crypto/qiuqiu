// 文本归一化 + CER(asr-eval)。规则对 ref/hyp 两边一致施加,保证引擎间相对可比。
// 归一化步骤:小写 → 已知外文球员名别名归到中文规范形 → 去标点/空白 →
// 阿拉伯数字逐字映射为中文数字字(0→〇..9→九,不加语法)。
// 已知局限记录在 docs/design/asr-selfhost-eval.md。

const ALIASES = [
  // 外文规范形:ASR 可能输出英文或中文译名,统一归到中文规范形再比。
  [/\bbellingham\b/g, '贝林厄姆'],
  [/\bpedri\b/g, '佩德里'],
  [/\bmusiala\b/g, '穆西亚拉'],
  [/\byamal\b/g, '亚马尔'],
  [/\brodrygo\b/g, '罗德里戈'],
  [/\bvinicius\b/g, '维尼修斯'],
  // 常见译名变体归一(仅影响本用例集覆盖的名字)。
  [/贝林汉姆/g, '贝林厄姆'],
  [/比林厄姆/g, '贝林厄姆'],
  [/法比安·鲁伊斯|法比安鲁依斯/g, '法比安鲁伊斯'],
];

const DIGITS = { 0: '〇', 1: '一', 2: '二', 3: '三', 4: '四', 5: '五', 6: '六', 7: '七', 8: '八', 9: '九' };

export function normalize(text) {
  let s = String(text ?? '').toLowerCase();
  for (const [re, to] of ALIASES) s = s.replace(re, to);
  s = s.replace(/[^\p{L}\p{N}]+/gu, ''); // 去标点/空白(含全角)
  s = s.replace(/[0-9]/g, (d) => DIGITS[d]);
  return s;
}

/** Levenshtein 距离(字符级,滚动数组,够用即可)。 */
export function levenshtein(a, b) {
  const m = a.length;
  const n = b.length;
  if (m === 0) return n;
  if (n === 0) return m;
  let prev = new Array(n + 1);
  let curr = new Array(n + 1);
  for (let j = 0; j <= n; j++) prev[j] = j;
  for (let i = 1; i <= m; i++) {
    curr[0] = i;
    for (let j = 1; j <= n; j++) {
      const cost = a[i - 1] === b[j - 1] ? 0 : 1;
      curr[j] = Math.min(prev[j] + 1, curr[j - 1] + 1, prev[j - 1] + cost);
    }
    [prev, curr] = [curr, prev];
  }
  return prev[n];
}

/** CER = editDistance(ref, hyp)/len(ref)(归一化后)。 */
export function cer(refText, hypText) {
  const ref = normalize(refText);
  const hyp = normalize(hypText);
  if (ref.length === 0) return { cer: hyp.length === 0 ? 0 : 1, ref, hyp, distance: hyp.length };
  const distance = levenshtein(ref, hyp);
  return { cer: distance / ref.length, ref, hyp, distance };
}

/** 热词命中:归一化后精确子串 + 容错窗(窗长=name,编辑距离≤tol)。 */
export function hotwordHits(hypText, hotwords, toleranceRatio = 0.2) {
  const hyp = normalize(hypText);
  const detail = hotwords.map((name) => {
    const n = normalize(name);
    let hit = hyp.includes(n);
    let fuzzy = hit;
    if (!hit && n.length > 1) {
      const tol = Math.max(1, Math.floor(n.length * toleranceRatio));
      for (let start = 0; start + n.length <= hyp.length && !fuzzy; start++) {
        const window = hyp.slice(start, start + n.length);
        if (levenshtein(n, window) <= tol) fuzzy = true;
      }
    }
    return { name, exact: hit, fuzzy };
  });
  const exactCount = detail.filter((d) => d.exact).length;
  const fuzzyCount = detail.filter((d) => d.fuzzy).length;
  return {
    detail,
    exactCount,
    fuzzyCount,
    total: hotwords.length,
    exactRate: hotwords.length ? exactCount / hotwords.length : null,
    fuzzyRate: hotwords.length ? fuzzyCount / hotwords.length : null,
  };
}

/** 复刻 backend/internal/asr/session.go mergeTranscript(partial 合并口径)。 */
export function mergeTranscript(existing, incoming) {
  existing = (existing ?? '').trim();
  incoming = (incoming ?? '').trim();
  if (!existing) return incoming;
  if (!incoming || existing.endsWith(incoming)) return existing;
  if (incoming.startsWith(existing)) return incoming;
  const e = [...existing];
  const inc = [...incoming];
  const limit = Math.min(e.length, inc.length);
  for (let overlap = limit; overlap > 0; overlap--) {
    if (e.slice(e.length - overlap).join('') === inc.slice(0, overlap).join('')) {
      return existing + incoming.slice(overlap);
    }
  }
  return existing + incoming;
}

/** 简单统计。 */
export function summarize(values) {
  const xs = values.filter((v) => Number.isFinite(v)).slice().sort((a, b) => a - b);
  if (!xs.length) return { n: 0 };
  const sum = xs.reduce((a, b) => a + b, 0);
  return {
    n: xs.length,
    avg: sum / xs.length,
    min: xs[0],
    max: xs[xs.length - 1],
    p50: xs[Math.floor(xs.length / 2)],
    p90: xs[Math.min(xs.length - 1, Math.floor(xs.length * 0.9))],
  };
}
