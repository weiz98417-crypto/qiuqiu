// 实战导演事件模型（ADR-0011 director-rewrite 步骤一）。
//
// 从 client/assets/live2d/operator-live-state.js 纯移植：18 个事件定义、
// 草稿状态机（updateDraft）、语音草稿合并与冲突、校验与事件载荷形状。
// scripts/check-director-event-model.mjs 会在 CI 里把本模块与老文件逐字节
// 对拍（payload JSON 相等），移植走样会直接红。
//
// 事实纪律不变：本模块只产出事件载荷，绝不直接写事实——发布仍走
// POST /api/matches/:id/events（或 correct）由后端事实账本裁决。

export type TeamId = string;

export interface EventRole {
  role: string;
  label: string;
}

export interface EventDefinition {
  label: string;
  group: string;
  action: string;
  intensity: number;
  roles: EventRole[];
  requiredRoles: string[];
  scoreDelta: number;
  playerOptional: boolean;
  opponentRoles: string[];
  multipleRoles: string[];
}

export interface Participant {
  role: string;
  name: string;
  teamId: string;
  teamName: string;
  resolved: boolean;
}

export interface Score {
  home: number;
  away: number;
}

export interface Draft {
  matchId: string;
  source: string;
  teamId: string | null;
  eventType: string | null;
  occurredPeriod: string;
  occurredSeconds: number;
  capturedClockVersion: number;
  primaryParticipant: Participant | null;
  participants: Participant[];
  description: string;
  intensity: number;
  recommendedAction: string;
  factStatus: string;
  deliveryMode: string;
  proactiveText: string;
  revisionOf: string | null;
  correctionReason: string;
  scoreOverride: Score | null;
  scoreBefore: Score | null;
  transcript: string;
  inferredFields: string[];
  fieldConfidence: Record<string, unknown>;
  teamName?: string;
}

export type DraftAction =
  | { type: 'select_team'; teamId?: TeamId }
  | { type: 'select_player'; name?: string; teamId?: TeamId; teamName?: string }
  | {
      type: 'select_event';
      eventType?: string | null;
      period?: string;
      elapsedSeconds?: number;
      clockVersion?: number;
    }
  | { type: 'set_participant'; role: string; name?: string; names?: string[]; teamId?: string; teamName?: string; resolved?: boolean }
  | { type: 'set_participants'; role: string; items?: Array<Partial<Participant>> }
  | { type: 'toggle_participant'; role: string; name: string; teamId?: string; teamName?: string; resolved?: boolean }
  | { type: 'set_field'; field: string; value: unknown }
  | { type: 'sync_clock'; period?: string; elapsedSeconds?: number; clockVersion?: number }
  | { type: 'apply_voice'; draft?: Partial<Draft> & { teamName?: string; participants?: Array<Partial<Participant>>; inferredFields?: string[]; fieldConfidence?: Record<string, unknown> } }
  | { type: 'clear'; period?: string; elapsedSeconds?: number; clockVersion?: number };

export interface VoiceConflict {
  field: string;
  current?: unknown;
  incoming?: unknown;
  teamName?: string;
  participant?: Partial<Participant>;
}

export function definition(
  label: string,
  group: string,
  action: string,
  intensity: number,
  roles: Array<[string, string]>,
  requiredRoles: string[],
  scoreDelta = 0,
  playerOptional = false,
  opponentRoles: string[] = [],
  multipleRoles: string[] = [],
): EventDefinition {
  return {
    label,
    group,
    action,
    intensity,
    roles: roles.map(([role, roleLabel]) => ({ role, label: roleLabel })),
    requiredRoles,
    scoreDelta,
    playerOptional,
    opponentRoles,
    multipleRoles,
  };
}

