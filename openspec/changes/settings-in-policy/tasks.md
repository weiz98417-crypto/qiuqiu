# Tasks: Settings In Policy

- [ ] 0.0 前置小 grilling：枚举对齐（initiative/analysis_appetite/banter_level ↔ policy 既有取值）、设置与 cue 冲突时的优先级、reason code 形态、eval 方案——产出 ADR。
- [ ] 1.1 偏好合并：回合入口读 user_character_settings，显式设置覆盖 cue 推断。
- [ ] 1.2 reason code（`policy_user_setting:<field>`）落 trace。
- [ ] 1.3 行为 eval（scripted settings fixture）：设置生效断言 + 未设置行为不变断言。
- [ ] 1.4 验证：全量 go test + eval 绿。

## Sequencing

打磨轮 3 之后；memory-in-policy 之前（同为"设置/记忆进策略"族，先设置后记忆由浅入深）。
