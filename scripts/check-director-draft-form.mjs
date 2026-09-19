#!/usr/bin/env node
// 导演页草稿↔表单映射纯 module 单测（openspec/changes/director-draft-form）。
//
// esbuild 转译 console/src/director/draft-form.ts（与 check-director-event-model.mjs
// 同一模式，零测试框架依赖），表驱动覆盖注释里记过 bug 的每类场景：
// preserveForm 回填、提交合并的静默丢失防护（手输时钟/主参与人）、
// __quiet__ 哨兵解码、provisional→pending、服务端锚定时钟插值。
//
// 用法：node scripts/check-director-draft-form.mjs（pr 档自动执行）。

import assert from 'node:assert/strict';
import test from 'node:test';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { createRequire } from 'node:module';

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const require = createRequire(import.meta.url);

const esbuild = require(join(repoRoot, 'console', 'node_modules', 'esbuild'));
async function bundleTs(entry) {
  const bundled = await esbuild.build({
    entryPoints: [join(repoRoot, 'console', 'src', 'director', entry)],
    bundle: true,
    format: 'esm',
    write: false,
    logLevel: 'silent',
  });
  return import(`data:text/javascript;base64,${Buffer.from(bundled.outputFiles[0].text).toString('base64')}`);
}
const mod = await bundleTs('draft-form.ts');
const model = await bundleTs('event-model.ts');
const { draftFromEvent, elapsedClockSeconds, formFromDraft, mergedDraftForSubmit } = mod;

// —— 事件模型辅助：造一个带行为与参与人的草稿 ——
function baseDraft() {
  let draft = model.createDraft({ matchId: 'demo', teamId: 'home' });
  draft = model.updateDraft(draft, { type: 'select_event', eventType: 'goal', period: 'first_half', elapsedSeconds: 735, clockVersion: 4 });
  draft = model.updateDraft(draft, { type: 'set_field', field: 'description', value: '禁区抢点破门' });
  draft = model.updateDraft(draft, { type: 'set_participant', role: 'scorer', name: '佩德里', teamId: 'home', teamName: '西班牙' });
  return draft;
}

test('elapsedClockSeconds：未运行回服务器值，运行中按锚点插值并钳非负', () => {
  assert.equal(elapsedClockSeconds({ elapsedSeconds: 735, running: false, anchorAt: null }, 1_000), 735);
  const anchor = '2026-09-19T00:00:30.000Z';
  const now = Date.parse('2026-09-19T00:01:00.000Z');
  assert.equal(elapsedClockSeconds({ elapsedSeconds: 600, running: true, anchorAt: anchor }, now), 630);
  // 锚点在未来：钳到 0
  assert.equal(elapsedClockSeconds({ elapsedSeconds: 10, running: true, anchorAt: anchor }, Date.parse('2026-09-19T00:00:00.000Z')), 0);
  // 非法锚点：退回服务器值
  assert.equal(elapsedClockSeconds({ elapsedSeconds: 88, running: true, anchorAt: 'not-a-date' }, now), 88);
});

test('formFromDraft：草稿全字段映射，pending/确认/比分覆盖/主参与人', () => {
  const form = formFromDraft(baseDraft());
  assert.equal(form.occurredClock, '12:15');
  assert.equal(form.factStatus, 'confirmed');
  assert.equal(form.description, '禁区抢点破门');
  assert.equal(form.mainPlayer, '佩德里');
  assert.equal(form.scoreHome, '');
  assert.equal(form.mode, 'auto');

  const pending = formFromDraft(model.updateDraft(baseDraft(), { type: 'set_field', field: 'factStatus', value: 'pending' }));
  assert.equal(pending.factStatus, 'pending');

  const quiet = formFromDraft(model.updateDraft(baseDraft(), { type: 'set_field', field: 'deliveryMode', value: 'quiet' }));
  assert.equal(quiet.mode, 'quiet');
});

test('mergedDraftForSubmit：表单字段逐项落草稿，pending→provisional', () => {
  const draft = baseDraft();
  const working = mergedDraftForSubmit({
    draft,
    form: { ...formFromDraft(draft), description: '改口：补射破门', mode: 'manual', proactive: '佩德里进球！' },
    factStatus: 'confirmed',
    fallbackSide: 'home',
    homeTeam: '西班牙',
    awayTeam: '德国',
  });
  assert.equal(working.description, '改口：补射破门');
  assert.equal(working.proactiveText, '佩德里进球！');
  assert.equal(working.deliveryMode, 'manual');
  assert.equal(working.factStatus, 'confirmed');
});

test('mergedDraftForSubmit：自动话术模式 proactive 落空串', () => {
  const draft = baseDraft();
  const working = mergedDraftForSubmit({
    draft,
    form: { ...formFromDraft(draft), mode: 'auto', proactive: '不该出现' },
    factStatus: 'confirmed',
    fallbackSide: 'home',
    homeTeam: '西班牙',
    awayTeam: '德国',
  });
  assert.equal(working.proactiveText, '');
});

test('mergedDraftForSubmit：pending 候选上线即 provisional（老页面语义）', () => {
  const draft = baseDraft();
  const working = mergedDraftForSubmit({
    draft,
    form: formFromDraft(draft),
    factStatus: 'pending',
    fallbackSide: 'home',
    homeTeam: '西班牙',
    awayTeam: '德国',
  });
  assert.equal(working.factStatus, 'provisional');
});

