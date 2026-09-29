# Tasks: 用户语音情绪旁送(波1)

- [ ] 4.1 sidecar:deploy compose profile `voice-input`(FastAPI+onnxruntime,SenseVoice-Small);模型下载脚本 hf-mirror+sha256 锁版本(E:\tools 先例);WS 契约 affect_result;sidecar 单测(标签/置信/空音频)。
- [ ] 4.2 服务端:relay 旁送复用 ambient_relay 模式(**在途上限信号量——270f535 教训:relay 无上限曾列 P2**);代次绑定防陈旧;话轮聚合+置信门;trace `user_affect` 键(新字段体系);静默降级+失败计数。
- [ ] 4.3 宪法负例 + 观测显形:情绪路径对事实账本零写入负例测试;运营台单轮回放情绪标签列(无正文,三层纪律);eval 静默降级用例。
- [ ] 4.4 门禁:go 全量 + pr tier + compose profile 隔离验证(默认不启动 voice-input 时 backend 无感知)。

## Sequencing

波C。依赖 trace-genai-alignment(user_affect 键用新字段体系);与 voice-streaming-delivery 服务端文件不交叉(relay 侧 vs 投递侧)可并行施工。波2(偏置接政策)在观测一个轮次后凭数据另立。