export const eventDefinitions: Record<string, EventDefinition> = {
  goal: definition('进球', '高频', 'celebrate', 5, [['scorer', '进球者'], ['assist', '助攻者'], ['pre_assist', '策动者'], ['defender', '防守相关']], ['scorer'], 1, false, ['defender'], ['assist', 'defender']),
  shot: definition('射门', '高频', 'focus', 3, [['shooter', '射门者'], ['assist', '传球者'], ['blocker', '封堵者'], ['keeper', '门将']], ['shooter'], 0, false, ['blocker', 'keeper'], ['assist', 'blocker']),
  big_chance: definition('绝佳机会', '高频', 'tense', 5, [['attacker', '进攻者'], ['passer', '传球者'], ['defender', '防守者'], ['keeper', '门将']], ['attacker'], 0, false, ['defender', 'keeper'], ['passer', 'defender']),
  save: definition('扑救', '高频', 'surprise', 4, [['keeper', '扑救门将'], ['shooter', '射门者']], ['keeper'], 0, false, ['shooter']),
  miss: definition('错失', '高频', 'miss', 4, [['shooter', '错失者'], ['assist', '传球者'], ['keeper', '门将']], ['shooter'], 0, false, ['keeper'], ['assist']),
  foul: definition('犯规', '高频', 'complain', 3, [['offender', '犯规者'], ['fouled', '被犯规者']], ['offender'], 0, false, ['fouled']),
  yellow_card: definition('黄牌', '纪律', 'complain', 3, [['offender', '吃牌者'], ['fouled', '对抗对象']], ['offender'], 0, false, ['fouled']),
  red_card: definition('红牌', '纪律', 'angry', 5, [['offender', '红牌球员'], ['fouled', '对抗对象']], ['offender'], 0, false, ['fouled']),
  penalty: definition('点球', '纪律', 'tense', 5, [['taker', '主罚者'], ['won_by', '造点者'], ['offender', '犯规者'], ['keeper', '门将']], [], 0, false, ['offender', 'keeper']),
  substitution: definition('换人', '人员 / 战术', 'analysis', 2, [['sub_on', '上场'], ['sub_off', '下场']], ['sub_on', 'sub_off']),
  tactical_shift: definition('战术变化', '人员 / 战术', 'analysis', 3, [['leader', '关键球员']], [], 0, true),
  pressure: definition('持续压迫', '人员 / 战术', 'focus', 4, [['attacker', '压迫发起'], ['target', '被压迫方']], [], 0, true, ['target']),
  injury: definition('伤停', '人员 / 战术', 'comfort', 3, [['injured', '受伤球员'], ['challenger', '对抗球员']], ['injured'], 0, false, ['challenger']),
  var_check: definition('VAR检查', '特殊', 'tense', 4, [['subject', '被检查球员'], ['affected', '受影响球员']], []),
  var_result: definition('VAR 结果', '特殊', 'analysis', 4, [['subject', '被检查球员'], ['affected', '受影响球员']], []),
  goal_cancelled: definition('进球取消', '特殊', 'analysis', 5, [['scorer', '原进球者'], ['affected', '受影响球员']], [], -1),
  score_correction: definition('比分更正', '特殊', 'analysis', 2, [], [], 0, true),
  operator_note: definition('备注', '特殊', 'analysis', 2, [['player', '相关球员']], [], 0, true),
};

export function createDraft(overrides: Partial<Draft> = {}): Draft {
  return {
    matchId: '',
    source: 'operator',
    teamId: null,
    eventType: null,
    occurredPeriod: 'pre_match',
    occurredSeconds: 0,
    capturedClockVersion: 0,
    primaryParticipant: null,
    participants: [],
    description: '',
    intensity: 3,
    recommendedAction: '',
    factStatus: 'confirmed',
    deliveryMode: 'auto',
    proactiveText: '',
    revisionOf: null,
    correctionReason: '',
    scoreOverride: null,
    scoreBefore: null,
    transcript: '',
    inferredFields: [],
    fieldConfidence: {},
    ...overrides,
  };
}

