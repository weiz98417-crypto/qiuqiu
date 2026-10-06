// local.go 承载本地 TTS 腿的 OpenAI 兼容 adapter（openspec/changes/
// tts-supply-switch）：POST {baseURL}/audio/speech（OpenAI 兼容端点形态，
// 自托管推荐 Fun-CosyVoice3，部署文档 docs/deploy/local-tts.md）。供应商
// 知识（端点/模型/音色/试合成探测）收敛在本 adapter 内；情感表现走
// instructions 字段（CosyVoice3 的 instruct 通道），VoiceOpts.Instruction
// 原样透传——映射表在部署文档，不在代码里写死供应商知识。
//
// 本地端点无标准流式形态：SynthesizeStream / SynthesizeStreamDetailed 以
// 「整段合成后单分片投出」实现（与 Miimo 流式失败回退的形态一致，投递层
// 已处理 WAV 单分片）；本地腿的价值在情感表现力与供给自主，不在首包延迟。
package tts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"qiuqiu/internal/resilience"
)

// probeUtterance 是健康探测的试合成文本：一句短语，足够让服务端完成
// 「模型已加载+全链可合成」的验证，又不至于拖慢探测周期。
const probeUtterance = "你好。"

// LocalClient 是本地 TTS 腿的 OpenAI 兼容 adapter。零值不可用；必须
// NewLocalClient(端点 URL) 构造——URL 由 QIUQIU_TTS_LOCAL_URL 注入，留空
// 即本地腿整体不存在（probe/开关都不可见）。
type LocalClient struct {
	baseURL    string
	model      string
	voice      string
	httpClient *http.Client
	breaker    *resilience.CircuitBreaker
}

func NewLocalClient(baseURL string) *LocalClient {
	return &LocalClient{
		baseURL:    strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		model:      "cosyvoice-v3",
		httpClient: &http.Client{Timeout: 30 * time.Second},
		breaker:    resilience.NewCircuitBreaker(3, 10*time.Second),
	}
}

func (c *LocalClient) WithModel(model string) *LocalClient {
	if strings.TrimSpace(model) != "" {
		c.model = strings.TrimSpace(model)
	}
	return c
}

func (c *LocalClient) WithVoice(voice string) *LocalClient {
	if strings.TrimSpace(voice) != "" {
		c.voice = strings.TrimSpace(voice)
	}
	return c
}

func (c *LocalClient) WithHTTPClient(httpClient *http.Client) *LocalClient {
	if httpClient != nil {
		c.httpClient = httpClient
	}
	return c
}

func (c *LocalClient) WithCircuitBreaker(breaker *resilience.CircuitBreaker) *LocalClient {
	if breaker != nil {
		c.breaker = breaker
	}
	return c
}

// Configured 报告本地腿是否已配置（端点非空）。
func (c *LocalClient) Configured() bool {
	return c != nil && strings.TrimSpace(c.baseURL) != ""
}

// Probe 是健康探测的一次尝试：试合成一句短句。成功=端点可达且模型可
// 合成；任何失败原样上抛（原因由探测层呈现给运营台）。
func (c *LocalClient) Probe(ctx context.Context) error {
	_, err := c.Synthesize(ctx, probeUtterance, VoiceOpts{})
	return err
}

func (c *LocalClient) synthesize(ctx context.Context, text string, opts VoiceOpts) (*SynthesizeResult, error) {
	if !c.Configured() {
		return nil, ErrNotConfigured
	}
	if err := c.breaker.Allow(time.Now().UTC()); err != nil {
		return nil, fmt.Errorf("local tts unavailable: %w", err)
	}
	payload := map[string]interface{}{
		"model":           c.model,
		"input":           text,
		"response_format": "wav",
	}
	if voice := strings.TrimSpace(opts.Voice); voice != "" {
		payload["voice"] = voice
	} else if strings.TrimSpace(c.voice) != "" {
		payload["voice"] = c.voice
	}
	// instructions 是情感指令通道（CosyVoice3 instruct）；不支持该扩展的
	// 兼容端点会忽略未知字段——表现力降级，功能不坏。
	if instruction := strings.TrimSpace(opts.Instruction); instruction != "" {
		payload["instructions"] = instruction
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/audio/speech", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "audio/wav")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.breaker.Failure(time.Now().UTC())
		return nil, fmt.Errorf("local tts request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		c.breaker.Failure(time.Now().UTC())
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("local tts error %d: %s", resp.StatusCode, string(errBody))
	}
	audio, err := io.ReadAll(resp.Body)
	if err != nil {
		c.breaker.Failure(time.Now().UTC())
		return nil, fmt.Errorf("local tts read: %w", err)
	}
	if len(audio) == 0 {
		c.breaker.Failure(time.Now().UTC())
		return nil, fmt.Errorf("local tts returned no audio")
	}
	c.breaker.Success()
	return &SynthesizeResult{AudioData: audio, MimeType: "audio/wav"}, nil
}

// Synthesize 整段合成（Synthesizer 接口实现）。
func (c *LocalClient) Synthesize(ctx context.Context, text string, opts VoiceOpts) (*SynthesizeResult, error) {
	if c == nil {
		return nil, ErrNotConfigured
	}
	return c.synthesize(ctx, text, opts)
}

// SynthesizeStream 是 Synthesizer 接口的流式位：本地端点无标准流式，整段
// 合成后以单个 WAV 分片投出（channel 由本方法持有，合成结束关闭）。
func (c *LocalClient) SynthesizeStream(ctx context.Context, text string, opts VoiceOpts) (<-chan []byte, error) {
	if c == nil || !c.Configured() {
		return nil, ErrNotConfigured
	}
	ch := make(chan []byte, 1)
	go func() {
		defer close(ch)
		result, err := c.synthesize(ctx, text, opts)
		if err != nil {
			return
		}
		ch <- result.AudioData
	}()
	return ch, nil
}

// SynthesizeStreamDetailed 是 StreamingSynthesizer 能力接口的实现：整段
// 合成、单分片回调（非 degraded——这不是降级，是本地腿的常态形态）。
func (c *LocalClient) SynthesizeStreamDetailed(ctx context.Context, text string, opts VoiceOpts, onChunk func(StreamChunk) error) error {
	if c == nil || !c.Configured() {
		return ErrNotConfigured
	}
	if onChunk == nil {
		onChunk = func(StreamChunk) error { return nil }
	}
	result, err := c.synthesize(ctx, text, opts)
	if err != nil {
		return err
	}
	return onChunk(StreamChunk{Data: result.AudioData, MimeType: result.MimeType})
}
