---
篇号: "18"
标题: API 与实时协议
状态: active
版本: v3.0
基线: a7e5100（2026-06-07）
文体: reference（查表体：路由表与消息表即本篇主体，论证入篇末设计说明）
上游: PRD 第四章 F10 客户端与运营台域（FR-OPS-001～012）、F9 身份域（FR-IDN-001～005/007/008/015）；ADR-0008/0010/0020/0021/0022；迁移源 v2 档案 42-API与实时协议设计、附录F
评测挂钩: console_golden_test.go（三表路由组合锁定=路由表核对测试）；operator-write 幂等单测；E2E runtime-e2e.mjs（真实 WS 话轮）；session-isolation E2E；登录 E2E；console-pages.spec.mjs；协议枚举对账以附录 C（协议字典）为 wire 示例基准
裁决点: ⚖️B12（42 章合同口径裁决：合同扩至现网全集——控制面 51+用户面族+login+WS 20/19，本篇即全集唯一底稿；talkativeness_ack 口径注记见 §7.2）
---

# 18 API 与实时协议

> 本篇基线：2026-06-07 仓库 a7e5100。

**本篇定位**：球球对外协议面的全集合同与索引——后端每一个 HTTP 端点、实时通道的每一条消息、每一套回执枚举，逐条登记并与代码路由表对账。本篇是 PRD FR-OPS-004「完整端点清单以规范 18 篇路由表为准」的唯一落点：**加端点先改本篇对账的路由表，文档引用先查本篇**。wire 示例与字段级字典归附录 C，本篇不重复示例。

**导读**：§1 协议总纲（双面/鉴权三态/兼容约定）｜§2 HTTP 端点全集（控制面 51+用户面族+错误码约定）｜§3 WS 协议全集（/ws/match 上 20 下 19、三套回执枚举、/ws/ops、降级与异常归并 §3.9）｜§4 延迟预算表（体验侧摘要）｜§5 兼容与演进｜§6 验收与评测挂钩｜§7 设计说明。

**事实依据**：本篇全部端点与消息逐条对照 `backend/cmd/server/router.go`、`console_api.go`、`match_operator_api.go`、`knowledge_api.go`、`main.go:649-760` 与 `watchconnection.go:991-1295`、`ops_stream.go`、`transcription.go`、`turn_relay.go`、`response_delivery.go` 现码核对（a7e5100）。代码中不存在、文档曾写的条目（`welcome` 首条下行、`reconnecting` 通道消息）在本篇显式注记，防回潮。

---

## 1. 协议总纲

### 1.1 两个面：HTTP 与 WS 各管什么

```mermaid
flowchart LR
    C[客户端 Flutter/web] -->|sessions 鉴权三态、me 隐私与可感面| U[用户面族 /api/sessions /api/me]
    C -->|目录与公开快照| M[比赛信息 /api/matches catalog·state·events·clock]
    C -->|实时流 /ws/match/{id}| W[WS 消息泵 上20/下19]
    O[运营台 console/] -->|控制面 51 三表| K["/api/console + /api/matches 运营面"]
    O -->|只读观测 /ws/ops| OPS[/ws/ops 无正文流/]
    E[评测与脚本] -->|只读 4 工具| MCP[/mcp 匿名 401/]
    U --> W
    M --> W
```

| 面 | 承载 | 划界 |
| --- | --- | --- |
| HTTP（`/api/…` 单体 mux） | 请求-响应型合同：会话签发、隐私三权、画像/手记/瞬间读写、运营台控制面 51 端点、比赛目录 | 一切「问一次答一次」的操作；写操作的幂等与审计在 HTTP 层约定 |
| WS（`/ws/match/{matchId}`） | 比赛实时流与语音话轮：快照/时钟/事件推送、对话回合、流式转写、播放回执、打断与设置回执 | 一切「连接期间持续发生」的交互；断线重连、补发与去重在 WS 层约定 |
| WS（`/ws/ops`） | 运营观测旁路：账本 Append 的实时广播（无正文） | 只读、单向下行；上行不收业务消息（ping 保活除外） |
| `/mcp` | 账本只读 MCP server（ADR-0022）：4 个查询工具 | 标准协议形态的只读出口；无任何写工具 |

协议面把「已经被裁决的状态」送到正确会话：不预测用户进度、不把候选当事实推送、客户端不直接改写比赛状态、重连不自动补播已取消内容。这些边界决定下面每张表里「没有哪些消息」。

### 1.2 鉴权三态

| # | 形态 | 凭证取得 | 携带方式 | scope 面 | 适用面 |
| --- | --- | --- | --- | --- | --- |
| 1 | **匿名会话**（C 端默认） | `POST /api/sessions/anonymous`（deviceId 换取） | `Authorization: Bearer <accessToken>`；WS 走子协议 `qiuqiu-auth.<base64url(token)>` | `user:chat` + `user:read`（终端用户侧全集） | 用户面族全部端点 + `/ws/match` |
| 2 | **C 端登录**（原地绑凭证，ADR-0020） | `POST /api/sessions/login`（须携当前有效会话，匿名即可） | 同形态 1——登录不换令牌体系，只在原 `usr_` 上加凭证 | 同形态 1 | 同形态 1（身份升级、面不变） |
| 3 | **运营 JWT**（ADR-0010 人类通道） | `POST /api/console/auth/login`（用户名+密码） | `Authorization: Bearer <15min HS256 JWT>`；刷新令牌 30 天单次轮换 | 按角色授 `operator:trace:read` / `operator:match:write` / `operator:fact:confirm` / `operator:fact:correct`；auditor 角色只有读 | 控制面 51 端点 + `/ws/ops` |

补充三条边界规则：

1. **机器通道**：运营个人 API 令牌（仅存 SHA-256 hash）与 legacy `APP_TOKEN` 在过渡期通行控制面（`operator_auth.go` legacy 分支）；密码账户存在即拒绝 legacy 令牌通道。开发档（非 production 且无 APP_TOKEN）匿名持 director 全 scope。
2. **`/mcp` 刻意不继承 legacy**：匿名一律 401；持有效凭证但缺只读 scope 403（ADR-0022）。
3. **四个公开读端点不包 withScope**：match 面 `clock/config/events/state` 的 GET 匿名得公开视图、持 `operator:trace:read` 得运营视图，判定在 handler 内以 `authz.view` 完成。
4. **401 与 403 的前端处置不同**：运营台 401=全局登出回登录页（令牌失效类），403=页内提示（scope 缺失类）；C 端 401=清档回匿名兜底（§2.4 第 3 行）。两种失败不同处置是合同的一部分，不得合并为统一错误页。

### 1.3 通用传输约定

| # | 约定 | 内容 |
| --- | --- | --- |
| 1 | JSON 字段命名 | 全协议 camelCase（`deliveryKey`/`utteranceId`/`traceId`/`refreshExpiresAt`）；枚举值小写蛇形（`text_fallback`/`turn_planned`） |
| 2 | 路径参数 | `{matchId}`/`{userId}`/`{traceId}` 式占位符即 ServeMux 通配段（Go 1.22+ `r.PathValue`）；无尾部斜杠形态 |
| 3 | CORS | `applyCORS` 统一施加：Origin 白名单校验（不过 403）、`Allow-Methods: DELETE, GET, OPTIONS, PATCH, POST`、`Allow-Headers: Content-Type, Authorization, Idempotency-Key`；OPTIONS 预检在进入 mux 前拦截，子树内任意路径一律 200 空体 |
| 4 | 请求体上限 | 用户面会话/资料族 16KB（`MaxBytesReader`）；控制面运营写 1MB；WS 单帧 1MB（§3.1）——超限即拒，服务端不做无限缓冲 |
| 5 | 响应纪律 | 用户面与控制面端点一律 `Cache-Control: no-store`；JSON 响应 UTF-8；错误体为纯文本 `http.Error` 或 JSON error 对象（501 降级族） |
| 6 | 幂等键头 | 运营写经 `Idempotency-Key` 头声明幂等键；同键重放返回原结果（§2.7） |

### 1.4 版本与兼容约定

| # | 约定 | 内容 |
| --- | --- | --- |
| 1 | 无统一信封 | 每类 WS 消息自带语义（`type` + 自需字段），没有共享包装层；唯一生成规则是状态回执按 `DeliveryStatus.Kind+"_status"` 动态拼名（`voice_status`、`first_meeting_status` 由此产生） |
| 2 | 增量字段 | 新增能力以增量字段进入；客户端对未知字段忽略，对未知消息类型显示中性状态或不呈现增强内容，绝不把未知解释为事实 |
| 3 | 枚举只增不删 | §3.4 各回执枚举的收缩是破坏性变更，须走裁决；`blocked` 这类别名值一经发布不删除 |
| 4 | 表即文档 | 新端点唯一合法路径：先在 `consoleRoutes`/`matchRoutes`/`knowledgeRoutes` 三表之一加一行（method/pattern/scope/handler），再写 handler；本篇 §2 与路由表 diff 为零（FR-OPS-004 验收） |
| 5 | openspec 咬合 | 行为变更走 openspec change，提案必须引用本篇「篇 ID+锚点」；落地后在篇末修订记录回写 change 名（00 篇 §6.7） |

