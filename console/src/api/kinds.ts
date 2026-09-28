// 交互账本 kind 的运营台单源标签（User 页交互历史与 LiveMonitor 事件流
// 共用口径；kind 集合见 backend/internal/interaction/ledger.go）。
export interface InteractionKindMeta {
  label: string;
  // 徽标色（antd Tag color；仅事件流等需要色块的页面消费）。
  color: string;
}

export const INTERACTION_KINDS: Record<string, InteractionKindMeta> = {
  // —— 用户页交互历史出现的 kind ——
  user_message: { label: '用户发言', color: 'default' },
  assistant_reply: { label: '球球回复', color: 'blue' },
  proactive: { label: '主动发言', color: 'orange' },
  playback_result: { label: '播报回执', color: 'cyan' },
  // —— /ws/ops 事件流出现的 kind ——
  turn_planned: { label: '回合规划', color: 'geekblue' },
  delivery: { label: '投递', color: 'green' },
  signal: { label: '信号', color: 'blue' },
  fact_revision: { label: '事实修订', color: 'orange' },
  media_delivery: { label: '媒体投递', color: 'purple' },
  turn_stale: { label: '回合过期', color: 'red' },
  backchannel: { label: '微反应', color: 'gold' },
  character_setting: { label: '人格设定', color: 'magenta' },
};

// kindLabel：中文标签，未知 kind 回落原值（与各页手写版 ?? kind 同语义）。
export function kindLabel(kind: string): string {
  return INTERACTION_KINDS[kind]?.label ?? kind;
}

// kindMeta：标签 + 徽标色，未知 kind 用 default 色。
export function kindMeta(kind: string): InteractionKindMeta {
  return INTERACTION_KINDS[kind] ?? { label: kind, color: 'default' };
}
