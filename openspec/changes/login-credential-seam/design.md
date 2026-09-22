# Design: Login Credential Seam

依据 ADR-0020（docs/adr/0020-end-user-login-credential-seam.md）。这里只记录 ADR 之下的实施形状。

## 语义

- **登录即注册（双合一）**：`POST /api/sessions/login` 带 Bearer 当前（匿名）会话 + `{identifier, password}`。
  - identifier 未占用 → `hash(password)` 写入 `user_credentials(user_id=当前 usr_)`，为当前 usr_ 签发新会话（原地升级，userId 不变）。
  - identifier 已占用 → argon2id 校验：通过则为**该账号**的 usr_ 签发会话（设备仍是当前设备；userId 可能变化=切换，Q5）；失败 401。
  - 并发竞态：插入撞唯一约束 → 重查按已占用处理。
- **identifier 校验**：邮箱格式（含 @、长度 ≤254）+ 小写归一；密码 8–128 字符。不发信、不验证归属（ADR-0020 决定 4）。
- **会话签发**：复用现有 `session()` 路径，scopes=user:chat,user:read，DeviceID 取当前 claims（同设备登录/切换）。切换后旧会话**不**自动吊销（多设备允许同时在线，Q5）。
- **登出** = 现有 `POST /api/sessions/revoke`（吊销当前会话）→ 客户端清凭证 → 回匿名（deviceId 映射还在，归还同一 usr_，Q6：登出不失忆）。
- **重用检测升级**：`Manager.Refresh` 中 `Rotate` 返回 `ErrInvalidToken`（旧哈希不匹配=重放）时，吊销整条会话再返回 401。MemoryStore/PostgresStore 的 Rotate 已在该语义下返回 ErrInvalidToken。

## argon2id 参数

RFC 9106 推荐第二档（memory=64MB 无法在受限环境保证，取第一档偏稳）：time=1, memory=64MB 的低交互替代——**time=2, memory=32MiB, threads=1, keyLen=32**，盐 16B 随机；存 PHC 字符串格式（`$argon2id$v=19$m=32768,t=2,p=1$<salt>$<hash>`），校验用恒定时间比较（`subtle.ConstantTimeCompare`）。参数进哈希串，将来可无损调参。

## 存储接口

`auth.Store` 增两方法（Memory/Postgres 双实现 + conformance）：

```go
CreateCredential(ctx, Credential) error            // 唯一冲突返回 ErrCredentialExists
FindCredential(ctx, identifier string) (Credential, error) // ErrNotFound
```

`Credential{UserID, Identifier, PasswordHash, CreatedAt, UpdatedAt}`。不提供改密/解绑（Non-goal）。

## 端点

`POST /api/sessions/login`：
- 鉴权：`Authorization: Bearer <accessToken>`（经 `manager.Authenticate`；过期则客户端先 refresh，现有编排已保证）。
- 成功 200 `sessionResponse(session)`（与 anonymous/refresh 同形；客户端自行对比 userId 识别切换）。
- 错误：400（格式/长度）、401（密码错/会话无效）、409 不用（竞态归入已占用路径）。
- CORS/Cache 头沿 `handleSessionAPI` 现状。

## 客户端编排

- `SessionService.login(baseUrl, identifier, password)`：先 `ensureSession`（保证有效 Bearer）→ POST login → `_save` 新凭证返回。
- `SessionService.logout(baseUrl)`：revoke 当前 → `_clear()` → `_anonymous()` 重建（同 usr_）。
- 登录页 `login_screen.dart`：邮箱+密码+协议勾选（未勾选禁用按钮）+错误/加载态；入口=设置页「账号与同步」段（含登录状态行：未登录/已登录 identifier）。**视觉等用户背景图，先用现有主题色**（tasks 如实记录）。
- 切换账号后（userId 变化）：调用方重建会话依赖（WS 重连走现有 ensureSession 路径，portrait 下次拉取自然换人）。

## 身份 eval（8 例，Go 层）

原地升级 userId 不变×3（含登录后画像/订阅仍可读）、冲突切换 userId 换目标×2、登出回匿名同 usr_×2、refresh 重用→会话吊销×1。auth 包单测 + session_api HTTP 测试承载（身份域无需 Playwright）。
