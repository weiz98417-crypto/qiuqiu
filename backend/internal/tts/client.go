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

// Client 是 Miimo TTS adapter：POST {baseURL}/chat/completions。整段模式
// 一次返回 base64 WAV；流式模式（stream.go）走 stream:true SSE，输出裸
// pcm16@24kHz 分片，失败回退整段。自然语言指令以额外 user message 拼进
// messages（Miimo 唯一的风格通道），两种模式共用同一组包。
type Client struct {
	apiKey            string
	baseURL           string
	model             string
	voice             string
	httpClient        *http.Client
	mockAudio         []byte
	breaker           *resilience.CircuitBreaker
	streamIdleTimeout time.Duration
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
		breaker:           resilience.NewCircuitBreaker(3, 10*time.Second),
		streamIdleTimeout: defaultStreamIdleTimeout,
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
	payload := c.buildMiimoPayload(text, opts, false)

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

// buildMiimoMessages 组装 messages：instruction 是 Miimo 唯一的风格通道，
// 占据首位（user role），待合成文本随后（assistant role）。整段与流式两
// 种模式共用，保证指令拼法一字不差。
func buildMiimoMessages(text string, opts VoiceOpts) []map[string]string {
	messages := make([]map[string]string, 0, 2)
	if instruction := strings.TrimSpace(opts.Instruction); instruction != "" {
		messages = append(messages, map[string]string{"role": "user", "content": instruction})
	}
	messages = append(messages, map[string]string{"role": "assistant", "content": text})
	return messages
}

// buildMiimoPayload 组装请求载荷，整段（stream=false）与流式（stream=true）
// 共用。音色/格式的默认与覆盖规则只有这一份：空音色与 cgSg 前缀（平台已
// 下线的预制音色 id）回落默认音色；整段默认 wav；流式固定 pcm——Miimo 流
// 式输出即 pcm16@24kHz 分片，qiuqiu 契约是本包只出裸 pcm16，WAV 封装留给
// 投递层（task 3.4），调用方在流式路径设置的 VoiceOpts.Format 不生效。
func (c *Client) buildMiimoPayload(text string, opts VoiceOpts, stream bool) map[string]interface{} {
	voice := strings.TrimSpace(opts.Voice)
	if voice == "" || strings.HasPrefix(voice, "cgSg") {
		voice = c.voice
	}
	format := defaultString(opts.Format, "wav")
	if stream {
		format = "pcm"
	}
	payload := map[string]interface{}{
		"model":    defaultString(c.model, "mimo-v2.5-tts"),
		"messages": buildMiimoMessages(text, opts),
		"audio": map[string]string{
			"format": format,
			"voice":  defaultString(voice, "冰糖"),
		},
	}
	if stream {
		payload["stream"] = true
	}
	return payload
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

// WithCircuitBreaker 替换默认熔断器（阈值/开窗可调；测试注入短开窗用）。
func (c *Client) WithCircuitBreaker(breaker *resilience.CircuitBreaker) *Client {
	if breaker != nil {
		c.breaker = breaker
	}
	return c
}
