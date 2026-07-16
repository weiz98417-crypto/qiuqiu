# 球球核心可信链改造方案

状态：已确认方案 v1.0
范围：审查项 1、2、3、6
对应问题：用户鉴权与身份冒用、比赛事实可信链、隐私与数据生命周期、导播事件幂等

## 1. 目标与边界

本方案把用户端、导播台、比赛事实层和球球关系层拆成清晰的信任边界，确保：

1. 用户只能操作和读取自己的会话数据，不能提交任意 `userId`。
2. 导播台和外部数据源产生的事件先进入事实账本，公开比分和球球回答只使用公开事实视图。
3. 原始音频不落库，文本、Trace 和关系记忆有明确的保留期、导出和删除路径。
4. 导播重复点击、网络重试和并发提交不会制造重复比赛事件。
5. 事实确认、球球问答、主动播报、客户端轮播使用同一套事实状态。

本阶段不处理 Live2D 纹理压缩、Android 正式签名、LLM 人味策略和外部数据源扩展。这些问题与本方案有接口关系，但不属于本次可信链改造的阻塞范围。

## 2. 当前实现与主要缺口

当前 WebSocket 鉴权仍允许同源客户端绕过共享 Token，消息体中的 `userId` 会参与关系状态处理。共享 `APP_TOKEN` 如果编译进客户端，就能被从 APK、JavaScript 或 WebSocket 握手中提取。

当前比赛事件已经有 `confirmed`、`status`、`RevisionOf` 和来源字段，但公开快照、事件检索和主动播报还没有统一的“只读公开事实视图”。来源冲突可以把比赛标记为 `conflict`，但没有完整的确认、撤销和调和流程。

当前 PostgreSQL 会保存转写、用户输入、球球回复、Trace 和关系记忆，但没有统一的用户删除、导出、过期清理和删除审计流程。

当前导播事件依赖来源事件 ID 等局部去重逻辑，人工快捷操作缺少请求级幂等键。按钮双击、长按、网络重试可能重复创建事件。

## 3. 目标架构

```text
用户端
  │  短期用户会话令牌
  ▼
用户 API / 用户 WebSocket ───────► 用户私有对话、关系记忆、Trace（按 userId 隔离）
  │
  └──────────────────────────────► PublicFactView（只读公开比赛事实）

导播台 / 外部数据源
  │  运营身份 / 来源身份 + Idempotency-Key
  ▼
事实命令入口 ──► 比赛事实账本 ──► 事实状态机 ──► PublicFactView
                         │                 │
                         │                 ├─ 球球问答
                         │                 ├─ 主动播报
                         │                 └─ 客户端轮播
                         ▼
                    Outbox 事件队列
```

信任边界分为三类：

| 主体 | 身份来源 | 允许操作 |
|---|---|---|
| 用户端 | 服务端签发的短期用户会话令牌 | 自己的对话、自己的偏好、公开比赛事实读取 |
| 导播台 | 独立运营令牌和角色权限 | 配置比赛、创建/确认/撤销/修订事实、查看运营 Trace |
| 外部数据源 | 来源配置和来源事件 ID | 提交候选事实，不直接获得用户或导播权限 |

## 4. 身份与鉴权方案

### 4.1 令牌拆分

移除客户端对共享 `APP_TOKEN` 的依赖，新增短期用户会话：

```text
POST /api/sessions/anonymous
POST /api/sessions/refresh
POST /api/sessions/revoke
```

用户会话令牌至少包含：

```json
{
  "sub": "usr_...",
  "sid": "ses_...",
  "deviceId": "dev_...",
  "scope": ["user:chat", "user:read"],
  "iat": 0,
  "exp": 0
}
```

令牌建议有效期 15 分钟，刷新令牌只存服务端哈希并可撤销。匿名会话是第一阶段方案，后续可以在同一 `sub` 模型上接入手机号、Apple 或其他登录方式。

导播台使用独立运营令牌，权限至少拆分为：

```text
operator:match:write
operator:fact:confirm
operator:fact:correct
operator:trace:read
```

