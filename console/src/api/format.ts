// 时间展示辅助：契约中所有时间均为 ISO/UTC 字符串，运营台统一本地化展示。
export function fmtTime(value?: string | null): string {
  if (!value) return '—';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

export function fmtDateTime(value?: string | null): string {
  if (!value) return '—';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`;
}

// 话题台账 / 引用审计的状态与 C2 原因码中文标签。
export const threadKindLabels: Record<string, string> = {
  unanswered_question: '未答提问',
  promise: '承诺',
  emotional_moment: '情绪时刻',
  prediction: '预测',
};

export const threadStateLabels: Record<string, string> = {
  open: '待答',
  addressed: '已答',
  expired: '过期',
};

// 话题状态标签配色（话题台账 / 用户页共用）。
export function threadStateTag(state: string): { color: string; label: string } {
  const colors: Record<string, string> = { open: 'gold', addressed: 'green', expired: 'red' };
  return { color: colors[state] ?? 'default', label: threadStateLabels[state] ?? state };
}

export function reasonCodeLabel(code: string): string {
  if (code.startsWith('proactive_citation:')) {
    return `主动引用 · ${code.slice('proactive_citation:'.length)}`;
  }
  const labels: Record<string, string> = {
    thread_addressed: '话题已答',
    thread_expired: '话题过期',
    relationship_decision: '关系决策',
    critical_fact_refreshed: '关键事实刷新',
    delivery_interrupted: '投递打断',
    unanswered_question: '未答提问',
    promise_due: '承诺到期',
  };
  return labels[code] ?? code;
}

// 话痨档位（用户权利，运营只读）。
export function talkativenessLabel(tier: string): string {
  const labels: Record<string, string> = {
    quiet: '安静',
    balanced: '均衡',
    chatty: '话痨',
  };
  return labels[tier] ?? tier;
}
