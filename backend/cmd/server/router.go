package main

// 路由收敛（openspec/changes/operations-live-stream 3.4+3.5）：console 与
// match 两个 API 面从「TrimPrefix + 手写字符串 switch + 每个 handler 内手抄
// authorize」迁移到 Go 1.22+ ServeMux 的 method+wildcard pattern（路径参数
// r.PathValue），加一份声明式注册表（每端点一行：method + pattern + scope +
// handler）与 withScope 鉴权装饰器。handler 体原样搬移，响应体与状态码不变。
//
// 与旧 switch 的行为差异（有意为之，仅此一处，见各入口注册表）：
//
//   - 已知路径 + 错误方法：旧 switch 落 default 返回 404；ServeMux 的
//     method-scoped pattern 按标准返回 405 Method Not Allowed（带 Allow 头
//     列出该路径支持的动词）。对方法用错给出标准信号比吞成 404 更有诊断
//     价值；golden/evals 锁定的全部 (method, path) 组合不受影响。
//   - 未知路径仍 404：没有 pattern 匹配时 ServeMux 走内建 NotFound，与旧
//     http.NotFound 同状态码同响应体；match 面旧的 `len(parts) < 2 ||
//     parts[0] == ""` 前置 404 语义由 pattern 的非空段匹配自然覆盖。
//   - OPTIONS 预检在进入 mux 之前拦截（mountRoutes）：子树内任意路径一律
//     200 空体，与旧入口的提前返回逐字节一致——否则 method-scoped pattern
//     会把预检打成 405。
//   - 旧行为里 strings.Trim 偶然容忍的尾部斜杠（如 "/api/matches/m1/start/"）
//     不再被匹配，按未知路径返回 404。

import (
	"net/http"

	"qiuqiu/internal/auth"
	"qiuqiu/internal/config"
)

// route 是注册表的一行。scope 为空串表示该端点不包 withScope，与迁移前
// 一致：login 是认证入口本身（无鉴权）；whoami 与 me/password 只解析身份
// 不做 scope 检查；refresh/logout 以 refresh token 为凭证；match 面四个
// 公开可降级读端点（clock/config/events/state 的 GET）在 handler 内用
// authz.view 自行区分公开/运营视图。
type route struct {
	method  string
	pattern string
	scope   string
	handler http.HandlerFunc
}

// withScope 把一个 handler 包上单 scope 鉴权：authorize 失败即 return
// （401/403 响应体由 authorize 自己写），成功才放行 next。原先手抄在各个
// handler 里的 authz.authorize 统一收敛到这里。
func withScope(authz operatorAuthz, scope string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := authz.authorize(w, r, scope); !ok {
			return
		}
		next(w, r)
	}
}

// operatorClaims 在 withScope 放行后重新解析调用者身份：事件发布/更正、
// 事实确认与冲突裁决、console 的 operator 管理与画像删除等端点要把
// operator subject 写进事件与审计行，而 withScope 的签名只透传 w/r。
// authorize 刚在同一请求上成功过，这里再失败只可能发生在两步之间令牌被
// 吊销的窄窗口——按 authorize 的口径回 401。
func operatorClaims(authz operatorAuthz, w http.ResponseWriter, r *http.Request) (auth.Claims, bool) {
	claims, ok := authz.claims(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return auth.Claims{}, false
	}
	return claims, true
}

// mountRoutes 把注册表按序装进一个 ServeMux，外层先过 CORS 再拦截 OPTIONS
// 预检，返回可直接挂在 /api/console/ 或 /api/matches/ 子树上的入口 handler。
func mountRoutes(cfg *config.Config, authz operatorAuthz, routes []route) http.HandlerFunc {
	mux := http.NewServeMux()
	for _, rt := range routes {
		handler := rt.handler
		if rt.scope != "" {
			handler = withScope(authz, rt.scope, handler)
		}
		mux.HandleFunc(rt.method+" "+rt.pattern, handler)
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if !applyCORS(w, r, cfg) {
			return
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		mux.ServeHTTP(w, r)
	}
}