function participant(role: string, name: string, teamId: TeamId | null | undefined, teamName?: string, resolved?: boolean): Participant {
  return { role, name, teamId: (teamId as string) || '', teamName: teamName || '', resolved: resolved !== false };
}

function unique(values: string[]): string[] {
  return [...new Set(values)];
}

function setParticipant(draft: Draft, role: string, name?: string, teamId?: string | null, teamName?: string, resolved?: boolean): void {
  if (!role) return;
  draft.participants = draft.participants.filter((item) => item.role !== role);
  if (name) draft.participants.push(participant(role, name, teamId ?? null, teamName, resolved));
}

function setParticipants(draft: Draft, role: string, names: string[] | undefined, teamId?: string | null, teamName?: string, resolved?: boolean): void {
  if (!role) return;
  draft.participants = draft.participants.filter((item) => item.role !== role);
  unique((names || []).map((name) => String(name || '').trim()).filter(Boolean)).forEach((name) => {
    draft.participants.push(participant(role, name, teamId ?? null, teamName, resolved));
  });
}

function setParticipantItems(draft: Draft, role: string, items: Array<Partial<Participant>>): void {
  if (!role) return;
  draft.participants = draft.participants.filter((item) => item.role !== role);
  const seen = new Set<string>();
  (items || []).forEach((item) => {
    const name = String(item?.name || '').trim();
    if (!name || seen.has(name)) return;
    seen.add(name);
    draft.participants.push(participant(role, name, item.teamId ?? '', item.teamName, item.resolved !== false));
  });
}

function appendParticipant(draft: Draft, role: string, name?: string, teamId?: string | null, teamName?: string, resolved?: boolean): void {
  if (!role || !String(name || '').trim()) return;
  const normalizedName = String(name).trim();
  if (draft.participants.some((item) => item.role === role && item.name === normalizedName && item.teamId === ((teamId as string) || ''))) return;
  draft.participants.push(participant(role, normalizedName, teamId ?? null, teamName, resolved));
}

function removeParticipant(draft: Draft, role: string, name: string, teamId?: string | null): void {
  draft.participants = draft.participants.filter(
    (item) => !(item.role === role && item.name === name && (!teamId || item.teamId === teamId)),
  );
}

export function roleTeamId(eventType: string | null, role: string, eventTeamId: string | null | undefined): string {
  if (!eventTeamId || !['home', 'away'].includes(eventTeamId)) return (eventTeamId as string) || '';
  const target = eventDefinitions[eventType as string];
  if (target?.opponentRoles?.includes(role)) return eventTeamId === 'home' ? 'away' : 'home';
  return eventTeamId;
}

export function roleAllowsMultiple(eventType: string | null, role: string): boolean {
  return Boolean(eventDefinitions[eventType as string]?.multipleRoles?.includes(role));
}

export function roleLabel(eventType: string | null, role: string): string {
  return eventDefinitions[eventType as string]?.roles?.find(({ role: key }) => key === role)?.label || role;
}

export function formatClock(seconds: number | undefined): string {
  const safe = Math.max(0, Number(seconds || 0));
  return `${String(Math.floor(safe / 60)).padStart(2, '0')}:${String(Math.floor(safe % 60)).padStart(2, '0')}`;
}

function hasDraftContent(draft: Draft): boolean {
  return Boolean(
    draft.eventType ||
      draft.primaryParticipant?.name ||
      (draft.participants || []).some((item) => item.name) ||
      String(draft.description || '').trim() ||
      String(draft.proactiveText || '').trim() ||
      String(draft.transcript || '').trim() ||
      draft.revisionOf ||
      String(draft.correctionReason || '').trim() ||
      draft.scoreOverride,
  );
}

