package ws

import (
	"testing"

	"qiuqiu/internal/auth"
)

// UpgradeOps 的放行判定真身在 handler.go：会话 claims 须带
// OperatorTraceRead；legacy 通道（运营员令牌/开发档匿名）按既有
// allowLegacyConnection 语义放行。用户会话（UserChat）不放行——ops 是
// 运营面，不是用户面。
func TestOpsAuthorization(t *testing.T) {
	operatorClaims := auth.Claims{Subject: "op-1"}
	operatorClaims.Scopes = []string{auth.ScopeOperatorTraceRead}
	if !opsAuthorized(operatorClaims, "whatever-token", func(token string) bool { return false }) {
		t.Fatal("claims with OperatorTraceRead must pass")
	}

	userClaims := auth.Claims{Subject: "fan-1"}
	userClaims.Scopes = []string{auth.ScopeUserChat}
	if opsAuthorized(userClaims, "user-token", func(token string) bool { return false }) {
		t.Fatal("user chat session must not pass ops authorization")
	}

	if !opsAuthorized(auth.Claims{}, "operator-token", func(token string) bool { return token == "operator-token" }) {
		t.Fatal("legacy operator token must pass")
	}
	if opsAuthorized(auth.Claims{}, "random-token", func(token string) bool { return false }) {
		t.Fatal("unrecognized token must not pass")
	}
	if !opsAuthorized(auth.Claims{}, "", nil) {
		t.Fatal("dev anonymous (no token, nil matcher consulted upstream) must pass via legacy branch")
	}
}
