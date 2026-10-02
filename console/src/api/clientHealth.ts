// 客户端语音健康遥测（快修 P1 观测面）：契约与
// backend/cmd/server/console_api.go 的 handleClientHealth wire 形状互指——
// 改动任何一侧都要同步另一侧。裁剪口径：只有事件种类/时间/比赛归属，
// 无用户身份、无正文（Operations Observation 纪律）。
import { api } from './client';

export interface ClientHealthEvent {
  kind: string;
  at: string;
  matchId?: string;
}

export interface ClientHealthSnapshot {
  counters: Record<string, number>;
  recent: ClientHealthEvent[];
}

// 事件种类的面板中文名；未知种类原样显示（服务端先行的种类不藏）。
export const clientHealthKindLabels: Record<string, string> = {
  self_interrupt_suspected: '疑似误打断',
  duplex_degraded: '抢话自动降级',
  duplex_recovered: '抢话自动恢复',
  lipsync_vendor_unavailable: '嘴型：wLipSync 不可用',
  lipsync_decode_failed: '嘴型：音频解码失败',
  lipsync_jitter_fallback: '嘴型：落在随机抖动兜底',
};

export const clientHealthApi = {
  snapshot: () => api<ClientHealthSnapshot>('/api/console/client-health'),
};
