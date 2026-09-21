# Design: Character Settings

## 服务形状

`relationship.CharacterSettings.Apply(ctx, userID, SettingChange) (RelationshipPreferences, error)`：校验（枚举合法、不改 stance 字段）→ CAS 更新 relationship_state.preferences（复用 PostgresRepository 既有存取）→ 返回前后值。话痨档迁入同一入口（底层仍写 user_preferences 表 + 状态推导，行为不变）。

## 三入口

- WS `set_character`：消息体 {field, value}，白名单字段（talkativeness/initiative/analysis_appetite/banter），即改即 ack（对齐 set_talkativeness 的 watchconnection 模式）。
- HTTP `/api/me/character`：GET 读 / PATCH 改（portrait_api 同款会话鉴权与账号作用域）。
- cue 词：policy 既有触发词原样（它们也是 adapter，不是遗留）。
每入口成功后记 Interaction Ledger 事件 `character.updated`（field/from/to/source）。

## 运营台

`/api/console/users/{id}` 读形状附 preferences 只读段（干预级别不变，只是可见）。

## 删除测试

删掉 CharacterSettings：三入口退回各自为政（cue 词-only 的现状），无悬空依赖。
