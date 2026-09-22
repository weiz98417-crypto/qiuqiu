# Evals 248: 评测集扩容（100 → 248）+ 基建修复

## Why

三波能力上完，行为面已比评测面宽（2026-09-23 用户裁决：200+，缺口维度用 AnySearch 对齐行业框架）。裸区盘点：router 真网验证（一直用 schema 形状锁替代——形状对≠路由对）、记忆端到端、降级路径故障注入、多通道限频冲突，全部无 eval 级覆盖。基建病灶：spec 残留固定 `waitForTimeout`（fulltime 时序脆弱根因）与 UI 文本轮询耦合（「后端做到了」和「页面渲染了」不分）。行业框架对齐：MoodBench 门槛层（红线先于能力）、INTIMA 边界维护、DialogGuard 五维社会心理风险（=拟人化新规八禁的评测化）、LongMemEval-V2 时序推理/弃权。

## What Changes

- **基建**：①全部 spec 固定 `waitForTimeout` 清零改 `expect.poll`；②共享等待 helper——轮询运营 API 的账本/投递结果（后端状态），等到再断言 UI；③两个留尾测试落地（router 真网、`submitDueReminders` WS 级）。
- **扩容矩阵**（100→248，233 例进门禁+15 真网独立档）：

| 域 | 新增 | 要点 |
|---|---|---|
| A 红线价值观 | 22 | 编造事实负例×5、知识域外诚实×4、事实撤销收回×3、不诱导情感依赖×4（DialogGuard/新规）、边界遵守×4、调侃收敛×2 |
| B 人格立场 | 18 | 立场施压×6、助手化拒绝×6、affect 连续×4、档位语气×2 |
| C 记忆回忆 | 22 | 跨场命中×6（依赖 memory-recall-fusion）、张冠李戴负例×4、时序推理×4、不相关不翻旧账×4、口味生效×4 |
| D 陪伴互动 | 22 | 情绪分档×8（含失球安慰）、chosen silence×6、中场/赛后节点×4、关系阶段差异×4 |
| E 知识域 | 18 | 知识负例×6、触发附句×6（依赖 knowledge-event-triggers）、订阅意图×4、比分口径×2 |
| F 多轮任务 | 14 | thread 回访×4、话题延续×4、工具结果使用×4、让路×2 |
| G 鲁棒降级 | 14 | embedding/Memobase/LLM 三路故障注入×9、WS 断连×2、多通道抢话×3 |
| H 语音链路 | 10 | ASR 噪声×4、TTS 失败降级×3、信号幂等×3 |
| I 登录身份 | 8 | 随 login-credential-seam 落地 |
| J router 真网 | 15 | 独立 spec+环境开关（`QIUQIU_ROUTER_NET=1` 且 key 在），CI 默认不跑 |

- **两轨制**：红线进门槛（确定性断言）；LLM-as-judge 离线档评织写语感（E 织写/F 安慰中场闲聊），本地/夜间跑、报告落 artifacts、不进门禁。
- 负例素材用 AnySearch 检索行业集（PersonaGym/INTIMA/LoCoMo 等）再人工校准，不闭门造车；全套资料对外口径「评测 100 例」随之更新真值。

## User Stories

1. As a 运营员, I want evals 覆盖红线/人格/记忆/降级全维度, so that 值守闭环的门面是真的。

## Non-goals

- 真 WebRTC 级语音延迟评测（I 系列自带）；judge 进门禁；对外发布评测报告。

## Success Criteria

- 248 例全绿且 CI 时长可接受；固定等待清零；真网档可独立执行；能力-评测对照表无裸区。
