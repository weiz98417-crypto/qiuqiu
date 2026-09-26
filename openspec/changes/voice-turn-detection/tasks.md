# Tasks: Voice Turn Detection

- [ ] 1.1 voice-duplex telemetry 数据盘点（抢断/误断/自打断分布）。
- [x] 1.2 三方案原型对比（a 调参 VAD / b 完形度规则 / c 自部署模型），决策+依据写回本 change design.md。（2026-09-25：离线评估 harness + 标注用例集 + 参数扫描实跑，判据对照全档指向 c，决策与升级触发条件已入 design.md）
- [x] 1.3 实施所选方案 + 超时降级链（模型→规则→固定静默）。（2026-09-25：降级链骨架落 `client/lib/services/turn_detector.dart` 并挂入 VADService 判句处，静默阈值参数化；c 的模型本体按边界另起实施轮，模型/规则为预留插槽）（2026-09-27：c 模型本体接线完成——LiveKit EOU 多语版 v0.4.1-intl sidecar（backend/cmd/turn-sidecar）+ 服务端 turn_query 转发 + 客户端 model 插槽远程化（RemoteTurnModel 提前问），降级链经 turn_result{isComplete:null} 天然闭环；模型清单/延迟实测/阈值校准决策入 design.md）
- [ ] 1.4 evals：停顿不抢答、噪声不误判、降级路径。
- [ ] 1.5 验证：全量门禁绿 + telemetry 对比记录。

## Sequencing

I 系列第 2 个，依赖 voice-duplex 的收音基线与 telemetry。
