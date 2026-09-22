# Tasks: Voice Turn Detection

- [ ] 1.1 voice-duplex telemetry 数据盘点（抢断/误断/自打断分布）。
- [ ] 1.2 三方案原型对比（a 调参 VAD / b 完形度规则 / c 自部署模型），决策+依据写回本 change design.md。
- [ ] 1.3 实施所选方案 + 超时降级链（模型→规则→固定静默）。
- [ ] 1.4 evals：停顿不抢答、噪声不误判、降级路径。
- [ ] 1.5 验证：全量门禁绿 + telemetry 对比记录。

## Sequencing

I 系列第 2 个，依赖 voice-duplex 的收音基线与 telemetry。
