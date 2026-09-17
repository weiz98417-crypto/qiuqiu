package main

// Operator authorization (ADR-0008 运营管理台：个人令牌身份).
//
// Dual-mode, decided per request on whether any operator row exists:
//
//   - Operators mode (operators table has rows): the bearer token is a
//     personal operator token. The SHA-256 hash is looked up per request —
//     no cached sessions, so revocation (row deletion) fails the very next
//     call — and the granted scopes come from the operator's role
//     (director = all four, auditor = TraceRead only). APP_TOKEN no longer
//     grants anything in this mode.
//
//   - Legacy single-token mode (no operator rows — the eval harness env sets
//     APP_TOKEN='qiuqiu-dev-token' with an empty operators table, and
//     existing deployments predate the console): the shared APP_TOKEN (or the
//     non-production empty-APP_TOKEN dev bypass) keeps working and carries
//     director scopes, so all pre-console operator behavior is unchanged.
//
// Scope enforcement is per-route: reads → TraceRead; match/automation/events/
// config/clock/sources/lifecycle writes → MatchWrite; facts
// confirm/revoke/reconcile + conflict resolution → FactConfirm; event
// corrections → FactCorrect. Console writes (thread ops, portrait
// privacy-ops) are operator writes → MatchWrite. Unauthenticated → 401 as
// today; authenticated but missing the scope → 403.

import (
	"net/http"
	"strings"

	"qiuqiu/internal/auth"
	"qiuqiu/internal/config"
	"qiuqiu/internal/operatorauth"
)

// operatorAuthz resolves the caller's operator identity and checks scopes.
// A nil operators directory (or an empty one) means legacy single-token mode.
type operatorAuthz struct {
	cfg       *config.Config
	operators operatorauth.Directory
}

func newOperatorAuthz(cfg *config.Config, operators operatorauth.Directory) operatorAuthz {
	return operatorAuthz{cfg: cfg, operators: operators}
}

// claims resolves the request's operator identity: personal token lookup when
// operator rows exist, otherwise the legacy APP_TOKEN/dev bypass.
func (a operatorAuthz) claims(r *http.Request) (auth.Claims, bool) {
	if a.cfg == nil {
		return auth.Claims{}, false
	}
	if a.operators != nil && a.operators.Count(r.Context()) > 0 {
		// ADR-0008 operators mode: only the table lookup applies.
		operator, ok := a.operators.Lookup(r.Context(), auth.BearerToken(r.Header.Get("Authorization")))
		if !ok {
			return auth.Claims{}, false
		}
		return auth.Claims{
			Subject: "operator:" + operator.Name,
			Scopes:  operatorauth.ScopesFor(operator.Role),
		}, true
	}
	return legacyOperatorClaims(r, a.cfg)
}

// legacyOperatorClaims is the pre-console behavior verbatim (shared
// APP_TOKEN / non-production dev bypass, both carrying director scopes).
func legacyOperatorClaims(r *http.Request, cfg *config.Config) (auth.Claims, bool) {
	if cfg.AppToken == "" && !strings.EqualFold(cfg.Environment, "production") {
		return auth.Claims{
			Subject: "operator:development",
			Scopes: []string{
				auth.ScopeOperatorMatchWrite,
				auth.ScopeOperatorFactConfirm,
				auth.ScopeOperatorFactCorrect,
				auth.ScopeOperatorTraceRead,
			},
		}, true
	}
	token := auth.BearerToken(r.Header.Get("Authorization"))
	if !cfg.OperatorTokenMatches(token) {
		return auth.Claims{}, false
	}
	return auth.Claims{
		Subject: "operator:default",
		Scopes: []string{
			auth.ScopeOperatorMatchWrite,
			auth.ScopeOperatorFactConfirm,
			auth.ScopeOperatorFactCorrect,
			auth.ScopeOperatorTraceRead,
		},
	}, true
}

// authorize enforces one scope on a protected operator route: 401 when the
// token is missing/invalid, 403 when the operator lacks the scope.
func (a operatorAuthz) authorize(w http.ResponseWriter, r *http.Request, scope string) (auth.Claims, bool) {
	claims, ok := a.claims(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return auth.Claims{}, false
	}
	if !claims.HasScope(scope) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return claims, false
	}
	return claims, true
}

// view reports whether the request carries an authenticated operator with the
// scope, for the public-degradable read endpoints (events/state/config/clock
// GET): anonymous stays on the degraded public view exactly as before, a
// scoped operator gets the enriched operator view.
func (a operatorAuthz) view(r *http.Request, scope string) (auth.Claims, bool) {
	claims, ok := a.claims(r)
	if !ok || !claims.HasScope(scope) {
		return auth.Claims{}, false
	}
	return claims, true
}

// operatorName strips the subject namespace so audit rows store the bare
// operator name (legacy modes attribute to "default"/"development").
func operatorName(claims auth.Claims) string {
	return strings.TrimPrefix(claims.Subject, "operator:")
}