运营令牌不能用于用户 WebSocket，用户会话令牌不能调用导播写接口。

### 4.2 服务端身份规则

- `userId` 只从已验证令牌的 `sub` 得出。
- 消息体中的 `userId` 不再作为身份来源；存在时只能用于兼容校验，必须与令牌一致，否则返回 `403`。
- `operatorId` 只从运营令牌读取，不接受前端提交值。
- Origin 校验只负责浏览器来源控制，不代替身份和权限校验。
- HTTP 和 WebSocket 复用同一身份解析器、过期检查和 Scope 检查。

### 4.3 兼容迁移

设置 `AUTH_MODE=dual` 时同时支持旧 Token 和新会话，但旧 Token 只能访问本地开发环境。生产环境切换到 `AUTH_MODE=session` 后，客户端必须先建立会话再连接 WebSocket。

兼容期所有旧 Token 使用情况写入审计日志，确认没有生产客户端依赖后删除旧路径。

## 5. 比赛事实可信链

### 5.1 事实状态机

```text
provisional ──► confirmed
      │              │
      └──────────────► revoked

provisional / confirmed ──► conflict ──► reconciled
```

- `provisional`：已收到来源或人工输入，但未达到公开确认条件。
- `confirmed`：满足来源和校验规则，可以进入公开事实视图。
- `revoked`：原事实被撤销，不再参与比分、问答或播报。
- `conflict`：多个来源或时间线互相矛盾，暂停事实确认。
- `reconciled`：导播完成调和，生成新的有效事实或明确撤销。

### 5.2 事实账本

在现有 `match_events` 基础上补齐以下字段或等价表：

| 字段 | 作用 |
|---|---|
| `fact_id` | 稳定的事实 ID，不随修订变化 |
| `revision` | 同一事实的修订版本 |
| `status` | `provisional/confirmed/revoked/conflict/reconciled` |
| `source_type` | `operator/provider/system` |
| `source_event_id` | 外部来源事件 ID |
| `confidence` | 来源置信度和规则计算结果 |
| `evidence` | 来源快照、操作说明或交叉验证信息 |
| `confirmed_by` | 确认该事实的运营主体 |
| `revision_of` | 被替换的事实版本 |
| `occurred_at` | 比赛内发生时间 |
| `recorded_at` | 服务端记录时间 |
| `public_at` | 进入公开事实视图的时间 |

同一外部来源使用 `(source_type, source_event_id)` 去重。人工事件必须使用客户端生成的操作 ID 和服务端幂等键，不能用“同一分钟同类型”做粗略去重。

### 5.3 公开事实视图

新增逻辑视图或服务接口 `PublicFactView`，只返回 `confirmed` 且未被撤销的事实。以下消费者禁止直接读取原始候选事件：

- 比分和比赛状态卡片
- 用户的“谁进球了”“现在比分多少”等回答
- 导播触发的主动播报
- 客户端状态轮播和 Live2D 状态文案

导播台可以同时看到 `provisional`、`conflict` 和 `confirmed`，并提供确认、撤销、选择来源和修订入口。

### 5.4 数据源处理

外部数据源事件进入候选队列，先持久化再推进来源游标。确认策略采用：

1. 单一来源事件默认 `provisional`。
2. 高可信来源满足完整性校验后，可以进入待确认队列。
3. 导播确认后进入 `confirmed`。
4. 同一时间线出现互斥事实时进入 `conflict`，暂停公开播报。
5. 队列写入失败不推进游标，后续轮询必须重试。

## 6. 隐私与数据生命周期

### 6.1 默认保留策略

| 数据 | 默认策略 | 说明 |
|---|---|---|
| 原始音频 | 不落库 | ASR 完成后立即释放 |
| ASR 转写 | 会话期 + 30 天 | 用户可提前删除 |
| 用户输入和球球回复 | 30 天 | 用于会话恢复和质量诊断 |
| Trace | 30 天 | 脱敏后保留诊断字段 |
| 关系记忆 | 用户主动删除前保留 | 允许单独清除 |
| 运营审计 | 180 天 | 保留操作结果，不保留正文 |

