package main

import (
	"context"
	"testing"

	"qiuqiu/internal/consoleauth"
	"qiuqiu/internal/operatorauth"
)

// 开发档演示运营员：空表才 seed、director 角色、setAt 非零（无强制改密）、
// 非 development 环境不动。
func TestSeedDemoOperator(t *testing.T) {
	store := operatorauth.NewMemoryStore()
	ctx := context.Background()

	// 非 development：不动。
	seedDemoOperator(ctx, store, "production", t.Logf)
	if store.Count(ctx) != 0 {
		t.Fatal("production env must not seed")
	}

	// 开关开启 + development：seed 成功且密码可验、无强制改密。
	t.Setenv("QIUQIU_DEMO_ACCOUNTS", "1")
	seedDemoOperator(ctx, store, "development", t.Logf)
	if store.Count(ctx) != 1 {
		t.Fatalf("count = %d, want 1", store.Count(ctx))
	}
	var passwords operatorauth.PasswordAccounts = store
	creds, err := passwords.Credentials(ctx, demoOperatorName)
	if err != nil {
		t.Fatalf("credentials: %v", err)
	}
	if creds.PasswordSetAt.IsZero() {
		t.Fatal("demo account must not carry the first-login force-change flag")
	}
	if !consoleauth.VerifyPassword(demoOperatorPassword, creds.PasswordHash) {
		t.Fatal("password 5055365 must verify")
	}

	// 幂等：再跑一次不重复。
	seedDemoOperator(ctx, store, "development", t.Logf)
	if store.Count(ctx) != 1 {
		t.Fatalf("idempotency broken: count = %d", store.Count(ctx))
	}
}

// VerifyPassword 形状锁定（防止 HashPassword/Verify 配对被改坏）。
func TestDemoPasswordHashRoundTrip(t *testing.T) {
	hash, err := consoleauth.HashPassword(demoOperatorPassword)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if !consoleauth.VerifyPassword(demoOperatorPassword, hash) {
		t.Fatal("round trip failed")
	}
	if consoleauth.VerifyPassword("5055366", hash) {
		t.Fatal("wrong password must not verify")
	}
}
