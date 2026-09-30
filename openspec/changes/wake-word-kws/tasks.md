# Tasks: 唤醒词 KWS
- [x] 10.1 评估门先行:误触样本集(足球解说+真实对话)+ 唤醒率测试;三判据实测(误触<1 次/小时 / 安静唤醒率>95% / cooldown 后二次唤醒);词表按结果定稿(三音节起步,不过门换词重测)。
- [x] 10.2 wake_service:sherpa_onnx KeywordSpotter 接入(int8 模型+起步参数);状态机(空闲监听 ⇄ 会话互斥切换,VAD 不跑省电);flutter test 状态机用例。
- [x] 10.3 唤醒链路:唤醒→asr_start 会话复用;presentation events `wake` 条目(三方镜像+契约测试);超时回静默;设置页开关(默认关)+首启一句话引导。
- [ ] 10.4 门禁:flutter test、dart analyze 零新增、web 构建绿(web 排除但构建不坏)、pr tier。

## Sequencing

波E(评估门后)。纯客户端,与 live2d-engine-swap 同在 client 侧错峰施工;评估门(10.1)可提前跑,不受施工顺序约束。

## 执行记录（2026-09-30）

- 10.1 定稿：「嘿球球」不过门（唤醒率恒 40–50%、黑球白球误触 1 次≈67.8/h），迭代换词后「你好球球」三判据全过（唤醒 10/10、误触 0/26、二次唤醒 2），参数 score=1.8 / threshold=0.3 / cooldown=1.5s。评估引擎走 sherpa-onnx node 绑定（本机无 python），迭代记录 `scripts/wake-eval/results/README.md`。
- 10.4 前三项已实测绿（flutter test 217 全过 / dart analyze 零新增 / `flutter build web` 成功）；pr tier（提交+PR）留主会话。真机尾项：Android 端整链（麦克风→KWS→唤醒→会话）与真实人声唤醒率/功耗，待真机人耳确认。