所有保留期必须配置化，不能只写在文档中。

### 6.2 用户接口

```text
GET    /api/me/privacy
GET    /api/me/export
DELETE /api/me/data
```

- `GET /api/me/export` 返回当前用户的结构化 JSON，包括会话、偏好和关系记忆；不导出内部密钥、其他用户数据或运营令牌。
- `DELETE /api/me/data` 必须幂等，删除对话、转写、Trace、关系记忆和设备会话。
- 删除任务异步执行，返回随机不可猜测的 `jobId`；会话被删除后，客户端可用 `jobId` 查询隐私状态，不返回用户 ID。
- 删除完成后写入不可逆的删除审计记录，但不保留被删除正文。

### 6.3 清理与防回写

- 后台清理任务按 `expires_at` 分批删除，避免一次性锁表。
- 删除操作先写 tombstone，再处理异步队列，防止旧任务重新写回。
- 缓存、内存关系状态和 Trace 读取都必须检查 tombstone。
- 备份删除受存储系统能力限制，文档要明确最长残留时间。

## 7. 导播事件幂等

幂等是指同一个请求重复到达时，系统只产生一个结果。它解决的是双击、网络重试和并发提交，不替代事实冲突校验。

### 7.1 请求协议

所有导播写接口要求请求头：

```text
Idempotency-Key: op_01J...
```

服务端保存：

```text
(match_id, idempotency_key, payload_hash, result_event_id, status, expires_at)
```

行为约定：

| 情况 | 响应 |
|---|---|
| 首次 Key | 创建事件并记录结果 |
| 相同 Key、相同 payload | 返回原事件，不重复广播 |
| 相同 Key、不同 payload | `409 Conflict` |
| Key 缺失 | `400 Bad Request` |
| Key 已过期 | 重新提交视为新操作 |

### 7.2 事务与广播

事件写入、幂等记录和 Outbox 消息在一个数据库事务中完成。后台发布器读取 Outbox，成功发送后标记完成，失败则按退避策略重试。客户端收到重复消息时也使用 `eventId` 去重。

修订事件、确认事件、撤销事件和配置变更都使用同一套幂等协议。

前端仍然增加 in-flight 锁和按钮状态，但前端锁只是体验优化，不能作为安全保证。

## 8. 实施阶段

### 阶段 0：冻结合同和迁移规则

- 定义令牌 Claims、Scope、错误码和身份中间件接口。
- 定义事实状态机和 `PublicFactView` 数据合同。
- 定义隐私数据分类、保留期和删除范围。
- 定义 Idempotency-Key 规范和 Outbox 状态。
- 输出数据库迁移草案和兼容开关。

交付物：本方案对应的 API 草案、数据库迁移草案、端到端用例清单。

### 阶段 1：身份与权限

- 实现匿名会话签发、刷新、撤销。
- 接入 HTTP 和 WebSocket 统一身份解析。
- 服务端绑定 `userId`，移除消息体身份信任。
- 导播台切换独立运营令牌和 Scope。
- 完成旧 Token 双轨迁移和审计。

### 阶段 2：事实账本和公开视图

- 增加事实版本、状态、证据和确认信息。
- 将比分、问答、播报和轮播切换到 `PublicFactView`。
- 增加确认、撤销、冲突调和接口。
- 修正数据源游标和失败重试顺序。
- 增加重启恢复和来源冲突测试。

### 阶段 3：隐私闭环

- 增加 `expires_at`、删除标记和用户归属索引。
- 实现导出、删除、状态查询接口。
- 实现分批清理任务和 tombstone 防回写。
- 清理日志和 Trace 中的正文敏感字段。
- 增加隐私设置页入口。

### 阶段 4：导播幂等与 Outbox

- 给所有导播写接口增加幂等记录和唯一约束。
- 将事件写入和广播拆成事务 + Outbox。
- 导播前端增加提交锁、失败重试和冲突提示。
- 外部来源继续按来源事件 ID 去重。

