// 草稿↔表单映射纯 module（event-model.ts 的姊妹，ADR-0011 导演页）。
// 零 React import、零请求：interface 即测试面，scripts/check-director-draft-form.mjs
// 直测本文件，不需要渲染组件。DirectorLive 只做 setState 与请求编排。

import { createDraft, formatClock, updateDraft, type Draft } from './event-model';
import type { DirectorEventRow } from './api';

// 表单状态（DraftCard 渲染；类型住在纯 module 侧，映射逻辑与形状同源）。
export interface DraftFormState {
  occurredClock: string;
  intensity: string;
  factStatus: string;
  action: string;
  mode: string;
  description: string;
  proactive: string;
  correctionReason: string;
  scoreHome: string;
  scoreAway: string;
  mainPlayer: string;
}

// 服务端锚定的比赛时钟插值：running 时在 elapsedSeconds 基础上叠加
// anchorAt 以来的流逝；锚点非法或未运行时退回服务器值。now 由调用方注入。
export function elapsedClockSeconds(clock: MatchClockLike, now: number): number {
  if (!clock.running || !clock.anchorAt) return clock.elapsedSeconds;
  const anchored = Date.parse(clock.anchorAt);
  if (Number.isFinite(anchored)) {
    return Math.max(0, clock.elapsedSeconds + Math.floor((now - anchored) / 1000));
  }
  return clock.elapsedSeconds;
}

interface MatchClockLike {
  elapsedSeconds: number;
  running: boolean;
  anchorAt?: string | null;
}

// 草稿→表单回填：只在草稿被整体替换（选行为/清空/拉回更正）时调用——
// 不能挂 [draft] 逐次同步：用户正在编辑的描述/话术会被草稿里的旧值清掉
// （老页面 renderCurrentDraft({ preserveForm: true }) 防的就是这个；
// preserve 语义由调用方掌握「何时调用」，本函数只负责「如何映射」）。
export function formFromDraft(source: Draft): DraftFormState {
  return {
    occurredClock: formatClock(source.occurredSeconds),
    intensity: String(source.intensity || 3),
    factStatus: source.factStatus === 'pending' ? 'pending' : 'confirmed',
    action: source.recommendedAction || '',
    mode: source.deliveryMode || 'auto',
    description: source.description || '',
    proactive: source.proactiveText || '',
    correctionReason: source.correctionReason || '',
    mainPlayer: source.primaryParticipant?.name || '',
    scoreHome: source.scoreOverride ? String(source.scoreOverride.home) : '',
    scoreAway: source.scoreOverride ? String(source.scoreOverride.away) : '',
  };
}

export interface SubmitMergeInput {
  draft: Draft;
  form: DraftFormState;
  // 提交按钮语义：confirmed=确认并发送；pending=暂存候选（上线即
  // provisional，老页面语义）。
  factStatus: 'confirmed' | 'pending';
  // 主参与人回填时草稿尚无队伍所用的兜底侧与队名。
  fallbackSide: 'home' | 'away';
  homeTeam: string;
  awayTeam: string;
}

// 提交前的表单→草稿合并：逐字段 set_field（proactive 只在人工话术模式
// 落值），再执行老页面 captureDraftFromForm 语义——表单手输的事件时间
// 与主参与人必须落回草稿，否则提交时静默丢失。
export function mergedDraftForSubmit(input: SubmitMergeInput): Draft {
  const { draft, form, factStatus, fallbackSide, homeTeam, awayTeam } = input;
  const wireFactStatus = factStatus === 'pending' ? 'provisional' : 'confirmed';
  let working = updateDraft(draft, { type: 'set_field', field: 'factStatus', value: wireFactStatus });
  working = updateDraft(working, { type: 'set_field', field: 'description', value: form.description });
  working = updateDraft(working, {
    type: 'set_field',
    field: 'proactiveText',
    value: form.mode === 'manual' ? form.proactive : '',
  });
  working = updateDraft(working, { type: 'set_field', field: 'deliveryMode', value: form.mode });
  working = updateDraft(working, { type: 'set_field', field: 'recommendedAction', value: form.action });
  working = updateDraft(working, { type: 'set_field', field: 'intensity', value: Number(form.intensity || 3) });
  working = updateDraft(working, { type: 'set_field', field: 'correctionReason', value: form.correctionReason });
  if (working.eventType === 'score_correction') {
    working = updateDraft(working, {
      type: 'set_field',
      field: 'scoreOverride',
      value: { home: Number(form.scoreHome), away: Number(form.scoreAway) },
    });
  }
  const clockMatch = /^([0-9]{1,2}):([0-9]{1,2})$/.exec(form.occurredClock.trim());
  if (clockMatch) {
    const seconds = Number(clockMatch[1]) * 60 + Number(clockMatch[2]);
    working = updateDraft(working, { type: 'set_field', field: 'occurredSeconds', value: seconds });
  }
  if (form.mainPlayer.trim() && !working.primaryParticipant?.name
      && !working.participants.some((item) => item.name)) {
    working = updateDraft(working, {
      type: 'select_player',
      name: form.mainPlayer.trim(),
      teamId: working.teamId || fallbackSide,
      teamName: working.teamId === 'away' ? awayTeam : homeTeam,
    });
  }
  return working;
}

export interface EventLoadInput {
  event: DirectorEventRow;
  matchId: string;
  // 拉回更正时的新时钟锚点（老页面语义：以当前时钟为基准重 capture）。
  clock: { period: string; elapsedSeconds: number; version: number };
}

// 事件行→草稿装载（拉回更正）：__quiet__ 哨兵解码为只记事实，
// provisional 事实回到 pending 候选，参与人原样带回（resolved）。
export function draftFromEvent({ event, matchId, clock }: EventLoadInput): Draft {
  return createDraft({
    matchId,
    source: 'operator',
    teamId: (event.teamId as 'home' | 'away') || null,
    eventType: event.eventType,
    occurredPeriod: event.period || clock.period,
    occurredSeconds: clock.elapsedSeconds,
    capturedClockVersion: Number(clock.version || 0),
    description: event.description || '',
    factStatus: event.factStatus === 'provisional' ? 'pending' : 'confirmed',
    recommendedAction: event.recommendedAction || '',
    intensity: Number(event.intensity || 3),
    revisionOf: event.revisionOf || event.id || null,
    correctionReason: '',
    participants: (event.participants || []).map((item) => ({
      role: item.role,
      name: item.name,
      teamId: item.teamId || '',
      teamName: item.teamName || '',
      resolved: true,
    })),
    proactiveText: ['__quiet__'].includes(event.proactiveText || '') ? '' : event.proactiveText || '',
    deliveryMode: event.proactiveText === '__quiet__' ? 'quiet' : event.proactiveText ? 'manual' : 'auto',
  });
}
