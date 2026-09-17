// 运营台 API 客户端：统一携带个人令牌，401/403 集中处理。
// 令牌存放于 localStorage['qiuqiu.console.token']（与 operator.html 的
// 'qiuqiu.operator.token' 同一套惯用法，见 docs/adr/0008）。

export const TOKEN_STORAGE_KEY = 'qiuqiu.console.token';

export class ApiError extends Error {
  status: number;
  body: string;

  constructor(status: number, body: string) {
    super(`API ${status}: ${body}`);
    this.status = status;
    this.body = body;
  }
}

export function getToken(): string {
  return localStorage.getItem(TOKEN_STORAGE_KEY) ?? '';
}

export function setToken(token: string): void {
  localStorage.setItem(TOKEN_STORAGE_KEY, token);
}

export function clearToken(): void {
  localStorage.removeItem(TOKEN_STORAGE_KEY);
}

// 401 时派发该事件，AppShell 监听后切回令牌页。
export const AUTH_INVALID_EVENT = 'qiuqiu:console-auth-invalid';

type AuthState = 'expired' | 'forbidden';

function notifyAuthInvalid(state: AuthState) {
  window.dispatchEvent(new CustomEvent<AuthState>(AUTH_INVALID_EVENT, { detail: state }));
}

export interface RequestOptions {
  method?: 'GET' | 'POST' | 'PATCH' | 'DELETE';
  body?: unknown;
  // 401 不触发全局令牌页（用于令牌校验本身）。
  skipAuthRedirect?: boolean;
}

export async function api<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const headers: Record<string, string> = {};
  const token = getToken();
  if (token) headers.Authorization = `Bearer ${token}`;
  if (options.body !== undefined) headers['Content-Type'] = 'application/json';
  // 写操作携带幂等键（与 operator-control 惯用法一致）。
  if (options.method && options.method !== 'GET') {
    headers['Idempotency-Key'] = `console-${Date.now()}-${Math.random().toString(16).slice(2)}`;
  }

  const response = await fetch(path, {
    method: options.method ?? 'GET',
    headers,
    body: options.body !== undefined ? JSON.stringify(options.body) : undefined,
  });

  if (response.status === 401 && !options.skipAuthRedirect) {
    notifyAuthInvalid('expired');
    throw new ApiError(401, '令牌无效或已过期');
  }
  if (response.status === 403 && !options.skipAuthRedirect) {
    notifyAuthInvalid('forbidden');
    throw new ApiError(403, await response.text().catch(() => '权限不足'));
  }
  if (!response.ok) {
    throw new ApiError(response.status, await response.text().catch(() => response.statusText));
  }
  const text = await response.text();
  return (text ? JSON.parse(text) : {}) as T;
}

// ---- 契约类型（SHARED API CONTRACT，backend/cmd/server/console_api.go） ----

export interface ConsoleMatch {
  matchId: string;
  state: string;
  onlineUsers: number;
}

export interface AuditRow {
  operatorName: string;
  action: string;
  object: string;
  createdAt: string;
}

export interface Overview {
  matches: ConsoleMatch[];
  onlineSessions: number;
  memory: {
    degraded: boolean;
    backlogDepth: number;
    recentAudit: AuditRow[];
  };
  threadAging: { today: number; d1to3: number; d3plus: number };
  recentProactive: { traceId: string; matchId: string; citation: string; createdAt: string }[];
  // intent-router C3 词汇漏斗：滚动没接明白率 + 最新 unroutable 样本。
  router?: {
    unknownTurns: number;
    totalTurns: number;
    unknownRate: number;
    topUnroutable: { userId: string; content: string; createdAt: string }[];
  };
}

export interface ConsoleUser {
  userId: string;
  online: boolean;
  talkativeness: string;
  openThreads: number;
  portraitUpdatedAt: string | null;
}

export interface ConsoleThread {
  id: string;
  userId: string;
  kind: string;
  content: string;
  state: string;
  ledgerSequence: number;
  createdAt: string;
}

export interface PortraitEntry {
  topic: string;
  subTopic: string;
  content: string;
  updatedAt: string;
}

export interface Portrait {
  entries: PortraitEntry[];
  updatedAt: string | null;
}

export interface DeliveryInterruption {
  traceId: string;
  matchId: string;
  at: string;
}

export interface OperatorRow {
  id: number;
  name: string;
  role: 'director' | 'auditor';
  createdAt: string;
}

// GET /api/console/whoami —— 头部姓名 + 写权限门控（director 才有
// operator:match:write）。
export interface WhoAmI {
  name: string;
  subject: string;
  scopes: string[];
}

// 后端 scope 常量（backend/internal/auth/session.go）。
export const SCOPE_MATCH_WRITE = 'operator:match:write';
export const SCOPE_TRACE_READ = 'operator:trace:read';

export interface TraceRow {
  id: string;
  matchId?: string;
  userId?: string;
  input?: string;
  output?: string;
  reason?: string;
  reasonCodes?: string[];
  relationshipDecision?: { reasonCodes?: string[] } | null;
  // intent-router 4.2：被路由回合携带的路由判定（意图/置信度/槽位）。
  router?: {
    intent: string;
    confidence: number;
    player?: string;
    team?: string;
    score?: string;
    replyUsed?: boolean;
  } | null;
  latencyMs?: number;
  createdAt?: string;
}

export interface MatchEventRow {
  id: string;
  eventType: string;
  clock?: string;
  teamName?: string;
  playerName?: string;
  description?: string;
  confirmed?: boolean;
  createdAt?: string;
}