---

## 2. HTTP 端点全集

控制面 51 端点（ServeMux 声明式路由表三张）+ 表外用户面族。幂等性列口径：**幂等（键）**= 走 `executeOperatorWrite` 幂等包裹（`Idempotency-Key` 头，同键重放返回原结果不产生二次副作用）；**草稿制**= 先草稿后发布两步；**条件写**= 带版本/门控条件的幂等写；读端点天然幂等不标注。

### 2.1 控制面 · console 面 19（console_api.go:158-184）

scope 缩写：读=`operator:trace:read`，写=`operator:match:write`。

| # | 方法/路径 | 用途 | scope | 幂等性 |
| --- | --- | --- | --- | --- |
| 1 | GET `/api/console/overview` | 概览九格聚合（活跃比赛/在线会话/话题老化/记忆健康/主动引用/词汇漏斗/微反应/语音健康/TTS 三态） | 读 | 读 |
| 2 | GET `/api/console/client-health` | 客户端语音健康遥测聚合（进程内环形缓冲，重启清零；wire 裁剪至 kind/at/matchId，无身份无正文） | 读 | 读 |
| 3 | GET `/api/console/matches/{matchId}/users` | 单场用户列表（在线/话痨档/开放话题/画像更新时间/偏好） | 读 | 读 |
| 4 | GET `/api/console/threads` | 话题台账列表 | 读 | 读 |
| 5 | PATCH `/api/console/threads/{threadId}` | 话题处置（关闭/老化）；审计留痕 | 写 | 幂等（键） |
| 6 | GET `/api/console/users/{userId}/portrait` | 用户画像只读（运营视图） | 读 | 读 |
| 7 | DELETE `/api/console/users/{userId}/portrait` | 删除用户画像（危险动作，前端二次确认）；审计留痕 | 写 | 幂等（键） |
| 8 | GET `/api/console/delivery-interruptions` | 投递打断记录（播放期被打断的投递视图） | 读 | 读 |
| 9 | GET `/api/console/config` | 运营观测页运行时配置（grafanaUrl；空=前端渲染部署指引占位） | 读 | 读 |
| 10 | GET `/api/console/tts-supply` | TTS 供给三态读（当前模式+本地腿健康门控快照） | 读 | 读 |
| 11 | PATCH `/api/console/tts-supply` | TTS 供给三态写（cloud/local/local_first）；幂等写+审计+即时生效；本地腿健康门控未过回 409 带原因；无持久化存储降级 501 | 写 | 条件写（健康门控） |
| 12 | GET `/api/console/operators` | 运营员列表；无持久化存储（DATABASE_URL 缺）降级 501 | 写 | 读 |
| 13 | POST `/api/console/operators` | 新建运营员：个人令牌仅创建时展示一次（仅存 SHA-256 hash）+12 字符临时密码（首登强制改密）；重名/非法角色 400 | 写 | 非幂等（重名 400） |
| 14 | DELETE `/api/console/operators/{operatorName}` | 吊销运营员（危险动作，二次确认） | 写 | 幂等（键） |
| 15 | GET `/api/console/whoami` | 当前身份解析（只解析不查 scope） | 无（仅身份） | 读 |
| 16 | POST `/api/console/auth/login` | 运营登录（ADR-0010）：PBKDF2-SHA256 100k 校验，签发 15min HS256 JWT+30 天单次刷新令牌；首登临时密码 JWT 无 scope；失败登录写审计 `auth.login_failed`（v1 不锁定） | 无（认证入口本身） | 非幂等（签发） |
| 17 | POST `/api/console/auth/refresh` | 刷新令牌轮换（旧行消费即删，重用即失效） | 无（以 refresh token 为凭证） | 非幂等（轮换） |
| 18 | POST `/api/console/auth/logout` | 退出：吊销当前设备刷新令牌 | 无（以 refresh token 为凭证） | 幂等 |
| 19 | PATCH `/api/console/me/password` | 自助改密（旧密码+新密码 ≥10 字符）；改密前无 scope 令牌不可用 | 无（仅身份+旧密码验证） | 条件写 |

### 2.2 控制面 · match 运营面 28（match_operator_api.go:294-322）

路径前缀 `/api/matches/{matchId}`；scope 缩写：读=`operator:trace:read`，写=`operator:match:write`，确认=`operator:fact:confirm`，更正=`operator:fact:correct`。

| # | 方法/路径 | 用途 | scope | 幂等性 |
| --- | --- | --- | --- | --- |
| 1 | GET `…/interaction` | 某用户交互账本分页（`userId` 必填；附 journey 投影与审计报告；cursor 分页） | 读 | 读 |
| 2 | POST `…/start` | 布阵开赛：停源+重置状态+写入新配置（空 lifecycle 默认 scheduled） | 写 | 幂等（键） |
| 3 | POST `…/reset` | 重置比赛状态（演示/复赛复位） | 写 | 幂等（键） |
| 4 | GET `…/sources` | 数据源状态读（provider 健康/挂载状态） | 读 | 读 |
| 5 | POST `…/sources/start` | 挂源启动 | 写 | 幂等（键） |
| 6 | POST `…/sources/stop` | 挂源停止 | 写 | 幂等（键） |
| 7 | POST `…/host` | 一键托管：ESPN summary 填充配置+开场+挂源一条龙 | 写 | 幂等（键） |
| 8 | POST `…/takeover` | 人工接管（停自动化，转人工驱动） | 写 | 幂等（键） |
| 9 | GET `…/automation` | 自动化开关读 | 读 | 读 |
| 10 | POST `…/automation` | 自动化开关写 | 写 | 幂等（键） |
| 11 | GET `…/clock` | 比赛时钟读（公开/运营双视图） | 公开 | 读 |
| 12 | PATCH `…/clock` | 时钟修正；`expectedVersion` 不符回 409，前端自动重读 | 写 | 条件写（版本） |
| 13 | POST `…/drafts/voice` | 导播语音草稿生成（草稿制第一步） | 写 | 草稿制 |
| 14 | POST `…/drafts/voice/publish` | 语音草稿发布（发布后进与球球自发播报同一投递管线，不绕过表达纪律） | 写 | 草稿制 |
| 15 | POST `…/facts/{factId}/confirm` | 事实人工确认（ADR-0024 双通道之运营腿） | 确认 | 幂等（键） |
| 16 | POST `…/facts/{factId}/revoke` | 事实撤销（触发 WS `match_fact_retracted` 下行） | 确认 | 幂等（键） |
| 17 | POST `…/facts/{factId}/reconcile` | 冲突调和 | 确认 | 幂等（键） |
| 18 | POST `…/conflicts/{conflictId}/resolve` | 冲突裁决（冲突永不自动裁决的人工落点） | 确认 | 幂等（键） |
| 19 | GET `…/facts/{factId}/revisions` | 事实修订史读 | 读 | 读 |
| 20 | GET `…/config` | 比赛配置读（公开/运营双视图） | 公开 | 读 |
| 21 | POST `…/config` | 比赛配置写 | 写 | 幂等（键） |
| 22 | POST `…/lifecycle` | 生命周期设置（scheduled/live/halftime/…） | 写 | 幂等（键） |
| 23 | GET `…/events` | 事件时间线读（公开/运营双视图） | 公开 | 读 |
| 24 | POST `…/events` | 事件创建（运营手动发事件） | 写 | 幂等（键） |
| 25 | GET `…/state` | 公开快照读（剥除完整性调试字段） | 公开 | 读 |
| 26 | GET `…/traces` | 回合 trace 列表（运营观测） | 读 | 读 |
| 27 | GET `…/traces/{traceId}` | 单条 trace 详情（含延迟分解 attach 载荷） | 读 | 读 |
| 28 | POST `…/events/{eventId}/correct` | 事件更正（修订号递增，低版本更新被客户端丢弃） | 更正 | 幂等（键） |

### 2.3 控制面 · knowledge 面 4（knowledge_api.go:71-74，挂 `/api/console/knowledge` 子树，ServeMux 最长前缀优先）

scope 缩写同上。store 缺席（无 DATABASE_URL）时各 handler 降级 501，与 Operators 页同款口径。

| # | 方法/路径 | 用途 | scope | 幂等性 |
| --- | --- | --- | --- | --- |
| 1 | GET `/api/console/knowledge` | 条目列表（q 检索/status 生效二态/due=review 待复查过滤/分页） | 读 | 读 |
| 2 | POST `/api/console/knowledge` | 新建条目（保存即生效，无草稿流） | 写 | 非幂等（新建） |
| 3 | GET `/api/console/knowledge/{entryId}` | 单条读取（worldinfo 参数学五字段在列） | 读 | 读 |
| 4 | PUT `/api/console/knowledge/{entryId}` | 全量更新条目 | 写 | 幂等（键） |

