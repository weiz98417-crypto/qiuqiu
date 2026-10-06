// 知识策展 API 客户端（knowledge-curation-console 7.3）：契约类型与
// backend/cmd/server/knowledge_api.go 的 wire 形状互指——改动任何一侧都要
// 同步另一侧。写路径复用 client.ts 的 api()（自动携带幂等键，保存即生效）。
import { api } from './client';

// 生效窗口二态：active = 已生效（effectiveAt <= now），pending = 待生效。
export type KnowledgeStatus = 'active' | 'pending';

export interface KnowledgeEntry {
  id: string;
  topics: string[];
  answer: string;
  source: string;
  confidence: number;
  effectiveAt: string;
  status: KnowledgeStatus;
  // 转会窗复查标记：生效窗口早于最近一次窗闭（7 月 1 日 / 1 月 1 日，
  // ADR-0017 转会窗复查制度）——「待复查」过滤器的事实口径。
  dueReview: boolean;
  triggers?: string[];
  quote?: string;
  // 检索后处理参数学五字段（knowledge-worldinfo）：默认值=现状行为。
  priority: number;
  inclusionGroup: string;
  stickyTurns: number;
  cooldownTurns: number;
  probability: number;
  createdBy: string;
  createdAt: string;
  updatedAt: string;
}

export interface KnowledgeList {
  entries: KnowledgeEntry[];
  total: number;
  page: number;
  pageSize: number;
}

export interface KnowledgeQuery {
  q?: string;
  status?: KnowledgeStatus | '';
  due?: 'review';
  page?: number;
  pageSize?: number;
}

// PUT 请求体：与 ADR-0017 字段一致 + 后处理参数学五字段
// （knowledge-worldinfo）。triggers/quote 属判罚事件附句策展，表单不编辑
// （后端保存时保留存量织写锚）。
export interface KnowledgeUpdate {
  topics: string[];
  answer: string;
  source: string;
  confidence: number;
  effectiveAt: string;
  priority: number;
  inclusionGroup: string;
  stickyTurns: number;
  cooldownTurns: number;
  probability: number;
}

// POST（新建条目）请求体：id 由策展人命名（slug），其余字段与更新一致；
// 后端同 id 已存在返回 409。
export interface KnowledgeCreate extends KnowledgeUpdate {
  id: string;
}

export const knowledgeApi = {
  list: (query: KnowledgeQuery = {}) => {
    const params = new URLSearchParams();
    if (query.q) params.set('q', query.q);
    if (query.status) params.set('status', query.status);
    if (query.due) params.set('due', query.due);
    if (query.page) params.set('page', String(query.page));
    if (query.pageSize) params.set('pageSize', String(query.pageSize));
    const qs = params.toString();
    return api<KnowledgeList>(`/api/console/knowledge${qs ? `?${qs}` : ''}`);
  },
  get: (id: string) =>
    api<{ entry: KnowledgeEntry }>(`/api/console/knowledge/${encodeURIComponent(id)}`),
  update: (id: string, payload: KnowledgeUpdate) =>
    api<{ entry: KnowledgeEntry }>(`/api/console/knowledge/${encodeURIComponent(id)}`, {
      method: 'PUT',
      body: payload,
    }),
  create: (payload: KnowledgeCreate) =>
    api<{ entry: KnowledgeEntry }>('/api/console/knowledge', {
      method: 'POST',
      body: payload,
    }),
};

// 生效状态的中文名与 antd tag 色——列表/详情两处共用，测试也按此断言。
export const knowledgeStatusLabels: Record<KnowledgeStatus, string> = {
  active: '生效中',
  pending: '待生效',
};

export const knowledgeStatusColors: Record<KnowledgeStatus, string> = {
  active: 'green',
  pending: 'orange',
};

// 表单校验口径（与后端 preparePut 同一约束的前端镜像）：confidence ∈
// [0,1]；topics 至少一个关键词；probability ∈ [0,1]（0=必中，后端归一
// 为 1）；sticky/cooldown 非负整数。
export function validateKnowledgeConfidence(value: number | null): string | undefined {
  if (value === null || Number.isNaN(value)) return '请填写确信度';
  if (value < 0 || value > 1) return '确信度必须在 0 到 1 之间';
  return undefined;
}

export function validateKnowledgeProbability(value: number | null): string | undefined {
  if (value === null || Number.isNaN(value)) return '请填写触发概率';
  if (value < 0 || value > 1) return '触发概率必须在 0 到 1 之间';
  return undefined;
}

export function validateKnowledgeTurns(value: number | null): string | undefined {
  if (value === null || Number.isNaN(value)) return '请填写轮数';
  if (!Number.isInteger(value) || value < 0) return '轮数必须是非负整数';
  return undefined;
}

// 新建条目 id 的前端镜像校验（与后端 knowledgeIDPattern 同一约束）：slug——
// 小写字母/数字开头，可含连字符（seed 惯用法如 rule-red-card），2-64 字符。
export function validateKnowledgeId(value: string | undefined): string | undefined {
  const id = (value ?? '').trim();
  if (!id) return '请填写条目 id';
  if (!/^[a-z0-9][a-z0-9-]{1,63}$/.test(id)) return 'id 只能是小写字母、数字和连字符（2-64 字符）';
  return undefined;
}
