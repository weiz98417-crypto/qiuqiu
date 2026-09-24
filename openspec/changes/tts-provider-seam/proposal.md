# TTS Provider Seam: 供应商解耦与情绪→语音指令映射

## Why

`backend/internal/tts/client.go` 内嵌 Miimo 供应商默认值（api.xiaomimimo.com、mimo-v2.5-tts、voice 冰糖、WAV 整段 base64），无 Provider 缝——对比 llm/router 已收敛 openaicompat（ADR-0012），TTS 是单 adapter 的假设性缝；换供应商/加 A/B 都要改调用方。合成整段 WAV 非流式（返回解析 choices[0].message.audio.data，无 stream 痕迹），首包延迟天花板锁死。同时 `SynthesizeWithInstruction` 的自然语言 instruction 通道（client.go:112-115，MIMO 唯一风格通道）没有被系统性利用——Affect State（情绪状态）到语音表现之间没有映射层，进球/红牌的 Backchannel 无情绪。ASR 同病（internal/asr/client.go）但本轮不动，如实收缩。

## What Changes

- **修订 ADR-0012**（修订注，走 0017 先例）：TTS 从「仅鉴权复用」扩大为收敛进统一 seam；ASR 维持现状、理由如实记录。修订**不触碰** ADR-0012 的被拒备选「统一信封覆盖 asr/tts」——TTS 载荷（audio 信封）形状原样，只收敛调用点进 seam。
- `internal/tts` 收敛 Provider seam（照 openaicompat 先例形状）：adapter A = Miimo 现役迁移（含现有熔断器）；第二 adapter = 测试替身（fake，返回确定性静音 WAV）——两个 adapter 使 seam 成真。
- 新增 **Affect State→语音指令 deep module**：纯函数映射（情绪状态 × 沟通动作 → instruction 文本），Backchannel 短反应映射到短指令与紧凑时长倾向；经 instruction 通道对现役 Miimo **即刻生效**，不等供应商切换。
- interface 留流式位（签名预留 `SynthesizeStream` 或等价），adapter A 如实整段实现并标注非流式——不假装流式；流式位为 voice-transport-upgrade 决策后的真流式 adapter 留口。

## User Stories

1. As a 用户, I want 进球时球球的声音有真实的兴奋, so that 陪伴感不靠文本单腿走路。
2. As a 后端维护者, I want TTS 供应商可替换, so that 表现力/成本不足时有杠杆、不用改调用方。
3. As a 测试者, I want 第二 adapter 是确定性替身, so that 合成链路与情绪映射可离线全测。
4. As a 真声验证者, I want release tier 的真声 smoke 带情绪样本, so that instruction 通道端到端有守卫。

## Non-goals

- ASR 收敛（如需要另立项）。
- 真实云 adapter B（MiniMax/ElevenLabs 等）——后置到「MIMO 表现力不足」有实际证据时。
- GTX 1660 自托管 CosyVoice（评估过：6GB 显存流式延迟勉强，不做；见 2026-09-25 调研）。
- 音色克隆、SSML、多 voice 管理。

## Success Criteria

- 双 adapter 同 interface 合成绿（fake 全离线）；情绪映射确定性快照单测（Affect×Act→instruction 表驱动）。
- 端到端：Backchannel 与 Watch Turn 的 TTS 调用携带 instruction（release tier 真声 smoke 验证可听差异）。
- 删除测试：删 seam 则供应商知识（URL/model/voice/组包）回到调用方——收敛一处，是加深。
- ADR-0012 修订注入库；pr tier 绿。