### 2.4 用户面族（表外，C 端合同；不计入控制面 51 数）

挂载于 `main.go:649-668`；除特别注明外鉴权=形态 1/2 会话 + `user:read` scope，缺失 401/403。store 缺位=空列表静默隐藏（不是错误）。

| # | 方法/路径 | 用途 | 鉴权 | 幂等性/关键语义 |
| --- | --- | --- | --- | --- |
| 1 | POST `/api/sessions/anonymous` | 签发匿名会话（deviceId 换 `usr_` 双令牌）；同 deviceId 归还同一身份 | 无（身份入口本身） | 幂等（同 deviceId 同 `usr_`）；隐私删除 pending/failed 期同 deviceId 被拒 409（客户端如实重置重开）；201 Created |
| 2 | POST `/api/sessions/login` | 登录/注册单框双合一（ADR-0020）：identifier 未绑定=注册（绑到当前 `usr_`），已绑定验密=切号；四路归并（注册/切换/密码错/注册竞态） | 须携当前有效会话（匿名即可） | 非幂等（会话签发）；identifier 形态=邮箱或 3-32 位用户名，密码 ≥7 位，argon2id 存储 |
| 3 | POST `/api/sessions/refresh` | 刷新令牌轮换（`<sessionID>.<secret>` 自识别格式；重用=泄露信号，整条会话链吊销） | 无（以 refresh token 为凭证） | 非幂等（轮换）；401=清档回匿名兜底 |
| 4 | POST `/api/sessions/revoke` | 吊销会话：带 Bearer 吊销 access，带 body refreshToken 吊销刷新腿 | 二选一凭证 | 幂等（已吊销容忍） |
| 5 | GET `/api/me/privacy` | 隐私状态查询；无 token 带 `jobId` 可查异步删除作业状态（响应剥除 userId） | `user:read`；jobId 匿名查询分支无 token 须 401 | 读 |
| 6 | GET `/api/me/export` | 全量导出 11 类对象族（清单唯一出处：17 篇 DAT-40）（FR-IDN-007） | `user:read` | 读；删除进行中 409、已删除 410 |
| 7 | DELETE `/api/me/data` | 全量删除（异步作业+横切阻断）：受理回 202 `{jobId,status}`，后台执行；删除期间 companion 写路径被挡；清理与保留期数值唯一出处：17 篇 DAT-42 | `user:read` | 异步幂等；重复提交 409（进行中）、已删 410 |
| 8 | GET `/api/me/character` | 人格互动规范读全量（对外只说互动偏好，不出现内部字段名） | `user:read` | 读 |
| 9 | PATCH `/api/me/character` | 改单槽位（field/value）；落互动账本 `character_setting` 行（入口=「http」） | `user:read` | 幂等（槽位覆盖）；非法槽位 400 |
| 10 | GET `/api/me/portrait` | 「球球懂我」画像读（Memobase 综合+本地 overlay 合成，与 realization prompt 同源） | `user:read` | 读 |
| 11 | PATCH `/api/me/portrait` | 修正画像条目（单条编辑 ≤120 rune；tombstone 立碑下一回合生效） | `user:read` | 幂等（覆盖写） |
| 12 | DELETE `/api/me/portrait` | 清除画像（本地立碑+尽力转发 Memobase） | `user:read` | 幂等 |
| 13 | GET `/api/me/threads` | 开放话题列表（state=open 且正文非空才返回） | `user:read` | 读 |
| 14 | GET `/api/me/moments` | 共同瞬间列表（≤50 条；向量路缺位=空列表静默） | `user:read` | 读 |
| 15 | DELETE `/api/me/moments/{momentId}` | 忘掉共同瞬间（物理删除） | `user:read` | 幂等；成功 200 `{ok}` |
| 16 | GET `/api/me/journal` | 球友手记列表（创建时间倒序） | `user:read` | 读 |
| 17 | GET `/api/me/journal/album` | 赛季记忆册（`?season=` 过滤，每场一条：比分+手记摘要+共同瞬间） | `user:read` | 读 |
| 18 | PUT `/api/me/journal/{entryId}/like` | 手记点赞/取消（body `{liked:bool}`） | `user:read` | 幂等（状态写）；条目不存在 404 |
| 19 | DELETE `/api/me/journal/{entryId}` | 忘掉手记（物理删，隐私生命周期） | `user:read` | 幂等；条目不存在 404 |
| 20 | GET `/api/matches/catalog` | 可进入场次目录（公开，匿名可读） | 无 | 读 |
| 21 | GET `/health` | 健康检查 | 无 | 读 |

#### 2.4.1 多入口一状态（用户可感数据的入口映射）

同一份用户数据常有 HTTP 与 WS 两个入口，状态唯一、入口并行——这是「三入口一状态」纪律（character-settings change 立法）：

| 数据 | HTTP 入口 | WS 入口 | 真源 |
| --- | --- | --- | --- |
| 人格互动规范 | GET/PATCH `/api/me/character` | `set_character` → `character_ack`（入口标记 ws） | character_settings 表（迁移 048）+互动账本 `character_setting` 行双落 |
| 话痨档 | （设置页本地保存，随 `user_speech` 携带 tier） | `set_talkativeness` → `talkativeness_ack` | 偏好存储 + 连接态；`session_opened` 时恢复持久化值（ack 口径见 §7.2 ⚖️B12） |
| 画像 | GET/PATCH/DELETE `/api/me/portrait` | — | Memobase 综合+本地 overlay（迁移 041 立碑）；与 realization prompt 同源 |
| 共同瞬间 | GET `/api/me/moments`、DELETE `…/{momentId}` | — | memory.Queue（向量路缺位=空列表静默） |
| 手记与赛季册 | GET `/api/me/journal(/album)`、PUT `…/like`、DELETE `…/{entryId}` | — | journal_entries 表（迁移 056） |

入口纪律：任何入口的写都必须落到同一份状态并留下同一形状的账本/审计痕迹；新入口不允许自建旁路存储。

### 2.5 非注册表面（不走三表，登记备查）

| 表面 | 形态 | 说明 |
| --- | --- | --- |
| `/mcp` | MCP over HTTP | 只读账本 4 工具：`match.read_snapshot`、`schedule.read_today`、`match.search_events`、`match.get_player_timeline`（companion 注册表 `ServedOverMCP` 标记单一源，一致性测试锁名）；匿名 401、缺 scope 403（§1.2） |
| `/ws/match/` | WebSocket | §3 全节；子树 `/ws/match/{matchId}` |
| `/ws/ops` | WebSocket | §3.7 |
| `/grafana/` | 同源反代 | 配置 `QIUQIU_GRAFANA_URL` 时反代上游；未配置时观测页渲染部署指引占位 |
| `/console`、`/console/` | 静态 SPA | `/console` 308 补斜杠；hash 路由 |
| `/` | 静态托管 | 客户端 web 构建产物（app.html 308 到 `/`） |
| `/live2d-assets/`、`/live2d.html` | 静态 | Live2D 资源带缓存头 |
| dev 页（`test-expressions.html`、`director-prototype.html`） | 静态 | 仅非 production 环境注册 |

### 2.6 错误码约定

| 状态码 | 语义 | 现网落点 |
| --- | --- | --- |
| 201 Created | 资源签发成功 | `POST /api/sessions/anonymous` |
| 400 Bad Request | 校验失败：invalid json、`userId` 缺失（interaction）、非法游标、非法设置槽位、`liked` 缺失、重名运营员、识别会话超限/坏分片（经 transcript_error 走 WS 面） | §2.1-2.4 对应行 |
| 405 Method Not Allowed | **已知路径+错误方法**在 ServeMux method-scoped pattern 下按标准回 405（带 `Allow` 头列出支持动词）——console/match/knowledge 三表统一（router.go 有意行为差异，旧「错误方法 404」记录作废）；用户面手写分支同语义（threads/moments/portrait/character 错方法 405+Allow 头） | 三表全部路径；用户面族 |
| 404 Not Found | 未知路径（无 pattern 匹配）；尾部斜杠不再容忍（旧 `strings.Trim` 偶然容忍作废）；用户面条目不存在（journal like/delete 404、privacy jobId 404） | 全面 |
| 409 Conflict | 条件冲突族：时钟 PATCH 版本不符；`DELETE /api/me/data` 删除进行中重复提交；export 于删除进行中；被冻结 deviceId（删除 pending/failed）请求匿名会话；tts-supply PATCH 本地腿健康门控未过（带原因） | §2.1-2.4 对应行 |
| 410 Gone | 删除语义终态：数据已删除后再 export/再查 410（`privacy.ErrDataDeleted`） | `/api/me/export`、privacy 状态族 |
| 202 Accepted | 异步删除受理（`{jobId,status}`，后台 ProcessDeletion） | `DELETE /api/me/data` |
| 401 Unauthorized | 凭证缺失/无效/身份不符；**`/mcp` 匿名一律 401**（刻意不继承 legacy APP_TOKEN 与开发旁路） | 用户面、控制面、MCP |
| 403 Forbidden | 持有效凭证但缺 scope（auditor 写请求、用户会话连 `/ws/ops`、MCP 缺只读 scope）；WS Origin 校验不过 | 全面 |
| 429 Too Many Requests | 每 IP WS 连接超上限（5 条） | `/ws/match` 握手 |
| 501 Not Implemented | 能力缺持久化存储的诚实降级（operators 管理、knowledge 条目、tts-supply 未接线） | §2.1/§2.3 对应行 |

