package main

// Grafana 同源反代（operations-metrics-stack）：console 观测页的 iframe
// 经 /grafana/ 前缀直连本机 Grafana——同源后前端可以往 iframe 注 CSS
// （隐藏 controls chrome「黑杠」）且不再依赖 allow_embedding。上游地址
// 取 QIUQIU_GRAFANA_URL；未配置时不注册路由（观测页落占位态）。

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

func grafanaProxyHandler(upstream string) http.HandlerFunc {
	target, err := url.Parse(strings.TrimRight(strings.TrimSpace(upstream), "/"))
	if err != nil || target.Host == "" {
		return func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "grafana upstream misconfigured", http.StatusBadGateway)
		}
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		// 保留 /grafana 前缀原样转发：上游 Grafana 开了
		// serve_from_sub_path + root_url 带子路径，自己认 /grafana/*；
		// 剥前缀反而触发它的 301 规范化自环。
		req.Host = target.Host // cookie/csrf 按上游 Host 走
	}
	return proxy.ServeHTTP
}

func grafanaProxyPrefix(upstream string) string {
	return strings.TrimRight(strings.TrimSpace(upstream), "/")
}
