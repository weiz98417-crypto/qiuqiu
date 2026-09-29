// promptfoo 观测层 provider（openspec/changes/promptfoo-observability）：
// 向运行中的 demo backend 发一条文本话轮（WS user_speech），等 qiuqiu_reply，
// stdout 出回复正文——promptfoo 的 exec provider 逐用例调用本脚本，judge
// 断言只作观测报告，永不参与门禁判定（两层分界立法见该 change proposal）。
//
// 前置：demo server 运行中（env QIUQIU_BASE_URL，默认
// http://127.0.0.1:18090，QIUQIU_DEMO_ACCOUNTS 场景；WS 走 legacy
// qiuqiu-dev-token 协议子协议，与 scripts/demo-seed.mjs 同款）。
//
// 用法：node scripts/promptfoo/realize.mjs --text "刚才谁进球了？"
// 环境变量：QIUQIU_BASE_URL / QIUQIU_PROMPTFOO_MATCH（默认 test）/
//           QIUQIU_PROMPTFOO_TIMEOUT_MS（默认 15000）

const baseUrl = process.env.QIUQIU_BASE_URL || 'http://127.0.0.1:18090';
const matchId = process.env.QIUQIU_PROMPTFOO_MATCH || 'test';
const timeoutMs = Number(process.env.QIUQIU_PROMPTFOO_TIMEOUT_MS || 15000);
const wsToken = process.env.APP_TOKEN || 'qiuqiu-dev-token';

function arg(name) {
  const index = process.argv.indexOf(`--${name}`);
  return index >= 0 ? process.argv[index + 1] : '';
}

const text = (arg('text') || '').trim();
if (!text) {
  console.error('usage: realize.mjs --text "<user utterance>"');
  process.exit(1);
}

const endpoint = new URL(baseUrl);
endpoint.protocol = endpoint.protocol === 'https:' ? 'wss:' : 'ws:';
endpoint.pathname = `/ws/match/${matchId}`;
const protocols = wsToken ? [`qiuqiu-auth.${Buffer.from(wsToken).toString('base64url')}`] : [];

const socket = new WebSocket(endpoint, protocols);
const timer = setTimeout(() => {
  console.error(`realize timeout after ${timeoutMs}ms`);
  process.exit(2);
}, timeoutMs);

socket.addEventListener('error', (event) => {
  clearTimeout(timer);
  console.error(`ws error: ${event.message || 'connection failed'}`);
  process.exit(2);
});

socket.addEventListener('open', () => {
  socket.send(JSON.stringify({
    type: 'user_speech',
    text,
    userId: 'promptfoo-probe',
  }));
});

socket.addEventListener('message', (event) => {
  let message;
  try {
    message = JSON.parse(typeof event.data === 'string' ? event.data : '');
  } catch {
    return; // 二进制帧（voice_audio）忽略
  }
  if (message.type === 'event' && message.event === 'qiuqiu_reply' && message.data?.text) {
    clearTimeout(timer);
    process.stdout.write(String(message.data.text));
    socket.close();
    process.exit(0);
  }
  if (message.type === 'auth_error') {
    clearTimeout(timer);
    console.error(`auth error: ${message.reason}`);
    process.exit(2);
  }
});
