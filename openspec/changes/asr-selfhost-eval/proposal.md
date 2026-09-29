# ASR 自托管离线评估与决策(FunASR 2pass,评估门)

## Why

主路 ASR 现状:MiMo 云批量 HTTP,partial=1.5s 窗口假流式(backend/internal/asr/session.go:12-14,每 1.5s 音频一次 ~1.75s 窗口转写),终稿整段重转写(~297ms 往返)。自托管 FunASR 2pass(paraformer-zh-streaming 流式 partial ~600ms + Seaco-Paraformer 热词终稿修正——球员名 biasing 正对口,热词表复用既有 voiceRecognitionHints 同源,cmd/server/transcription.go:293-315)的收益=partial 快 2.5 倍 + 语音不出本机 + 无 API 依赖/无并发上限。但调研数字≠本机实测——参照 turn-detection 先例(e9b393b:15 用例离线评估推翻了 0.5 阈值的预注册判断),主路切换必须先过离线评估门。

## What Changes

- **评估资产**:用例集(球员名中外文混杂/比分数字/中文口语碎句/直播间噪声环境)+ MiMo vs FunASR 2pass 同批对照;离线评估脚本入库(可重复跑)。
- **判据(先立,可实测后修订)**:①partial 首 token < 800ms;②final 准确率不劣于 MiMo(同批样本);③热词命中率显著优于无热词;④CPU 资源占用可接受(单机 4c8G 参照,FunASR 官方口径 ~32 路并发)。
- **决策输出**:过门→另立替换 change(主路切 FunASR,降级链照 turn-sidecar 先例:缺席回退 MiMo 行为,seaco 热词表接 voiceRecognitionHints 一处不改);不过门→维持 MiMo,决策+数据落 design 文档。
- **本 change 只有评估与决策,不动主路一行代码。**

## User Stories

1. As a 主路 owner, I want 实测数据再切 ASR, so that 不被调研报告的数字带沟里。

## Non-goals

- 主路替换实施(过门后另立 change)。
- Qwen3-ASR(需 GPU+无热词,判据不占优,排除并记录)。
- sherpa-onnx 流式 zipformer(中文流式模型偏老 2023,准确率低于 Paraformer,备胎记录)。

## Success Criteria

- 评估脚本+用例集入库可重复;
- 决策记录(过/不过+四判据实测数据)落 docs/design/asr-selfhost-eval.md。