export interface InteractionEventRow {
  id: string;
  kind: string;
  userId: string;
  inputText?: string;
  outputText?: string;
  deliveryState?: string;
  createdAt: string;
}

// 自动化播报策略（matchstate.AutomationPolicy；mode: active|paused，
// cooldownSeconds 合法域 0-300，eventTypes 见 AUTOMATION_EVENT_OPTIONS）。
export interface AutomationPolicy {
  mode: 'active' | 'paused';
  eventTypes: string[];
  cooldownSeconds: number;
}

// 数据源状态（datasource.MatchSourceStatus；type: manual|replay|api-sports，
// state: ready|standby|running|stopped|error|unconfigured，
// freshness: fresh|degraded|unknown|offline）。
export interface SourceStatus {
  type: string;
  state: string;
  freshness: string;
  latencyMs?: number;
  latencyP95Ms?: number;
  userMayLead?: boolean;
  expectedDelay?: string;
  error?: string;
}

export interface MatchSourceStatus {
  matchId: string;
  activeSource: string;
  sources: Record<string, SourceStatus>;
}

export const consoleApi = {
  overview: () => api<Overview>('/api/console/overview'),
  matchUsers: (matchId: string) =>
    api<{ users: ConsoleUser[] }>(`/api/console/matches/${encodeURIComponent(matchId)}/users`),
  matchEvents: (matchId: string) =>
    api<{ events: MatchEventRow[] }>(`/api/matches/${encodeURIComponent(matchId)}/events`),
  interaction: (matchId: string, userId: string) => {
    const params = new URLSearchParams({ userId, limit: '50' });
    return api<{ events: InteractionEventRow[] }>(
      `/api/matches/${encodeURIComponent(matchId)}/interaction?${params.toString()}`,
    );
  },
  threads: (query: { userId?: string; state?: string } = {}) => {
    const params = new URLSearchParams();
    if (query.userId) params.set('userId', query.userId);
    if (query.state) params.set('state', query.state);
    const qs = params.toString();
    return api<{ threads: ConsoleThread[] }>(`/api/console/threads${qs ? `?${qs}` : ''}`);
  },
  patchThread: (threadId: string, action: 'address' | 'expire') =>
    api<{ thread: ConsoleThread }>(`/api/console/threads/${encodeURIComponent(threadId)}`, {
      method: 'PATCH',
      body: { action },
    }),
  portrait: (userId: string) =>
    api<Portrait>(`/api/console/users/${encodeURIComponent(userId)}/portrait`),
  deletePortraitSlot: (userId: string, topic: string, subTopic: string) => {
    const params = new URLSearchParams({ topic, subTopic });
    return api<Record<string, never>>(
      `/api/console/users/${encodeURIComponent(userId)}/portrait?${params.toString()}`,
      { method: 'DELETE' },
    );
  },
  deliveryInterruptions: () =>
    api<{ recent: DeliveryInterruption[] }>('/api/console/delivery-interruptions'),
  // 既有路由：引用过滤参数由 backend 侧新增。
  traces: (matchId: string, citationPrefix: string, limit = 50) => {
    const params = new URLSearchParams({ limit: String(limit) });
    if (citationPrefix) params.set('citation', citationPrefix);
    return api<{ traces: TraceRow[] }>(
      `/api/matches/${encodeURIComponent(matchId)}/traces?${params.toString()}`,
    );
  },
  // Operators 页（director-only）+ 当前运营员身份。
  whoami: () => api<WhoAmI>('/api/console/whoami'),
  operators: () => api<{ operators: OperatorRow[] }>('/api/console/operators'),
  createOperator: (name: string, role: 'director' | 'auditor') =>
    api<{ operator: OperatorRow; token: string }>('/api/console/operators', {
      method: 'POST',
      body: { name, role },
    }),
  revokeOperator: (name: string) =>
    api<Record<string, never>>(`/api/console/operators/${encodeURIComponent(name)}`, {
      method: 'DELETE',
    }),
  // 比赛层设置（operator.html #automation/#sources 的迁移动能，Task 2.7）。
  getAutomation: (matchId: string) =>
    api<{ policy: AutomationPolicy }>(`/api/matches/${encodeURIComponent(matchId)}/automation`),
  setAutomation: (matchId: string, policy: AutomationPolicy) =>
    api<{ policy: AutomationPolicy }>(`/api/matches/${encodeURIComponent(matchId)}/automation`, {
      method: 'POST',
      body: policy,
    }),
  getSources: (matchId: string) =>
    api<{ status: MatchSourceStatus }>(`/api/matches/${encodeURIComponent(matchId)}/sources`),
  startSource: (
    matchId: string,
    config: { type: string; fixtureId?: number; expectedDelay?: string },
  ) =>
    api<{ status: MatchSourceStatus }>(
      `/api/matches/${encodeURIComponent(matchId)}/sources/start`,
      { method: 'POST', body: config },
    ),
  stopSources: (matchId: string) =>
    api<{ status: MatchSourceStatus }>(`/api/matches/${encodeURIComponent(matchId)}/sources/stop`, {
      method: 'POST',
      body: {},
    }),
  // 人工接管：停用外部数据源 + 自动化策略置 paused。
  takeover: (matchId: string) =>
    api<{ policy: AutomationPolicy; status: MatchSourceStatus }>(
      `/api/matches/${encodeURIComponent(matchId)}/takeover`,
      { method: 'POST', body: {} },
    ),
};
