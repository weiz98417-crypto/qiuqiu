# 流式话务链:句粒度投递与 TTS 真流式(Phase A)

## Why

每个陪看回合的首响延迟 ≈ 2-4s,主墙在 TTS 整段合成:`tts.Synthesizer` 的接口形状(整段字节进出)把流式挡在签名层——一次回复只调一次合成(conversation/response_delivery.go:295),整段 base64 WAV 一次返回(tts/client.go:99-181),`SynthesizeStream` 是预留位、现役 adapter 一律 ErrNotSupported(client.go:185-187);文字气泡先行、音频整段后到,客户端 FIFO 按双帧配对消费整段(match_session_controller.dart:1080-1110)。优秀开源语音 agent 同口径 0.7-1.2s(dev.to 30 栈实测最佳 0.73-1.45s;LiveKit 结论:LLM TTFT+TTS TTFB 占总延迟 90%+)。

**MiMo TTS 基础模型已支持真流式**(stream:true + pcm16@24kHz SSE,官方文档已核;qiuqiu 用的正是基础模型+内置音色「冰糖」)——零供应商迁移换质变。叠加项:事实刷新时整轮 generate 重跑(main.go:988-1010)、deliveryKey 无类型字符串协议(4 语义 9 包散布)、操作台与 WS 双装配线(main.go:955-984 vs watchconnection.go:731-744)。

## What Changes(Phase A)

- **投递缝深化(第一刀)**:deliveryKey 升级 owned type + 构造器收敛(四种语义:事件外推键/投递去重键/backchannel 前缀/主动投递前缀——watchconnection.go:366,401,453,457,998 各处裸字符串拼 key 全部收敛);双装配线合一为单一 ResponseDeliveryService 路径(操作台 HTTP 路径与 WS 路径走同一条装配线)。
- **TTS 真流式 adapter**:`SynthesizeStream` 变现役——MiMo stream:true SSE 吃 pcm16 分片,服务端按句聚合成完整句音频帧;失败(SSE 断流/超时/政策变化)adapter 内回退整段 Synthesize,熔断沿用,调用方无感。ADR-0012 修订随附(流式位变现役;当年拒绝的是假流式,不是真流式)。
- **句聚合器**:LLM 整段回复(现状不动——realizer 80 token 预算天然短)→ 按句切分逐句喂流式 TTS。算法语义抄 pipecat `aggregators/sentence.py`(BSD-2)与 stream2sentence(MIT),Go 重写。
- **句粒度下发**:voice_audio 增句序号形态(每句一对 metadata+binary 双帧,presentation 随首句);客户端 FIFO 配对消费机制复用(多条音频排队已有 backchannel 先例);web/native 双端同步、契约测试先行(9b6e1f0 教训:web 构建打坏是惯犯)。
- **打断 flush**:用户抢话(PlaybackInterruptGate 既有)→ 取消在途 TTS 请求 + 剩余句不下发 + 账本落 interrupted(投递七态句无关,直接兼容);**播了就是播了**——已播句不撤回。
- **事实刷新重跑交互**:关键事实版本变化时,已播句不撤回,重生成只影响未播部分;首句未下发则整轮重来无痕。
- **观测**:trace 新增 tts_first_audio 锚点(五段变六段,事后 attach 体系照旧,字段名按 trace-genai-alignment 新体系)。

Phase B(realizer token 流式+字节级下发)后置评估,不在本 change;Phase A 真机数据决定是否开。

## User Stories

1. As a 用户, I want 球球接话像真人一样快(说完→首音频 ≈1s 量级), so that 陪看对话不打断情绪。
2. As a 调试者, I want tts_first_audio 进延迟分解, so that 首响可归因、可进 grafana-alerting 的告警规则。

## Non-goals

- Phase B(LLM token 流式+字节级流式下发)——realizer 80 token 预算让 LLM 段天然短,边际收益待真机数据。
- ASR 任何改动(终稿整段重转写 ~297ms 留 asr-selfhost-eval)。
- backchannel 短 TTS 流式化(≤10 字整段 one-shot 本来就快,ADR-0016 形态不动)。
- companion/watchconnection god file 整体拆分(独立轮次;本 change 只动投递缝)。
- 文本按句流式下发(维持整段 qiuqiu_reply 全文气泡,边听边读;**对话上下文以全文记**——pipecat「未播文本不入上下文」在 qiuqiu 不适用:文本已全文投递,这是与生态实践的刻意分歧,记入 ADR-0012 修订)。
- 投递七态枚举改动(句无关,直接兼容)。

## Success Criteria

- 确定性 eval:多句回复逐句下发顺序与配对 / 打断后剩余句不再下发 / 事实刷新重跑只影响未播部分 / 流式失败回退整段路径;
- 首响预算:EOU 34ms + ASR final ~300ms + LLM 整段 + 首句 TTS ~400ms ≈ 1.2-1.5s(真机 p90 按既有预采纪律验收:30 样本×口径,voice-transport design 的 800ms 门);
- tts_first_audio 锚点进 trace,grafana-alerting 语音规则补段;
- web 构建绿 + 双端契约测试绿;go 全量 + pr tier 绿。
