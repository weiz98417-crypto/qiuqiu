# Design: Character Settings

## 服务形状

（as-built）`relationship.CharacterSettings.Set/Get`：按用户独立表 user_character_settings（migration 048）持久化三槽位（initiative/analysis_appetite/banter_level），槽位白名单校验；relationship_states 是按 (user, match) 的状态，人格设置需要跨场稳定的一份状态，故独立成表。话痨档暂留原路径（迁移如实收缩）。cue 词入口仍写旧状态——三入口一状态的完全收敛（含 policy 运行时读取）为后续任务。

## 三入口

- WS `set_character`：消息体 {field, value}，槽位白名单（initiative/analysis_appetite/banter_level），即改即 ack（对齐 set_talkativeness 的 watchconnection 模式）。
- HTTP `/api/me/character`：GET 读 / PATCH 改（portrait_api 同款会话鉴权与账号作用域）。
- cue 词：policy 既有触发词原样（遗留入口，仍写旧状态——收敛为后续任务）。

（as-built）Ledger 记账 `character.updated` 与运营台只读段未随本波落地，如实留尾（tasks 5.2 尾注 / 5.3 未勾）。

## 运营台

`/api/console/users/{id}` 读形状附 preferences 只读段（干预级别不变，只是可见）——留尾。

## 删除测试

删掉 CharacterSettings：三入口退回各自为政（cue 词-only 的现状），无悬空依赖。
