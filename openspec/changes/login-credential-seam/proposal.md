# Login Credential Seam: 客户端登录凭证缝（邮箱+密码）

## Why

匿名身份的设备标识是唯一凭据——卸载/换机即失去全部共同记忆（ADR-0001 边界）。运营端早有密码登录+JWT（ADR-0010），客户端缺升级路径。ADR-0020 已定：镜像运营端模式（邮箱+密码），绑定不换 ID，匿名保持默认形态；短信/微信/Apple/Passkey 全部按资格包分期表后置，不在本 change。

## What Changes

- migration 049 `user_credentials(user_id, identifier UNIQUE, argon2id 哈希, created_at, updated_at)`。
- `internal/auth`：argon2id 哈希/校验；`Manager.Login(ctx, currentClaims, identifier, password)`——identifier 未占用=给当前 `usr_*` 原地注册绑定（记忆零迁移）；已占用=验密后为该账号签发会话（切换，Q5 语义）；错误密码=401。
- `POST /api/sessions/login`（Bearer 当前会话 + identifier/password），响应与 anonymous/refresh 同形（sessionResponse）。
- 安全补强（ADR-0020 决定 6）：刷新令牌重用检测从「拒绝」升级为「吊销整条会话」。
- 客户端：`SessionService.login/logout`；登录页（邮箱+密码+协议勾选，现成组件自组，视觉等背景图后定稿）；设置页「账号与同步」入口与登录状态行。
- CONTEXT.md 新增「正式身份（Verified Identity）」词条、「匿名身份」词条修订（设备标识=默认凭据）。

## User Stories

1. As a 用户, I want 用邮箱密码登录, so that 换机/重装后球球还认识我（画像/记忆/订阅原地保留）。
2. As a 用户, I want 登出后回到本机匿名态, so that 登出不失忆（同 usr_）。

## Non-goals

- 邮箱验证/SMTP/找回（运营端重置，console 面板后置）；微信/Apple/手机号/一键登录/Passkey（资格包）；年龄字段与未成年人模式；登录时自动合并账号；自助解绑；Web 端令牌 cookie 化；登录页最终视觉（等用户背景图）。登录页不显示任何置灰三方按钮。

## Success Criteria

- 全量 go test + flutter test 绿；身份 eval 8 例落地（原地升级记忆保留×3、冲突切换×2、登出×2、重用吊销×1）；evals 存量 100 不漂移。
