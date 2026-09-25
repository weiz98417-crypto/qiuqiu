package ambient

// sidecar HTTP client（openspec/changes/ambient-audio-observation 6.1）。
// 与 internal/asr.Client 同族：短超时 + 熔断 + 失败静默——气氛旁路是纯增量，
// sidecar 不可用时旁路静默消失（失败丢弃+计数），绝不阻塞、绝不报错风暴。

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"qiuqiu/internal/resilience"
)

// DefaultTimeout 是 sidecar 调用的默认预算（design.md：超时短，如 500ms）。
const DefaultTimeout = 500 * time.Millisecond

// Client 调 SenseVoice AED sidecar 的 POST /aevents：音频分片进、JSON 事件
// 数组出。零值不可用；用 NewClient 构造，baseURL 留空即旁路整体停用。
type Client struct {
	baseURL    string
	httpClient *http.Client
	breaker    *resilience.CircuitBreaker
	dropped    atomic.Int64
}

// NewClient 构造指向 sidecar 的客户端；baseURL 为空表示旁路停用（Classify
// 直接返回 ErrNotConfigured，接线层跳过）。熔断 3 次失败开 10 秒，与 ASR 一致。
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL:    strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		httpClient: &http.Client{Timeout: DefaultTimeout},
		breaker:    resilience.NewCircuitBreaker(3, 10*time.Second),
	}
}

// WithTimeout 覆盖默认调用预算（config AMBIENT_AED_TIMEOUT_MS 注入）。
func (c *Client) WithTimeout(timeout time.Duration) *Client {
	if timeout > 0 && c.httpClient != nil {
		c.httpClient.Timeout = timeout
	}
	return c
}

// WithHTTPClient 注入测试/定制传输。
func (c *Client) WithHTTPClient(httpClient *http.Client) *Client {
	if httpClient != nil {
		c.httpClient = httpClient
	}
	return c
}

// Enabled 报告旁路是否配置了 sidecar 端点（nil 客户端或空端点 = 停用）。
func (c *Client) Enabled() bool {
	return c != nil && c.baseURL != ""
}

// Failures 返回旁路静默丢弃计数（sidecar 失败/熔断拒发/坏响应），
// 供摘除场景测试与运维观测：计数增长而主链路无感。
func (c *Client) Failures() int64 {
	if c == nil {
		return 0
	}
	return c.dropped.Load()
}

// CircuitState 暴露熔断状态（观测用）。
func (c *Client) CircuitState() resilience.State {
	if c == nil {
		return resilience.StateOpen
	}
	return c.breaker.State()
}

// Classify 把一个 pcm16/16k 单声道音频分片送 sidecar 判定，返回气氛事件。
// 调用失败返回错误并由接线层静默丢弃（本方法只计数，不 log 不重试）。
func (c *Client) Classify(ctx context.Context, pcm []byte) ([]Event, error) {
	if c == nil || !c.Enabled() {
		return nil, ErrNotConfigured
	}
	if len(pcm) == 0 {
		return nil, nil
	}
	now := time.Now().UTC()
	if err := c.breaker.Allow(now); err != nil {
		c.dropped.Add(1)
		return nil, fmt.Errorf("ambient sidecar unavailable: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/aevents", bytes.NewReader(pcm))
	if err != nil {
		c.dropped.Add(1)
		return nil, err
	}
	// 音频分片规格与 ASR 主路一致（pcm_s16le/16kHz/单声道），raw body 直送。
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.breaker.Failure(now)
		c.dropped.Add(1)
		return nil, fmt.Errorf("ambient sidecar request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		c.breaker.Failure(now)
		c.dropped.Add(1)
		return nil, fmt.Errorf("ambient sidecar error %d", resp.StatusCode)
	}
	var raw []Event
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&raw); err != nil {
		c.breaker.Failure(now)
		c.dropped.Add(1)
		return nil, fmt.Errorf("ambient sidecar decode: %w", err)
	}
	c.breaker.Success()
	return sanitizeEvents(raw, now), nil
}

// sanitizeEvents 逐条过 sanitize：未知种类丢弃、置信度夹取、补 ts。
func sanitizeEvents(raw []Event, now time.Time) []Event {
	events := make([]Event, 0, len(raw))
	for _, event := range raw {
		if sanitized, ok := event.sanitize(now); ok {
			events = append(events, sanitized)
		}
	}
	return events
}

// ErrNotConfigured 表示旁路未配置端点（接线层据此整体跳过）。
var ErrNotConfigured = fmt.Errorf("ambient sidecar endpoint is not configured")