function mergeField(target: Draft, source: Record<string, unknown>, field: string, conflicts: VoiceConflict[]): void {
  const incoming = source[field];
  if (incoming === undefined || incoming === null || incoming === '') return;
  const current = (target as unknown as Record<string, unknown>)[field];
  if (current === undefined || current === null || current === '' || (typeof current === 'number' && current === 0)) {
    (target as unknown as Record<string, unknown>)[field] = incoming;
  } else if (current !== incoming) {
    conflicts.push({ field, current, incoming, teamName: field === 'teamId' ? String(source.teamName ?? '') : '' });
  }
}

function cloneDraft(draft: Draft): Draft {
  return createDraft({
    ...(draft as Draft),
    participants: (draft.participants || []).map((item) => ({ ...item })),
    primaryParticipant: draft.primaryParticipant ? { ...draft.primaryParticipant } : null,
    scoreOverride: draft.scoreOverride ? { ...draft.scoreOverride } : null,
    scoreBefore: draft.scoreBefore ? { ...draft.scoreBefore } : null,
    inferredFields: [...(draft.inferredFields || [])],
    fieldConfidence: { ...(draft.fieldConfidence || {}) },
  });
}

function publicEventDescription(eventType: string, description: string, playerName: string): string {
  const text = String(description || '').trim();
  const player = String(playerName || '').trim();
  if (eventType !== 'goal' || !player || text.includes(player)) return text;
  return `${player}${text || '进球了。'}`;
}

export function updateDraft(draft: Draft, action: DraftAction): Draft {
  const next = cloneDraft(draft);
  switch (action.type) {
    case 'select_team': {
      const teamId = (action.teamId as string) || null;
      if (teamId !== next.teamId) {
        next.teamId = teamId;
        next.primaryParticipant = null;
        next.participants = [];
      }
      return next;
    }
    case 'select_player': {
      if (action.teamId && action.teamId !== next.teamId) {
        next.teamId = action.teamId;
        next.participants = [];
      }
      next.primaryParticipant = action.name
        ? participant('', action.name, next.teamId, action.teamName, true)
        : null;
      const role = eventDefinitions[next.eventType as string]?.roles?.[0]?.role;
      if (role && action.name) setParticipant(next, role, action.name, next.teamId, action.teamName, true);
      return next;
    }
    case 'select_event': {
      const eventType = action.eventType || null;
      const target = eventDefinitions[eventType as string];
      next.eventType = target ? eventType : null;
      next.participants = [];
      next.description = '';
      next.proactiveText = '';
      next.scoreOverride = null;
      next.scoreBefore = null;
      next.recommendedAction = target?.action || '';
      next.intensity = target?.intensity || 3;
      next.factStatus = eventType === 'var_check' ? 'provisional' : 'confirmed';
      if (eventType === 'score_correction') {
        next.teamId = null;
        next.primaryParticipant = null;
        next.participants = [];
        next.deliveryMode = 'quiet';
      }
      next.inferredFields = [];
      next.fieldConfidence = {};
      if (action.period) next.occurredPeriod = action.period;
      if (Number.isFinite(action.elapsedSeconds)) next.occurredSeconds = Math.max(0, action.elapsedSeconds as number);
      if (Number.isFinite(action.clockVersion)) next.capturedClockVersion = action.clockVersion as number;
      const role = target?.roles?.[0]?.role;
      if (role && next.primaryParticipant?.name) {
        setParticipant(next, role, next.primaryParticipant.name, next.teamId, next.primaryParticipant.teamName, true);
      }
      return next;
    }
    case 'set_participant':
      if (Array.isArray(action.names)) {
        setParticipants(next, action.role, action.names, action.teamId || next.teamId, action.teamName, action.resolved !== false);
      } else {
        setParticipant(next, action.role, action.name, action.teamId || next.teamId, action.teamName, action.resolved !== false);
      }
      if (eventDefinitions[next.eventType as string]?.roles?.[0]?.role === action.role) {
        const primary = Array.isArray(action.names) ? action.names[0] : action.name;
        next.primaryParticipant = primary
          ? participant(action.role, primary, action.teamId || next.teamId, action.teamName, action.resolved !== false)
          : null;
      }
      return next;
    case 'set_participants': {
      setParticipantItems(next, action.role, action.items || []);
      if (eventDefinitions[next.eventType as string]?.roles?.[0]?.role === action.role) {
        const primary = next.participants.find((item) => item.role === action.role && item.name);
        next.primaryParticipant = primary ? { ...primary } : null;
      }
      return next;
    }
    case 'toggle_participant': {
      const teamId = action.teamId || next.teamId;
      const same = next.participants.find(
        (item) => item.role === action.role && item.name === action.name && (!teamId || item.teamId === teamId),
      );
      if (same) removeParticipant(next, action.role, action.name, teamId);
      else appendParticipant(next, action.role, action.name, teamId, action.teamName, action.resolved !== false);
      if (eventDefinitions[next.eventType as string]?.roles?.[0]?.role === action.role) {
        const primary = next.participants.find((item) => item.role === action.role && item.name);
        next.primaryParticipant = primary ? { ...primary } : null;
      }
      return next;
    }
    case 'set_field':
      if (Object.prototype.hasOwnProperty.call(next, action.field)) {
        (next as unknown as Record<string, unknown>)[action.field] = action.value;
      }
      return next;
    case 'sync_clock':
      if (hasDraftContent(next)) return next;
      if (action.period) next.occurredPeriod = action.period;
      if (Number.isFinite(action.elapsedSeconds)) next.occurredSeconds = Math.max(0, action.elapsedSeconds as number);
      if (Number.isFinite(action.clockVersion)) next.capturedClockVersion = action.clockVersion as number;
      return next;
    case 'apply_voice':
      return applyVoiceDraft(next, action.draft || {}).draft;
    case 'clear':
      return createDraft({
        matchId: next.matchId,
        occurredPeriod: action.period || next.occurredPeriod,
        occurredSeconds: Number.isFinite(action.elapsedSeconds) ? (action.elapsedSeconds as number) : next.occurredSeconds,
        capturedClockVersion: Number.isFinite(action.clockVersion) ? (action.clockVersion as number) : next.capturedClockVersion,
        deliveryMode: next.deliveryMode,
      });
    default:
      return next;
  }
}

