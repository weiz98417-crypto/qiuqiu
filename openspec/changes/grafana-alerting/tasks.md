# Tasks: Grafana 告警链路

- [ ] 2.1 provisioning:deploy/grafana/provisioning/alerting/ 落规则与 contact point YAML——四条规则(语音延迟 P95/熔断开合/WS 饱和丢弃/fresher 超窗),阈值宽松起步且为显式参数;钉钉(内建 DingTalk 类型)+通用 webhook 兜底;恢复通知开。
- [ ] 2.2 真实到达验证:Grafana 重载规则生效;手动造数触发→通知实际到达钉钉群;告警查询与面板查询口径一致性核对(同表同列)。
- [ ] 2.3 console Observation 页 alert list iframe 嵌入(复用同源反代+CSS 隐藏 chrome)。
- [ ] 2.4 ADR-0021 修订注(告警 as code 同源、只读边界不变)+ 规则阈值观测一周记录(收紧动作留尾如实记)。

## Sequencing

波A,独立可先行;与 trace-genai-alignment 无文件交叉可并行。语音延迟规则在 voice-streaming-delivery 落地后补 tts_first_audio 段(小尾巴,单独提交)。
