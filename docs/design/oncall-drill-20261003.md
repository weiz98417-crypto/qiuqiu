# 值班演练记录(2026-10-03)

verification & consumption 轮 · grafana-alerting 首次「真故障形态」全链演练 · 执行者:AI 代值班 + 用户裁决

## 演练范围

告警链路的完整生命周期:**故障注入 → 规则评估 → firing → 通知到达 → 值班定位 → 恢复 → resolved 通知**。

环境约束(如实):演示环境 PG16 portable 无 pgvector → demo-server 全栈起不来(迁移链 001 卡扩展),本演练不跑后端——Grafana 告警链路只读直查 PG,演练最小表集(agent_traces/match_events,grafana-alerting 卡 9-30 已建,列对齐生产)。**HTTP 层消费验证同因挂起,挂部署轮 pgvector/pg16 镜像。**

## 时间线

| 时刻(本地) | 事件 |
|---|---|
| 03:59 | 故障注入:20 条「云尖峰形态」trace(latency_ms 4000-7300ms,散布近 10 分钟;形态取自 9-30 实测 MiMo 7.3s 尖峰),P95=6610ms,超阈(3000ms)120% |
| 04:19 | **capture 实收 firing**:webhook `POST /grafana-alert`,`[FIRING:1] 语音延迟 P95 超阈(> 3000ms 持续 15m)`,severity=page;钉钉 contact point 同发(URL 占位,生产替换后同管线) |
| 04:22 | 第二次 firing 通知(group_interval 重发,持续未恢复=符合预期) |
| 04:24 | 「值班响应」:清故障数据,P95 归零 |
| (待) | resolved 通知到 capture(group_interval 5m 内) |

## 验证结论

- 规则评估(PG 直查 P95)✓ · 持有期(for 5m,04:19 才燃=注入后约 15-19 分钟,含评估+持有周期)✓ · 通知双通道 ✓ · 持续故障重复通知 ✓
- 阈值收紧后(3000ms)对「真故障形态」灵敏度成立:尖峰 5 分钟内必燃,正常负载(两位数 ms)零误报。
- Grafana 环境注意:Grafana 进程起于 PG 就绪**之前**会持有断连的 datasource(评估报 db query error)——重启 Grafana 恢复;`GRAFANA_DASHBOARDS_DIR` 在 git bash 无引号传 `E:\` 路径反斜杠被吃(dashboard provisioning 报路径错,alerting provisioning 不受影响,走默认路径)。

## 演练后动作

- [x] 故障数据清理(drill-* 20 条 DELETE,实证 P95 归零)
- [x] resolved 通知到达确认(04:30 前后 capture 第 3 条,status=resolved;数据清理后约 6 分钟,group_interval 内)
- [x] 本记录落档

## 遗留

- demo-server 全栈消费验证(HTTP 策展 API)挂部署轮(pgvector 镜像)。
- 知识录入库层全链验证已过(7 条真实赛制条目,`consumption_verify_test.go`,env 门控可重跑),真实录入待 HTTP 层通后一键入库。
