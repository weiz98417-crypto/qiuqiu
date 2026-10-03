# Tasks: smart-turn v3.2 换代评测

- [ ] 6.1 中文评测集:采集(auto-hosting 流量+补录)≥200 样本,人工标注说完点,三类覆盖(激动/平静/犹豫停顿);harness 对齐 client/tool/turn_detection_eval.dart。
- [ ] 6.2 离线对比:现役 396MB vs v3.2 int8(16kHz PCM 坑用例);报告落 docs/evals;判据:准确率≥现役且抢话误判≤现役。
- [ ] 6.3 线上影子:v3.2 并行 sidecar 双答、现役裁决,两周;ClientHealthLedger/duplex_event 对比记录。
- [ ] 6.4a 过门切换:onnxruntime-go 内嵌(或薄 sidecar)替换 + 裁 396MB;pr tier + 真机抢话回归。
- [ ] 6.4b 不过门分支:评测数据与冻结记录落库,change 归档。

## Sequencing

波3。评测集素材依赖 auto-hosting(波1B)真实流量——波1B soak 完成后本 change 才有素材,顺序约束;若 auto-hosting 延期,人工补录先行、流量素材后补。影子期与 tts-supply-switch 无交叉。
