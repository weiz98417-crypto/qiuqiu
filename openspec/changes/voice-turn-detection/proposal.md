# Voice Turn Detection: 说完判定与语义轮次检测

## Why

当前「用户说完」判定=纯 VAD 静默超时——观赛场景两类真实失败：①用户停顿思考（「那个进球……嗯怎么说呢」）被提前判完，球球抢答截胡；②环境噪声/长尾拖音误判未完。行业已收敛出语义轮次检测（LiveKit transformer turn detection、TEN Turn Detection 可自部署模型）。「AI 是否听完了」是全双工体验（voice-duplex）之后下一个瓶颈。

## What Changes

- **评估先行**：收集 voice-duplex 上线后的抢断/误断 telemetry（自打断事件+提前应答事件），量化纯 VAD 的失败率与形态。
- **方案决策（本 change 的核心交付）**：三选一并记录决策依据——a) 调参版纯 VAD（静默时长自适应：语速/停顿分布）b) 轻量语义特征（尾词/疑问句式/ASR 部分转写的完形度规则）c) 自部署轮次检测模型（TEN Turn Detection 等，本地推理，与 bge-m3 同 Ollama/自托管纪律）。
- 实施所选方案 + 降级链（模型超时→规则→固定静默）。

## User Stories

1. As a 用户, I want 我说话打磕巴时球球等我说完, so that 不被抢话也不被干等。

## Non-goals

- 全双工收音（voice-duplex 已做）；S2S 端到端语音模型接入（远期）。

## Success Criteria

- 决策记录（含数据依据）入 design；抢断/误断率较纯 VAD 基线下降（telemetry 对比）；降级链可用；evals H 域补说完判定例。

## 修订（2026-09-25 · 生态对标轮预注册）

- **晋级判据**：1.2 评估 a) 调参纯 VAD 时，若标注样本误判（抢答/干等）＞15% 或轮次延迟 p90＞1.2s → 直接跳 c) 自部署模型（首选 LiveKit smart-turn / pipecat smart-turn 线的 ONNX 小模型，本地推理与 bge-m3 同 Ollama 纪律，CPU 可跑）；b) 完形度规则仅作降级链中间档，不单独评估。依据：2026-09-25 生态调研（smart-turn BSD-2 / LiveKit Apache-2.0，BERT 级 CPU 可跑）。
- **数据采集并批**：延迟分解与 voice-transport-upgrade 的端到端分解（采集→上行→ASR→决策→TTS 首包→播放起）同批一次采齐，不跑两遍。
