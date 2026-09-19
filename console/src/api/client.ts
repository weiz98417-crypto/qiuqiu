// 运营台 API 客户端：统一携带个人令牌，401/403 集中处理。
// 令牌存放于 localStorage['qiuqiu.console.token']（与 operator.html 的
// 'qiuqiu.operator.token' 同一套惯用法，见 docs/adr/0008）。

export const TOKEN_STORAGE_KEY = 'qiuqiu.console.token';

export class ApiError extends Error {
  status: number;
  body: string;

  constructor(status: number, body: string, message?: string) {
    super(message || `API ${status}: ${body}`);
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

// ---- ADR-0010 人类通道会话：访问令牌只存内存，刷新令牌存 localStorage ----

export const REFRESH_STORAGE_KEY = 'qiuqiu.console.refresh';

let accessToken = '';

function getAccessToken(): string {
  return accessToken;
}

export function clearAccessToken(): void {
  accessToken = '';
}

export function getRefreshToken(): string {
  return localStorage.getItem(REFRESH_STORAGE_KEY) ?? '';
}

export function setRefreshToken(token: string): void {
  localStorage.setItem(REFRESH_STORAGE_KEY, token);
}

export function clearRefreshToken(): void {
  localStorage.removeItem(REFRESH_STORAGE_KEY);
}

export interface LoginResponse {
  accessToken: string;
  refreshToken: string;
  passwordChangeRequired: boolean;
  operator: { name: string; role: string; scopes: string[] };
}

export interface SessionOperator {
  name: string;
  role: string;
  scopes: string[];
}

// sessionOperator 解码访问令牌载荷（仅用于头部展示与身份预热，不做授权判断）。
export function sessionOperator(): SessionOperator | null {
  const token = accessToken;
  if (!token) return null;
  const parts = token.split('.');
  if (parts.length !== 3) return null;
  try {
    // base64url → 字节 → UTF-8 文本：atob 给出的是 Latin-1 串，中文载荷
    // 必须经 TextDecoder 解码。
    const bytes = Uint8Array.from(atob(parts[1].replace(/-/g, '+').replace(/_/g, '/')), (c) => c.charCodeAt(0));
    const payload = JSON.parse(new TextDecoder().decode(bytes));
    if (typeof payload.sub === 'string' && payload.sub) {
      return {
        name: payload.sub,
        role: typeof payload.role === 'string' ? payload.role : '',
        scopes: Array.isArray(payload.scopes)
          ? payload.scopes.filter((s: unknown): s is string => typeof s === 'string')
          : [],
      };
    }
  } catch {
    // 非法载荷按未登录处理。
  }
  return null;
}

// login 用用户名+密码换取访问/刷新令牌对（ADR-0010 登录流）。
export async function login(username: string, password: string): Promise<LoginResponse> {
  const response = await fetch('/api/console/auth/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, password }),
  });
  if (!response.ok) {
    throw new ApiError(response.status, await response.text().catch(() => response.statusText));
  }
  const body = (await response.json()) as LoginResponse;
  accessToken = body.accessToken;
  setRefreshToken(body.refreshToken);
  return body;
}

let refreshInFlight: Promise<boolean> | null = null;

// refreshSession 用刷新令牌换新令牌对（轮换：旧刷新令牌一次性作废）。
// 单飞并发：页面重载时多个请求同时 401，只发一次刷新——轮换令牌是一次性
// 的，第二个并发刷新会拿着已被轮换的旧令牌失败并把整个会话清掉。
export function refreshSession(): Promise<boolean> {
  if (!refreshInFlight) {
    refreshInFlight = doRefreshSession().finally(() => {
      refreshInFlight = null;
    });
  }
  return refreshInFlight;
}

async function doRefreshSession(): Promise<boolean> {
  const refreshToken = getRefreshToken();
  if (!refreshToken) return false;
  try {
    const response = await fetch('/api/console/auth/refresh', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ refreshToken }),
    });
    if (!response.ok) {
      clearRefreshToken();
      clearAccessToken();
      return false;
    }
    const body = (await response.json()) as LoginResponse;
    accessToken = body.accessToken;
    setRefreshToken(body.refreshToken);
    return true;
  } catch {
    return false;
  }
}

