# 附录D：比赛事实状态机、数据字典与消息示例

> 文档类型：可复用领域参考  
> 适用阶段：产品、协议、POC 和工程 Evals 已经需要共享同一套事实、版本与可见性语言时  
> 决策状态：首轮字段与状态合同。字段用于表达已批准的边界，不允许用自由文本绕过事实确认、剧透保护或用户控制。  
> 公开资料访问日：2026-01-28。

## 1. 核心模型：信号不会自动变成用户事实

```mermaid
flowchart LR
    S[允许的外部观察] --> C[事实声明\n候选]
    C --> R{调和与证据门}
    R -->|不足或冲突| H[核对中\n不呈现结果]
    R -->|满足条件| F[已确认事实版本]
    F --> V{用户进度、模式、终态\n是否允许可见}
    V -->|否| Q[仅保留为可请求状态]
    V -->|是| P[文字、字幕、语音、舞台计划]
    N[更正或撤销] --> F
    E[结束、静音、删除] --> V
```

这套模型有四层对象：比赛本身、对比赛的事实声明、支持声明的证据引用、用户当场的观察与控制。角色表达和运营动作不属于事实层，不能直接写入或提升事实。

## 2. 不变量

```mermaid
mindmap
  root((不变量))
    候选不等于已确认
    旧版本不能覆盖新版本
    更正必须指向被替换版本
    进度未知不产生结果型呈现
    结束压过排队输出
    删除屏障压过缓存与重试
    接收时间不等于发生时间
```

- 每条 `fact_claim` 都属于一个场次、一个类型和一个版本；对象不清楚时保持候选或冲突。
- 接收时间、来源观察时间和比赛发生时间分别记录；不能用任一个冒充另一个。
- `candidate`、`conflicted`、`retracted` 不能驱动比分、结果性语气、兴奋动作或通知。
- 更正、撤回和会话结束必须使相关的旧呈现计划失效，而不是只改当前文本。
- 用户观察模式属于当前会话，不能因缓存、重连或角色记忆被悄悄放宽。
- 删除屏障建立后，旧队列、离线任务、缓存和恢复流程都不能重新写入或读取被覆盖的资料。

## 3. 事实状态机

```mermaid
stateDiagram-v2
    [*] --> received: 收到格式有效的声明
    received --> candidate: 已完成场次、对象与许可校验
    candidate --> confirmed: 证据与调和门通过
    candidate --> conflicted: 不同声明无法一致
    conflicted --> candidate: 新证据缩小冲突
    conflicted --> confirmed: 达到确认门
    confirmed --> corrected: 新版本替换关键字段
    confirmed --> retracted: 官方或等价证据撤销
    corrected --> corrected: 后续更正
    corrected --> retracted: 撤销当前版本
    received --> rejected: 格式、范围或许可不合格
    candidate --> expired: 核对窗口结束
    conflicted --> expired: 无法安全解决
    rejected --> [*]
    expired --> [*]
    retracted --> [*]
```

```mermaid
flowchart TD
    A[任意事实声明] --> B{场次、对象、范围和许可有效？}
    B -->|否| X[rejected：不入账、不呈现]
    B -->|是| C{证据能确认？}
    C -->|否，且无冲突| D[candidate：仅核对]
    C -->|冲突| E[conflicted：冻结结果性呈现]
    C -->|是| F[confirmed：生成新事实版本]
    F --> G{之后出现更高优先级证据？}
    G -->|更正| H[corrected：保留替换链]
    G -->|撤销| I[retracted：使旧呈现失效]
```

## 4. 调和、版本与呈现

```mermaid
sequenceDiagram
    participant A as 来源 A
    participant B as 来源 B
    participant L as 事实账本
    participant R as 调和器
    participant Q as 可见性裁决
    A->>L: 候选进球 v1
    L->>R: 对象与范围校验
    R-->>Q: candidate，不呈现结果
    B->>L: 相同事件的独立支持 v2
    L->>R: 证据门通过
    R-->>Q: confirmed v2
    A->>L: 官方撤销 v3
    L->>R: 关联被替换版本
    R-->>Q: retracted v3，抑制旧计划
```

确认并不要求固定数量的来源，而要求该类型的确认规则、来源独立性、对象匹配和允许范围都被写清。冲突状态不选择“看起来更像真的”一方；它只说明当前不能把结果交给用户。

## 5. 数据字典

```mermaid
erDiagram
    MATCH ||--o{ FACT_CLAIM : contains
    FACT_CLAIM ||--o{ EVIDENCE_REF : supported_by
    FACT_CLAIM ||--o{ FACT_CLAIM : supersedes
    MATCH ||--o{ OBSERVATION_CONTEXT : viewed_in
    OBSERVATION_CONTEXT ||--o{ EVENT_ENVELOPE : qualifies
    FACT_CLAIM ||--o{ EVENT_ENVELOPE : may_trigger
```

