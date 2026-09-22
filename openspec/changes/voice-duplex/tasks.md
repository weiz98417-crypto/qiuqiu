# Tasks: Voice Duplex

- [ ] 1.0 现状盘点：原生+web 播放期收音/ASR 会话/中断语义实测，差距清单入 tasks（先于一切改动）。
- [ ] 1.1 客户端播放期收音保持 + 抢断判定（置信门/时长门/squash 窗，config 可关）。
- [ ] 1.2 stopPlayback→interrupt→用户话轮接管链路 + 恢复播放兜底。
- [ ] 1.3 服务端：播放期 asr_chunk 接纳确认（或放开）；中断取消语义复用确认。
- [ ] 1.4 自打断检测/降级 + telemetry 事件。
- [ ] 1.5 evals：抢断正例、回声负例、降级开关；平台各一轮校准。
- [ ] 1.6 验证：全量 go test + flutter test + evals 绿。

## Sequencing

I 系列第 1 个（架构评审 I 项，用户裁决一步到位）。独立于 A-H；建议排其后专注实施。
