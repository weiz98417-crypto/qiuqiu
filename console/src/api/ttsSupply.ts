// 语音供给三态（tts-supply-switch）：契约与
// backend/cmd/server/console_api.go 的 handleGetTTSSupply wire 形状互指——
// 改动任何一侧都要同步另一侧。供给是部署级事实：全局单值、写走幂等+
// 审计、运行中即时生效。
import { api } from './client';

export interface LocalProbeSnapshot {
  configured: boolean;
  available: boolean;
  reason?: string;
  lastCheckAt?: string;
  probes: number;
  failures: number;
}

export interface TtsSupplyState {
  mode: string;
  local: LocalProbeSnapshot;
  localSelectable: boolean;
  localFailures: number;
  cloudFallbacks: number;
}

// 三态面板文案；mode 原值与服务端词汇一致（cloud/local/local_first）。
export const ttsSupplyModeLabels: Record<string, string> = {
  cloud: '云 API（现役）',
  local: '本地引擎',
  local_first: '本地优先 · 失败回云',
};

export const ttsSupplyApi = {
  get: () => api<TtsSupplyState>('/api/console/tts-supply'),
  update: (mode: string) =>
    api<TtsSupplyState>('/api/console/tts-supply', { method: 'PATCH', body: { mode } }),
};
