# Tasks: Grafana 告警链路

- [x] 2.1 provisioning:deploy/grafana/provisioning/alerting/ 落规则与 contact point YAML——四条规则(语音延迟 P95/熔断开合/WS 饱和丢弃/fresher 超窗),阈值宽松起步且为显式参数;钉钉(内建 DingTalk 类型)+通用 webhook 兜底;恢复通知开。

  **四条规则的落库面调查与决定(2026-09-30,诚实优先于凑数):**

  - **① 语音延迟 P95 → 规则 `qiuqiu-voice-latency-p95`(已落)**。同面板「语音总延迟 P50/P90」口径:agent_traces.latency_ms、percentile_cont、latency_ms > 0;分位 0.9→0.95、窗口 5m→15m。阈值 p95 > 5000ms(显式参数,演示环境正常 p95 两位数 ms,只有真故障才响),for 5m。
  - **② 熔断器开合 → 换型为 `qiuqiu-provider-error-count`(已落,换型)**。调查:resilience.CircuitBreaker(backend/internal/resilience/circuit_breaker.go)是进程内状态,llm/tts/asr/ambient 四个 client 各持一个(均 NewCircuitBreaker(3, 10s)),CircuitState() 只在内存暴露,**不落 PG、无 SQL 查询面**。换型为同源意图的可查代理:agent_traces.error 非空计数(熔断开启必然先有连续调用失败,账本 error 字段就是这些失败的落库面)。口径:30m 窗口错误 > 5 条(熔断阈值为连续 3 次失败,取 30m>5 保证只有持续性故障达线)。
  - **③ /ws/ops 饱和丢弃率 → 不可查,降级为文档记录(未规则化)**。调查:OpsStream.dropped(backend/cmd/server/ops_stream.go:68)是进程内 atomic.Int64,Dropped() 诊断口在全仓无任何 PG 落库消费方——Grafana 只能查数据源,此指标当前**无诚实数据面,不造代理凑数**。留尾:后端把 dropped 周期落快照表后再补第 4 条规则(rules.yml 文件头留有同等注释)。这也是 rules.yml 只有 3 条规则的原因。
  - **④ poller freshness 超窗 → 换型为 `qiuqiu-poller-stall`(已落,代理)**。调查:freshness 推导(backend/internal/datasource/manager.go deriveSourceStatus, fresh/degraded/offline 标签 + 6–20s 动态阈值)在内存 SourceStatus,**不落 PG**。换型为同管线可查代理:api-sports 事件在 match_events 的落库停摆(poller 停摆的直接后果就是 provider 事件断流)——近 24h 有过 provider 事件(=有比赛进行中)但已 15m 无新事件;无 provider 事件时查询空返回 → noDataState: OK(无比赛不停摆不误报)。
  - contact point `qiuqiu-alerts`:一个 contact point 挂两集成——dingding(钉钉,url 占位 REPLACE_ME_BEFORE_PROD,生产前替换)+ webhook(兜底,指向本机 capture 19999);两集成均 disableResolveMessage: false(恢复通知开)。通知策略 policies.yml:默认路由 → qiuqiu-alerts,按 grafana_folder+alertname 分组聚合,provenance=file。
  - **实测勘误(dingtalk→dingding)**:Grafana 13.2.2 OSS 的钉钉集成类型字符串是 **`dingding`**(grafana/alerting 模块 receivers/schema/known_types.go: DingDingType = "dingding"),proposal 写的「内建 DingTalk 类型」按字面写 `type: dingtalk` 会让整个 provisioning 模块启动失败(unknown integration type: dingtalk,连规则一起加载不了,实测复现)。字段名是 **msgType**(非 messageType,取值 link/markdown)。
  - **env 插值实测**:alerting provisioning 文件支持 ${VAR} 环境变量插值——VAR 设值时 url 展开为实值、加载成功;VAR unset 时展开为空串,dingding 校验「could not find url property in settings」,provisioning 模块失败、Grafana 起不来。故默认占位符 + 注释,不走 env 形态(生产走 env 须保证 VAR 非空)。