export function applyVoiceDraft(
  draft: Draft,
  incoming: Partial<Draft> & { participants?: Array<Partial<Participant>>; inferredFields?: string[]; fieldConfidence?: Record<string, unknown> } = {},
): { draft: Draft; conflicts: VoiceConflict[] } {
  const next = cloneDraft(draft);
  const conflicts: VoiceConflict[] = [];
  const scalarFields = ['teamId', 'eventType', 'occurredPeriod', 'occurredSeconds', 'capturedClockVersion', 'description', 'transcript'];
  scalarFields.forEach((field) => mergeField(next, incoming as Record<string, unknown>, field, conflicts));
  const source = incoming as Record<string, unknown>;
  if (source.teamName && !next.teamName) next.teamName = String(source.teamName);
  if (next.eventType && !draft.eventType) {
    const target = eventDefinitions[next.eventType];
    next.recommendedAction = target?.action || next.recommendedAction;
    next.intensity = target?.intensity || next.intensity;
  }
  (incoming.participants || []).forEach((item) => {
    const role = String(item.role || '');
    const current = next.participants.filter((existing) => existing.role === role);
    const multi = roleAllowsMultiple(next.eventType, role);
    if (!current.length) {
      setParticipant(next, role, item.name, item.teamId || next.teamId, item.teamName, item.resolved !== false);
    } else if (multi) {
      if (!current.some((existing) => existing.name === item.name)) {
        appendParticipant(next, role, item.name, item.teamId || next.teamId, item.teamName, item.resolved !== false);
      }
    } else if (current[0].name !== item.name) {
      conflicts.push({ field: `participants.${role}`, current: current[0].name, incoming: item.name, participant: { ...item } });
    }
  });
  next.inferredFields = unique([...(next.inferredFields || []), ...(incoming.inferredFields || [])]);
  next.fieldConfidence = { ...(next.fieldConfidence || {}), ...(incoming.fieldConfidence || {}) };
  const firstRole = eventDefinitions[next.eventType as string]?.roles?.[0]?.role;
  const primary = next.participants.find((item) => item.role === firstRole);
  if (primary) next.primaryParticipant = { ...primary };
  return { draft: next, conflicts };
}

