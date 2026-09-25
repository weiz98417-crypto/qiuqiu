# Tasks: TTS Provider Seam

- [x] 4.1 ADR-0012 修订注：TTS 收敛进 seam 的范围扩大（ASR 维持现状+理由）。
- [x] 4.2 Provider seam 落形（Synthesizer interface + VoiceOpts{Instruction, Format, Voice} + 流式签名预留）+ Miimo adapter 迁移（熔断器保留）+ fake adapter（确定性静音 WAV）。
- [x] 4.3 情绪→指令 deep module：`instructionFor(affect, act, utterLen)` 表驱动 + 快照单测。
- [x] 4.4 接线：Backchannel 与 Watch Turn 的 TTS 调用带 instruction（Affect State 来源为既有情绪状态）。
- [x] 4.5 release tier 真声 smoke 增情绪样本；pr tier 绿。

## Sequencing

六卡实施波 4（voice-duplex 之后：流式位为 transport 决策留口，情绪指令不依赖 duplex）。真实云 adapter B 显式后置——触发条件：真声 smoke 或用户实测给出「MIMO 表现力不足」证据。