// changeSelfPassword 自助改密：旧密码必填，新密码最短 10 字符
//（PATCH /api/console/me/password，ADR-0010）。改密成功后立即用刷新令牌
// 换发新访问令牌——首登场景下旧令牌是不带 scope 的强制改密令牌，不换发
// 会被服务端 403。
export async function changeSelfPassword(oldPassword: string, newPassword: string): Promise<void> {
  await api('/api/console/me/password', {
    method: 'PATCH',
    body: { oldPassword, newPassword },
  });
  await refreshSession();
}

// logout 吊销当前设备的刷新令牌并清空本地会话。
export async function logout(): Promise<void> {
  const refreshToken = getRefreshToken();
  try {
    if (refreshToken) {
      await fetch('/api/console/auth/logout', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ refreshToken }),
      });
    }
  } finally {
    clearAccessToken();
    clearRefreshToken();
  }
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
  // 内部标记：刷新重试后不再二次刷新。
  retriedAfterRefresh?: boolean;
}

// ---- 写路径传输策略（与 operator.html 的 operatorRequestJSON 同语义）----
// 一次逻辑请求 = 一把幂等键：inflight 按 method:path:body 去重并发（双击只发
// 一次），5xx 单次重试与 401 续期重放复用同一把键，409 冲突不重试并给出
// 冲突文案，Idempotency-Replayed 回填到响应对象。parity harness 只锁
// payload 形状，这层语义由 scripts/check-console-transport.mjs 守护。

const WRITE_RETRY_DELAY_MS = 250;

const inflightWrites = new Map<string, Promise<unknown>>();

function newIdempotencyKey(): string {
  if (globalThis.crypto?.randomUUID) return globalThis.crypto.randomUUID();
  return `console-${Date.now()}-${Math.random().toString(16).slice(2)}`;
}

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

// apiError 解析错误体：JSON 的 error 字段优先，409 组装冲突文案。
async function apiError(response: Response): Promise<ApiError> {
  const raw = await response.text().catch(() => '');
  let message = raw.trim();
  try {
    message = JSON.parse(raw).error || message;
  } catch {
    // 裸文本错误体原样使用。
  }
  if (response.status === 409) message = `提交内容冲突：${message || '请刷新后重试'}`;
  if (!message && response.status === 403) message = '权限不足';
  return new ApiError(response.status, raw, message || `请求失败 ${response.status}`);
}

export async function api<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const method = options.method ?? 'GET';
  const bodyJson = options.body !== undefined ? JSON.stringify(options.body) : undefined;
  if (method === 'GET') return executeRequest<T>(path, method, bodyJson, options);
  const inflightKey = `${method}:${path}:${bodyJson ?? '{}'}`;
  const existing = inflightWrites.get(inflightKey);
  if (existing) return existing as Promise<T>;
  const request = executeRequest<T>(path, method, bodyJson, options);
  inflightWrites.set(inflightKey, request);
  try {
    return await request;
  } finally {
    inflightWrites.delete(inflightKey);
  }
}