export function applyVoiceConflict(draft: Draft, conflict?: VoiceConflict): Draft {
  const next = cloneDraft(draft);
  if (!conflict?.field) return next;
  if (conflict.field.startsWith('participants.') && conflict.participant) {
    const item = conflict.participant;
    setParticipant(next, String(item.role || ''), item.name, item.teamId || next.teamId, item.teamName, item.resolved !== false);
    const firstRole = eventDefinitions[next.eventType as string]?.roles?.[0]?.role;
    if (item.role === firstRole) next.primaryParticipant = { ...(item as Participant) };
  } else if (Object.prototype.hasOwnProperty.call(next, conflict.field)) {
    (next as unknown as Record<string, unknown>)[conflict.field] = conflict.incoming;
    if (conflict.field === 'teamId' && conflict.teamName) next.teamName = conflict.teamName;
    if (conflict.field === 'eventType') {
      const target = eventDefinitions[next.eventType as string];
      const allowedRoles = new Set((target?.roles || []).map(({ role }) => role));
      next.participants = next.participants.filter((item) => allowedRoles.has(item.role));
      const primaryRole = target?.roles?.[0]?.role;
      next.primaryParticipant = next.participants.find((item) => item.role === primaryRole) || null;
      next.recommendedAction = target?.action || next.recommendedAction;
      next.intensity = target?.intensity || next.intensity;
    }
  }
  next.inferredFields = unique([...(next.inferredFields || []), conflict.field.split('.')[0]]);
  return next;
}

export function validateDraft(draft: Draft): { ready: boolean; errors: string[] } {
  const errors: string[] = [];
  const target = eventDefinitions[draft.eventType as string];
  if (!target) errors.push('请选择比赛行为');
  if (target && !draft.teamId && !target.playerOptional) errors.push('请选择球队');
  if (target) {
    target.requiredRoles.forEach((role) => {
      const item = draft.participants.find((participant) => participant.role === role && participant.name);
      if (!item) errors.push(`请填写${roleLabel(draft.eventType, role)}`);
      else if (item.resolved === false) errors.push(`请确认球员：${item.name}`);
    });
    target.roles.forEach(({ role }) => {
      const expectedTeam = roleTeamId(draft.eventType, role, draft.teamId);
      draft.participants
        .filter((participant) => participant.role === role && participant.name)
        .forEach((item) => {
          if (item.teamId && expectedTeam && item.teamId !== expectedTeam) {
            errors.push(`${roleLabel(draft.eventType, role)}必须属于${expectedTeam === draft.teamId ? '进攻方' : '防守方'}`);
          }
        });
    });
  }
  if (draft.eventType === 'substitution') {
    const teams = unique(draft.participants.filter((item) => item.name).map((item) => item.teamId).filter(Boolean) as string[]);
    if (teams.length > 1) errors.push('换上与换下球员必须属于同一球队');
  }
  if (draft.eventType === 'goal') {
    const wrongTeam = draft.participants.find(
      (item) => ['scorer', 'assist', 'pre_assist'].includes(item.role) && item.teamId && item.teamId !== draft.teamId,
    );
    if (wrongTeam) errors.push(`${roleLabel(draft.eventType, wrongTeam.role)}必须属于进球队伍`);
  }
  if (draft.eventType === 'goal_cancelled' && !draft.revisionOf) errors.push('请选择要取消的原进球');
  if (draft.eventType === 'var_result' && !draft.revisionOf) errors.push('请选择 VAR 正在审查的原事实');
  if (!String(draft.description || '').trim()) errors.push('请填写事件描述');
  if ((draft.revisionOf || draft.eventType === 'score_correction') && !String(draft.correctionReason || '').trim()) errors.push('请填写更正或采用原因');
  if (draft.eventType === 'score_correction') {
    const score = draft.scoreOverride;
    if (!score || !Number.isInteger(Number(score.home)) || !Number.isInteger(Number(score.away)) || Number(score.home) < 0 || Number(score.away) < 0) {
      errors.push('请填写有效的目标比分');
    } else if (draft.scoreBefore && Number(score.home) === Number(draft.scoreBefore.home) && Number(score.away) === Number(draft.scoreBefore.away)) {
      errors.push('目标比分必须与当前公开比分不同');
    }
  }
  if (draft.deliveryMode === 'manual' && !String(draft.proactiveText || '').trim()) errors.push('请填写球球主动话术');
  return { ready: errors.length === 0, errors };
}

