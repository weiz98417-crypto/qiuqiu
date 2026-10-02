# Tasks: 流式话务链(Phase A)

- [x] 3.1 投递缝深化:deliveryKey owned package(internal/deliverykey 五构造器,五处裸拼迁移);双装配线合一(collectingResponseSink + 请求内投递服务,操作台 nil synthesizer 分岔的内联 TTS 块删除,合成/去重/记账单路径);既有 12 suite schema 同步点全过。commit 4959424。
- [x] 3.2 TTS 流式 adapter:SynthesizeStreamDetailed(SSE pcm16 分片)经可选能力接口 StreamingSynthesizer 变现役——Synthesizer 主接口签名一字未动(cmd/server 三个测试替身零迁移);断流/超时/非 200/零分片回退整段(Degraded REPLACE 契约),调用方取消不回退不熔断;httptest 覆盖正常流/断流/超时/回退/取消/中止/payload 一致性。commit c1c1e71。
- [x] 3.3 句聚合器:internal/speech(pipecat 语义 Go 重写:引号归属/数字句点不切/安全阀 120 rune/Flush 必调);增量≡整段等价用例。
- [x] 3.4 句粒度下发协议:conversation.StreamingResponseSynthesizer 可选能力接口;deliverStreaming(AudioStarted 占位→逐句逐帧→FirstAudioMS/SentenceCount 回带);voice_audio 帧增 sentenceIndex(>0 才发,老客户端零感知;客户端 conversation 源本不去重,FIFO 天然支持);帧=完整 WAV24k(pcm16 分片聚合;mock 完整产物直通)。commit aa8b8ec。
- [x] 3.5 打断 flush + 事实刷新交互:在途合成 ctx cancel→interrupt()账本 interrupted、剩余句不再合成不再下发(「播了就是播了」:已发句不撤回);**事实刷新整轮重跑在 Phase A 天然安全**——刷新判定发生在投递开始前,首句未下发即重来无痕(设计预期,无代码改动);eval 三族(顺序/打断丢剩余/句中失败 fallback)。
- [x] 3.6 观测:tts_first_audio 第六锚点(voice_stages.go 常量 VoiceStageTTSFirstAudio;voiceStageBuffers 增 turnDecided 耗时记录,audio 半合成落段);grafana-alerting 规则①b(首响 P95>2500ms,Grafana 13.2.2 provisioning 重载实载 4 规则);ADR-0012 修订(流式位变现役+「上下文记全文」刻意分歧)。
- [x] 3.7 ADR-0012 修订 + 门禁:ADR-0012 2026-09-30 修订注落库;go build 全仓 + go test ./... 33 包全绿(2026-09-30);web 构建与真机预采(30 样本×口径)留尾——**client 端零改动**是本轮刻意裁决:conversation 源多帧客户端天然支持,web/native 漂移风险面未触碰;Phase B 判据(真机 p90>1.5s 且 LLM 段占主导才开)待预采数据。

## Sequencing

波B,**依赖波A trace-genai-alignment 先落**(已满足)。完成后真机预采数据同时服务 Phase B 判据。与 live2d-engine-swap 在 client 侧交叉(live2d_view.dart)——本轮③未动 live2d_view,交叉未发生。

## 最终裁决(2026-10-01):句粒度降级回整段

韵律盲测(10 对双盲,用户亲测)逐句版 80% 被嫌弃(判据线 30%),返工一轮(全文长度基准+延续感指令)后按用户裁决**直接降级回整段合成**——整段的全文韵律上下文不可替代。延迟代价(首响实测 3-7s)如实接受。SynthesizeResponseStream/deliverStreaming/truncated/sentenceIndex wire 从生产路径拆除;流式合成与句聚合器作为已测库保留;真机预采与 Phase B 判据随之冻结(见 ADR-0023 消费先于产能)。

## CEO 审查外部声音增量(2026-09-30)

- 句间韵律连续性未评估:各句独立合成+独立 instruction,句界可能有韵律断裂——真机预采时加**韵律盲测**(整段 vs 句粒度 A/B,人耳判自然度);N 句 N 次 HTTP 的计费面在 MiMo 结束限时免费后复估。
- 流结束信号:**已部分兑现**(e108c26):sentenceCount 随帧上 wire(>0 才发),客户端可据 index==count-1 判终;Final 旗标仍未上 wire(无消费者);Phase B 字节级流式时再补逐字节终信号。
- 部分交付后静默的补注:截断时全文文本气泡已在屏(文本先行设计),音频截断的用户损失=表达力非信息;真机验证时确认体感。