async function executeRequest<T>(
  path: string,
  method: 'GET' | 'POST' | 'PATCH' | 'DELETE',
  bodyJson: string | undefined,
  options: RequestOptions,
): Promise<T> {
  // 幂等键一次逻辑请求一把：5xx 重试与续期重放复用同一把键，服务端的
  // 幂等去重才能把同一次提交认成一条事件。
  const idempotencyKey = newIdempotencyKey();
  let lastError: Error | null = null;
  for (let attempt = 0; attempt < 2; attempt += 1) {
    const headers: Record<string, string> = {};
    // ADR-0010：访问令牌（人类通道）优先，机令牌（evals/脚本通道）兜底。
    const token = getAccessToken() || getToken();
    if (token) headers.Authorization = `Bearer ${token}`;
    if (bodyJson !== undefined) headers['Content-Type'] = 'application/json';
    if (method !== 'GET') headers['Idempotency-Key'] = idempotencyKey;

    let response: Response;
    try {
      response = await fetch(path, { method, headers, body: bodyJson });
    } catch (error) {
      lastError = error instanceof Error ? error : new Error(String(error));
      if (attempt > 0) throw lastError;
      await delay(WRITE_RETRY_DELAY_MS);
      continue;
    }

    if (response.status === 401 && !options.skipAuthRedirect) {
      // 有刷新令牌时先尝试无感续期，成功则原请求（同一幂等键）重放一次。
      if (!options.retriedAfterRefresh && (getRefreshToken() || getAccessToken())) {
        const refreshed = await refreshSession();
        if (refreshed) {
          return executeRequest<T>(path, method, bodyJson, { ...options, retriedAfterRefresh: true });
        }
      }
      clearAccessToken();
      notifyAuthInvalid('expired');
      throw new ApiError(401, '令牌无效或已过期');
    }
    if (response.status === 403 && !options.skipAuthRedirect) {
      notifyAuthInvalid('forbidden');
      throw await apiError(response);
    }
    if (response.ok) {
      const text = await response.text();
      const data = (text ? JSON.parse(text) : {}) as T;
      if (data && typeof data === 'object') {
        (data as Record<string, unknown>).idempotencyReplayed =
          response.headers.get('Idempotency-Replayed') === 'true';
      }
      return data;
    }
    const error = await apiError(response);
    if (error.status < 500 || attempt > 0) throw error;
    lastError = error;
    await delay(WRITE_RETRY_DELAY_MS);
  }
  throw lastError ?? new ApiError(500, '提交失败');
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

// 比赛事件行（导演页与 Match 页共用；导演页视角的字段全集，
// backend handleMatchAPI 的事件列表输出）。
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

export interface ScoreLike {
  home: number;
  away: number;
}

export interface MatchClockState {
  period: string;
  elapsedSeconds: number;
  running: boolean;
  anchorAt?: string | null;
  version: number;
}

export interface VoiceDraftResponse {
  transcript?: string;
  draft?: Record<string, unknown>;
  warnings?: string[];
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
    api<{ events: DirectorEventRow[]; conflicts?: DirectorConflict[] }>(
      `/api/matches/${encodeURIComponent(matchId)}/events`,
    ),
  // ---- 实战导演页（ADR-0011）：请求形状与 operator-control evals 断言一致 ----
  matchConfig: (matchId: string) =>
    api<Record<string, unknown>>(`/api/matches/${encodeURIComponent(matchId)}/config`),
  matchClock: (matchId: string) =>
    api<{ clock: MatchClockState; snapshot?: { score?: ScoreLike } }>(
      `/api/matches/${encodeURIComponent(matchId)}/clock`,
    ),
  patchClock: (matchId: string, command: Record<string, unknown>) =>
    api<{ clock: MatchClockState; snapshot?: { score?: ScoreLike } }>(
      `/api/matches/${encodeURIComponent(matchId)}/clock`,
      { method: 'PATCH', body: command },
    ),
  publishEvent: (matchId: string, payload: unknown) =>
    api<{ event?: DirectorEventRow; snapshot?: { score?: ScoreLike }; idempotencyReplayed?: boolean }>(
      `/api/matches/${encodeURIComponent(matchId)}/events`,
      { method: 'POST', body: payload },
    ),
  correctEvent: (matchId: string, eventId: string, payload: unknown) =>
    api<{ event?: DirectorEventRow; snapshot?: { score?: ScoreLike }; idempotencyReplayed?: boolean }>(
      `/api/matches/${encodeURIComponent(matchId)}/events/${encodeURIComponent(eventId)}/correct`,
      { method: 'POST', body: payload },
    ),
  factTransition: (matchId: string, factId: string, action: string) =>
    api<unknown>(`/api/matches/${encodeURIComponent(matchId)}/facts/${encodeURIComponent(factId)}/${action}`, {
      method: 'POST',
    }),
  resolveConflict: (matchId: string, conflictId: string, body: Record<string, unknown>) =>
    api<unknown>(`/api/matches/${encodeURIComponent(matchId)}/conflicts/${encodeURIComponent(conflictId)}/resolve`, {
      method: 'POST',
      body,
    }),
  submitVoiceDraft: (matchId: string, body: Record<string, unknown>) =>
    api<VoiceDraftResponse>(`/api/matches/${encodeURIComponent(matchId)}/drafts/voice`, { method: 'POST', body }),
  publishVoiceDraft: (matchId: string, body: Record<string, unknown>) =>
    api<{ event?: DirectorEventRow; snapshot?: { score?: ScoreLike }; idempotencyReplayed?: boolean }>(
      `/api/matches/${encodeURIComponent(matchId)}/drafts/voice/publish`,
      { method: 'POST', body },
    ),
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
