#!/usr/bin/env node
// director-rewrite 步骤一的对拍守卫（openspec/changes/director-rewrite
// tasks 1.1）：把 client/assets/live2d/operator-live-state.js（老页面事件
// 模型，IIFE 挂 globalThis.OperatorLiveState）与
// console/src/director/event-model.ts（React 移植版，经 esbuild 转译）放进
// 同一进程，对同一批草稿场景逐字段比较 —— 校验结果与 toEventPayload 的
// JSON 必须完全相等（byte-equal payload shapes）。
//
// 用法：node scripts/check-director-event-model.mjs（pr 档自动执行）。

import { readFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import vm from 'node:vm';
import { createRequire } from 'node:module';

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const require = createRequire(import.meta.url);

// —— 老模型：IIFE 在沙箱里执行，挂到 sandbox.OperatorLiveState ——
const legacySource = readFileSync(
  join(repoRoot, 'client', 'assets', 'live2d', 'operator-live-state.js'),
  'utf8',
);
const sandbox = { globalThis: {}, console };
sandbox.globalThis = sandbox;
vm.createContext(sandbox);
vm.runInContext(legacySource, sandbox, { filename: 'operator-live-state.js' });
const legacy = sandbox.OperatorLiveState;
if (!legacy) throw new Error('legacy OperatorLiveState missing');

// —— 新模型：esbuild（console 的既有 devDependency）转译后导入 ——
const esbuild = require(join(repoRoot, 'console', 'node_modules', 'esbuild'));
const bundled = await esbuild.build({
  entryPoints: [join(repoRoot, 'console', 'src', 'director', 'event-model.ts')],
  bundle: true,
  format: 'esm',
  write: false,
  logLevel: 'silent',
});
const modernUrl = `data:text/javascript;base64,${Buffer.from(bundled.outputFiles[0].text).toString('base64')}`;
const modern = await import(modernUrl);

let checks = 0;
function same(label, left, right) {
  checks += 1;
  const a = JSON.stringify(left);
  const b = JSON.stringify(right);
  if (a !== b) {
    console.error(`MISMATCH ${label}:\n  legacy: ${a}\n  modern: ${b}`);
    process.exit(1);
  }
}

// 1. 18 个事件定义逐键一致（roles 结构新模型为 {role,label}，单独映射比较）。
const legacyDefs = legacy.eventDefinitions;
const modernDefs = modern.eventDefinitions;
same('definition keys', Object.keys(legacyDefs).sort(), Object.keys(modernDefs).sort());
for (const [key, def] of Object.entries(legacyDefs)) {
  same(`definition.${key}`, { ...def, roles: def.roles }, {
    ...modernDefs[key],
    roles: modernDefs[key].roles.map(({ role, label }) => [role, label]),
  });
}

// 2. 场景批：同一串动作走两套 updateDraft，比较整份草稿与载荷 JSON。
function scenario(actions, payloadContext, postProcess) {
  let legacyDraft = legacy.createDraft({ matchId: 'demo', teamId: 'home' });
  let modernDraft = modern.createDraft({ matchId: 'demo', teamId: 'home' });
  for (const action of actions) {
    legacyDraft = legacy.updateDraft(legacyDraft, action);
    modernDraft = modern.updateDraft(modernDraft, structuredClone(action));
  }
  const left = postProcess ? postProcess(legacyDraft) : legacyDraft;
  const right = postProcess ? postProcess(structuredClone(modernDraft)) : structuredClone(modernDraft);
  same(`draft:${actions.map((a) => a.type).join('>')}`, left, right);
  return { legacyDraft, modernDraft };
}

const ctx = { score: { home: 1, away: 0 }, homeTeam: '西班牙', awayTeam: '德国' };

// 场景 A：进球（参与人槽位 + 强制参与人 + 载荷）。
{
  const actions = [
    { type: 'select_event', eventType: 'goal', period: 'first_half', elapsedSeconds: 735, clockVersion: 4 },
    { type: 'set_field', field: 'description', value: '禁区抢点破门' },
    { type: 'set_participant', role: 'scorer', name: '佩德里', teamId: 'home', teamName: '西班牙' },
    { type: 'set_participant', role: 'assist', name: '亚马尔', teamId: 'home', teamName: '西班牙' },
  ];
  const { legacyDraft, modernDraft } = scenario(actions, ctx);
  same('payload:goal', legacy.toEventPayload(legacyDraft, ctx), modern.toEventPayload(modernDraft, ctx));
}

// 场景 B：无队伍的战术变化 + 备注（playerOptional 路径）。
{
  const actions = [
    { type: 'select_event', eventType: 'tactical_shift', period: 'second_half', elapsedSeconds: 3600, clockVersion: 9 },
    { type: 'set_field', field: 'description', value: '阵型收缩' },
    { type: 'set_participant', role: 'leader', name: '法比安' },
  ];
  scenario(actions, ctx);
}

// 场景 C：比分更正（scoreOverride + 更正原因 + quiet 通道；老页面的比分
// 更正同样必须带事件描述，模板「人工核对后更正当前比分。」即为此）。
{
  const actions = [
    { type: 'select_event', eventType: 'score_correction', period: 'second_half', elapsedSeconds: 3900, clockVersion: 12 },
    { type: 'set_field', field: 'description', value: '人工核对后更正当前比分。' },
    { type: 'set_field', field: 'scoreOverride', value: { home: 2, away: 0 } },
    { type: 'set_field', field: 'scoreBefore', value: { home: 1, away: 0 } },
    { type: 'set_field', field: 'correctionReason', value: '人工核对记分牌' },
  ];
  const { legacyDraft, modernDraft } = scenario(actions, ctx);
  same('payload:score_correction', legacy.toEventPayload(legacyDraft, ctx), modern.toEventPayload(modernDraft, ctx));
}

// 场景 D：校验错误一一对应（缺行为/缺参与人/换人跨队/空描述）。
{
  const cases = [
    [],
    [{ type: 'select_event', eventType: 'penalty' }],
    [{ type: 'select_event', eventType: 'substitution', period: 'first_half' }, { type: 'set_field', field: 'description', value: '换人' },
     { type: 'set_participant', role: 'sub_on', name: 'A', teamId: 'home' }, { type: 'set_participant', role: 'sub_off', name: 'B', teamId: 'away' }],
    [{ type: 'select_event', eventType: 'goal', period: 'first_half' }, { type: 'set_field', field: 'description', value: '  ' }],
  ];
  for (const actions of cases) {
    let legacyDraft = legacy.createDraft({ matchId: 'demo', teamId: 'home' });
    let modernDraft = modern.createDraft({ matchId: 'demo', teamId: 'home' });
    for (const action of actions) {
      legacyDraft = legacy.updateDraft(legacyDraft, action);
      modernDraft = modern.updateDraft(modernDraft, structuredClone(action));
    }
    same(`validate:${actions.map((a) => a.type).join('>')}`, legacy.validateDraft(legacyDraft), modern.validateDraft(modernDraft));
  }
}

// 场景 E：工具函数（formatClock / roleTeamId / roleAllowsMultiple / roleLabel）。
for (const seconds of [0, 59, 60, 754, null, undefined]) {
  same(`formatClock:${seconds}`, legacy.formatClock(seconds), modern.formatClock(seconds));
}
for (const [eventType, role, teamId] of [
  ['goal', 'defender', 'home'], ['goal', 'scorer', 'home'], ['goal', 'scorer', 'away'], ['goal', 'scorer', ''], ['penalty', 'keeper', 'away'],
]) {
  same(`roleTeamId:${eventType}:${role}:${teamId}`, legacy.roleTeamId(eventType, role, teamId), modern.roleTeamId(eventType, role, teamId));
  same(`roleAllowsMultiple:${eventType}:${role}`, legacy.roleAllowsMultiple(eventType, role), modern.roleAllowsMultiple(eventType, role));
  same(`roleLabel:${eventType}:${role}`, legacy.roleLabel(eventType, role), modern.roleLabel(eventType, role));
}

console.log(`director event model parity: ${checks} checks ok`);
