package mcpserve

// MCP 鉴权缝（mcp-registry-serve 5.2）：凭证解析在 cmd/server 装配时注入
//（运营台 JWT/个人令牌同体系，ADR-0010），本包只认 Principal——accept
// interfaces，包内不 import 凭证实现。

import (
	"net/http"

	"qiuqiu/internal/auth"
)

// ReadScope 是 MCP 读工具要求的运营 scope：director 与 auditor 都持有
// （ADR-0010 的角色→scope 映射），即任一运营角色可读；越 scope 403。
const ReadScope = auth.ScopeOperatorTraceRead

// Principal 是一次 MCP 请求解析出的运营身份。
type Principal struct {
	Operator string
	Scopes   []string
}

// Authenticator resolves the request credential to a Principal. Implementations
// must share the operations-console credential system (ADR-0010); MCP never
// authenticates anonymous callers — a false return becomes HTTP 401.
type Authenticator interface {
	Authenticate(*http.Request) (Principal, bool)
}

// AuthenticatorFunc adapts a function to the Authenticator interface.
type AuthenticatorFunc func(*http.Request) (Principal, bool)

// Authenticate implements Authenticator.
func (f AuthenticatorFunc) Authenticate(r *http.Request) (Principal, bool) {
	return f(r)
}

// HasReadScope reports whether the principal holds the read scope.
func HasReadScope(principal Principal) bool {
	for _, scope := range principal.Scopes {
		if scope == ReadScope {
			return true
		}
	}
	return false
}