export interface EventPayload {
  source: string;
  providerName: string;
  eventType: string | null;
  period: string;
  clock: string;
  teamId: string;
  teamName: string;
  playerName: string;
  participants: Array<{ role: string; name: string; teamId: string; teamName: string }>;
  score: Score;
  intensity: number;
  confirmed: boolean;
  factStatus: string;
  description: string;
  recommendedAction: string;
  tags: string[];
  proactiveText: string;
  revisionOf: string;
  evidence: Record<string, string>;
}

export function toEventPayload(
  draft: Draft,
  context: { score?: Score | null; homeTeam?: string; awayTeam?: string },
): EventPayload {
  const validation = validateDraft(draft);
  if (!validation.ready) throw new Error(validation.errors[0]);
  const target = eventDefinitions[draft.eventType as string];
  const score: Score = {
    home: Number(context.score?.home || 0),
    away: Number(context.score?.away || 0),
  };
  if (draft.eventType === 'score_correction') {
    score.home = Number(draft.scoreOverride?.home);
    score.away = Number(draft.scoreOverride?.away);
  }
  if (target.scoreDelta && draft.teamId) score[draft.teamId as 'home' | 'away'] += target.scoreDelta;
  const primaryRole = target.roles?.[0]?.role;
  const primary = draft.participants.find((item) => item.role === primaryRole) || draft.primaryParticipant;
  const description = publicEventDescription(draft.eventType as string, draft.description, primary?.name || '');
  return {
    source: draft.source || 'operator',
    providerName: draft.source === 'operator_voice' ? 'director-voice' : 'director-console',
    eventType: draft.eventType,
    period: draft.occurredPeriod,
    clock: formatClock(draft.occurredSeconds),
    teamId: draft.teamId || '',
    teamName: draft.teamId === 'away' ? (context.awayTeam || '') : draft.teamId === 'home' ? (context.homeTeam || '') : '',
    playerName: primary?.name || '',
    participants: draft.participants.filter((item) => item.name).map(({ role, name, teamId, teamName }) => ({ role, name, teamId, teamName })),
    score,
    intensity: Number(draft.intensity || target.intensity || 3),
    confirmed: draft.factStatus === 'confirmed',
    factStatus: draft.factStatus,
    description,
    recommendedAction: draft.recommendedAction || target.action,
    tags: [`clockVersion=${draft.capturedClockVersion}`, `input=${draft.source || 'operator'}`],
    proactiveText: draft.deliveryMode === 'quiet' ? '__quiet__' : draft.deliveryMode === 'manual' ? String(draft.proactiveText || '').trim() : '',
    revisionOf: draft.revisionOf || '',
    evidence: draft.revisionOf || draft.eventType === 'score_correction' ? { correctionReason: String(draft.correctionReason || '').trim() } : {},
  };
}
