# Operations Metrics Stack: 观测指标栈最小闭环（Grafana as code）

## Why

时序/指标类观测（延迟分布、抢断率、频次、漏斗、送达率）自写 = 每个面板走一遍「后端路由→handler→client.ts→页面→mock→golden」七处改动，单人开发永远排不上——而这是开源最成熟的地带。数据已全部落在 Postgres 账本（`agent_traces`/`interaction_ledger`/`delivery_ledger`/`proactive_reminders`），缺的只是只读查询面。

## What Changes

- **Grafana 13 OSS Windows 二进制本机闭环**（`E:\tools\grafana`，下载页手动切 OSS；2026-09-28 核查：v13.2.2，`allow_embedding`+匿名 Viewer 路径无弃用）+ Postgres 只读账号 `qiuqiu_grafana_ro`（仅 SELECT 四张账本表）。
- **provisioning as code**：`deploy/grafana/`（datasource yaml + 六面板 dashboard JSON+SQL 进仓库，改面板走评审）。
- **六面板**（默认窗口 24h/7d 两档）：
  1. 语音总延迟 P50/P90 时序 — `agent_traces.latency_ms`
  2. 延迟分段堆积 — `voice->'latencyStages'`（**依赖 operations-turn-replay 波 1 落地后才有数据**）
  3. 抢断/打断率 — `delivery_ledger` interrupted 占比 + `interaction_ledger` playback_state
  4. backchannel 发出率 — 发出数（`interaction_ledger kind='backchannel'`）对白名单事件数（`match_events` 白名单类型计数）的比值；`Decide` 拒绝路径无痕，不承诺「限频命中」粒度（留尾）
  5. 投递七态漏斗 — `delivery_ledger` state 计数
  6. 主动触达送达率 — `proactive_reminders`：口径 **delivered / (delivered + suppressed + missed)**，missed = status 仍非 delivered 且 `expire_at` 已过（表无 expired 状态值，045 migration 实证）。
- **console NAV「观测」项**：纯 `<iframe>` kiosk（不使用已 404 的 grafana-iframe-react）；`QIUQIU_GRAFANA_URL` 未配置时显示部署指引占位（与 Operators 页 501 降级同模式）。
- **ADR-0021**：运营观测只读旁路——账本仍是事实源（ADR-0002 不破）、隐私三层（聚合面无正文/回放同 User 页权限/实时流无正文）、负结论（不迁 low-code/CRUD 框架、不引 Centrifugo/Supabase Realtime、Langfuse 后置）。

## User Stories

1. As a 运营员, I want 打开观测页看到延迟与打断率的趋势, so that 不用等开发者跑日志。
2. As a 单人开发者, I want 新指标只是加一条 SQL 面板, so that 观测债不再累积。

## Non-goals

- auth-proxy 生产化与 compose 部署（部署轮合流）；Metabase（部署轮留尾）；Langfuse/Phoenix（触发后置）；活跃会话数面板（无持久数据源，等波 3 ops 流）。

## Success Criteria

- 六面板 provisioning 重启加载绿；观测页配置/占位两态渲染正确；面板 SQL 无任何用户正文列（隐私纪律，评审时逐条过）；console build + pr tier 绿。
