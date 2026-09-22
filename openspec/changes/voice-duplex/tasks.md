# Tasks: Voice Duplex

- [x] 1.0 现状盘点（2026-09-23 完成）：
  - 收音：连续对话开启时 VAD freeTalk 全程运行（match_screen 726/837/848/930/1057），播放期**收音不停**——半双工的根因不在采集，在下发链路。
  - 转写：VAD→`asr_chunk`→`asr.StreamSession`→`transcript_partial` 全通，播放期 chunk 是否被会话接纳待查（transcription.go 会话生命周期）。
  - 打断：`{'type':'interrupt'}` 通道存在但只由用户操作触发（match_session_controller.interrupt），**无 VAD 抢话自动触发**。
  - 回声规避：无任何实现（播放的 TTS 会被拾音）。
  - 结论：1.1 抢断判定（置信门+时长门+squash 窗）是核心缺口；1.3 需确认播放期 asr_chunk 在服务端的接纳；1.2 服务端中断语义已就绪可复用。
- [ ] 1.1 客户端播放期收音保持 + 抢断判定（置信门/时长门/squash 窗，config 可关）。
- [ ] 1.2 stopPlayback→interrupt→用户话轮接管链路 + 恢复播放兜底。
- [ ] 1.3 服务端：播放期 asr_chunk 接纳确认（或放开）；中断取消语义复用确认。
- [ ] 1.4 自打断检测/降级 + telemetry 事件。
- [ ] 1.5 evals：抢断正例、回声负例、降级开关；平台各一轮校准。
- [ ] 1.6 验证：全量 go test + flutter test + evals 绿。

## Sequencing

I 系列第 1 个（架构评审 I 项，用户裁决一步到位）。独立于 A-H；建议排其后专注实施。
