package main

// 运营演示账号 seed（2026-09-29 运营端演示轮）：开发档启动时幂等创建
// admin/5055365（director 角色，setAt 非零跳过首登强制改密）。客户端
// test/5055365 无需 seed——登录凭证缝绑定即注册（ADR-0020）。

import (
	"context"
	"os"
	"strings"
	"time"

	"qiuqiu/internal/consoleauth"
	"qiuqiu/internal/operatorauth"
)

// demoOperatorName/demoOperatorPassword 是开发档固定演示凭据；演示令牌
// 同样确定值（dev-only，仅 development 环境生效）。
const (
	demoOperatorName     = "admin"
	demoOperatorPassword = "5055365"
	demoOperatorToken    = "qiuqiu-admin-dev-token"
)

// seedDemoOperator 在空表时创建演示运营员；任何能力缺失（无密码账户能
// 力、已有人账号）静默跳过——它只为「新机器起后端就能登录」服务。
func seedDemoOperator(ctx context.Context, store operatorauth.Directory, environment string, logf func(string, ...any)) {
	// 显式开关：默认关闭。演示 seed 一旦执行，密码账户存在即拒绝 legacy
	// 令牌通道——门禁与既有脚本全部依赖 APP_TOKEN，绝不能默认种。
	if os.Getenv("QIUQIU_DEMO_ACCOUNTS") != "1" {
		return
	}
	if environment != "" && !strings.EqualFold(environment, "development") {
		return
	}
	if counter, ok := store.(interface{ Count(context.Context) int64 }); !ok || counter.Count(ctx) > 0 {
		return
	}
	passwords, ok := store.(operatorauth.PasswordAccounts)
	if !ok {
		return
	}
	if _, err := store.Seed(ctx, demoOperatorName, demoOperatorToken, operatorauth.RoleDirector); err != nil {
		logf("demo operator seed skipped: %v", err)
		return
	}
	hash, err := consoleauth.HashPassword(demoOperatorPassword)
	if err != nil {
		logf("demo operator seed skipped: %v", err)
		return
	}
	// setAt 非零：演示账号不做首登强制改密（直接可用）。
	if err := passwords.SetPasswordCredentials(ctx, demoOperatorName, hash, time.Now().UTC()); err != nil {
		logf("demo operator password seed skipped: %v", err)
		return
	}
	logf("demo operator seeded: %s (development only, director role, fixed demo password)", demoOperatorName)
}
