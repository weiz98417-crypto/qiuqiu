// 实战导演页（新版）API 门面（ADR-0011）：端点与传输策略统一在
// consoleApi（../api/client），这里只按导演页的命名习惯再导出——
// 端点注册全仓只有一种风格，写路径语义（去重/重试/409/replay）由
// api() 统一提供。

import { consoleApi } from '../api/client';

export interface RosterPlayer {
  number: string;
  name: string;
  position: string;
  lineup: string;
}

export type {
  DirectorConflict,
  DirectorEventRow,
  MatchClockState,
  ScoreLike,
  VoiceDraftResponse,
} from '../api/client';

export const loadConfig = consoleApi.matchConfig;
export const loadClock = consoleApi.matchClock;
export const patchClock = consoleApi.patchClock;
export const loadEvents = consoleApi.matchEvents;
export const publishEvent = consoleApi.publishEvent;
export const correctEvent = consoleApi.correctEvent;
export const factTransition = consoleApi.factTransition;
export const resolveConflict = consoleApi.resolveConflict;
export const submitVoiceDraft = consoleApi.submitVoiceDraft;
export const publishVoiceDraft = consoleApi.publishVoiceDraft;