- [x] 2.2 真实到达验证:Grafana 重载规则生效;手动造数触发→通知实际到达钉钉群;告警查询与面板查询口径一致性核对(同表同列)。

  **验证记录(2026-09-30 本机闭环,钉钉为占位 URL 故到达面=通用 webhook capture,scripts/grafana-alert-capture.mjs @127.0.0.1:19999):**

  - provisioning 重载后 API 核对:GET /api/v1/provisioning/alert-rules → 3 条规则齐(qiuqiu-voice-latency-p95 / qiuqiu-provider-error-count / qiuqiu-poller-stall);GET /api/v1/provisioning/contact-points → qiuqiu-alerts 两集成(dingding[REDACTED]+webhook);GET /api/v1/provisioning/policies → {"receiver":"qiuqiu-alerts","provenance":"file"};三规则评估 health=ok、state=inactive。
  - 真实到达(必燃法,全管线):API 临时建 canary 规则(rawSql `SELECT 1`,阈值 >0)→ 评估周期 1m 后 state=firing → **capture server 实收 1 条 POST**(2026-09-30 06:43:20 +08,payload:`[FIRING:1] canary 必燃(SELECT 1 阈值>0,验证后删除) qiuqiu-operations (qiuqiu-canary)`,alertCount=1);改 SQL 为 `SELECT 0` → **实收第 2 条 resolved POST**(06:48:20 +08,恰为 group_interval 5m,`[RESOLVED] ...`,恢复通知开得到实证);验证完 canary 已 DELETE(DELETE 返回 204),规则面还原为 3 条。
  - 钉钉真实到达:占位 URL 下钉钉侧发送会失败(Grafana 侧日志可见),生产替换真实机器人 webhook 后走同一管线;本机验收本体(webhook 实收 POST)已达成。
  - 口径一致性:规则① SQL 即面板「语音总延迟 P50/P90」SQL 抬分位/窗口(同表 agent_traces、同列 latency_ms、同算法 percentile_cont、同条件 latency_ms>0),逐字核对一致。

- [x] 2.3 console Observation 页 alert list iframe 嵌入(复用同源反代+CSS 隐藏 chrome)。

  Observation.tsx:面板 iframe(60vh)+ 告警规则 iframe(`/alerting/list?kiosk`,40vh,标题「告警规则(qiuqiu-operations,钉钉 + webhook 兜底)」)纵向堆叠;注入机制泛化为 `iframe[data-grafana-embed]` 双 iframe 共用(告警页追加 nav-toolbar/navbar 隐藏);零新依赖。console `tsc --noEmit` 过、vitest 9/9 过(Observation 页无专属测试,全量跑了 console 现有 2 个测试文件)。面板高度 78vh→60vh 是为两 iframe 同屏的取舍。

- [x] 2.4 ADR-0021 修订注(告警 as code 同源、只读边界不变)+ 规则阈值观测一周记录(收紧动作留尾如实记)。

  ADR-0021 末尾追加修订注(同既有修订注格式:文末 blockquote)。阈值观测一周记录:**2026-09-30 起算**,首周观察三规则 firing/误报与 p95/错误计数分布;收紧动作(分位窗口缩窄、错误阈值降档、停摆窗 900s→60s 量级)留尾至观测期满,届时按 data 决定,不拍脑袋。本机演示环境无真实负载,「只有真故障才响」原则下首周预期零 firing。

## Sequencing

波A,独立可先行;与 trace-genai-alignment 无文件交叉可并行。语音延迟规则在 voice-streaming-delivery 落地后补 tts_first_audio 段(小尾巴,单独提交)——留尾:届时在规则①加 voice->'latencyStages'->>'tts_synthesized' 分段口径。
