# smart-turn v3.2 换代评测(测合适就换)

## Why

话轮检测 sidecar(pipecat smart-turn)现用旧版系 396MB ONNX 模型,p90 34ms。上游已发 v3.2:Whisper Tiny 骨干、8M 参数、int8 量化仅 **8MB**、CPU ~10ms、23 语种、数据集+训练脚本全开源(BSD-2)——资源 -98%,且小到可用 onnxruntime-go 内嵌主进程、整体裁掉独立 sidecar。

**不能直接换**:v3.2 换了骨干,对中文口语(尤其看球场景的激动语速/韵律)的判断质量无任何数据;而「说完没」的判断是抢话/呆等体验的地基。韵律盲测教训(句粒度 80% 嫌弃→回滚)刚立了 ADR-0023:用户可感的结论必须来自真实场景。用户裁决(grilling Q7):**测合适就换**。

## What Changes

1. **中文评测集**:自建——中文观赛/闲聊语音样本(来源:auto-hosting 真实陪看流量录音 + 人工补录),人工标注说完点;标注格式对齐 client/tool/turn_detection_eval.dart 既有 harness。
2. **离线对比**:396MB 现役 vs v3.2(8MB int8)同集评测——判据:说完点准确率不低于现役、抢话误判率(把没说完判完)不高于现役;已知坑纳入用例:输入必须 16kHz PCM,采样率不符静默误判。
3. **线上影子(两周)**:v3.2 起并行 sidecar,turn_query 双模型同答、现役裁决,对比 duplex_event(self_interrupt_suspected/degraded)与 ClientHealthLedger 计数。
4. **过门切换**:onnxruntime-go 内嵌 v3.2(或保留极薄 sidecar 形态,按内嵌实测定)→ 裁 396MB 模型与旧 sidecar;**不过门**:记录评测数据、change 关闭归档、旧模型继续(一个字节不浪费)。

## Non-goals

- LiveKit turn-detector(文本端 EOU,自定义模型许可)A/B——记录为后续选项,本轮只评 smart-turn 系(用户裁决范围)。
- 训练/微调自有模型(数据集在,但先看零样本表现)。
- turn_query/turn_result 协议任何变化。

## Success Criteria

- 评测集 ≥200 样本、覆盖观赛激动/平静闲聊/犹豫停顿三类;两模型对比报告落库(docs/evals 惯例);
- 影子两周数据:误打断率不升;
- 过门→切换后 pr tier + 真机抢话体验回归绿;不过门→冻结记录完整可追溯。
