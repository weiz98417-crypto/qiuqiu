# Grafana 告警链路:值班感知补齐(零新组件)

## Why

运营端只有面板没有告警——出事靠人盯。可告警指标全部现成:五段语音延迟 P95、熔断器开合(tts/asr/llm 三 breaker)、/ws/ops 饱和丢弃率、api-sports poller freshness 与 P95(datasource/manager.go:343-404 已推导)。Grafana 13.2 本机二进制+provisioning as code 已立(ADR-0021),告警规则是同一套 IaC 的自然延伸;DingTalk 是 Grafana 内建 contact point 类型(配置非开发,国内直达)。

## What Changes

- 四条首发规则(落 `deploy/grafana/provisioning/alerting/`):①语音延迟 P95 超阈(现五段;voice-streaming-delivery 落地后补 tts_first_audio 段)②熔断器开启 ③/ws/ops 饱和丢弃率增长 ④poller freshness 超窗(停摆检测)。
- contact points:钉钉(内建 DingTalk 类型,自定义机器人 webhook)+通用 webhook 兜底;恢复通知开。
- 告警查询走既有 `qiuqiu_grafana_ro` 只读账号(无新权限面);阈值**首发宽松**(演示环境无真实负载,「只有真故障才响」),YAML 显式参数,观测一周后收紧。
- console Observation 页嵌 Grafana alert list 面板 iframe(复用既有同源反代+CSS 隐藏 chrome 的全部机制)。
- ADR-0021 修订注:告警 provisioning 与面板同源同纪律,只读边界不变。

## User Stories

1. As a 运营值班者, I want 真故障主动找到我(钉钉), so that 不用盯面板。
2. As a 运维者, I want 告警规则 as code, so that 可评审、可回放、可归因。

## Non-goals

- Uptime Kuma 外部拨测(后置部署轮;当前单机演示无拨测对象)。
- 值班排班(轻量开源真空:OnCall OSS 已归档;钉钉群 @人 即值班)。
- Keep 告警聚合(Elastic 收购后许可证待观察,二阶段)。
- 自建告警中心/告警进 /ws/ops 流(iframe 嵌入即可)。
- Grafana 进 compose(照 ADR-0021 生产化后置既定立场)。

## Success Criteria

- provisioning 重载后规则出现在 Grafana 告警页;
- rule test/手动造数真实触发,通知**实际到达钉钉群**(验收本体,不能只看 UI firing);
- 规则 YAML 进 repo 评审;告警查询口径与面板口径一致性核对(同表同列,防「告警的数和面板的数对不上」)。
