# Trusted Interaction Foundation Design

## Contract boundaries

```text
user session token  -> user HTTP/WebSocket -> own conversation and public facts
operator token      -> operator HTTP      -> match fact commands and traces
provider identity   -> ingestion boundary -> provisional fact candidates
```

身份字段不从请求体建立。用户 ID 来自已验证会话令牌的 `sub`，运营 ID 来自运营令牌。Origin 只做来源校验，不承担授权职责。

## Data contracts

### `user_sessions`

保存短期用户会话的不可逆 Token 哈希、设备标识、Scope、过期和撤销时间。明文令牌不落库。

### `fact_revisions`

保存事实的稳定 ID、版本、来源、状态、证据和公开时间。状态由 `provisional`、`confirmed`、`revoked`、`conflict`、`reconciled` 组成。公开事实查询只选择 `confirmed` 且未被撤销的版本。

### `idempotency_records`

以 `(match_id, idempotency_key)` 为唯一键保存请求摘要和结果事件。相同键相同 payload 返回原结果，相同键不同 payload 返回冲突。

### `outbox_messages`

与事实写入同一事务，发布失败时由后台任务重试。Outbox 状态不代表用户已经消费，只代表服务端是否已完成发布。

### Privacy metadata

现有会话、Trace、关系状态和关系记忆表增加 `expires_at`、`deleted_at` 等元数据。`privacy_tombstones` 用于阻止删除期间的异步任务回写。

## Migration rules

- 所有 DDL 使用 `IF NOT EXISTS` 或 `ADD COLUMN IF NOT EXISTS`。
- 不删除、不重命名现有字段。
- 新索引优先使用部分索引，避免影响现有查询。
- 新表只被后续阶段读取；本阶段不改变现有运行时查询。
- 迁移文件通过现有 `runMigrations` 按文件名排序、单事务执行。

## Future API contract

后续阶段按以下接口实现，阶段 0 只冻结名称和数据结构：

```text
POST /api/sessions/anonymous
POST /api/sessions/refresh
POST /api/sessions/revoke

GET  /api/me/privacy
GET  /api/me/export
DELETE /api/me/data

POST /api/matches/{matchId}/facts/{factId}/confirm
POST /api/matches/{matchId}/facts/{factId}/revoke
POST /api/matches/{matchId}/facts/{factId}/reconcile
```

导播所有写请求要求 `Idempotency-Key`。用户请求不会接受客户端提交的身份字段作为授权依据。
