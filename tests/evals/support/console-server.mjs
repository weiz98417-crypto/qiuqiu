import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { extname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

// console E2E 用的内联静态服务 + 契约回退代理（无第三方依赖）：
//
// 1. 静态：伺服 console/dist（Vite 构建产物，base=/console/，HashRouter
//    路由，因此任意 /console* 路径都回落到 index.html）。
// 2. 代理：/api/* 原样转发到真实 Go 后端（方法、Authorization、
//    Content-Type、Idempotency-Key 与请求体全透传，响应状态与 body 回写）。
// 3. 契约回退：当后端对 /api/console/** 返回 404（并行落地的
//    console_api.go 尚未部署）——或该路径命中 `forceMockPrefixes`（路由已
//    落地但 eval 后端集合为空、无法驱动写流程时，由 spec 显式指定）——按
//    SHARED API CONTRACT 用 mockState 应答，并打上 `x-console-contract-mock: 1`
//    头。回退层自己校验令牌：无效令牌 401，auditor 写操作 / 运营员管理
//    403 —— 与真实后端语义一致。后端路由带数据落地后回退自动停用。

const consoleDist = resolve(fileURLToPath(new URL('../../../console/dist', import.meta.url)));

const MIME = {
  '.html': 'text/html; charset=utf-8',
  '.js': 'text/javascript; charset=utf-8',
  '.css': 'text/css; charset=utf-8',
  '.svg': 'image/svg+xml',
  '.json': 'application/json; charset=utf-8',
  '.map': 'application/json; charset=utf-8',
  '.woff2': 'font/woff2',
};

const ALL_SCOPES = ['operator_match_write', 'fact_confirm', 'fact_correct', 'trace_read'];

export function createContractMockState({ appToken }) {
  const now = new Date().toISOString();
  return {
    appToken,
    operatorSeq: 0,
    operatorSecrets: new Map(), // token → { name, role }
    operators: [
      {
        name: '值班导演',
        role: 'director',
        scopes: ALL_SCOPES,
        createdAt: now,
        revokedAt: null,
      },
    ],
    overview: {
      matches: [{ matchId: 'demo-console-e2e', state: 'live', onlineUsers: 2 }],
      onlineSessions: 3,
      memory: {
        degraded: false,
        backlogDepth: 4,
        recentAudit: [
          { operatorName: '值班导演', action: 'thread_address', object: 'thread-1', createdAt: now },
        ],
      },
      threadAging: { today: 2, d1to3: 1, d3plus: 0 },
      recentProactive: [
        {
          traceId: 'trace-proactive-1',
          matchId: 'demo-console-e2e',
          citation: 'proactive_citation:goal',
          createdAt: now,
        },
      ],
    },
    users: [
      { userId: 'console-user-1', online: true, talkativeness: 'chatty', openThreads: 2, portraitUpdatedAt: now },
      { userId: 'console-user-2', online: false, talkativeness: 'quiet', openThreads: 0, portraitUpdatedAt: null },
    ],
    threads: [
      {
        id: 'thread-1', userId: 'console-user-1', kind: 'prediction',
        content: '上半场你预测西班牙 2:0，现在还作数吗？', state: 'open', ledgerSequence: 12, createdAt: now,
      },
      {
        id: 'thread-2', userId: 'console-user-1', kind: 'unanswered_question',
        content: '亚马尔下场还上吗？', state: 'open', ledgerSequence: 13, createdAt: now,
      },
      {
        id: 'thread-3', userId: 'console-user-2', kind: 'promise',
        content: '下次进球给你讲越位规则', state: 'addressed', ledgerSequence: 9, createdAt: now,
      },
    ],
    portraits: {
      'console-user-1': {
        entries: [
          { topic: '球队', subTopic: '主队', content: '偏爱西班牙，常提 2010 年世界杯', updatedAt: now },
          { topic: '观赛习惯', subTopic: '时段', content: '只在晚间看球，早上不聊比赛', updatedAt: now },
        ],
        updatedAt: now,
      },
    },
  };
}

// 返回 null 表示没有 mock 覆盖该路由（继续透传后端响应）。
function mockResponse(state, method, pathname, auth, bodyText) {
  const director = auth.role === 'director';
  const requireDirector = () => (director ? null : { status: 403, body: { error: 'insufficient scope' } });
  const ok = (body) => ({ status: 200, body });

  if (pathname === '/api/console/overview' && method === 'GET') {
    return ok(state.overview);
  }
  if (/^\/api\/console\/matches\/[^/]+\/users$/.test(pathname) && method === 'GET') {
    return ok({ users: state.users });
  }
  if (pathname === '/api/console/threads' && method === 'GET') {
    return null; // 在下方带 query 处理
  }
  if (pathname === '/api/console/threads' && method !== 'GET') {
    return { status: 405, body: { error: 'method not allowed' } };
  }
  if (/^\/api\/console\/threads\/[^/]+$/.test(pathname) && method === 'PATCH') {
    const forbidden = requireDirector();
    if (forbidden) return forbidden;
    const threadId = pathname.split('/').pop();
    const thread = state.threads.find((row) => row.id === decodeURIComponent(threadId));
    if (!thread) return { status: 404, body: { error: 'thread not found' } };
    let action = '';
    try {
      action = JSON.parse(bodyText || '{}').action ?? '';
    } catch {
      action = '';
    }
    if (action !== 'address' && action !== 'expire') {
      return { status: 400, body: { error: 'action must be address|expire' } };
    }
    thread.state = action === 'address' ? 'addressed' : 'expired';
    return ok({ thread });
  }
  if (/^\/api\/console\/users\/[^/]+\/portrait$/.test(pathname) && method === 'GET') {
    const userId = decodeURIComponent(pathname.split('/')[4]);
    const portrait = state.portraits[userId] ?? { entries: [], updatedAt: null };
    return ok(portrait);
  }
  if (/^\/api\/console\/users\/[^/]+\/portrait$/.test(pathname) && method === 'DELETE') {
    const forbidden = requireDirector();
    if (forbidden) return forbidden;
    const userId = decodeURIComponent(pathname.split('/')[4]);
    const portrait = state.portraits[userId];
    if (portrait) portrait.entries = [];
    return ok({});
  }
  if (pathname === '/api/console/operators' && method === 'GET') {
    const forbidden = requireDirector();
    if (forbidden) return forbidden;
    return ok({
      operators: state.operators.map(({ name, role, scopes, createdAt, revokedAt }) => ({
        name, role, scopes, createdAt, revokedAt,
      })),
    });
  }
  if (pathname === '/api/console/operators' && method === 'POST') {
    const forbidden = requireDirector();
    if (forbidden) return forbidden;
    let parsed = {};
    try {
      parsed = JSON.parse(bodyText || '{}');
    } catch {
      return { status: 400, body: { error: 'invalid json' } };
    }
    const name = String(parsed.name ?? '').trim();
    const role = parsed.role === 'director' ? 'director' : 'auditor';
    if (!name) return { status: 400, body: { error: 'name is required' } };
    if (state.operators.some((row) => row.name === name)) {
      return { status: 409, body: { error: 'operator already exists' } };
    }
    state.operatorSeq += 1;
    const token = `mock-operator-token-${state.operatorSeq}-${Math.random().toString(16).slice(2, 10)}`;
    const createdAt = new Date().toISOString();
    state.operators.push({ name, role, scopes: role === 'director' ? ALL_SCOPES : ['trace_read'], createdAt, revokedAt: null });
    state.operatorSecrets.set(token, { name, role });
    return ok({
      operator: { name, role, scopes: role === 'director' ? ALL_SCOPES : ['trace_read'], createdAt, revokedAt: null },
      token,
    });
  }
  if (/^\/api\/console\/operators\/[^/]+$/.test(pathname) && method === 'DELETE') {
    const forbidden = requireDirector();
    if (forbidden) return forbidden;
    const name = decodeURIComponent(pathname.split('/').pop());
    const index = state.operators.findIndex((row) => row.name === name);
    if (index === -1) return { status: 404, body: { error: 'operator not found' } };
    state.operators.splice(index, 1); // 吊销 = 删行，立即生效
    return ok({});
  }
  if (pathname === '/api/console/delivery-interruptions' && method === 'GET') {
    return ok({ recent: [] });
  }
  return null;
}

function authenticate(state, req) {
  const header = req.headers.authorization ?? '';
  const match = /^Bearer (.+)$/.exec(header);
  const supplied = match ? match[1].trim() : '';
  if (!supplied) return { status: 401, role: null };
  if (supplied === state.appToken) return { status: 0, role: 'director' };
  const known = state.operatorSecrets.get(supplied);
  if (known && state.operators.some((row) => row.name === known.name)) {
    return { status: 0, role: known.role };
  }
  return { status: 401, role: null };
}

export async function startConsoleStaticServer({ backendURL, mockState, forceMockPrefixes = [] }) {
  const shouldForceMock = (pathname) =>
    forceMockPrefixes.some((prefix) => pathname.startsWith(prefix));

  const server = createServer(async (req, res) => {
    const url = new URL(req.url ?? '/', 'http://console.test');

    if (url.pathname.startsWith('/api/')) {
      const chunks = [];
      for await (const chunk of req) chunks.push(chunk);
      const bodyText = Buffer.concat(chunks).toString('utf8');

      const headers = {};
      for (const name of ['authorization', 'content-type', 'idempotency-key', 'accept']) {
        if (req.headers[name]) headers[name] = req.headers[name];
      }
      let upstream;
      try {
        upstream = await fetch(`${backendURL}${url.pathname}${url.search}`, {
          method: req.method,
          headers,
          body: methodHasBody(req.method) ? bodyText : undefined,
        });
      } catch (error) {
        res.writeHead(502, { 'content-type': 'text/plain; charset=utf-8' });
        res.end(`console proxy upstream error: ${String(error)}`);
        return;
      }

      const engageMock =
        upstream.status === 404 ||
        (shouldForceMock(url.pathname) && url.pathname.startsWith('/api/console/'));
      if (engageMock) {
        const auth = authenticate(mockState, req);
        if (auth.status !== 0) {
          res.writeHead(auth.status, { 'content-type': 'application/json; charset=utf-8', 'x-console-contract-mock': '1' });
          res.end(JSON.stringify({ error: auth.status === 401 ? 'unauthorized' : 'forbidden' }));
          return;
        }
        // 带 query 的列表路由单独处理（pathname 匹配不含 query）。
        let mocked = mockResponse(mockState, req.method, url.pathname, auth, bodyText);
        if (!mocked && url.pathname === '/api/console/threads' && req.method === 'GET') {
          const userId = url.searchParams.get('userId') ?? '';
          const threadState = url.searchParams.get('state') ?? '';
          const rows = mockState.threads.filter(
            (row) => (!userId || row.userId === userId) && (!threadState || row.state === threadState),
          );
          mocked = { status: 200, body: { threads: rows } };
        }
        if (mocked) {
          res.writeHead(mocked.status, {
            'content-type': 'application/json; charset=utf-8',
            'x-console-contract-mock': '1',
          });
          res.end(JSON.stringify(mocked.body ?? {}));
          return;
        }
      }

      const buffer = Buffer.from(await upstream.arrayBuffer());
      res.writeHead(upstream.status, {
        'content-type': upstream.headers.get('content-type') ?? 'application/json; charset=utf-8',
      });
      res.end(buffer);
      return;
    }

    // 静态资源：/console/assets/* → dist/assets/*；/console* → index.html（HashRouter）。
    let relative = url.pathname.replace(/^\/console\/?/, '');
    if (!relative || relative.endsWith('/')) relative = 'index.html';
    const filePath = resolve(join(consoleDist, decodeURIComponent(relative)));
    if (!filePath.startsWith(consoleDist)) {
      res.writeHead(403);
      res.end('forbidden');
      return;
    }
    try {
      const file = await readFile(filePath);
      res.writeHead(200, { 'content-type': MIME[extname(filePath)] ?? 'application/octet-stream' });
      res.end(file);
    } catch {
      // HashRouter：非资源路径一律回落 index.html。
      try {
        const index = await readFile(join(consoleDist, 'index.html'));
        res.writeHead(200, { 'content-type': MIME['.html'] });
        res.end(index);
      } catch (error) {
        res.writeHead(500, { 'content-type': 'text/plain; charset=utf-8' });
        res.end(`console/dist missing — run "npm run build" in console/ first (${String(error)})`);
      }
    }
  });

  await new Promise((resolveListen, reject) => {
    server.once('error', reject);
    server.listen(0, '127.0.0.1', () => resolveListen());
  });
  const { port } = server.address();
  return {
    baseURL: `http://127.0.0.1:${port}`,
    async close() {
      await new Promise((resolveClose) => server.close(resolveClose));
    },
  };
}

function methodHasBody(method) {
  return method !== 'GET' && method !== 'HEAD';
}
