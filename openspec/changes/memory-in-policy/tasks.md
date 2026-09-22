# Tasks: Memory In Policy

- [ ] 0.0 前置小 grilling：「记忆信号 × 决策输出」映射表定稿（锚定案例：支持球队丢球→安慰优先）+ ADR（确定性内核首次消费长期记忆的策略设计）。
- [ ] 1.1 Director.Apply 增记忆信号输入（Portrait 口味 + Recall 强相关时刻，幅度保守）。
- [ ] 1.2 每映射的 reason code（`policy_memory:<signal>`）落 trace/账本。
- [ ] 1.3 director_test 扩展（每映射一断言）。
- [ ] 1.4 行为 eval（scripted memory fixture）。
- [ ] 1.5 验证：全量 go test + eval 绿；既有 841 行 director_test 零回归。

## Sequencing

settings-in-policy 之后。触发条件补充：语义记忆真数据案例（"该 recall 却没 recall"）出现时优先级上调——记录于本行，不另立文档。
