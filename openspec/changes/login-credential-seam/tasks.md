# Tasks: Login Credential Seam

- [ ] 1.1 migration 049 `user_credentials`（identifier 唯一索引、updated_at 触发器不必——显式写入）。
- [ ] 1.2 `internal/auth/credentials.go`：argon2id 哈希/校验（PHC 串、恒定时间比较）+ identifier 归一/校验。
- [ ] 1.3 `Store` 增 `CreateCredential`/`FindCredential`：MemoryStore + PostgresStore（含 integration 测试，postgres 缺席自跳过）。
- [ ] 1.4 `Manager.Login`（注册/切换/密码错/竞态归并）+ `Refresh` 重用检测升级为吊销整条会话。
- [ ] 1.5 身份单测 8 例（原地×3、切换×2、登出×2、重用吊销×1）。
- [ ] 1.6 `POST /api/sessions/login` 端点 + `session_api_test.go` 补例（Bearer 缺失 401、格式 400、成功 200）。
- [ ] 1.7 客户端 `SessionService.login/logout` + 单测。
- [ ] 1.8 登录页 `login_screen.dart` + 设置页「账号与同步」入口与状态行；widget 测试。视觉占位=现有主题（等背景图，如实留尾）。
- [ ] 1.9 CONTEXT.md「正式身份」词条 + 「匿名身份」修订（设备标识=默认凭据）。
- [ ] 1.10 验证：全量 go test + flutter test + 存量 evals 不漂移；逐项 commit。

## Sequencing

架构评审 2026-09-23 共识 A 项（[[ADR-0020]]）；不依赖其他 change。B（动作回归）可并行。