### 2.7 运营写幂等与审计机制（控制面写端点共用）

控制面全部写端点（除两条草稿制端点的草稿步骤）经同一条 `executeOperatorWrite` 幂等包裹（`operator_write_api.go`），机制四步：

| 步 | 机制 | 合同 |
| --- | --- | --- |
| 1 | 幂等键受理 | 请求头 `Idempotency-Key` 定位 outbox（operator_idempotency_outbox，迁移 017）；同键且同请求体的重放直接返回原响应，不产生二次副作用 |
| 2 | 业务执行 | handler 内业务操作执行一次；失败经 `operatorError` 映射为稳定状态码（400 校验/409 条件冲突/501 能力缺位） |
| 3 | 结果记账 | 成功响应体写入 outbox 供重放；outbox 行与业务写在同一事务语义下提交 |
| 4 | 审计留痕 | `appendAudit`：3 秒独立预算写 `operator_audit` 表（操作人 `operator:<name>`、动作、object、结果）；审计失败仅记日志——不回滚业务、不拖慢响应 |

草稿制例外：`POST …/drafts/voice` 生成草稿、`POST …/drafts/voice/publish` 发布——草稿步骤本身不幂等生效（每次生成新草稿），发布步骤走幂等包裹；这是控制面唯一草稿族（FR-OPS-005，旧「18 类运营行为均只生成草稿」表述作废）。

审计纪律：每笔写操作实名归因；事件与审计行携带 operator subject（`operatorClaims` 在 withScope 放行后重新解析，两步之间令牌被吊销的窄窗口按 401 回）。

---

## 3. WS 协议全集

### 3.1 /ws/match/{matchId}：连接生命周期与保护数字

生命周期四相（upgrade → identify-and-subscribe → message-pump → teardown），断线重连=新连接重走前两相：

| 相 | 发生什么 | 合同 |
| --- | --- | --- |
| 升级握手 | Origin 校验、凭据认证（子协议）、每 IP 连接数检查 | 任一步不过连接不建立（403/401/429） |
| 身份识别与订阅 | `identify` 声明身份；服务端先推 `match_snapshot`、随后 `match_clock`，再订阅比赛流 | **首条下行次序固定：快照→时钟，先于一切事件与话轮** |
| 消息泵 | 比赛事件、时钟轮播（每条时钟推送后紧跟新快照）、对话回复、语音转写、控制回执并行流动 | §3.2/§3.3 全集 |
| 收尾 | `session_closed` 或心跳看守判死；按同一套顺序释放会话资源 | 迟到结果不再投递为新呈现 |

连接保护数字（全为现网实现值）：

| 参数 | 值 | 出处 |
| --- | --- | --- |
| Origin 校验不过 | 403 拒绝 | ws handler |
| 每 IP 并发连接上限 | 5，超出 429 | config.go |
| 单帧读上限 | 1MB（约 15 秒 16kHz 单声道语音），超限断连 | config.go |
| 读写超时 | 60s / 10s | config.go |
| 服务端协议层 ping | 30s | ws/handler.go |
| 客户端应用层心跳 | ping 25s → pong | websocket_service.dart |
| 会话看守 | 每 30s 重验凭据；过期/吊销回 `auth_error`（reason: session expired or revoked）并断开 | watchconnection.go serveHeartbeat |
| WS 凭证载体 | 子协议 `qiuqiu-auth.<base64url(token)>` | handler.go / websocket_service.dart |

身份一致性：`identify`/`session_opened`/`session_closed`/`first_meeting`/`user_speech`/`asr_start` 携带的 `userId` 与会话凭据不一致即回 `auth_error`（reason: 身份不符）并终止；匿名连接在需要身份的消息上回 `voice_status{failed, reason:"identity required"}` 或拒绝执行。

### 3.1.1 客户端重连阶梯（websocket_service.dart 现网实现值）

| 项 | 值 | 合同 |
| --- | --- | --- |
| 自动重连上限 | 5 次，超出进入 failed 终态（界面给出手动重进入口） | `_maxReconnectAttempts = 5` |
| 退避节奏 | `2^(n-1)` 秒，钳位 1–8s | 指数退避不风暴 |
| 令牌先行 | 重连前先持存量刷新令牌换新 access；刷新 401 即清档回匿名（匿名默认形态兜底） | §2.4 第 3 行语义的客户端侧 |
| 代际守卫 | 连接代际计数防旧连接事件串台：旧代际的迟到消息整代丢弃 | 重连竞态不互相污染 |
| 未连接不发 | 断线期 user_speech 不上送，整段音频进本地补发队列（≤4 条 FIFO，§3.5） | 诚实丢弃不无限积压 |
| 手动重连 | 界面手动重连立即执行（不受退避等待约束） | `beginManualReconnect` |
| 本地合成事件 | 重连期 UI 状态由客户端本地合成 `reconnecting` 事件驱动——**不上线**（§3.4 注记） | 服务端零感知 |

### 3.1.2 媒体与字段格式约定

| 项 | 约定 |
| --- | --- |
| 音频编码 | PCM 16kHz / 单声道 / 16bit；`asr_chunk.audio` 为 base64；`sequence` 从 0 递增，乱序块拒绝 |
| 分片节奏 | 客户端约 1.5s 一片（0.25s 重叠）；整段识别上限 30s，超长块拒绝 |
| utteranceId | 一次语音输入全生命周期的串联键：`asr_start` 开、四件套共用、`transcript_*` 与 `turn_query`/`turn_result` 按它回 |
| signalId | 话轮级稳定键：客户端可携带，缺省服务端按 `turn_<userId>_<nanotime>` 生成；延迟锚点按它挂 |
| deliveryKey | 投递去重键，三类内容（事件/呈现/音频）各自携带；比赛事件格式 `事件ID:factRevision:factStatus` |
| timezone | `user_speech`/`asr_start` 由客户端 `withClientContext` 自动附加，服务端透传进会话（相对时间表达的解析依据） |
| traceId | 回合观测键：对话回复、voice_audio、播放回执、trace 查询端点共用；`/api/matches/{id}/traces/{traceId}` 即它的运营读面 |

### 3.2 上行全集：客户端 → 服务端 20 类（watchconnection.go 读循环逐 case）

行数 20 即对账锚点：读循环 case 数多一个或少一个都是破坏性变更，须同时改本篇与附录 C。



| # | type | 字段摘要 | 触发时机 | 服务端处理/体验含义 |
| --- | --- | --- | --- | --- |
| 1 | `ping` | — | 周期心跳（25s） | 回 `pong` |
| 2 | `identify` | userId | 连接建立后声明身份 | 与凭据比对；不符 `auth_error` 断开 |
| 3 | `set_talkativeness` | talkativeness（quiet/normal/active） | 用户改话痨档 | 即时持久化+回 `talkativeness_ack`；未知值回落 normal |
| 4 | `set_character` | field/value | 用户改人格互动规范（三入口一状态之 WS 入口） | 持久化+落 `character_setting` 账本行+回 `character_ack` |
| 5 | `user_activity` | state（speaking/idle；只判 speaking） | 用户说话状态变化 | speaking 时取消查询、打断主动发言排程 |
| 6 | `session_opened` | userId | 进入比赛页/重连成功 | 绑定会话、恢复持久化话痨档、会话观察、**按原 deliveryKey 回放未送达回复**、恢复待定观察与话题回合 |
| 7 | `session_closed` | userId/reason | 离开比赛页 | 释放会话资源并回执 |
| 8 | `first_meeting` | userId/nickname/favoriteTeam | 初见未完成时提交资料 | 安排确定性四变体问候（30s TTL、幂等静音门）；昵称截 20 rune、主队截 24 rune |
| 9 | `interrupt` | — | 用户主动打断 | 取消查询+打断排程；回 `interrupt`（expression=listening） |
| 10 | `user_speech` | text+audio 可双带、talkativeness、signalId、timezone（客户端自动附） | 提交一个完整话轮（文字或整段音频；**也是断线补发载体**） | 记延迟锚点 speech_received，进对话管线；无身份回 `voice_status{failed}` |
| 11 | `asr_start` | utteranceId/signalId/timezone/识别提示 | 一次流式识别开始（用户动作开启） | 开识别会话；每连接同时在识别会话上限 8，超限 `transcript_error` |
| 12 | `asr_chunk` | utteranceId/sequence/audio(base64 PCM) | 流式音频分片 | 按 sequence 重组；乱序/超长（30s 整段上限）拒绝；合法分片并行旁送气氛 sidecar（旁路失败静默） |
| 13 | `asr_finish` | utteranceId | 说完信号 | 结束识别，出终稿；延迟锚点 asr_finish |
| 14 | `asr_cancel` | utteranceId | 用户中途放弃 | 片段即刻停用（取消后不进识别） |
| 15 | `voice_playback` | traceId/state（六值枚举 §3.4） | 回合语音播放状态变化 | 推进投递七态+调度器 PlaybackChanged+关系投递观察 |
| 16 | `reply_displayed` | traceId | 文字回复已上屏 | 文字账本 AcknowledgeText，投递态推进 text_delivered |
| 17 | `playback_result` | deliveryKey/state（四终态枚举 §3.4）/reason | 播放终态实报 | 定位投递记录推进七态；迟到回执只记 `client_late`，不回改既有终态 |
| 18 | `duplex_event` | event（三事件 §3.4）/streak/transcript | 播放期抢话遥测（判定在客户端） | 只落结构化日志+客户端健康账本（正文不落，只记 rune 数）；不参与判定 |
| 19 | `lipsync_event` | reason | 嘴型退化遥测（客户端限频后的状态沿） | 只落结构化日志+健康账本（`lipsync_<reason>`）；不参与任何判定 |
| 20 | `turn_query` | utteranceId/text | 说完判定 model 插槽提前问（静默累计 600ms 起） | 独立协程转发 turn sidecar（预算 500ms、单连接在途上限 4、饱和即回 null）；结论以 `turn_result` 回同一 utteranceId；文本不入日志 |

