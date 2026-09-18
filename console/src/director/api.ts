// 实战导演页（新版）API 客户端（ADR-0011）：请求形状与 operator-control
// evals 断言逐字一致 —— 每个写操作携带 Idempotency-Key，Authorization 走
// 运营台统一令牌逻辑（JWT 或机令牌）。

import { api } from '../api/client';

export interface RosterPlayer {
  number: string;
  name: string;
  position: string;
  lineup: string;
}

export interface MatchClockState {
  period: string;
  elapsedSeconds: number;
  running: boolean;
  anchorAt?: string | null;
  version: number;
}

export async function loadConfig(matchId: string): Promise<Record<string, unknown>> {
  return api(`/api/matches/${encodeURIComponent(matchId)}/config`);
}

export async function loadClock(matchId: string): Promise<{ clock: MatchClockState; snapshot?: { score?: ScoreLike } }> {
  return api(`/api/matches/${encodeURIComponent(matchId)}/clock`);
}

export interface ScoreLike {
  home: number;
  away: number;
}

export async function patchClock(
  matchId: string,
  command: Record<string, unknown>,
): Promise<{ clock: MatchClockState; snapshot?: { score?: ScoreLike } }> {
  return api(`/api/matches/${encodeURIComponent(matchId)}/clock`, { method: 'PATCH', body: command });
}

export async function loadEvents(
  matchId: string,
): Promise<{ events: DirectorEventRow[]; conflicts?: DirectorConflict[] }> {
  return api(`/api/matches/${encodeURIComponent(matchId)}/events`);
}

export interface DirectorEventRow {
  id: string;
  factId?: string;
  eventType: string;
  clock?: string;
  period?: string;
  teamId?: string;
  teamName?: string;
  playerName?: string;
  participants?: Array<{ role: string; name: string; teamId?: string; teamName?: string }>;
  score?: ScoreLike;
  reportedScore?: ScoreLike;
  effectiveScoreAfter?: ScoreLike;
  intensity?: number;
  confirmed?: boolean;
  factStatus?: string;
  status?: string;
  description?: string;
  recommendedAction?: string;
  proactiveText?: string;
  evidence?: { correctionReason?: string } & Record<string, unknown>;
  revisionOf?: string;
}

export interface DirectorConflict {
  id: string;
  status?: string;
  members?: Array<{ role: string; factId: string }>;
  edges?: Array<{ leftFactId: string; rightFactId: string }>;
}

export function publishEvent(matchId: string, payload: unknown): Promise<{ event?: DirectorEventRow; snapshot?: { score?: ScoreLike } }> {
  return api(`/api/matches/${encodeURIComponent(matchId)}/events`, { method: 'POST', body: payload });
}

export function correctEvent(matchId: string, eventId: string, payload: unknown): Promise<{ event?: DirectorEventRow; snapshot?: { score?: ScoreLike } }> {
  return api(`/api/matches/${encodeURIComponent(matchId)}/events/${encodeURIComponent(eventId)}/correct`, {
    method: 'POST',
    body: payload,
  });
}

export function factTransition(matchId: string, factId: string, action: string): Promise<unknown> {
  return api(`/api/matches/${encodeURIComponent(matchId)}/facts/${encodeURIComponent(factId)}/${action}`, {
    method: 'POST',
  });
}

export function resolveConflict(matchId: string, conflictId: string, body: Record<string, unknown>): Promise<unknown> {
  return api(`/api/matches/${encodeURIComponent(matchId)}/conflicts/${encodeURIComponent(conflictId)}/resolve`, {
    method: 'POST',
    body,
  });
}

export interface VoiceDraftResponse {
  transcript?: string;
  draft?: Record<string, unknown>;
  warnings?: string[];
}

export function submitVoiceDraft(matchId: string, body: Record<string, unknown>): Promise<VoiceDraftResponse> {
  return api(`/api/matches/${encodeURIComponent(matchId)}/drafts/voice`, { method: 'POST', body });
}

export function publishVoiceDraft(matchId: string, body: Record<string, unknown>): Promise<{ event?: DirectorEventRow; snapshot?: { score?: ScoreLike } }> {
  return api(`/api/matches/${encodeURIComponent(matchId)}/drafts/voice/publish`, { method: 'POST', body });
}