### 5.1 `match`：稳定的场次边界

- `match_id`：不透明稳定标识；不能由队名拼接，也不暴露外部账户。
- `competition_id`、`season_id`、`stage_id`：赛事、赛季和阶段范围；共同解释场次，不能只依赖自然年。
- `home_participant`、`away_participant`：规范化参与方对象；必须和允许来源的场次匹配。
- `scheduled_at`：计划开始时间，采用 RFC 3339；不是实际开球或当前进度。
- `status`、`status_version`：场次状态与单调版本；拒绝迟到的旧状态覆盖。
- `source_scope`：许可和可用来源范围；不能因后续方便扩大用途。

### 5.2 `fact_claim`：一条可追溯的事实声明

- `claim_id`：声明稳定标识；同一逻辑声明的新版本可关联但不能混写。
- `match_id`、`claim_type`、`subject`、`value`：所属场次、类型、对象和结构化值；自由文本不能替代这些字段。
- `occurred_at`、`received_at`、`source_observed_at`：发生、接收和来源观察时间；缺失就标未知。
- `knowledge_state`：`received`、`candidate`、`confirmed`、`conflicted`、`corrected`、`retracted`、`expired` 或 `rejected`。
- `version`、`supersedes`：单调版本与替换链；旧版本不得回写覆盖。
- `evidence_refs`、`confidence_note`：最小证据索引与可审计理由；不向普通用户泄露原始凭证。

### 5.3 `evidence_ref`：支持声明但不扩大资料范围

- `evidence_id`：单条证据标识；不等同于公开可浏览链接。
- `source_class`、`independence_group`：来源类别与独立性分组；多条同源转述不能伪装成独立支持。
- `source_received_at`、`integrity_status`：获取时刻与完整/缺字段/撤销等状态。
- `usage_scope`：该证据可支持什么类型的声明和什么用户范围；范围不自动外溢。

### 5.4 `observation_context`：用户当场可见性与控制

- `session_id`、`match_id`：当次交互与可选场次边界；不作为长期关系标识。
- `spoiler_mode`：严格保护、主动查看或允许更新等明确选择；必须用户可见、可改、可撤回。
- `information_density`、`audio_mode`、`motion_mode`：信息密度、声音和动态选项；静音和低动态不应让核心状态消失。
- `ended_at`、`control_version`：会话终态和控制版本；终态优先于任何迟到输出。
- `last_seen_fact_version`：用户确实看过的最后事实版本；仅用于解释更正，不是放宽剧透的授权。

### 5.5 `event_envelope`：跨通道投递封装

- `message_id`、`message_type`、`contract_version`：唯一性、消息族和合同版本。
- `occurred_at`、`published_at`、`sequence`：业务发生、允许分发和会话内顺序；三者分别表达不同语义。
- `causation_id`、`correlation_id`：上游原因与本次流程关联；不能放入私人原文或可识别资料。
- `payload`：与消息类型对应的经验证结构；禁止由客户端自由拼接。
- `visibility`：`hidden`、`available_on_request`、`visible` 或 `suppressed`；由事实、进度、模式和终态共同裁决。

## 6. 消息示例

### 6.1 候选事实：只进入核对

```json
{
  "message_id": "msg-1001",
  "message_type": "fact.candidate",
  "contract_version": "v1",
  "sequence": 18,
  "occurred_at": "2026-01-28T18:42:10Z",
  "published_at": "2026-01-28T18:42:14Z",
  "visibility": "hidden",
  "payload": {
    "claim_id": "claim-501",
    "match_id": "match-901",
    "knowledge_state": "candidate",
    "reason": "awaiting_reconciliation"
  }
}
```

### 6.2 已确认事实：仍要经过观察模式

```json
{
  "message_id": "msg-1002",
  "message_type": "fact.confirmed",
  "contract_version": "v1",
  "sequence": 19,
  "causation_id": "claim-501",
  "occurred_at": "2026-01-28T18:43:02Z",
  "published_at": "2026-01-28T18:43:05Z",
  "visibility": "available_on_request",
  "payload": {
    "claim_id": "claim-501-v2",
    "match_id": "match-901",
    "knowledge_state": "confirmed",
    "fact_version": 2
  }
}
```

### 6.3 更正或撤回：不能静默改写

```json
{
  "message_id": "msg-1003",
  "message_type": "fact.retracted",
  "contract_version": "v1",
  "sequence": 20,
  "occurred_at": "2026-01-28T18:46:40Z",
  "published_at": "2026-01-28T18:46:45Z",
  "visibility": "visible",
  "payload": {
    "claim_id": "claim-501-v3",
    "supersedes": "claim-501-v2",
    "knowledge_state": "retracted",
    "user_explanation": "此前状态已被撤回，当前不再作为比赛结果呈现。"
  }
}
```