test('mergedDraftForSubmit：手输时钟落回草稿（静默丢失防护），非法格式保留原值', () => {
  const draft = baseDraft();
  const withClock = mergedDraftForSubmit({
    draft,
    form: { ...formFromDraft(draft), occurredClock: '45:30' },
    factStatus: 'confirmed',
    fallbackSide: 'home',
    homeTeam: '西班牙',
    awayTeam: '德国',
  });
  assert.equal(withClock.occurredSeconds, 45 * 60 + 30);

  const kept = mergedDraftForSubmit({
    draft,
    form: { ...formFromDraft(draft), occurredClock: '垃圾' },
    factStatus: 'confirmed',
    fallbackSide: 'home',
    homeTeam: '西班牙',
    awayTeam: '德国',
  });
  assert.equal(kept.occurredSeconds, draft.occurredSeconds);
});

test('mergedDraftForSubmit：无参与人时主参与人回填；已有参与人不覆盖', () => {
  let draft = model.createDraft({ matchId: 'demo', teamId: null });
  draft = model.updateDraft(draft, { type: 'select_event', eventType: 'kickoff', period: 'first_half', elapsedSeconds: 0, clockVersion: 1 });
  const backfilled = mergedDraftForSubmit({
    draft,
    form: { ...formFromDraft(draft), mainPlayer: ' 穆西亚拉 ' },
    factStatus: 'confirmed',
    fallbackSide: 'away',
    homeTeam: '西班牙',
    awayTeam: '德国',
  });
  assert.equal(backfilled.primaryParticipant?.name, '穆西亚拉');
  assert.equal(backfilled.primaryParticipant?.teamId, 'away');
  // 注意：teamId 取 fallbackSide，但 teamName 按回填前的 teamId 判断——
  // 原组件即如此（提取保真），与旧 operator.html 行为一致，待另行修正。
  assert.equal(backfilled.primaryParticipant?.teamName, '西班牙');

  const withScorer = baseDraft();
  const untouched = mergedDraftForSubmit({
    draft: withScorer,
    form: { ...formFromDraft(withScorer), mainPlayer: '不该覆盖' },
    factStatus: 'confirmed',
    fallbackSide: 'home',
    homeTeam: '西班牙',
    awayTeam: '德国',
  });
  assert.equal(untouched.primaryParticipant?.name, '佩德里');
});

test('mergedDraftForSubmit：仅比分更正落 scoreOverride，其他事件不落', () => {
  let draft = model.createDraft({ matchId: 'demo', teamId: 'home' });
  draft = model.updateDraft(draft, { type: 'select_event', eventType: 'score_correction', period: 'second_half', elapsedSeconds: 3900, clockVersion: 9 });
  const corrected = mergedDraftForSubmit({
    draft,
    form: { ...formFromDraft(draft), scoreHome: '2', scoreAway: '1' },
    factStatus: 'confirmed',
    fallbackSide: 'home',
    homeTeam: '西班牙',
    awayTeam: '德国',
  });
  assert.deepEqual(corrected.scoreOverride, { home: 2, away: 1 });

  const goal = mergedDraftForSubmit({
    draft: baseDraft(),
    form: { ...formFromDraft(baseDraft()), scoreHome: '9', scoreAway: '9' },
    factStatus: 'confirmed',
    fallbackSide: 'home',
    homeTeam: '西班牙',
    awayTeam: '德国',
  });
  // 草稿模型的 scoreOverride 默认 null（非 undefined）。
  assert.equal(goal.scoreOverride, null);
});

test('draftFromEvent：__quiet__ 解码为只记事实，普通话术为 manual，空为 auto', () => {
  const clock = { period: 'second_half', elapsedSeconds: 3000, version: 7 };
  const quiet = draftFromEvent({
    event: { id: 'e1', eventType: 'goal', proactiveText: '__quiet__', participants: [] },
    matchId: 'demo',
    clock,
  });
  assert.equal(quiet.proactiveText, '');
  assert.equal(quiet.deliveryMode, 'quiet');

  const manual = draftFromEvent({
    event: { id: 'e2', eventType: 'goal', proactiveText: '好球！', participants: [] },
    matchId: 'demo',
    clock,
  });
  assert.equal(manual.proactiveText, '好球！');
  assert.equal(manual.deliveryMode, 'manual');

  const auto = draftFromEvent({
    event: { id: 'e3', eventType: 'save', participants: [] },
    matchId: 'demo',
    clock,
  });
  assert.equal(auto.deliveryMode, 'auto');
});

test('draftFromEvent：provisional→pending、参与人带回 resolved、revisionOf 锚定', () => {
  const loaded = draftFromEvent({
    event: {
      id: 'e9',
      eventType: 'yellow_card',
      period: 'second_half',
      teamId: 'away',
      description: '战术犯规',
      factStatus: 'provisional',
      recommendedAction: 'complain',
      intensity: 4,
      participants: [{ role: 'offender', name: '穆西亚拉', teamId: 'away', teamName: '德国' }],
    },
    matchId: 'demo',
    clock: { period: 'fulltime', elapsedSeconds: 5400, version: 12 },
  });
  assert.equal(loaded.factStatus, 'pending');
  assert.equal(loaded.eventType, 'yellow_card');
  assert.equal(loaded.teamId, 'away');
  assert.equal(loaded.occurredPeriod, 'second_half');
  assert.equal(loaded.occurredSeconds, 5400);
  assert.equal(loaded.capturedClockVersion, 12);
  assert.equal(loaded.recommendedAction, 'complain');
  assert.equal(loaded.intensity, 4);
  assert.equal(loaded.revisionOf, 'e9');
  assert.deepEqual(loaded.participants, [
    { role: 'offender', name: '穆西亚拉', teamId: 'away', teamName: '德国', resolved: true },
  ]);
  assert.equal(loaded.description, '战术犯规');
});

console.log('director draft-form checks: done');
