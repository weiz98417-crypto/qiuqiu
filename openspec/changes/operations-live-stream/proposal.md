# Operations Live Stream: ops 实时流 + console 路由收敛

## Why

导播在 DirectorLive 只看自己注入的回显，看不到全场活跃会话的实时状态；ledger 事件落库后运营只能翻页回看。同时 console 双 API 面（console_api.go 手写字符串 switch + match_operator_api.go ~20 处手抄 `authz.authorize` + 三处手抄审计三段式）让每个新端点都要拼一遍纪律——ops 流要新增端点与 scope 声明，正好逼出路由收敛。

## What Changes

- **ops 实时流**：hub 加 `/ws/ops` 单一全局主题，复用现有 WS Claims 提升（subprotocol/identity）+ `TraceRead` scope 守卫（auditor 与 director 均可订）。事件源 = interaction ledger `Append` 的 observer 旁路（`observingLedger` 装饰器，Append 后非阻塞广播，饱和丢弃仅计数）。事件形状 `{kind, matchId, userId, traceId?, latencyMs?}`——**永不携带正文**（CONTEXT.md 运营观测词条纪律）。
- **直播监听页**（NAV 新项，Chatwoot inbox 双栏交互）：左栏实时事件流+按 kind/user 过滤，右栏点事件跳转关联 trace 的回放（波 1 TurnReplay 数据面）；DirectorLive 工作流不动。
- **路由收敛（E）**：console_api.go / match_operator_api.go 手写 switch → Go 1.27 标准库 `net/http.ServeMux` pattern（`GET /api/console/threads/{id}` 等；2026-09-28 核查：pattern 无演进也无坑，不引 chi）；`withScope(scope, withAudit(intent, handler))` 装饰器链收敛手抄 authorize 与审计三段式。**行为保持不变**：错误响应文本、状态码一律原样（golden/evals 断言不被破坏），收敛调用方式而非输出格式。

## User Stories

1. As a 导播, I want 直播监听页看到全场话轮/打断/微反应实时流, so that 人工注入时机贴合现场。
2. As a 审计员, I want 实时流只含事件类型与延迟, so that 质检不打穿用户隐私。
3. As a 单人开发者, I want 新 console 端点只写一份声明, so that 漏抄鉴权/审计从纪律问题变成结构问题。

## Non-goals

- 按 match 粒度订阅（单一主题先行；「auditor 不该看某比赛」的需求出现再裂变 scope——一个适配器是假设缝）；Centrifugo/Supabase Realtime（负结论，ADR-0021）；正文/转写推送；错误格式统一（行为变化单列，不混入重构）。

## Success Criteria

- ops 流单测：observer 广播、无正文断言（结构体层面禁字段）、饱和丢弃计数、TraceRead 拒绝路径。
- 路由迁移后 golden + console evals + pr tier 全绿（行为不变回归）；`withScope` 全覆盖原 20 处 authorize 调用点；go 全量 + `-race` 抽验绿。