### 6.4 会话结束：让迟到输出失效

```json
{
  "message_id": "msg-1004",
  "message_type": "session.ended",
  "contract_version": "v1",
  "sequence": 21,
  "occurred_at": "2026-01-28T18:47:00Z",
  "published_at": "2026-01-28T18:47:00Z",
  "visibility": "visible",
  "payload": {
    "session_id": "session-301",
    "control_version": 7,
    "suppress_after_sequence": 21,
    "user_explanation": "本场陪看已结束，不会再自动呈现内容。"
  }
}
```

### 6.5 问题说明：保留下一步，不泄露不该显示的内容

```json
{
  "message_id": "msg-1005",
  "message_type": "problem",
  "contract_version": "v1",
  "sequence": 22,
  "visibility": "visible",
  "payload": {
    "code": "FACTS_STILL_RECONCILING",
    "title": "比赛状态仍在核对中",
    "next_actions": ["查看已确认状态", "稍后主动刷新", "结束本场"],
    "spoiler_safe": true
  }
}
```

## 7. 顺序、去重与重连

```mermaid
flowchart TD
    A[客户端收到封装消息] --> B{会话终态或控制版本已压过它？}
    B -->|是| X[抑制，不呈现]
    B -->|否| C{message_id 已处理？}
    C -->|是| D[忽略副作用，保留原确认]
    C -->|否| E{sequence 与事实版本连续且可接受？}
    E -->|否| F[请求最小快照，不猜缺失内容]
    E -->|是| G[更新结构化状态]
    G --> H{资格允许当前媒介？}
    H -->|是| I[呈现已批准计划]
    H -->|否| J[保持隐藏或按需可见]
```

幂等键用于用户动作：同一次结束、删除、保存选择或取消重复提交，必须返回同一已生效结果，而非制造多次声音、多个提醒或多个资料对象。

## 8. 可见性不是单独的“通知开关”

```mermaid
flowchart TB
    F[事实状态] --> V{可见性裁决}
    P[用户进度] --> V
    M[严格保护/信息密度] --> V
    C[静音、仅文字、低动态] --> V
    T[会话终态与删除屏障] --> V
    V --> H[hidden\n不向用户呈现]
    V --> R[available_on_request\n用户主动查看]
    V --> S[visible\n允许文字基线]
    V --> X[suppressed\n因控制或终态失效]
    S --> A[字幕、语音和舞台仅在各自允许时增强]
```

严格保护下，候选、冲突和任何可推断结果的信息都保持 `hidden`；用户主动查看也只能得到已经确认且在其明确范围内可见的内容。静音改变输出媒介，不改变事实状态；结束和删除改变后续可见性与读写许可。

## 9. 首轮验收场景

```mermaid
mindmap
  root((必须覆盖))
    事实
      冲突来源不选边
      确认后出现撤销
      旧版本迟到
    控制
      静音时仍有文字
      结束竞态抑制输出
      删除阻断回写
    重连
      序列缺口只取快照
      旧客户端安全降级
    可访问
      低动态和读屏完成任务
```

每个场景采用合成或获授权输入，记录触发序列、预期领域状态、禁止的用户可见结果和可重放步骤。事实、控制和资料权利属于硬门：一旦失败，不能用满意度、模型评分或平均延迟抵消。

## 10. 与产品、POC 和工程 Evals 的交接

```mermaid
flowchart LR
    A[本附录\n状态、字段、消息语义] --> B[42 协议设计\n跨端合同]
    A --> C[44 技术 POC\n版本、取消、删除实验]
    A --> D[45 产品评测\n用户可见风险场景]
    B --> E[57 工程 Evals\n夹具、断言与回归]
    C --> E
    D --> E
```

附录只统一术语和可复用对象，不做用户价值、技术选型或发布决定。任何字段变更都必须同时审阅：它是否改变事实等级、可见性、用户控制、资料范围或 Evals 的可重放场景。

## 11. 公开资料

- [IETF, RFC 3339](https://www.rfc-editor.org/rfc/rfc3339)：时间戳表达与时区语义。
- [IETF, RFC 6455](https://www.rfc-editor.org/rfc/rfc6455)：实时消息通道与关闭语义。
- [IETF, RFC 9110](https://www.rfc-editor.org/rfc/rfc9110)：状态、条件与幂等概念。
- [IETF, RFC 7807](https://www.rfc-editor.org/rfc/rfc7807)：结构化问题说明。
- [OWASP, API Security Top 10](https://owasp.org/www-project-api-security/)：对象授权和资料暴露风险。
