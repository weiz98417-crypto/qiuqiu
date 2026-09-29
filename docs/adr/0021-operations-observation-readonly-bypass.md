# 0021 · 运营观测只读旁路（Grafana as code，观测不建新事实源）

日期：2026-09-28（operations-metrics-stack）

## 背景

C 端六轮落地语音双工/smart-turn/backchannel/RAG 后，运营观测缺口显形：指标类观测（延迟分布、抢断率、微反应频次、投递漏斗、触达送达率）自写意味着每个面板走「后端路由→handler→client.ts→页面→mock→golden」七处改动，单人开发永远排不上；而数据早已全部落在 Postgres 账本。开源调研（2026-09）结论：Grafana 13（AGPL）单二进制 + Postgres 只读数据源即覆盖全部事实型面板；低代码管理台/实时广播服务/Langfuse v4 全家桶在这个体量下均为负收益（见被否决方案）。

## 决定

1. **账本仍是唯一事实源**（ADR-0002 不破）：Grafana 经只读账号直查 Postgres 四表（agent_traces/interaction_ledger/delivery_ledger/proactive_reminders），权限仅 SELECT。观测是旁路 adapter——删除 deploy/grafana 与观测页，系统原样。
2. **面板 provisioning as code**：datasource 与 dashboard JSON 进仓库 `deploy/grafana/`，改面板走评审——面板 SQL 就是指标定义，散在个人浏览器里会漂移。
3. **隐私三层**（CONTEXT.md「Operations Observation」词条的落地纪律）：
   - 指标面（Grafana）：只输出聚合数字（count/percentile/占比），禁止任何正文列（input/output/asrText/phrase）；面板快照与告警通知天然继承此纪律。
   - 单轮回放（console TurnReplay）：转写正文仅在内嵌于既有权限页（CitationAudit 抽屉/User 页）时可见，不设独立路由即不产生新入口。
   - 实时流（operations-live-stream）：只推事件类型/计数/延迟，正文在序列化前的结构体层面就不存在。
4. **生产化后置**：本机闭环（Windows 二进制 + 匿名 Viewer 绑 127.0.0.1）；auth-proxy 反代统一鉴权与 compose 部署合流到部署轮。Metabase（运营自助 SQL）同轮后置。

## 被否决的替代方案

- **Appsmith/refine/react-admin 迁移**：7 页高度定制的审计/时间线/状态板换成 low-code 或 CRUD 框架 = 第二套鉴权 + 版本漂移，无用户可见收益；手写页是资产不是债务。
- **Centrifugo / Supabase Realtime**：已有自研 WS hub，ops 订阅约 200 行自写；引外部广播服务是一个适配器变两个。
- **Langfuse v4 自托管**：Postgres+ClickHouse+Redis+S3 四件套对单人项目运维偏重；留尾（触发：回放+面板用一阵后仍缺跨轮检索/离线评分），届时倾向 Phoenix 单容器（ELv2、Dynatrace 收购后仍高频发版、OTel Go SDK v1.46 手打 span 可行）。
- **Helicone 自托管 / Superset / SigNoz**：代理层 50 行自写更省 / Metabase 单容器已覆盖 / 无基础设施监控刚需时是纯运维负担。
- **活跃会话数面板**：无持久数据源（在线数在内存），落快照表与波 3 ops 流重复建设——砍掉，等 ops 流。

## 后果

- 新指标成本从 7 处代码降为 1 条 SQL 面板；观测债停止累积。
- `agent_traces` 无清理任务、`interaction_ledger` 只有 (user_id, match_id, created_at) 索引：默认窗口 24h/7d 下单机量级可接受；变慢再补部分索引（留尾在 tasks）。
- backchannel「限频命中」无数据源（Decide 拒绝路径无痕），面板口径为发出数÷白名单事件数；拒绝归因留尾。

> 2026-09-30 修订（告警链路，openspec/changes/grafana-alerting）：告警规则与
> contact point/通知策略进 `deploy/grafana/provisioning/alerting/`，与本决定 2
> 同一 as-code 纪律——SQL 就是阈值定义，改规则走评审；告警查询仍走只读账号
> `qiuqiu_grafana_ro`，只读边界不变（决定 1 不破）。两点实测补充：(a) 钉钉
> 集成在 Grafana 13.2 OSS 的类型字符串是 `dingding`（非 dingtalk），字段名
> `msgType`；(b) 落库面调查后不可查的指标不凑数——熔断器开合（进程内
> resilience.CircuitBreaker）换型为 agent_traces.error 计数、poller freshness
> （内存 SourceStatus）换型为 match_events provider 事件停摆检测，/ws/ops
> 饱和丢弃（内存计数）暂不规则化、降级为文档记录，待后端落快照表补齐。
> 告警通知是本决定隐私三层之「指标面」的自然延伸：通知 payload 只含规则
> 名/标签/聚合值，不含正文列。
