# Tasks: 用户语音情绪旁送(波1)

- [x] 4.1 sidecar:backend/cmd/voice-input-sidecar(FastAPI+sherpa-onnx SenseVoice int8,POST /affect→{label,confidence},情绪 token→小写标签;模型未就位 503 降级);模型下载脚本 scripts/voice-model/download.mjs(hf-mirror+sha256 清单);voice-fake 确定性替身(cmd/voice-fake);compose profile `voice-input` 隔离(默认不启动,backend 无 depends_on)。commit c1c1e71。
- [x] 4.2 服务端:cmd/server/useraffect_relay(在途上限 4 信号量——270f535 教训;代次=signalID 绑定,等待者模式双时序 bind:结论先到暂存/挂点先到登记 traceID 事后合并);话轮聚合=单次整段 PCM 分类(话轮级落一次,非逐 partial);置信门 0.55(QIUQIU_USER_AFFECT_MIN_CONFIDENCE 可配);trace voice.userAffect 键(VoiceTraceMetadata.UserAffect,AttachVoiceStages PG jsonb 原子合并同步);静默降级+失败计数(Dropped);sidecar 客户端 internal/useraffect(短超时+熔断+失败静默,ambient 同族)。配置 QIUQIU_USER_AFFECT_URL 留空即整体停用。
- [x] 4.3 宪法负例 + 观测显形:TestUserVoiceAffectNeverEntersTheFactLedger(ambient tripwire 同规格:事实账本零行/投影零推进/比分未动,唯一落点=观测合并通道);console TurnReplay userAffect chip(标签+置信,无正文,缺省不占位;vitest 13/13+tsc 干净)。
- [ ] 4.4 门禁收口:compose profile 隔离验证(默认不启动 voice-input 时 backend 无感知——代码级成立:URL 空 relay 为 nil;docker 实拉起验证留尾部署轮);go 全量+pr tier 在全量门禁节统一跑;**波2(情绪偏置进 Affect State 政策)另立 change**,观测一个轮次后凭数据开工。

## Sequencing

波C。依赖 trace-genai-alignment(已满足);与 voice-streaming-delivery 服务端文件无交叉(已并行落地)。波2(偏置接政策)在观测数据确认标签质量后另立。
