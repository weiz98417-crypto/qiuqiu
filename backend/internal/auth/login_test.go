package auth

// 登录凭证缝身份测试（login-credential-seam tasks 1.5，8 例）：
// 原地升级×3、冲突切换×2、登出回匿名×2、重用吊销×1。

import (
	"context"
	"errors"
	"testing"
)

func newLoginTestManager(t *testing.T) *Manager {
	t.Helper()
	store := NewMemoryStore()
	manager, err := NewManager(store, "test-session-signing-key-0123456789")
	if err != nil {
		t.Fatal(err)
	}
	return manager
}

func mustAnonymous(t *testing.T, manager *Manager, deviceID string) Session {
	t.Helper()
	session, err := manager.IssueAnonymous(context.Background(), deviceID)
	if err != nil {
		t.Fatal(err)
	}
	return session
}

// 原地升级 1/3：匿名会话登录后 userId 不变（绑定不换 ID，记忆零迁移）。
func TestLoginUpgradesAnonymousInPlace(t *testing.T) {
	manager := newLoginTestManager(t)
	anonymous := mustAnonymous(t, manager, "dev_1")

	session, err := manager.Login(context.Background(), anonymous.Claims, "Fan@Example.COM ", "password123")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if session.Claims.Subject != anonymous.Claims.Subject {
		t.Fatalf("userId changed on login: %s → %s", anonymous.Claims.Subject, session.Claims.Subject)
	}
	// identifier 归一为小写。
	if session.Claims.DeviceID != "dev_1" {
		t.Fatalf("device changed: %q", session.Claims.DeviceID)
	}
}

// 原地升级 2/3：登录后的会话真实可用（Authenticate 通过、作用域一致）。
func TestLoginSessionAuthenticates(t *testing.T) {
	manager := newLoginTestManager(t)
	anonymous := mustAnonymous(t, manager, "dev_1")

	session, err := manager.Login(context.Background(), anonymous.Claims, "fan@example.com", "password123")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	claims, err := manager.Authenticate(context.Background(), session.AccessToken)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if claims.Subject != anonymous.Claims.Subject || !claims.HasScope(ScopeUserChat) {
		t.Fatalf("claims = %+v", claims)
	}
}

// 原地升级 3/3：同一 identifier 二次登录（重连/换会话）仍是同一账号。
func TestLoginAgainSameAccount(t *testing.T) {
	manager := newLoginTestManager(t)
	anonymous := mustAnonymous(t, manager, "dev_1")
	first, err := manager.Login(context.Background(), anonymous.Claims, "fan@example.com", "password123")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	second, err := manager.Login(context.Background(), first.Claims, "fan@example.com", "password123")
	if err != nil {
		t.Fatalf("second Login: %v", err)
	}
	if second.Claims.Subject != first.Claims.Subject {
		t.Fatalf("userId changed across logins: %s → %s", first.Claims.Subject, second.Claims.Subject)
	}
}

// 冲突切换 1/2：另一设备用已绑定 identifier 登录 → 切到该账号（userId 变为目标）。
func TestLoginSwitchesToExistingAccount(t *testing.T) {
	manager := newLoginTestManager(t)
	owner := mustAnonymous(t, manager, "dev_owner")
	if _, err := manager.Login(context.Background(), owner.Claims, "fan@example.com", "password123"); err != nil {
		t.Fatalf("owner Login: %v", err)
	}

	visitor := mustAnonymous(t, manager, "dev_new")
	session, err := manager.Login(context.Background(), visitor.Claims, "fan@example.com", "password123")
	if err != nil {
		t.Fatalf("visitor Login: %v", err)
	}
	if session.Claims.Subject != owner.Claims.Subject {
		t.Fatalf("switch userId = %s, want owner %s", session.Claims.Subject, owner.Claims.Subject)
	}
	if session.Claims.DeviceID != "dev_new" {
		t.Fatalf("switch device = %s, want visitor device", session.Claims.DeviceID)
	}
}

// 冲突切换 2/2：密码错误拒绝，不切换。
func TestLoginRejectsWrongPassword(t *testing.T) {
	manager := newLoginTestManager(t)
	owner := mustAnonymous(t, manager, "dev_owner")
	if _, err := manager.Login(context.Background(), owner.Claims, "fan@example.com", "password123"); err != nil {
		t.Fatalf("owner Login: %v", err)
	}

	visitor := mustAnonymous(t, manager, "dev_new")
	if _, err := manager.Login(context.Background(), visitor.Claims, "fan@example.com", "wrong-password"); !errors.Is(err, ErrInvalidLogin) {
		t.Fatalf("err = %v, want ErrInvalidLogin", err)
	}
}

// 登出回匿名 1/2：吊销登录会话后，同设备再取匿名身份归还同一 usr_（登出
// 不等于失忆）。
func TestLogoutReturnsToSameAnonymousIdentity(t *testing.T) {
	manager := newLoginTestManager(t)
	anonymous := mustAnonymous(t, manager, "dev_1")
	session, err := manager.Login(context.Background(), anonymous.Claims, "fan@example.com", "password123")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if err := manager.RevokeRefresh(context.Background(), session.RefreshToken); err != nil {
		t.Fatalf("logout: %v", err)
	}

	again := mustAnonymous(t, manager, "dev_1")
	if again.Claims.Subject != anonymous.Claims.Subject {
		t.Fatalf("anonymous identity changed: %s → %s", anonymous.Claims.Subject, again.Claims.Subject)
	}
}

// 登出回匿名 2/2：被吊销的登录会话不可再用。
func TestLogoutRevokesSession(t *testing.T) {
	manager := newLoginTestManager(t)
	anonymous := mustAnonymous(t, manager, "dev_1")
	session, err := manager.Login(context.Background(), anonymous.Claims, "fan@example.com", "password123")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if err := manager.RevokeRefresh(context.Background(), session.RefreshToken); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if _, err := manager.Authenticate(context.Background(), session.AccessToken); !errors.Is(err, ErrRevoked) {
		t.Fatalf("err = %v, want ErrRevoked", err)
	}
}

// 重用吊销：旧刷新令牌重放 → 拒绝且整条会话吊销（新令牌也失效）。
func TestRefreshReuseRevokesSession(t *testing.T) {
	manager := newLoginTestManager(t)
	anonymous := mustAnonymous(t, manager, "dev_1")

	first, err := manager.Refresh(context.Background(), anonymous.RefreshToken)
	if err != nil {
		t.Fatalf("first refresh: %v", err)
	}
	if _, err := manager.Refresh(context.Background(), anonymous.RefreshToken); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("replay err = %v, want ErrInvalidToken", err)
	}
	if _, err := manager.Refresh(context.Background(), first.RefreshToken); !errors.Is(err, ErrRevoked) {
		t.Fatalf("session not revoked after replay: %v", err)
	}
}
