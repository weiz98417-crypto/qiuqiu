# Ambient Audio Observation: 球场气氛作观察旁证

## Why

比赛事实只来自数据源 poller 与运营台两条路（ADR-0002 事实源纪律内），但陪伴语境里球场气氛（欢呼/嘘声/音量骤变）常先于比分到达——用户看球时系统对现场气氛完全失聪。SenseVoice（MIT，9.4k★，已迁 QwenAudio org）的音频事件检测（AED）分支可作 docker-compose sidecar 分析观赛会话**已有**的麦克风流。Neuro-sama 验证了事件驱动陪伴反应的粘性上限；气氛信号是「比赛事件→伴随反应」打法的下一格。

## What Changes

- SenseVoice AED sidecar（docker-compose 新服务）：输入观赛会话已上传的音频分片，输出气氛信号事件（欢呼/嘘声/音量骤变 + 置信度）。无状态 HTTP，便于摘除。
- 气氛信号只进 **observation store 作外部主张旁证**（确认/矛盾/悬置的辅助证据，最低权重档）——**永不作 Match Fact、永不进事实账本**（ADR-0002；意图路由器「不发明比赛事实」同规矩）。
- 隐私口径：不新增采集（复用 ASR 已有音频流，不另开麦克风通道）、不存原始音频（sidecar 无状态、事件即弃）、仅观赛会话内生效、设置面加一行说明；不做独立开关（跟随会话）。
- 可独立摘除：sidecar 不可用时气氛旁路静默消失，主链路无感（无超时阻塞、无错误风暴）。

## User Stories

1. As a 用户, I want 球球听得到我家的欢呼, so that 它的伴随反应踩在真实气氛上而不是只等比分推送。
2. As a 事实纪律维护者, I want 气氛永远变不成比分, so that 事实账本不被软证据污染。
3. As a 隐私敏感用户, I want 氛围分析不存音频、会话外不生效, so that 不新增被采集的东西。

## Non-goals

- 气氛→Match Fact 的任何直连路径（本 change 的宪法线）。
- 原始音频存储/回放/上传留存。
- 多人观赛房间的气氛聚合（远期，触动单租户假设）。
- 用 AED 做 ASR 转写替代（ASR 主链路不动）。

## Success Criteria

- 确定性测试：AED 事件→观察通道→旁证标注落库；**断言不进事实账本**（负例测试）。
- sidecar 摘除场景测试：compose 停服务，Watch Turn 全链路照常（pr tier 可离线验证 fake sidecar）。
- 删除测试：删 sidecar 与旁路则回到现状——气氛感知是纯增量，不重构既有观察通道。