### 阶段 5：端到端验证与灰度

- 用两组用户会话验证数据隔离。
- 用导播台录入候选事件、确认事件、撤销事件和修订事件。
- 用用户端验证比分、问答、主动播报和轮播的一致性。
- 模拟重复点击、断网重试、服务重启和删除期间异步任务。
- 先在演示环境启用新链路，再切生产 `AUTH_MODE=session`。

## 9. 数据库迁移草案

建议新增或调整以下对象，最终以实际 SQL 评审为准：

```text
user_sessions
  id, user_id, device_id, token_hash, scopes, expires_at, revoked_at, created_at

fact_revisions
  fact_id, revision, match_id, status, source_type, source_event_id,
  confidence, evidence_json, confirmed_by, revision_of, occurred_at,
  recorded_at, public_at

idempotency_records
  match_id, idempotency_key, payload_hash, result_event_id,
  status, created_at, expires_at

outbox_messages
  id, aggregate_type, aggregate_id, payload_json,
  status, attempts, next_attempt_at, published_at

privacy_tombstones
  user_id, job_id, requested_at, completed_at, updated_at, scope, reason, status, error
```

关键约束：

- `user_sessions.token_hash` 唯一。
- `idempotency_records(match_id, idempotency_key)` 唯一。
- `fact_revisions(match_id, source_type, source_event_id, revision)` 唯一。
- 用户数据表必须有 `user_id` 和可清理时间字段。
- 事实公开查询必须有 `status` 和 `public_at` 索引。

## 10. 验收标准

### 鉴权

- 伪造 `userId` 不能读取或写入其他用户数据。
- 用户会话令牌不能调用导播写接口。
- 导播令牌不能读取用户私密对话。
- 过期、撤销和错误 Scope 均返回明确错误。

### 事实

- `provisional` 事件不会改变用户公开比分。
- `conflict` 状态不会触发确定性播报。
- `confirmed`、`revoked`、`reconciled` 会同步影响问答、播报和轮播。
- 服务重启后事实状态和来源游标不丢失。

### 隐私

- 用户导出只包含自己的数据。
- 删除后对话、Trace、关系记忆和会话均不可再查询。
- 过期任务可重复执行，不误删其他用户。
- 删除期间的异步任务不会回写已删除数据。

### 幂等

- 同一 Key 并发提交 10 次只创建一个事件。
- 相同 Key 不同 payload 返回 `409`。
- 网络重试不会重复主动播报。
- 事件写入成功但首次广播失败时，Outbox 能自动补发。

## 11. 观测与回滚

必须记录但不记录正文的指标：

- 会话签发、刷新、撤销和鉴权失败次数。
- 事实状态转换和冲突数量。
- 隐私导出、删除、清理成功/失败数量。
- 幂等命中、payload 冲突、Outbox 重试数量。
- 公开事实视图与原始事件不一致的告警。

每个阶段使用独立 feature flag。回滚只关闭新入口，不删除已写入的事实版本、幂等记录和隐私 tombstone。数据库迁移采用向前兼容方式，确保旧服务可以读取新增字段。

## 12. 已确认默认决策

1. 身份：先做匿名设备会话，后续接入正式账户体系。
2. 事实：外部数据源默认进入 `provisional`，导播确认后公开。
3. 隐私：原始音频不落库，文本和 Trace 默认保留 30 天。
4. 幂等：所有导播写接口统一要求 `Idempotency-Key`。

这些决策确认后，下一步进入阶段 0，先提交接口合同和数据库迁移草案，再开始代码实现。

## 相关文档

- [Companion Agent Boundaries and Evals](companion-agent-boundaries-and-evals.md)
- [Demo Runbook](demo-runbook.md)
- [Humanity Runtime Technical Design](humanity-runtime-technical-design.md)
- [Trusted Interaction Foundation OpenSpec](../openspec/changes/trusted-interaction-foundation/proposal.md)
