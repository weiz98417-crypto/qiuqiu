// Package useraffect 感知用户语音情绪（User Voice Affect，CONTEXT.md）：
// 话轮级副语言信号——用户怎么说的，不是说了什么。sidecar（SenseVoice，
// compose profile voice-input）HTTP 客户端与 internal/ambient.Client 同族：
// 短超时 + 熔断 + 失败静默。宪法线（与球场气氛同规格）：情绪信号永不进
// 比赛事实账本，只落语音 trace 观测与（波2）情绪偏置。
package useraffect

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"qiuqiu/internal/resilience"
)

// DefaultTimeout 是 sidecar 调用的默认预算：整段话轮音频（≤30s）的
// SenseVoice 推理在 CPU 上亚秒级，预算放宽到 2s（真机实测后可收紧）。
const DefaultTimeout = 2 * time.Second

var ErrNotConfigured = errors.New("user affect sidecar is not configured")

// Client 调 voice-input sidecar 的 POST /affect：整段话轮 PCM 进、
// {label, confidence} 出。零值不可用；NewClient 构造，baseURL 留空即旁路
// 停用（Classify 返回 ErrNotConfigured）。熔断 3 次失败开 10 秒，与 ambient
// 一致。
type Client struct {
	baseURL    string
	httpClient *http.Client
	breaker    *resilience.CircuitBreaker
	dropped    atomic.Int64
}

// NewClient 构造指向 voice-input sidecar 的客户端；baseURL 为空表示旁路
// 停用。
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL:    strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		httpClient: &http.Client{Timeout: DefaultTimeout},
		breaker:    resilience.NewCircuitBreaker(3, 10*time.Second),
	}
}

// WithTimeout 覆盖默认调用预算。
func (c *Client) WithTimeout(timeout time.Duration) *Client {
	if timeout > 0 && c.httpClient != nil {
		c.httpClient.Timeout = timeout
	}
	return c
}

// Dropped 返回静默丢弃计数（熔断开启/非 200/解码失败的累计）。
func (c *Client) Dropped() int64 {
	if c == nil {
		return 0
	}
	return c.dropped.Load()
}

// Signal 是一次话轮的情绪结论。Confidence 是 sidecar 的伪置信（SenseVoice
// 输出情绪 token 无校准概率，sidecar 常量 0.6 起步）——聚合与置信门在
// relay 层，本包只透传。
type Signal struct {
	Label      string  `json:"label"`
	Confidence float64 `json:"confidence"`
}

type sidecarResponse struct {
	Label      string  `json:"label"`
	Confidence float64 `json:"confidence"`
}

// Classify 把一段话轮 PCM（s16le 16k mono）交给 sidecar。
func (c *Client) Classify(ctx context.Context, pcm []byte) (Signal, error) {
	if c == nil || c.baseURL == "" {
		return Signal{}, ErrNotConfigured
	}
	now := time.Now()
	if err := c.breaker.Allow(now); err != nil {
		c.dropped.Add(1)
		return Signal{}, errors.New("user affect breaker open")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/affect", bytes.NewReader(pcm))
	if err != nil {
		c.dropped.Add(1)
		return Signal{}, err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.breaker.Failure(now)
		c.dropped.Add(1)
		return Signal{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		c.breaker.Failure(now)
		c.dropped.Add(1)
		io.Copy(io.Discard, resp.Body)
		return Signal{}, errors.New("user affect sidecar status " + resp.Status)
	}
	var decoded sidecarResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		c.breaker.Failure(now)
		c.dropped.Add(1)
		return Signal{}, err
	}
	c.breaker.Success()
	if strings.TrimSpace(decoded.Label) == "" {
		return Signal{}, errors.New("user affect sidecar returned empty label")
	}
	return Signal{Label: strings.TrimSpace(decoded.Label), Confidence: decoded.Confidence}, nil
}
