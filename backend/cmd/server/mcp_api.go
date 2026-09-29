package main

// MCP 只读账本端点装配（mcp-registry-serve 5.2）：internal/mcpserve 的
// 只读 server 挂到现有 ServeMux 的 /mcp，凭证走运营台同体系（ADR-0010）。
// 解析器刻意不继承 legacyOperatorClaims 的共享 APP_TOKEN/开发旁路——MCP
// 端点对匿名永远 401；JWT 人通道或个人令牌机通道二选一，缺只读 scope 403。

import (
	"net/http"
	"strings"

	"qiuqiu/internal/auth"
	"qiuqiu/internal/companion"
	"qiuqiu/internal/config"
	"qiuqiu/internal/consoleauth"
	"qiuqiu/internal/mcpserve"
	"qiuqiu/internal/operatorauth"
)

// mcpIdentityResolver resolves MCP bearer credentials strictly through the
// operations-console channels: HS256 JWT (human channel, ADR-0010) or a
// personal operator token (machine channel, ADR-0008). The legacy shared
// APP_TOKEN / non-production dev bypass is intentionally NOT honored here —
// the MCP endpoint is anonymous-hostile by contract (匿名一律 401).
type mcpIdentityResolver struct {
	operators operatorauth.Directory
	jwtSecret string
}

// Authenticate implements mcpserve.Authenticator.
func (resolver mcpIdentityResolver) Authenticate(r *http.Request) (mcpserve.Principal, bool) {
	bearer := auth.BearerToken(r.Header.Get("Authorization"))
	if bearer == "" {
		return mcpserve.Principal{}, false
	}
	// Human channel first: a bearer that looks like a JWT must verify against
	// the console secret; a failed verification is a hard 401 (no fallthrough
	// that could turn a malformed JWT into an anonymous request).
	if consoleauth.LooksLikeJWT(bearer) && strings.TrimSpace(resolver.jwtSecret) != "" {
		claims, err := consoleauth.VerifyJWT(bearer, resolver.jwtSecret)
		if err != nil {
			return mcpserve.Principal{}, false
		}
		scopes := claims.Scopes
		// ADR-0010 locked decision 1: a JWT minted against a first-login temp
		// password carries no scopes until the password is changed.
		if !claims.PasswordSet {
			scopes = nil
		}
		return mcpserve.Principal{Operator: claims.Sub, Scopes: scopes}, true
	}
	// Machine channel: personal operator token, hash looked up per request
	// (revocation = row deletion, same as the rest of the operator API).
	if resolver.operators == nil || resolver.operators.Count(r.Context()) == 0 {
		return mcpserve.Principal{}, false
	}
	operator, ok := resolver.operators.Lookup(r.Context(), bearer)
	if !ok {
		return mcpserve.Principal{}, false
	}
	return mcpserve.Principal{Operator: operator.Name, Scopes: operatorauth.ScopesFor(operator.Role)}, true
}

// mountMCPServer is the single wiring point called from main(): mounts the
// four read-only ledger tools on /mcp behind console credentials.
func mountMCPServer(mux *http.ServeMux, cfg *config.Config, matches mcpserve.MatchReader, schedules companion.ScheduleReader, operators operatorauth.Directory) {
	mcpserve.Mount(mux, mcpserve.Deps{Matches: matches, Schedules: schedules}, mcpIdentityResolver{
		operators: operators,
		jwtSecret: cfg.JWTSecret,
	})
}
