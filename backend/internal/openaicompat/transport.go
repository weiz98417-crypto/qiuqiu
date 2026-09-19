// Package openaicompat 是 OpenAI 兼容 /chat/completions 端点的共享传输层
// （openspec/changes/llm-transport-seam）：鉴权约定一处定义，HTTP 执行与
// 韧性策略作为参数。llm 与 router 是它的两个真实 adapter；asr/tts 复用
// 鉴权辅助（其载荷是音频专用格式，不并入统一信封）。零外部依赖。
//
// 韧性策略说明：传输层默认零重试——调用方自行决定策略。llm 在自己的
// 调用序列上叠加熔断器（resilience.CircuitBreaker）；router 的单次调用
// （ADR-0009 锁定决策）即「不开重试」的原样表达。流式（SSE）暂留 llm：
// 只有它一个消费者，第二个消费者出现前不抽象（假设 seam 不立）。
package openaicompat

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Endpoint 定位一个 OpenAI 兼容端点。
type Endpoint struct {
	BaseURL string
	APIKey  string
	Model   string
}

// UsesAPIKeyAuth 报告该端点是否走 api-key 头：MiMo 平台（xiaomimimo.com
// 域名或 mimo- 模型前缀）用 api-key，其余平台用 Bearer。这是全仓库唯一的
// 鉴权分支实现——平台约定变更只改这里。
func UsesAPIKeyAuth(e Endpoint) bool {
	return strings.Contains(e.BaseURL, "xiaomimimo.com") || strings.HasPrefix(e.Model, "mimo-")
}

// SetAuthHeaders 按平台约定写鉴权头。
func SetAuthHeaders(req *http.Request, e Endpoint) {
	if UsesAPIKeyAuth(e) {
		req.Header.Set("api-key", e.APIKey)
		return
	}
	req.Header.Set("Authorization", "Bearer "+e.APIKey)
}

// Post 向 {BaseURL}{path} 发一次 JSON POST：写鉴权与 Content-Type，执行
// 请求并读回响应体（上限 limit 字节）。非 2xx 返回携带状态码与响应体的
// 错误；调用方决定重试/熔断策略，本层不做。
func Post(ctx context.Context, client *http.Client, e Endpoint, path string, payload []byte, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.BaseURL+path, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	SetAuthHeaders(req, e)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("transport: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return body, fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}
