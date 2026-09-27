# Tasks: Operations Metrics Stack

- [x] 2.1 ADR-0021《运营观测只读旁路》：引入决策+隐私三层+负结论（不迁框架/不引 Centrifugo/Langfuse 后置）。
- [x] 2.2 `deploy/grafana/init.sql`：只读账号（GRANT SELECT 四表）+ datasource provisioning yaml。
- [x] 2.3 六面板 dashboards JSON as code（面板②标注波 1 数据依赖，落地前显示 no data 属预期）。
- [x] 2.4 console 观测页：iframe kiosk + `QIUQIU_GRAFANA_URL` 未配置占位降级；NAV 注册。
- [x] 2.5 运维文档：Windows 本机起停（E:\tools\grafana、127.0.0.1 绑定、切换 OSS 步骤）。
- [x] 2.6 验证：面板 SQL 隐私审查（无正文列）逐条过；console spec 观测页两态 mock；console build + pr tier 绿。

## Sequencing

波 2（与波 1 并行；仅面板②依赖波 1 的 latencyStages 数据）。

## 触发型留尾（Q3 标准格式）

- 触发：部署轮 compose 落地；动作：Grafana 生产化（auth-proxy 反代统一鉴权 + compose 服务）+ Metabase 单容器（`MB_DB_TYPE=postgres`，注意 JAR 裸跑需 Java 21、官方镜像免管）。
- 触发：波 1 回放 + 六面板用一阵后仍缺跨轮 LLM 检索/离线评分/prompt 版本对比；动作：Langfuse/Phoenix OTLP 双写（倾向 Phoenix 单容器起步：Dynatrace 收购后仍高频发版、ELv2 未变、比 Langfuse v4 四件套 Postgres+ClickHouse+Redis+S3 轻；Go 无官方埋点包，用 OTel Go SDK v1.46 手打 span）。
- 触发：波 3 ops 流稳定；动作：活跃会话数面板（广播计数入 Grafana 或面板直查落库快照）。
- 触发：面板④按 kind 聚合变慢；动作：`interaction_ledger(kind, created_at)` 部分索引。
- 触发：运营要看微反应「为什么没发」（quiet/限频/风暴去重归因）；动作：`backchannel.Decide` 拒绝路径补痕（连接内计数→定期落 ledger 聚合行），面板④升级为带归因的发出率。
