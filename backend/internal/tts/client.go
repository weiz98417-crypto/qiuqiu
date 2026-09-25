// Package tts 承载 TTS Provider seam：Synthesizer 接口是唯一调用面，
// 供应商知识（URL/model/voice/WAV 组包/熔断器）收敛在各 adapter 内
// （ADR-0012 修订）。Miimo adapter 是现役生产实现；NewMockClient 是
// 确定性测试替身。
package tts

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"qiuqiu/internal/openaicompat"
	"qiuqiu/internal/resilience"
)

var ErrNotConfigured = errors.New("tts provider is not configured")

// Client 是 Miimo TTS adapter：POST {baseURL}/chat/completions，整段
// base64 WAV 返回（非流式），自然语言指令以额外 user message 拼进
// messages（Miimo 唯一的风格通道）。
type Client struct {
	apiKey     string
	baseURL    string
	model      string
	voice      string
	httpClient *http.Client
	mockAudio  []byte
	breaker    *resilience.CircuitBreaker
}

func NewClient(apiKey string) *Client {
	return &Client{
		apiKey:  apiKey,
		baseURL: "https://api.xiaomimimo.com/v1",
		model:   "mimo-v2.5-tts",
		voice:   "冰糖",
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		breaker: resilience.NewCircuitBreaker(3, 10*time.Second),
	}
}

// NewMockClient 是 fake adapter：回放注入的确定性音频字节，不发网络
// 请求、不失败，供离线链路与情绪映射测试使用。
func NewMockClient(audio []byte) *Client {
	return &Client{mockAudio: append([]byte(nil), audio...)}
}

func (c *Client) WithBaseURL(baseURL string) *Client {
	c.baseURL = strings.TrimRight(baseURL, "/")
	return c
}

func (c *Client) WithHTTPClient(httpClient *http.Client) *Client {
	if httpClient != nil {
		c.httpClient = httpClient
	}
	return c
}

func (c *Client) WithModel(model string) *Client {
	if strings.TrimSpace(model) != "" {
		c.model = strings.TrimSpace(model)
	}
	return c
}

func (c *Client) WithVoice(voice string) *Client {
	if strings.TrimSpace(voice) != "" {
		c.voice = strings.TrimSpace(voice)
	}
	return c
}

func (c *Client) CircuitState() resilience.State {
	if c == nil {
		return resilience.StateOpen
	}
	return c.breaker.State()
}

type SynthesizeResult struct {
	AudioData []byte
	Duration  time.Duration
	MimeType  string
}

// Synthesize 调 Miimo TTS 合成整段音频。VoiceOpts.Instruction 作为表演
// 指令占据 messages 首位（user），待合成文本是 assistant message；
// VoiceOpts 的空字段回落 adapter 默认（wav / 冰糖）。
func (c *Client) Synthesize(ctx context.Context, text string, opts VoiceOpts) (*SynthesizeResult, error) {
	start := time.Now()
	if c == nil {
		return nil, ErrNotConfigured
	}
	if len(c.mockAudio) > 0 {
		return &SynthesizeResult{AudioData: append([]byte(nil), c.mockAudio...), Duration: time.Since(start), MimeType: "audio/mpeg"}, nil
	}
	if strings.TrimSpace(c.apiKey) == "" {
		return nil, ErrNotConfigured
	}
	if err := c.breaker.Allow(time.Now().UTC()); err != nil {
		return nil, fmt.Errorf("tts unavailable: %w", err)
	}
	voice := strings.TrimSpace(opts.Voice)
	// cgSg 前缀是旧预制音色 id：平台已不支持，原样回落默认音色。
	if voice == "" || strings.HasPrefix(voice, "cgSg") {
		voice = c.voice
	}
	messages := make([]map[string]string, 0, 2)
	if instruction := strings.TrimSpace(opts.Instruction); instruction != "" {
		messages = append(messages, map[string]string{"role": "user", "content": instruction})
	}
	messages = append(messages, map[string]string{"role": "assistant", "content": text})
	payload := map[string]interface{}{
		"model":    defaultString(c.model, "mimo-v2.5-tts"),
		"messages": messages,
		"audio": map[string]string{
			"format": defaultString(opts.Format, "wav"),
			"voice":  defaultString(voice, "冰糖"),
		},
	}

	body, _ := json.Marshal(payload)
	url := fmt.Sprintf("%s/chat/completions", strings.TrimRight(c.baseURL, "/"))
	httpReq, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	openaicompat.SetAuthHeaders(httpReq, openaicompat.Endpoint{BaseURL: c.baseURL, APIKey: c.apiKey, Model: c.model})
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		c.breaker.Failure(time.Now().UTC())
		return nil, fmt.Errorf("tts request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		c.breaker.Failure(time.Now().UTC())
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("tts error %d: %s", resp.StatusCode, string(errBody))
	}

	var raw struct {
		Choices []struct {
			Message struct {
				Audio struct {
					Data string `json:"data"`
				} `json:"audio"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		c.breaker.Failure(time.Now().UTC())
		return nil, fmt.Errorf("tts decode: %w", err)
	}
	if len(raw.Choices) == 0 || strings.TrimSpace(raw.Choices[0].Message.Audio.Data) == "" {
		c.breaker.Failure(time.Now().UTC())
		return nil, fmt.Errorf("tts returned no audio")
	}
	audio, err := decodeBase64(raw.Choices[0].Message.Audio.Data)
	if err != nil {
		c.breaker.Failure(time.Now().UTC())
		return nil, fmt.Errorf("tts decode audio: %w", err)
	}

	c.breaker.Success()
	return &SynthesizeResult{
		AudioData: audio,
		Duration:  time.Since(start),
		MimeType:  "audio/wav",
	}, nil
}

// SynthesizeStream 如实暴露 Miimo 的整段合成现实：没有流式能力，返回
// ErrNotSupported，不实现假流式。
func (c *Client) SynthesizeStream(ctx context.Context, text string, opts VoiceOpts) (<-chan []byte, error) {
	return nil, ErrNotSupported
}

func decodeBase64(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if idx := strings.Index(value, ","); idx >= 0 {
		value = value[idx+1:]
	}
	return base64.StdEncoding.DecodeString(value)
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
