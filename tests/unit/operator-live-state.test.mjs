import assert from 'node:assert/strict';
import test from 'node:test';

await import('../../client/assets/live2d/operator-live-state.js');

const state = globalThis.OperatorLiveState;

test('switching teams clears the other team player and roles', () => {
  let draft = state.createDraft({ occurredPeriod: 'first_half', occurredSeconds: 600 });
  draft = state.updateDraft(draft, { type: 'select_team', teamId: 'away' });
  draft = state.updateDraft(draft, { type: 'select_event', eventType: 'goal', period: 'first_half', elapsedSeconds: 600, clockVersion: 1 });
  draft = state.updateDraft(draft, { type: 'select_player', teamId: 'away', teamName: '德国', name: '哈弗茨' });
  draft = state.updateDraft(draft, { type: 'select_team', teamId: 'home' });
  assert.equal(draft.teamId, 'home');
  assert.equal(draft.primaryParticipant, null);
  assert.deepEqual(draft.participants, []);
});

test('switching behavior removes goal-only state', () => {
  let draft = state.createDraft({ teamId: 'home' });
  draft = state.updateDraft(draft, { type: 'select_player', teamId: 'home', teamName: '西班牙', name: '佩德里' });
  draft = state.updateDraft(draft, { type: 'select_event', eventType: 'goal', period: 'first_half', elapsedSeconds: 741, clockVersion: 2 });
  draft = state.updateDraft(draft, { type: 'set_field', field: 'description', value: '佩德里进球。' });
  draft = state.updateDraft(draft, { type: 'set_field', field: 'proactiveText', value: '进了！' });
  draft = state.updateDraft(draft, { type: 'select_event', eventType: 'yellow_card', period: 'first_half', elapsedSeconds: 750, clockVersion: 2 });
  assert.equal(draft.eventType, 'yellow_card');
  assert.equal(draft.description, '');
  assert.equal(draft.proactiveText, '');
  assert.equal(draft.recommendedAction, 'complain');
  assert.deepEqual(draft.participants.map((item) => item.role), ['offender']);
});

test('clock synchronization updates only an empty draft', () => {
  let empty = state.createDraft({ teamId: 'home' });
  assert.equal(state.hasDraftContent(empty), false);
  empty = state.updateDraft(empty, {
    type: 'sync_clock', period: 'first_half', elapsedSeconds: 720, clockVersion: 4,
  });
  assert.equal(empty.occurredPeriod, 'first_half');
  assert.equal(empty.occurredSeconds, 720);
  assert.equal(empty.capturedClockVersion, 4);

  let selected = state.updateDraft(empty, {
    type: 'select_event', eventType: 'shot', period: 'first_half', elapsedSeconds: 721, clockVersion: 4,
  });
  assert.equal(state.hasDraftContent(selected), true);
  selected = state.updateDraft(selected, {
    type: 'sync_clock', period: 'first_half', elapsedSeconds: 900, clockVersion: 5,
  });
  assert.equal(selected.occurredSeconds, 721);
  assert.equal(selected.capturedClockVersion, 4);
});

test('voice input fills empty fields but reports conflicting draft values', () => {
  const current = state.createDraft({ teamId: 'home', eventType: 'shot', description: '佩德里射门。' });
  const result = state.applyVoiceDraft(current, {
    source: 'operator_voice',
    teamId: 'away',
    eventType: 'goal',
    description: '哈弗茨进球。',
    transcript: '德国进球了',
    participants: [{ role: 'scorer', name: '哈弗茨', teamId: 'away', resolved: true }],
  });
  assert.equal(result.draft.teamId, 'home');
  assert.equal(result.draft.eventType, 'shot');
  assert.equal(result.draft.transcript, '德国进球了');
  assert.deepEqual(result.conflicts.map((item) => item.field), ['teamId', 'eventType', 'description']);
});

test('voice conflicts require an explicit choice before publishing', () => {
  const existing = state.createDraft({
    teamId: 'home',
    eventType: 'shot',
    description: '主队射门。',
    participants: [{ role: 'shooter', name: '佩德里', teamId: 'home', teamName: '西班牙', resolved: true }],
  });
  const merged = state.applyVoiceDraft(existing, {
    teamId: 'away',
    teamName: '德国',
    eventType: 'goal',
    description: '客队进球。',
  });

  assert.equal(merged.conflicts.length, 3);
  const appliedTeam = state.applyVoiceConflict(merged.draft, merged.conflicts.find((item) => item.field === 'teamId'));
  assert.equal(appliedTeam.teamId, 'away');
  assert.equal(appliedTeam.teamName, '德国');
  assert.ok(appliedTeam.inferredFields.includes('teamId'));
});

test('fact corrections require an auditable reason', () => {
  const correction = state.createDraft({
    teamId: 'home',
    eventType: 'shot',
    description: '佩德里射门偏出。',
    revisionOf: 'evt_1',
    participants: [{ role: 'shooter', name: '佩德里', teamId: 'home', teamName: '西班牙', resolved: true }],
  });
  assert.equal(state.validateDraft(correction).ready, false);

  correction.correctionReason = '原记录误写为射正，按回放修正。';
  assert.equal(state.validateDraft(correction).ready, true);
  const payload = state.toEventPayload(correction, { score: { home: 0, away: 0 }, homeTeam: '西班牙', awayTeam: '德国' });
  assert.equal(payload.evidence.correctionReason, '原记录误写为射正，按回放修正。');
});

test('score correction is a quiet audited fact command', () => {
  const draft = state.createDraft({
    eventType: 'score_correction',
    description: '比分更正为0比0。',
    scoreOverride: { home: 0, away: 0 },
    scoreBefore: { home: 1, away: 0 },
    correctionReason: '现场记分牌回退，原进球无效。',
    deliveryMode: 'quiet',
  });
  assert.equal(state.validateDraft(draft).ready, true);
  const payload = state.toEventPayload(draft, { score: { home: 1, away: 0 }, homeTeam: '利物浦', awayTeam: '切尔西' });
  assert.deepEqual(payload.score, { home: 0, away: 0 });
  assert.equal(payload.proactiveText, '__quiet__');
  assert.equal(payload.evidence.correctionReason, '现场记分牌回退，原进球无效。');
});

test('goal cancellation must revise the referenced goal and remove its score', () => {
  const draft = state.createDraft({
    teamId: 'home',
    eventType: 'goal_cancelled',
    description: 'VAR判定越位，进球取消。',
    correctionReason: 'VAR回放确认越位。',
  });
  assert.equal(state.validateDraft(draft).ready, false);
  draft.revisionOf = 'evt_goal';
  assert.equal(state.validateDraft(draft).ready, true);
  const payload = state.toEventPayload(draft, { score: { home: 1, away: 0 }, homeTeam: '西班牙', awayTeam: '德国' });
  assert.deepEqual(payload.score, { home: 0, away: 0 });
  assert.equal(payload.revisionOf, 'evt_goal');
});

test('VAR result must reference the fact under review', () => {
  const draft = state.createDraft({
    teamId: 'home',
    eventType: 'var_result',
    description: 'VAR 确认判罚结果。',
    correctionReason: '视频回放已经完成核对。',
  });
  assert.equal(state.validateDraft(draft).ready, false);
  draft.revisionOf = 'evt_under_review';
  assert.equal(state.validateDraft(draft).ready, true);
});
