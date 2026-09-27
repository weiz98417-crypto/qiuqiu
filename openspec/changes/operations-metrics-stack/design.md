# Design: Operations Metrics Stack

## 只读旁路架构

账本仍是唯一事实源（ADR-0002）：Grafana 经只读账号 `qiuqiu_grafana_ro` 直查 Postgres，权限仅 `SELECT` 四表（agent_traces/interaction_ledger/delivery_ledger/proactive_reminders），物理上写不了任何账本。观测面板是旁路 adapter——删除测试：删 `deploy/grafana` 与观测页，console 回到七页原样，账本无损。

## 隐私三层（ADR-0021 主体，CONTEXT.md「Operations Observation」词条已立）

- 面板 SQL 只输出聚合数字（count/percentile/占比），**禁止**任何正文列（input/output/asrText/phrase）；用户维度最多到 user_id 哈希或计数 Top-N（默认不开放 user_id 明文列）。
- 面板快照/告警通知天然继承此纪律——配置里不引用含正文字段即不泄漏。
- 单轮回放（波 1）与实时流（波 3）各按其词条边界，不经 Grafana。

## 本机闭环形态（Q1-c）

- Grafana 13 OSS Windows 二进制（`E:\tools\grafana`，仓库不存二进制，README 记下载与切换 OSS 步骤）；`GF_SECURITY_ALLOW_EMBEDDING=true` + 匿名 Viewer（仅观测组织）。
- 生产化（auth-proxy 反代注入运营员身份、compose）后置到部署轮：本机闭环的匿名 Viewer 只绑 127.0.0.1 监听，不构成暴露面。
- console 侧：`QIUQIU_GRAFANA_URL` 环境变量 → Go 后端注入观测页配置接口（或构建时 env，实施取简者）；未配置渲染占位指引卡。

## 数据口径备忘（面板 SQL 直接引用）

- 分段堆积用 `voice->'latencyStages'->>'turn_decided'` 等 JSONB 取值；值为相对 speech_received 的累计毫秒（波 1 语义）。
- backchannel 面板口径：发出数 ÷ 白名单事件数（`match_events` 的 big_chance/miss/save/var_check 计数）；`backchannel.Decide` 的拒绝路径（quiet/限频/风暴去重）当前无痕——若将来要看拒绝归因，先给 Decide 拒绝路径补痕（留尾），不在此面板预支。
- 送达率面板的 missed 判定：`status != 'delivered' AND expire_at < now()`——045 表无 expired 状态值，勿写 `status='expired'`。
- `interaction_ledger` 现有索引仅 (user_id, match_id, created_at)：按 kind 的时序聚合走顺序扫描，30 天 expires_at 窗口下单机量级可接受；面板默认窗口 24h/7d。索引不预加（YAGNI），留尾。
- `agent_traces` 无清理任务：面板②长期运行按窗口过滤即可。

## 删除测试

删 deploy/grafana：console 观测页退占位态，账本无损——旁路成立。
