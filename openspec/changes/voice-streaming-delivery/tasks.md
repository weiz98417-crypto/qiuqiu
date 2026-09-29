# Tasks: 流式话务链(Phase A)

- [ ] 3.1 投递缝深化:deliveryKey owned type + 四语义构造器收敛(私有化拼接,17 引用点迁移,playbackEventID 转义入构造器);双装配线合一(main.go:955-984 与 watchconnection.go:731-744 → ResponseDeliveryService 单路径,操作台 nil synthesizer 分岔消失);既有 12 suite schema 同步点全过。
- [ ] 3.2 TTS 流式 adapter:SynthesizeStream 现役(MiMo stream:true + pcm16 SSE,句聚合);失败回退整段(SSE 断/超时),熔断沿用;单测覆盖 httptest SSE 正常流/断流/超时/回退四态。
- [ ] 3.3 句聚合器(Go 重写 pipecat 语义):句尾标点+残余 flush;与 realizer 80 token 预算的句数上下界;单测(中英混排/无标点尾句/空句/单句回复退化为现状)。
- [ ] 3.4 句粒度下发协议:voice_audio 句序号双帧形态(元数据帧带 sentenceIndex,presentation 随首句);音频封装选型(WAV 头首选)实测定稿;客户端 FIFO 扩展句序号顺序配对;web/native 双实现+契约测试先行。
- [ ] 3.5 打断 flush + 事实刷新交互:在途合成 context cancel、剩余句丢弃、账本 interrupted(七态不动);事实刷新只重生成未播部分(deliveryKey 代次防错配);eval 三族用例(打断/重跑/回退)。
- [ ] 3.6 观测:tts_first_audio 锚点(trace-genai-alignment 字段体系,五段变六段);grafana-alerting 语音延迟规则补段。
- [ ] 3.7 ADR-0012 修订 + 门禁:流式位变现役/句粒度契约/播了就是播了/上下文记全文的刻意分歧/回退链;go 全量、flutter test、pr tier(流式用例进 tier)、web 构建绿;真机预采排期(30 样本×口径)留尾如实记。

## Sequencing

波B,**依赖波A trace-genai-alignment 先落**(新锚点直接用新字段体系)。完成后真机预采数据同时服务 Phase B 判据。与 user-voice-affect(服务端 relay 侧)文件不交叉可并行;与 live2d-engine-swap 客户端侧交叉(live2d_view.dart),错峰:①先③后。
