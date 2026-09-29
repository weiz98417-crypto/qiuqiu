#!/usr/bin/env node
// Grafana 告警真实到达验证用 capture server（openspec/changes/grafana-alerting）。
// 零依赖：node scripts/grafana-alert-capture.mjs [port]
//   POST /grafana-alert  —— Grafana webhook contact point 的落点：打印 payload
//                           摘要并计数（「真实到达」的证据面）。
//   GET  /               —— 查看 { received, firstAt, lastAt, lastSummary }。
// 生产等价物是钉钉群机器人；本脚本只用于本机验证通知链路确实打通。
import http from 'node:http';

const port = Number(process.argv[2] ?? 19999);
const hits = [];
const server = http.createServer((req, res) => {
  if (req.method === 'POST') {
    const chunks = [];
    req.on('data', (chunk) => chunks.push(chunk));
    req.on('end', () => {
      const raw = Buffer.concat(chunks).toString('utf8');
      let summary;
      try {
        const body = JSON.parse(raw);
        const alerts = Array.isArray(body.alerts) ? body.alerts : [];
        summary = {
          status: body.status,
          title: body.title,
          ruleTitle: alerts.map((a) => a.labels?.alertname ?? '?').join(','),
          alertCount: alerts.length,
        };
      } catch {
        summary = { rawLength: raw.length };
      }
      hits.push({ at: new Date().toISOString(), path: req.url, summary });
      console.log(
        `[capture] #${hits.length} ${new Date().toISOString()} POST ${req.url} ` +
          JSON.stringify(summary),
      );
      res.writeHead(200, { 'content-type': 'application/json' });
      res.end('{"ok":true}');
    });
    return;
  }
  res.writeHead(200, { 'content-type': 'application/json' });
  res.end(JSON.stringify({ received: hits.length, hits }, null, 2));
});

server.listen(port, '127.0.0.1', () => {
  console.log(`[capture] listening on http://127.0.0.1:${port}/grafana-alert`);
});
