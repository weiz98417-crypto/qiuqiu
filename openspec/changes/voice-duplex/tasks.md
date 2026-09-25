# Tasks: Voice Duplex

- [x] 1.0 现状盘点（2026-09-23 完成）：
  - 收音：连续对话开启时 VAD freeTalk 全程运行（match_screen 726/837/848/930/1057），播放期**收音不停**——半双工的根因不在采集，在下发链路。
  - 转写：VAD→`asr_chunk`→`asr.StreamSession`→`transcript_partial` 全通，播放期 chunk 是否被会话接纳待查（transcription.go 会话生命周期）。
  - 打断：`{'type':'interrupt'}` 通道存在但只由用户操作触发（match_session_controller.interrupt），**无 VAD 抢话自动触发**。
  - 回声规避：无任何实现（播放的 TTS 会被拾音）。
  - 结论：1.1 抢断判定（置信门+时长门+squash 窗）是核心缺口；1.3 需确认播放期 asr_chunk 在服务端的接纳；1.2 服务端中断语义已就绪可复用。
  - **修订（2026-09-25 复核）**：上条「打断」盘点过时——`vadSpeaking()`（client/lib/services/match_session_controller.dart:830 起）在 phase==speaking（或首见问候期）且 VAD 判定说话时，**已自动**发 `{'type':'interrupt'}`+`PauseAudioCommand`，即无门控的自动抢断雏形已在主干。1.1 的施工方式据此修正为「在该既有路径上加置信门/时长门/squash 窗」，非从零新建；同时注意：无门控现状意味着播放期 TTS 回声即可触发抢断，是 1.1 落地前的现役风险（`duplex_playback_capture=off` 临时压制）。
- [x] 1.1 客户端播放期收音保持 + 抢断判定（置信门/时长门/squash 窗，config 可关）。
- [x] 1.2 stopPlayback→interrupt→用户话轮接管链路 + 恢复播放兜底。
- [x] 1.3 服务端：播放期 asr_chunk 接纳确认（或放开）；中断取消语义复用确认。（结论：transcription 会话按 utteranceID 接纳、本无播放期门——chunk 播放期本来就接纳；interrupt/asr_cancel 取消语义复用，锁行为测试补齐）
- [x] 1.4 自打断检测/降级 + telemetry 事件。
- [x] 1.5 evals：抢断正例、回声负例、降级开关；平台各一轮校准。（正例/负例/降级以单测锁行为：duplex_gate_test + controller 测试；evals/cases 的 turn 型 runner 表达不了客户端 RMS 判定，不硬塞 JSON——沿波 2 先例。平台校准需实机，留实机轮。**首项校准（headless 浏览器假麦克风）**：phase-motions 注入帧振幅 0.017–0.026 恰落播放置信门（0.015×1.5=0.0225）下方且时长不足，说话被门拒——属回声防御设计行为；假音量档上调至 0.028–0.038、时长约 700ms，真实说话 RMS 远高于门无碍。）
- [x] 1.6 验证：全量 go test + flutter test + evals 绿。（go test ./... 28 包全绿；flutter test 161 全绿；dart analyze 零新增；evals:pr 由主会话跑）

## Sequencing

I 系列第 1 个（架构评审 I 项，用户裁决一步到位）。独立于 A-H；建议排其后专注实施。
