import { readFile } from 'node:fs/promises';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

// presentation-map.json consistency check (ADR-0007 single source, offline
// tier): the JSON must parse, the expression/motion key sets must be
// non-empty and resolve into the Live2D model asset, and every performance
// string "expression/motion" (acts/events/phases/delivery) must reference
// keys that exist in those two tables. The Dart contract test locks the
// client constants to the same JSON (three-way with the model asset).
const repoRoot = join(dirname(fileURLToPath(import.meta.url)), '..');
const mapPath = join(repoRoot, 'client', 'assets', 'live2d', 'models', 'qiuqiu', 'presentation-map.json');
const modelPath = join(repoRoot, 'client', 'assets', 'live2d', 'models', 'qiuqiu', 'female_01Arkit_6.model3.json');

const map = JSON.parse(await readFile(mapPath, 'utf8'));
const model = JSON.parse(await readFile(modelPath, 'utf8'));
const failures = [];

const expressions = map.expressions ?? {};
const motions = map.motions ?? {};
if (!Object.keys(expressions).length) failures.push('expressions table is empty');
if (!Object.keys(motions).length) failures.push('motions table is empty');

const modelMotionGroups = model?.FileReferences?.Motions ?? {};
const modelExpressionCount = (model?.FileReferences?.Expressions ?? []).length;

// Alias layers of the client contract (mirrors of
// client/lib/services/presentation_state.dart and
// backend/internal/relationship/presentation_vocabulary.go): performance
// strings may use legacy names, e.g. events.var_check "tense/tense" or
// delivery.interrupted "confused/listening" (whose motion names live outside
// the motions section and absorb into the listen/idle groups).
const expressionAliases = { low: 'sad', tense: 'nervous', deflated: 'sad' };
const motionAliases = {
  hold: 'focus', settle: 'idle', slump: 'idle', nod: 'agree',
  listening: 'listen',
  confused: 'idle',
};

function resolvesExpression(name) {
  if (name in expressions) return true;
  const alias = expressionAliases[name];
  return alias !== undefined && alias in expressions;
}

function resolvesMotion(name) {
  const canonical = motionAliases[name] ?? name;
  if (canonical in motions) return true;
  return canonical in modelMotionGroups; // group names: the surfaces pick the variant
}

for (const [name, index] of Object.entries(expressions)) {
  if (!Number.isInteger(index) || index < 0 || index >= modelExpressionCount) {
    failures.push(`expression "${name}" index ${index} is outside the model's ${modelExpressionCount} expression files`);
  }
}

for (const [name, motion] of Object.entries(motions)) {
  const group = modelMotionGroups[motion?.group];
  if (!Array.isArray(group)) {
    failures.push(`motion "${name}" targets unknown model group "${motion?.group}"`);
  } else if (!Number.isInteger(motion?.variant) || motion.variant < 0 || motion.variant >= group.length) {
    failures.push(`motion "${name}" variant ${motion?.variant} is outside group "${motion.group}" (${group.length} motions)`);
  }
}

// The empty expression file (index 0) must only carry neutral faces
// (design contract test 3: thinking must not render no face).
const neutralExpressions = new Set(['focus', 'idle', 'listening']);
for (const [name, index] of Object.entries(expressions)) {
  if (index === 0 && !neutralExpressions.has(name)) {
    failures.push(`expression "${name}" binds the empty expression file (index 0)`);
  }
}

// Performance strings: "expression/motion" with both keys resolvable.
const performanceRows = [];
for (const [act, row] of Object.entries(map.acts ?? {})) {
  for (const [quadrant, raw] of Object.entries(row ?? {})) {
    if (quadrant === 'energyDelta') continue;
    performanceRows.push([`acts.${act}.${quadrant}`, raw]);
  }
}
for (const [key, raw] of Object.entries(map.events ?? {})) performanceRows.push([`events.${key}`, raw]);
for (const [key, raw] of Object.entries(map.phases ?? {})) performanceRows.push([`phases.${key}`, raw]);
for (const [key, raw] of Object.entries(map.delivery ?? {})) performanceRows.push([`delivery.${key}`, raw]);
for (const [key, raw] of performanceRows) {
  if (raw === 'affect-idle-tier') continue; // client idle tier picker owns this phase
  const parts = typeof raw === 'string' ? raw.split('/') : [];
  if (parts.length !== 2) {
    failures.push(`${key}: "${raw}" is not an "expression/motion" performance string`);
    continue;
  }
  const [expression, motion] = parts;
  if (!resolvesExpression(expression)) {
    failures.push(`${key}: expression "${expression}" is not in the expressions table`);
  }
  if (!resolvesMotion(motion)) {
    failures.push(`${key}: motion "${motion}" is not in the motions table`);
  }
}

if (failures.length) {
  throw new Error(`presentation-map.json failed validation:\n  - ${failures.join('\n  - ')}`);
}
console.log(
  `presentation-map.json OK: ${Object.keys(expressions).length} expressions, ` +
  `${Object.keys(motions).length} motions, all performance rows resolvable`,
);
