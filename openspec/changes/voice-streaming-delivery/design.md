# Design: 流式话务链(Phase A)

## 延迟预算(说完 → 首段音频可播)

| 段 | 现状 | Phase A 后 | 依据 |
|---|---|---|---|
| EOU 判定 | 34ms(smart-turn p90) | 不变 | turn-sidecar 实测 |
| ASR final | ~300ms(整段重转写往返) | 不变(本 change 不动) | 真机 MIMO 实测 297ms |
| LLM | 整段 0.5-1s | 不变(realizer 80 token 预算,天然短) | llm/client.go:17 |
| TTS 首句 | **整段 1-3s(主墙)** | 首句 ~400ms(句粒度流水) | MiMo stream:true;句长 ~15 字 |
| **合计** | ≈2-4s | **≈1.2-1.5s** | dev.to 30 栈最佳 0.73-1.45s 对照 |

Phase B(若开):LLM token 流式把「整段 LLM」压到「首句 LLM」≈200-500ms,再省 0.3-0.5s;字节级下发再省句内合成尾。**判据:Phase A 真机 p90 若仍 >1.5s 且归因显示 LLM 段占主导,则开 Phase B;否则不开。**

## 投递缝深化(施工序:先于一切流式改动)

1. **deliveryKey owned type**:新类型封四语义(event 外推 matchstate.DeliveryKey(ev) / 投递去重 / `backchannel-<id>` / 主动投递前缀 `reminder:<id>`、`open-thread-recovery:<id>`、`first-meeting:<user>`、`observation_resolution`);构造器私有化字符串拼接,17 个引用点(9 后端包)逐个迁移;playbackEventID 百分号转义(delivery.go:388-399)收进构造器。
2. **双装配线合一**:操作台 HTTP 路径(completeVoiceSessionWithOptions,main.go:955-984,靠传 nil synthesizer 分岔)与 WS 路径(watchconnection.go:731-744)收敛为 ResponseDeliveryService 单路径——合成只在投递服务内一次,分岔消失。

## 协议形态(句粒度)

```
服务端 → 客户端:
  {type:"voice_audio", mime, traceId, sentenceIndex, sentenceCount?, byteLength,
   eventId, deliveryKey, source}   ← 元数据帧(每句一对)
  <binary: 句完整音频(pcm16 或带 WAV 头,spec 定稿时按客户端播放器现状定)>
客户端: FIFO 按 deliveryKey+句序号配对;presentation 随首句元数据帧;
打断: 服务端取消在途合成(context cancel)→ 不再发剩余句 → 账本 interrupted;
     客户端播完当前句即停(既有 pause 语义,playback_result 实报照旧)。
```

投递七态(planned/text_delivered/audio_started/completed/interrupted/skipped/failed)是话轮级,句粒度不引入新形态;「播了就是播了」= 打断时已播句不撤回、playback_result 实报照旧、对话上下文记全文(文本已全文投递)。

## 事实刷新重跑(main.go:988-1010)

- 首句未下发:整轮重来无痕(现状行为)。
- 已有句下发:不撤回;取消剩余合成,用新事实重生成**未播部分**(从已播句数起),deliveryKey 换新代次防客户端配对错乱。

## 打断 flush 语义(对照 pipecat base_output.py,取三件)

1. TTS 停止合成(在途 HTTP/SSE 请求 context cancel);
2. 排空下发队列(剩余句丢弃,不下发);
3. 播放队列由客户端 PlaybackInterruptGate 既有机制处理(已验证,不重造)。
**不取**:「未播文本不入 assistant 上下文」——见 proposal Non-goals,与生态的刻意分歧记入 ADR-0012 修订。

## 风险栏

- MiMo 流式限时免费的商业变化:回退整段路径即对冲(adapter 内降级,无第二供应商依赖)。
- pcm16 vs WAV 封装:客户端 flutter_soloud/web 播放器现状以完整 blob 播放,句帧保持完整音频格式,选型在 task 3.4 实测定(WAV 头封装最稳,首选)。
- web/native 双实现漂移:契约测试先行,两端的句序号配对逻辑共用纯 Dart 段(match_session_controller 已双端共用,扩展而非新建)。
- 与 live2d-engine-swap 在 client 侧交叉(live2d_view.dart 音频消费/FIFO):施工错峰,①先③后。