### 3.3 下行全集：服务端 → 客户端 19 类

行数 19 即对账锚点；全集口径=生产发送点清点（测试构造的字面量不计入）。`voice_status`/`first_meeting_status` 由 `DeliveryStatus.Kind+"_status"` 动态拼名产生（§1.4 约定 1），新增状态回执 kind 即自动入列，须同步本篇。



| # | type | 关键字段 | 触发时机 | 体验含义 |
| --- | --- | --- | --- | --- |
| 1 | `match_snapshot` | data=公开快照 | 连接建立首条、时钟轮播后、每条事件后 | 界面整体校准的唯一权威；客户端不猜缺口 |
| 2 | `match_clock` | version/period/elapsed/running/anchorAt | 时钟更新 | 刷新比赛时间；后跟新快照，客户端无需轮询 |
| 3 | `match_event` | data+snapshot+`deliveryKey`（格式 `事件ID:factRevision:factStatus`） | 一条公开事实成立 | 呈现事件；客户端按 deliveryKey 去重、按修订号丢弃旧版 |
| 4 | `match_fact_retracted` | eventId/factId | 一条事实被撤销 | 撤下由该事实生成的回复与标记（含移除待播音频），不等其自然过期 |
| 5 | `event`（`event=qiuqiu_reply`） | text/traceId/source/deliveryKey/presentation/reason? | 对话回复就绪 | 文字呈现（字幕区）；source=`recovered_delivery`=重连回放、`first_meeting`=初见（TTL 30s） |
| 6 | `presentation` | 表演计划+eventId+deliveryKey+source+citation | 仅动作与表情计划（无正文） | 驱动 Live2D 表演 |
| 7 | `transcript_partial` | utteranceId/revision/text/provider | 流式识别中间结果 | 边说边上屏；旧 revision 不覆盖新 |
| 8 | `transcript_final` | 同上 | 识别终稿 | 字幕定稿并交对话层 |
| 9 | `transcript_error` | utteranceId/revision/reason/`recoverable` | 识别出错 | recoverable=true 不终止会话（本句可重说）；false 终止转写链 |
| 10 | `voice_status` | state（三态 §3.4）+reason?/traceId? | 语音链路状态变化 | text_fallback=切文字继续；tts_fallback=字幕兜底；failed=本句失败说明 |
| 11 | `first_meeting_status` | state（delivered/skipped）+reason?/traceId? | 初见问候投递终态 | skipped 时 reason=`first_meeting_already_delivered`（幂等静音门）；与客户端收口键 `first_meeting_completed` 联动 |
| 12 | `talkativeness_ack` | tier/persisted | `set_talkativeness` 处理完 | 服务端回执（消费口径见 §7.2 ⚖️B12） |
| 13 | `character_ack` | field/value/persisted | `set_character` 处理完 | 设置写回确认，与 HTTP PATCH /api/me/character 三入口一状态 |
| 14 | `turn_result` | utteranceId/isComplete（true/false/**null**）/probability? | turn_query 的转发结论 | null=未配置/超时/熔断/4xx——客户端插槽下探静默档，服务端零特殊分支 |
| 15 | `voice_audio` | 文本元数据帧（mime/traceId/eventId/deliveryKey/source/byteLength）+二进制音频帧**成对** | 回合语音投递（整段）；backchannel 变体无 traceId/eventId、带「（小声）」text 微标注 | 字幕与声音同步播放的前提；按 deliveryKey 去重 |
| 16 | `interrupt`（下行回执） | expression=listening | `interrupt` 上行处理完 | 球球转聆听表演的确认 |
| 17 | `pong` | — | `ping` 应答 | 心跳保活 |
| 18 | `auth_error` | reason（身份不符 / identity required / session expired or revoked） | 凭据不符/过期/吊销（7 处发送点） | 客户端无专门分支，走通用重连 |
| 19 | `session_closed`（下行回执） | reason | `session_closed` 上行处理完 | 本场结束确认 |

> **`welcome` 注记（反幻影）**：`/ws/match` **从不发送** `welcome`——首条下行是 `match_snapshot`。现网唯一在用的 `welcome` 是 `/ws/ops` 首条下行（变体字段 `stream:"ops"`，ops_stream.go）；客户端对 welcome 零处理分支。v2 附录F 曾把 welcome 列为语音连接首条下行，系幻影声明，v3 全库清除（章程 F9）。

### 3.4 回执枚举与状态词典（全库唯一口径，与 PRD 语音域词典同源）

| 词典 | 全集 | 载体/备注 |
| --- | --- | --- |
| **voice_playback 六值**（上行） | `started` / `ended` / `completed` / `interrupted` / `skipped` / `blocked` | WS 上行；**blocked 为合法别名**，双端映射到 skipped（客户端 playbackResultState + 服务端 deliveryStateForPlayback 两处）；completed 为 ended 别名；started 在本地存在但不作为终态上报 |
| **playback_result 四终态**（上行） | `completed` / `interrupted` / `skipped` / `failed`（**无 started**） | WS 上行终态实报；迟到回执只记 `client_late` 不回改终态 |
| **voice_status 三态**（下行） | `failed` / `text_fallback` / `tts_fallback` | 下行状态通报；reason/traceId 可选随附 |
| first_meeting_status 两态（下行） | `delivered` / `skipped` | 初见问候投递回执 |
| turn_result.isComplete 三值（下行） | `true` / `false` / `null` | null=未配置/超时/熔断/4xx，语义=插槽下探 |
| duplex_event 三事件（上行） | `self_interrupt_suspected` / `duplex_degraded` / `duplex_recovered` | 客户端健康遥测 |
| 客户端健康事件 6 种 | duplex 三事件 + `lipsync_vendor_unavailable` / `lipsync_decode_failed` / `lipsync_jitter_fallback` | `/api/console/client-health` 与结构化日志的 kind 全集 |
| 投递七态（账本） | `planned → text_delivered / audio_started → completed / interrupted / skipped / failed` | delivery_ledger；两条回执上行共同推进 |
| 话痨三档 | quiet / normal / active（默认标准档=normal；未知值回落 normal） | set_talkativeness / talkativeness_ack 的 tier 值 |
| FactStatus 五态 | provisional / confirmed / revoked / conflict / reconciled | deliveryKey 第三段与 match_event 携带的 factStatus |

> **`reconnecting` 注记（反幻影）**：`reconnecting` **不是 wire 消息**——它是客户端 websocket_service.dart 本地合成进消息流供 UI 显示的事件（「正在回到比赛现场…」），服务端从不收发。协议清单不收录它；客户端连接状态五态（connecting/connected/reconnecting/disconnected/failed）与服务器无关，属客户端本地状态机。

### 3.4.1 回执→投递七态推进映射（服务端记账口径）

两条播放回执上行共同推进 delivery_ledger 的投递七态；映射在客户端 playbackResultState 与服务端 deliveryStateForPlayback 两处定义，逐值一致：

| 上行 | 值 | 推进到 |
| --- | --- | --- |
| `voice_playback` | started | audio_started |
| `voice_playback` | ended / completed（别名） | completed |
| `voice_playback` | interrupted | interrupted |
| `voice_playback` | skipped / blocked（别名） | skipped |
| `voice_playback` | 其他未知值 | failed |
| `playback_result` | completed / interrupted / skipped / failed | 同名终态（实时实报 source=`client`；迟到回执 source=`client_late` 只追加账本、不回改既有终态） |
| `reply_displayed` | — | text_delivered（文字账本 AcknowledgeText） |

「被拦截（blocked）计入跳过（skipped）」的语义经双端两处映射证实为真；但「跳过」**不是客户端本地播放状态名**——本地枚举是 started/ended|completed/interrupted/blocked/failed，六值 wire 枚举是本地状态经映射后的合同面。

### 3.4.2 协议与客户端本地状态机的边界

| 状态机 | 归属 | 与协议的关系 |
| --- | --- | --- |
| 会话相位 11 态（idle/welcoming/listening/userSpeaking/understanding/speaking/permissionDenied/offline/failed/reconnecting/recovered） | 客户端本地（MatchSessionPhase） | 由 wire 消息驱动迁移，但相位本身不上线；`interrupted` 不是常驻相位（处理后即回 listening/userSpeaking/understanding） |
| 连接状态 5 态（connecting/connected/reconnecting/disconnected/failed） | 客户端本地（SocketStatus） | 纯本地；`reconnecting` 合成事件由此产生（§3.4 注记） |
| 唤醒状态机 6 态（disabled/starting/armed/awakening/suspended/failure） | 客户端本地（wake_service） | 唤醒词命中后经与收音入口同一套上行进入话轮；状态机本身不上线 |

写法纪律：把上述任何本地状态写成「通道消息」或「服务端状态」均为幻影分类（v2 26 章「已打断常驻节点」即前车之鉴——打断后回到三相位之一，无独立常驻态）。

### 3.5 送达合同：不丢、不重、不越界

| 环节 | 现网做法 | 用户可感知结果 |
| --- | --- | --- |
| 客户端去重 | 反应、呈现、音频各自按 deliveryKey 去重（四类 Deduplicator，各容量 256）；事件级 LRU 512 | 重连或重复推送不会让同一句话弹两次 |
| 修订保序 | 每条事实带 factRevision，低于当前版本直接丢弃；deliveryKey 三段式绑修订与状态 | 比分更正后不被迟到旧数字「弹回」 |
| 服务端去重 | signalDeduper（TTL 15min、容量 4096）+ 投递账本按 deliveryKey 唯一约束 | 同一请求重复提交不产生第二次副作用 |
| 断线回放 | `session_opened` 后，已生成未送达的回复按原 deliveryKey 回放（只取 resolved） | 回来能看到「刚才该出现的内容」，不会双份 |
| 语音补发 | 断线漏掉的语音排入客户端补发队列：上限 4 条 FIFO，超限淘汰最旧；已取消绝不补发；串行发一条确认一条 | 最多补最近几句，不淹没刚回到比赛的人 |
| 主动回合时效 | critical TTL 45s / normal TTL 12s；初见问候 TTL 30s | 过期内容不再打扰，赛点信息留更久 |

### 3.6 /ws/ops：运营观测实时流（无正文流）

| 项 | 合同 |
| --- | --- |
| 事件源 | interaction ledger Append 成功后的旁路广播（observingLedger 装饰器）；喂流失败不影响账本；零订阅同样计入丢弃 |
| 首条下行 | `welcome`（变体字段 `stream:"ops"`）——全库唯一现役 welcome |
| 下行事件 kind 共 9 类 | `turn_planned` / `delivery` / `signal` / `fact_revision` / `media_delivery` / `playback_result` / `turn_stale` / `backchannel` / `character_setting` |

9 类 kind 各自的产生时机（interaction ledger 落账即广播）：

| kind | 何时产生 | 观测价值 |
| --- | --- | --- |
| `turn_planned` | 一个回合计划落账（自发/被触发） | 唯一携带 latencyMs 的 kind（从 trace 载荷提取） |
| `delivery` | 投递状态推进（文本/音频/终态） | 投递七态分布的实时面 |
| `signal` | 用户话轮信号落账 | 话轮节奏观测（无正文） |
| `fact_revision` | 事实修订（确认/撤销/调和/更正）落账 | 事实域人工干预痕迹 |
| `media_delivery` | 媒体记账事件（`media:<user>:<match>:<trace>:<state>`） | 音频投递健康 |
| `playback_result` | 播放终态实报落账（client/client_late 双源） | 播放期打断率面板数据源 |
| `turn_stale` | 回合过期作废（TTL 到期） | 过期丢弃量化 |
| `backchannel` | 伴随反应发出 | 微反应 24h 面板数据源 |
| `character_setting` | 用户改人格互动规范落账（HTTP 或 WS 入口） | 设置变更观测 |
| wire 字段 9 个 | type/kind/matchId/userId/traceId?/deliveryState?/playbackState?/latencyMs?/at；**正文字段在结构体层面不存在**（非「约定不填」，是物理没有）；latencyMs 仅 turn_planned 从 trace 载荷提取 |
| 鉴权 | 复用会话鉴权链升级：须 `operator:trace:read`（或 legacy 运营令牌/开发档匿名）；用户会话升级成功后立即拒绝断开（ClosePolicyViolation） |
| 队列与保活 | 服务端队列 256，饱和丢弃只计数（`Dropped()`，不做 Grafana 告警——诚实降级）；连接 ping 30s |
| 上行 | 不收任何业务消息（读循环仅维持连接；协议层 ping 保活） |
| 客户端（直播监听页） | 列表上限 200 行；断线重连退避 5s 起指数封顶 30s；暂停=入列冻结（暂停期事件丢弃不缓存）；按 traceId 一键回合回放（WhyDrawer） |

### 3.7 失败时的语言（协议失败路径 → 用户呈现）

每一个失败路径对应一句用户能懂的话；协议不暴露供应商细节、内部原因码或未确认赛果：

| 失败场景 | 协议表现 | 用户看到 |
| --- | --- | --- |
| 凭据无效或身份不符 | `auth_error`（带 reason），必要时断开 | 重新进入的入口，不是报错堆栈 |
| 麦克风权限被拒 | 不开始录音、不反复弹窗；相位 `permissionDenied`（本地） | 完整文字路径保留，说明如何继续 |
| 流式识别中断 | `transcript_error`（recoverable=true），句尾整段音频重试 | 提示转写中断、句尾自动重试 |
| 识别最终失败 | `voice_status{failed}`+reason，转写链终止 | 语音没听清的说明，文字输入照常 |
| 语音链路不可用 | `voice_status{text_fallback}` | 对话无缝切文字 |
| 合成失败 | `voice_status{tts_fallback}`+traceId | 看得到回应文字、听不到声音；故障不演成情绪事件 |
| 用户静音/系统拦截播放 | `playback_result{skipped}`（blocked 别名同映射） | 字幕照常，不重播不补偿 |
| 用户打断 | `interrupt` 下行回执（expression=listening）；服务端停止排程 | 立刻安静，无迟到内容 |
| 连接来源异常/超限 | 握手被拒（403/429） | 连接不建立；正常用户不受影响 |
| 会话过期/吊销 | 看守回 `auth_error` 并断开 | 通用重连阶梯接管（§3.1.1） |
| 本场结束 | 终态以快照与事件到达；拒绝一切迟到内容 | 结束状态保持，无告别挽留 |

最小资料原则贯穿这些消息：接口只收集完成当次动作所需的最小字段；关联键（`deliveryKey`/`utteranceId`/`traceId`）服务于顺序与审计，不携带私人原文或原始音频（正文只在 `event`/`voice_audio`/`transcript_*` 三族按需到达用户本人连接）。

### 3.8 一轮语音话轮的消息时序（走查）

一条连接上「用户开口→听到回应→回执收尾」的完整消息序列（正常路径 8 步 + 异常支线）：

| 步 | 方向 | 消息 | 说明 |
| --- | --- | --- | --- |
| 1 | C→S | `asr_start`（utteranceId） | 用户动作开启采集；服务端开识别会话（≤8 路/连接） |
| 2 | C→S / S→C | `asr_chunk` ×N ⇄ `transcript_partial` ×N | 边说边上屏；识别会话并行旁送气氛 sidecar（默认关） |
| 3 | C→S / S→C | `turn_query` → `turn_result` | 静默累计 600ms 起提前问；null 即下探静默档 |
| 4 | C→S / S→C | `asr_finish` → `transcript_final` | 终稿带 revision 交对话层 |
| 5 | S→C | `event`（qiuqiu_reply）+ `presentation` + `voice_audio`（双帧） | 文字、表演、语音同窗投递；各带 deliveryKey |
| 6 | C→S | `voice_playback{started}` | 投递态推进 audio_started |
| 7 | C→S | `duplex_event`（如有抢话）或 `interrupt` | 播放期抢话遥测/打断；打断即本机静音+服务端停排程 |
| 8 | C→S | `voice_playback{ended}` + `playback_result{completed}` + `reply_displayed` | 终态实报推进账本 completed / text_delivered |

异常支线（任一步可切入，全部落回文字）：

| 支线 | 消息序列 |
| --- | --- |
| 用户中途放弃 | `asr_cancel` → 片段即刻停用，无转写产出 |
| 识别失败 | `transcript_error`（recoverable 链）→ 句尾整段重试 → 仍失败 `voice_status{failed}` → 文字输入照常 |
| 合成失败 | `voice_status{tts_fallback}` → 字幕兜底，回合照常完成 |
| 事实撤销 | `match_fact_retracted` → 撤下由该事实生成的回复与待播音频 |
| 断线 | 客户端本地合成 `reconnecting`（不上线）→ 重连阶梯（§3.1.1）→ `session_opened` 回放未送达（原 deliveryKey）→ 补发队列 ≤4 条 FIFO |

### 3.9 降级与异常（散点语义归并视图）

本节把散落在 §2/§3 的降级与异常处置归并为一处索引；每行只收拢既有合同的语义与指针，不另立新规（合同本体以所引节为准）：

| 异常面 | 现网处置 | 合同锚 |
| --- | --- | --- |
| WS 断线重连 | 客户端自动重连上限 5 次（超出进 failed 终态，界面给手动重进入口）；指数退避 `2^(n-1)` 秒钳位 1–8s；重连前令牌先行（刷新 401 清档回匿名）；代际守卫防旧连接事件串台；断线期不发送、本地补发队列 ≤4 条 FIFO | §3.1.1 |
| 半开连接处置 | 服务端协议层 ping 30s + 看守每 30s 重验凭据，过期/吊销回 `auth_error` 断开；读写超时 60s/10s、单帧 1MB 超限断连——停发 30 秒内判死触发重连，无静默僵尸连接 | §3.1 |
| 鉴权失败分流 | 401 与 403 处置分工是合同一部分：C 端 401=清档回匿名兜底、运营台 401=全局登出回登录页、403=页内提示（scope 缺失），不得合并为统一错误页 | §1.2、§2.6 |
| HTTP 错误码语义 | 已知路径错方法 405（带 Allow）；尾部斜杠 404；条件冲突族 409（时钟版本/删除进行中/冻结 deviceId/健康门控）；删除终态 410；异步受理 202；能力缺持久化存储诚实降级 501（operators/knowledge/tts-supply） | §2.6 |
| WS 握手拒绝 | Origin 不过 403、每 IP 超 5 连接 429、凭据无效 401——连接不建立，正常用户不受影响 | §3.1 |
| 送达异常 | 同 deliveryKey 重推不双份；低 revision 丢弃；断线重连回放未送达回复；迟到回执只记 `client_late` 不回改终态；主动回合 TTL 到期不投递 | §3.5、§3.4.1 |
| 失败的用户语言 | 每一个失败路径对应一句用户能懂的话（转写中断/语音不可用/合成失败/打断/过期），协议不暴露供应商细节与内部原因码 | §3.7 |

归并纪律：新增异常/降级路径先落对应节合同，再回写本表索引；两处不一致即为缺陷态。

---

## 4. 延迟预算表（体验侧摘要）

本表是协议侧的体验承诺摘要；测量方法、分段口径与告警运维细则的唯一点是 **20 篇（语音链路 runbook）与 21 篇（SLO）**，本篇不复制细则。

| 项 | 值 | 口径 |
| --- | --- | --- |
| 首响（用户说完→文字+整段语音到齐） | **3-7 秒为接受现实** | 整段投递纪律（2026-06-01 韵律盲测 80% 嫌弃后裁决）；「首句可播」「首响 1.2-1.5s」类表述一律违规 |
| 端到端延迟发布闸门 | P95 < 3000ms | `backend/cmd/latency-test` 五场景（进球/黄牌/半场/射门/开场）工具测得；Grafana 判值同口径 |
| 告警判断值 | 语音延迟 P95>3000ms、首响 P95>1800ms、供应商错误>3 条/30m、poller 停摆>600s | rules.yml 现行评估器参数（历史宽松值 5000/2500/5/900 已废） |
| turn sidecar 判定延迟 | p50 24.6ms / p90 34.1ms；转发预算 500ms | LiveKit EOU q8 本地推理；超预算回 isComplete:null 下探静默档 |
| 说完判定节奏 | 提前问 600ms / 重发节流 300ms / 在途超时 450ms / 静默过渡档 1400ms（自适应 +25% 封顶 2000ms） | 三插槽链 model→completenessRule（预留）→silence |
| 主动回合 TTL | critical 45s / normal 12s；初见问候 30s | 过期不投递 |
| 心跳与判死 | 服务端 ping 30s / 客户端 25s / 看守重验 30s；读写超时 60s/10s | 停发 30 秒内判死触发重连 |

### 4.1 延迟分解锚点（观测面，体验侧摘要的测量基础）

一轮语音话轮的等待被拆为六个锚点逐段测量（`logVoiceLatency` 落结构化日志，trace 诞生后经 attach 键级合并进 `agent_traces.voice.latencyStages`）：

| 阶段键 | 起点→终点 | 含义 |
| --- | --- | --- |
| `speech_received` | 用户开口→服务端收到 `user_speech`/`asr_start` | 采集与上送耗时（含 VAD 判定） |
| `asr_finish` | `asr_start`→`asr_finish` 到达 | 用户说话时长（客户端口径） |
| `asr_final` | 会话开始→识别终稿 | 流式识别耗时 |
| `turn_decided` | 话轮开始→说完判定出结论 | 三插槽判定耗时（model/silence 分源记录） |
| `audio_delivered` | 话轮开始→voice_audio 投递 | 生成+合成+投递累计 |
| `text_delivered` | 话轮开始→event(qiuqiu_reply) 投递 | 文字先行的到达点 |
| `first_audio` | （TTS 元数据随附）合成首字节 | 首响告警（1800ms 规则）的判值来源 |

分段均值进 Grafana「延迟分段均值」面板；单段劣化先于总延迟劣化被看见。测量纪律与工具细则见 20 篇。

---

## 5. 兼容与演进

| # | 规则 | 内容 |
| --- | --- | --- |
| 1 | 字段增删 | 新增字段对旧客户端透明（忽略未知字段）；删除或改义既有字段、收缩 §3.4 枚举 = 破坏性变更，必须走裁决与全端联测，不允许随常规 change 夹带 |
| 2 | 消息新增 | 新 WS 消息类型先入本篇 §3.2/§3.3 表（含触发时机与回执关系），再写代码；旧客户端对未知消息中性处理（§1.4 约定 2） |
| 3 | 端点新增 | 唯一路径=三表加行（§1.4 约定 4）；带 scope 的运营端点禁止表外散挂；用户面族新端点挂 `/api/me/` 等既有前缀，不计入控制面 51 数 |
| 4 | 枚举别名 | `blocked`（→skipped）、`completed`（→ended）这类兼容别名一经发布不删；新枚举值优先走「新增值」而非「改义旧值」 |
| 5 | openspec 联动 | 行为变更的 change 提案引用本篇「18 篇+锚点」；本篇修订记录回写 change 名；路由表改动由 golden 测试强制与本篇对账（§6） |
| 6 | wire 示例归属 | 请求/响应与消息的 JSON 示例一律落附录 C（协议字典与示例集），本篇只立合同与枚举；两处互链，示例不入本篇防重复漂移 |
| 7 | 演进方向 | 统一封装层+显式可见性四态仍是远期方向；现网选择更直接的消息面（每条消息自带语义）。新能力落地前，§1.4 兼容底线不变 |
| 8 | 机器可读形态（规划中（未实现，P2）；2026-06-09 对标增补） | 协议字典未来提供 **AsyncAPI 形态机器可读规范**：以附录 C（协议字典与示例集）为唯一源生成 AsyncAPI YAML/JSON Schema，CI 校验字典与 schema 同源（人读字典与机器可读件禁止各自演化）。选型随埋点 ⚖️B22 裁决同批进行（对称先例＝31 篇 §7.1 schema 注册表「唯一 schema 源+生成 JSON Schema」同款形态）；落地前本篇与附录 C 的人读字典＋「行数即锚点」对账（§6.1/§7.4、附录 C §1 约定 4）仍为唯一对账纪律 |

---

## 6. 验收与评测挂钩

### 6.1 路由表核对法（本篇独有验收形态）

1. **golden 锁表**：`console_golden_test.go` 锁定三表全部 (method, path, scope) 组合——端点增删必红；本篇 §2.1-2.3 表与代码 diff 为零是 FR-OPS-004 的验收标准。
2. **引用先核对**：任何文档引用端点前按本篇路由表核对；引用表中不存在的端点违规（FR-OPS-004 反例）。
3. **计数口径**：控制面恒等式 51 = 19（console）+ 28（match）+ 4（knowledge）；用户面族（§2.4）不计入。

### 6.2 E2E 与测试清单

| 挂钩 | 覆盖 |
| --- | --- |
| `console_golden_test.go` | 三表组合+scope 列锁定（路由表核对测试） |
| operator-write 幂等单测 | 同 Idempotency-Key 重放只生效一次；auditor 写 403 |
| `runtime-e2e.mjs` | 真实 WS 话轮（连接→快照时钟→user_speech→回合投递） |
| session-isolation E2E | 匿名会话语义、双设备双连接隔离、登录四路归并 |
| 登录 E2E / client 会话服务测试族 | 勾选门、退出链、刷新重用整链吊销 |
| `console-pages.spec.mjs`（Playwright） | 运营台页面与登录页 |
| operator-control evals | 导播草稿请求形状逐字断言（parity 基线） |
| voice_test.go / 连接生命周期测试 | 四相次序（快照先行）、心跳看守、1MB/429/403 保护数字、first-meeting 展示 |
| 协议枚举对账 | 上行 20/下行 19 与附录 C（协议字典）对账；三套回执枚举全集以本篇 §3.4 为准 |

### 6.3 本篇级验收断言

按四个面分组的断言清单（每条即一个可执行检查）：

| 面 | 断言 |
| --- | --- |
| HTTP 鉴权面 | 匿名调 `/api/console/overview` 得 401；auditor 写请求得 403；匿名调 `/mcp` 得 401（不继承 legacy）；持 `user:read` 调 `/api/me/export` 成功 |
| HTTP 错误面 | 已知路径错方法得 405+Allow 头；尾部斜杠得 404；时钟 PATCH 版本不符得 409；删除进行中重复 `DELETE /api/me/data` 得 409、已删后 export 得 410；无 DATABASE_URL 时 operators/knowledge 得 501 |
| HTTP 幂等面 | 同 Idempotency-Key 重放写端点只生效一次且返回原响应；`POST /api/sessions/anonymous` 同 deviceId 归还同一 `usr_`；被冻结 deviceId 得 409 且客户端重置后可复现 |
| WS 次序面 | 握手成功后首条消息必为 `match_snapshot`、第二条为 `match_clock`（welcome 反幻影断言同处验证）；时钟推送后紧跟新快照 |
| WS 枚举面 | `voice_status` 只出现三态值；`playback_result` 只出现四终态值（truncated 分支为客户端死代码，不入断言）；`voice_playback` 六值全接受且 blocked→skipped 双端一致；`turn_result.isComplete` 拔掉 sidecar 后恒为 null 且客户端静默档接管 |
| WS 送达面 | 同 deliveryKey 重推不双份呈现；低 revision 更新被丢弃；断线重连后未送达回复按原 deliveryKey 回放；补发队列 ≤4 条 FIFO 且已取消内容绝不补发 |
| /ws/ops 面 | 用户会话升级即被拒（ClosePolicyViolation）；wire 事件无正文字段（结构体级断言）；饱和只丢不阻塞账本主写 |
| 静态面 | `/console` 308 补斜杠；`/` 托管 web 产物（app.html 308 到 `/`）；live2d 静态资源带缓存头；dev 页仅非 production 注册 |
| 隐私面 | 导出恰为 11 类对象族；删除受理后 companion 写路径被挡；观测三面（client-health/ops 流/Grafana）无身份明文与正文 |

---

## 7. 设计说明（写法依据与裁决注记，非需求条款）

### 7.1 与旧 42 章（v2 档案）的差异全集

1. **用户面族整段转正**：旧 42 章自称「合同与索引」但漏记约 50 条——`/api/me` 前缀 0 命中、`/api/sessions/login` 整行缺席、journal 的 `/album` 与 `/{id}/like` 子资源、moments/threads/character/portrait 端点行全缺。本篇 §2.4 为其唯一转正落点。
2. **WS 全集补齐**：旧 42 章上行缺 9/20（session_opened/closed、first_meeting、user_activity、set_character、playback_result、turn_query、duplex_event、lipsync_event）、下行缺 6/19（turn_result、voice_audio、character_ack、first_meeting_status、session_closed 回执、auth_error）；三套回执枚举零记载。本篇 §3 为全集底稿。
3. **控制面从「引用委托」升为逐条表**：旧 42 章对 `/api/console/*` 只有引用委托句；本篇按对账底稿逐条转正 51 行，含 scope 与幂等性列。
4. **保留不推倒**：旧 42 章经对账逐句为真的部分（五合同原则、连接保护数字、送达合同表、失败语言表）语义保留，已分别吸收进 §1.1/§3.1/§3.5/§3.7；v2 卷5 55 章的三流顺序与全部数字骨架（保护数字/重连参数/三道去重/TTL/409 自愈）经逐项核验现势度最高，同样吸收进 §3.1/§3.5，未做无据改动。

### 7.4 对账方法与维护纪律

本篇与代码的同步不靠自觉，靠三条机械可查的纪律：

1. **路由表核对法**：本篇 §2.1-2.3 逐行来自三张声明式路由表；核对动作=`console_golden_test.go` 绿 + 本篇表与三表 diff 为零。任何一方单独改动即为失败态。
2. **枚举对账法**：本篇 §3.2/§3.3 行数（20/19）与 §3.4 枚举全集是验收断言本身；对账动作=读循环 case 数 + 生产发送点 `"type":` 字面量清点（测试构造不计）。协议消息的新增/删除必须同时落本篇与附录 C。
3. **反幻影三查**：交付前 grep 三项——`welcome`（只允许出现在 /ws/ops 语境）、`reconnecting` 作为通道消息的表述（只允许「客户端本地合成」语境）、`truncated` 作为现役枚举（禁）。命中即改，评审复核时作独立检查项（词汇自查表另按 00 篇 §7.2 执行）。

### 7.2 F10：talkativeness_ack 口径（⚖️B12 注记）

后端对 `set_talkativeness` 即时持久化并下发 `talkativeness_ack`（tier+persisted）；**客户端当前不消费该回执**——档位真源是本地保存回调，服务端持久化值在 `session_opened` 时恢复。v2 文档「客户端以 ack 为真源」与实现不符（章程幻影 F10）。本篇按裁决默认写法记录：回执保留在合同中（它确实现网下发），消费方标注为「客户端未消费」；是否改为客户端消费 ack、或从合同移除，挂 ⚖️B12 待裁决（登记见 37 篇）。

### 7.3 其他写法注记

1. **scope 计数**：PRD FR-OPS-003「3 scope」为口径概述；代码实有 4 个 scope 常量（`operator:fact:correct` 自列，事件更正端点专用）+ auditor 只读角色。本篇按代码 4 scope 写。
2. **`truncated` 死分支**：句粒度拆除后服务端不再产生 voice_status 的 truncated 态；客户端残留分支为死代码（代码卫生，回写时勿入枚举）。
3. **对账的 S→C 枚举 21 行 vs 本篇 19**：对账中 `user_speech`/`asr_start`/`asr_finish` 三个 S→C 字面量仅存在于测试构造，非生产发送点；本篇按生产发送点口径收 19 类。
4. **旧附录F 的处置**：按对账精确缺单并入本篇 §3；附录 C（协议字典）承接 wire 示例集职能，两处互链不重复。
5. **「写端点 18 个」口径**：概览层口径（console 写 + match 写 + knowledge 写的运营写总数），草稿制仅语音草稿一族 2 端点（F12 处置）；本篇逐行表为唯一精确口径，两处不冲突。

---

## 修订记录

| 版本 | 日期 | 依据 | 变更摘要 |
| --- | --- | --- | --- |
| v3.0 | 2026-06-08 | PRD 第四章 F10/F9（FR-OPS-001~012、FR-IDN-001~005/007/008/015）；迁移源 v2 档案 42 章+附录F；ADR-0008/0010/0020/0021/0022 | 初版。协议总纲（双面/鉴权三态/兼容约定五条）；HTTP 全集——控制面 51 端点三表逐条（方法/路径/用途/scope/幂等）+用户面族 21 行+非注册表面+错误码约定九行；WS 全集——/ws/match 生命周期与保护数字、上行 20 类逐条、下行 19 类逐条（welcome/reconnecting 双反幻影注记）、回执枚举与状态词典九行、送达合同六行、/ws/ops 全合同；延迟预算表（指针 20 篇）；兼容与演进六条；验收（路由表核对法+E2E 清单）；设计说明（与旧 42 章差异四条、F10 talkativeness_ack ⚖️B12 注记、scope 计数等注记） |
| v3.0.1 | 2026-06-09 | 复核修复（增设降级与异常节） | 增设 §3.9「降级与异常」归并视图：WS 断连重连阶梯、半开连接处置、鉴权失败分流、HTTP 错误码语义、握手拒绝、送达异常、失败用户语言七散点收拢为索引表；导读同步 |
| v3.0.2 | 2026-06-09 | 对标补齐（benchmark-r4 B4；gap-system.md） | §5 兼容与演进表新增约定 8「机器可读形态」：协议字典未来提供 AsyncAPI 形态机器可读规范（以附录 C 为唯一源生成，CI 校验字典与 schema 同源；选型随埋点 ⚖️B22 裁决同批，对称先例 31 篇 §7.1），标规划中（未实现，P2）；落地前人读字典＋行数对账仍为唯一纪律 |
| v3.0.3 | 2026-06-09 | 终审收口（终审报告 P2：新增节题标注统一） | §5 约定 8 行「2026-06-09 对标补齐」改「2026-06-09 对标增补」（全库新增节标注统一；与附录 C 约定 7 同批） |
