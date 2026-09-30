// stream.go 承载 Miimo TTS 的真流式合成（voice-streaming-delivery task 3.2）：
// mimo-v2.5-tts 基础模型支持 stream:true 的 SSE 流式，输出 pcm16 24kHz 单声道
// base64 分片（可拼接）；voicedesign/voiceclone 的假流式不碰。流式失败
// （SSE 断流/空闲超时/非 200/零分片）时 adapter 内回退整段 Synthesize，
// 熔断沿用 Client 既有 breaker，调用方无感。流式分片是裸 pcm16，不做 WAV
// 封装——封装是投递层（task 3.4）的事。
package tts

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"qiuqiu/internal/openaicompat"
)

// MimeTypeStreamPCM 标识流式分片的音频格式：裸 pcm16（有符号 16 位小端）、
// 24kHz、单声道、无 WAV 封装。rate 参数是投递层组 WAV 头所需的采样率事实。
const MimeTypeStreamPCM = "audio/pcm;rate=24000"

// defaultStreamIdleTimeout 是流式空闲超时默认值：连续这么久没有任何 SSE
// 分片到达（含首片），判定流已死，回退整段合成。10s 偏保守——宁可晚一点
// 回退，不误伤慢启动的真流。
const defaultStreamIdleTimeout = 10 * time.Second

// errStreamIdleTimeout 是内部空闲超时的 cancel cause：用它把「我们主动
// 掐掉的死流」与「调用方取消」区分开（前者回退，后者必须如实让路）。
var errStreamIdleTimeout = errors.New("tts stream idle timeout")

// errStreamBroken 是 SSE 流异常终结（EOF 没等到 [DONE]、读错误、分片
// 解不开）的统一标记。
var errStreamBroken = errors.New("tts stream ended unexpectedly")

// StreamChunk 是流式合成回调收到的一个音频分片。
type StreamChunk struct {
	// Data 是音频字节：流式分片为裸 pcm16（MimeTypeStreamPCM）；回退分片
	// 是整段 Synthesize 的产物（默认 WAV 容器），以 MimeType 区分。
	Data []byte
	// MimeType 标识本分片格式："audio/pcm;rate=24000"（流式）或整段
	// Synthesize 的返回（"audio/wav"）。
	MimeType string
	// Degraded 为 true 表示流式失败、本分片是回退整段合成的全量音频：
	// 回调方必须用本分片**替换**此前为本流缓冲的所有分片（此前分片只是
	// 残缺前缀，拼接会重复内容），如此调用方才真正「无感」。
	Degraded bool
}

// StreamingSynthesizer 是流式合成的增强能力接口（可选能力模式）：与
// Synthesizer 组合而非修改它——Synthesizer 的实现遍及 cmd/server 的测试
// 替身，改形状即全量波及。需要句粒度下发与降级可观测的调用方（投递层
// task 3.4/3.5）以类型断言取用：
//
//	if streaming, ok := synthesizer.(tts.StreamingSynthesizer); ok { ... }
//
// 不具备该能力的合成器（含测试替身）自然落回整段 Synthesize。
type StreamingSynthesizer interface {
	// SynthesizeStreamDetailed 流式合成：每个 pcm16 分片经 onChunk 交付，
	// onChunk 返回 error 表示调用方要中止（不再回退，错误如实上抛）。
	// 流式失败且尚可回退时，adapter 内降级整段 Synthesize，以单个
	// Degraded=true 的分片回调全量音频。合成根本无法开始（未配置/熔断
	// 打开）或回退也失败时返回 error。
	SynthesizeStreamDetailed(ctx context.Context, text string, opts VoiceOpts, onChunk func(StreamChunk) error) error
}

// SynthesizeStreamDetailed 是 *Client 对 StreamingSynthesizer 的实现，见
// 接口注释。分片按到达顺序回调；同一文本只会有一次 Degraded=true 的收尾。
func (c *Client) SynthesizeStreamDetailed(ctx context.Context, text string, opts VoiceOpts, onChunk func(StreamChunk) error) error {
	if c == nil {
		return ErrNotConfigured
	}
	if onChunk == nil {
		onChunk = func(StreamChunk) error { return nil }
	}
	if len(c.mockAudio) > 0 {
		// mock 替身：回放注入的确定性音频，单分片、不降级，不发网络请求。
		return onChunk(StreamChunk{Data: append([]byte(nil), c.mockAudio...), MimeType: "audio/mpeg"})
	}
	if strings.TrimSpace(c.apiKey) == "" {
		return ErrNotConfigured
	}
	if err := c.breaker.Allow(time.Now().UTC()); err != nil {
		return fmt.Errorf("tts unavailable: %w", err)
	}

	_, err, aborted := c.streamOnce(ctx, text, opts, onChunk)
	if err == nil {
		c.breaker.Success()
		return nil
	}
	// 调用方经 onChunk 主动中止（含打断 cancel 的转发）：不回退、不记熔断。
	// 打断的调用没有结论——半开探测名额照旧归还，否则熔断停在 half_open
	// 假死（Allow 永远拒绝）。
	if aborted {
		c.breaker.ReleaseProbe()
		return err
	}
	// 调用方取消了整个合成（打断语义，task 3.5 依赖）：不回退、不记熔断，
	// 剩余音频不再合成——回退会违背「剩余句不下发」。探测名额同上归还。
	if ctx.Err() != nil {
		c.breaker.ReleaseProbe()
		return ctx.Err()
	}
	// 流式失败（断流/超时/非 200/零分片）：记一次熔断失败后回退整段。
	// 回退走 Synthesize，其内部对 breaker 的记功/记账照旧——连续失败会
	// 把熔断打进 open，届时 Allow 直接拒绝回退，整链如实报错。
	c.breaker.Failure(time.Now().UTC())
	result, ferr := c.Synthesize(ctx, text, opts)
	if ferr != nil {
		return ferr
	}
	return onChunk(StreamChunk{Data: result.AudioData, MimeType: result.MimeType, Degraded: true})
}

// SynthesizeStream 是 Synthesizer 接口的流式位（签名锁死，见 synthesizer.go）：
// 现已变现役——真流式分片按序投递；流式失败时回退整段，以单个分片投出
// 全量音频（channel 形态无元数据，降级标记只在 SynthesizeStreamDetailed
// 可见）。起始性失败（未配置）同步返回 error；channel 由本方法持有并在
// 合成结束后关闭，ch 只在错误为 nil 时有效。
func (c *Client) SynthesizeStream(ctx context.Context, text string, opts VoiceOpts) (<-chan []byte, error) {
	if c == nil {
		return nil, ErrNotConfigured
	}
	// 结构性检查同步返回，保持既有调用方对 ErrNotConfigured 的预期；熔断
	// 状态留给 SynthesizeStreamDetailed 判定（Allow 消耗半开探测名额，
	// 不能在这里预支一次）。
	if len(c.mockAudio) == 0 && strings.TrimSpace(c.apiKey) == "" {
		return nil, ErrNotConfigured
	}
	ch := make(chan []byte, 4)
	go func() {
		defer close(ch)
		_ = c.SynthesizeStreamDetailed(ctx, text, opts, func(chunk StreamChunk) error {
			select {
			case ch <- chunk.Data:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	return ch, nil
}

// streamOnce 发起一次 SSE 流式合成，把分片逐个交给 onChunk。返回已交付
// 分片数；err 非 nil 说明流未正常走完，aborted 标记错误源自 onChunk
// （调用方主动中止，外层据此跳过回退）。
func (c *Client) streamOnce(ctx context.Context, text string, opts VoiceOpts, onChunk func(StreamChunk) error) (delivered int, err error, aborted bool) {
	streamCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	payload := c.buildMiimoPayload(text, opts, true)
	body, _ := json.Marshal(payload)
	url := fmt.Sprintf("%s/chat/completions", strings.TrimRight(c.baseURL, "/"))
	httpReq, rerr := http.NewRequestWithContext(streamCtx, http.MethodPost, url, bytes.NewReader(body))
	if rerr != nil {
		return 0, rerr, false
	}
	openaicompat.SetAuthHeaders(httpReq, openaicompat.Endpoint{BaseURL: c.baseURL, APIKey: c.apiKey, Model: c.model})
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	// 空闲超时：定时器直接 cancel 请求（context cancel 会打断阻塞中的
	// body 读），每收到一行就续期。cause 用 errStreamIdleTimeout 标记，
	// 与调用方取消严格区分。
	idleFor := c.streamIdleTimeout
	if idleFor <= 0 {
		idleFor = defaultStreamIdleTimeout
	}
	idle := time.AfterFunc(idleFor, func() { cancel(errStreamIdleTimeout) })
	defer idle.Stop()

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		if errors.Is(context.Cause(streamCtx), errStreamIdleTimeout) {
			return delivered, errStreamIdleTimeout, false
		}
		return delivered, err, false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return delivered, fmt.Errorf("tts stream error %d: %s", resp.StatusCode, string(errBody)), false
	}

	sawDone := false
	// consume 消化一个 SSE 事件（攒起的 data 行，兼容单事件多 data 行的
	// 标准形态）；done=事件是 [DONE]，err=分片解码或回调中止。
	consume := func(lines []string) (done bool, err error) {
		if len(lines) == 0 {
			return false, nil
		}
		data := strings.Join(lines, "\n")
		if strings.TrimSpace(data) == "[DONE]" {
			return true, nil
		}
		audio, derr := decodeStreamDelta(data)
		if derr != nil {
			return false, derr
		}
		if len(audio) == 0 {
			return false, nil
		}
		if cerr := onChunk(StreamChunk{Data: audio, MimeType: MimeTypeStreamPCM}); cerr != nil {
			aborted = true
			return false, cerr
		}
		delivered++
		return false, nil
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 4<<20)
	var dataLines []string
	for scanner.Scan() {
		idle.Reset(idleFor)
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "data:"):
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		case strings.TrimSpace(line) != "":
			// event:/retry:/注释行：与音频无关，忽略。
		default:
			// 空行是 SSE 事件边界。
			done, cerr := consume(dataLines)
			dataLines = nil
			if cerr != nil {
				return delivered, cerr, aborted
			}
			if done {
				sawDone = true
			}
		}
		if sawDone {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		if errors.Is(context.Cause(streamCtx), errStreamIdleTimeout) {
			return delivered, errStreamIdleTimeout, false
		}
		return delivered, fmt.Errorf("%w: %v", errStreamBroken, err), false
	}
	// 流以 EOF 收尾：先消化攒着未遇空行的事件，再判定终结形态。
	if _, cerr := consume(dataLines); cerr != nil {
		return delivered, cerr, aborted
	}
	if !sawDone {
		// 服务器没发 [DONE] 就断了连接：按断流处理。
		return delivered, errStreamBroken, false
	}
	if delivered == 0 {
		// 流「正常」走完但零分片：按异常处理（供应商空转/静默失败），
		// 回退整段，调用方总能拿到音频。
		return delivered, errStreamBroken, false
	}
	return delivered, nil, false
}

// decodeStreamDelta 解析 openai-compat chat completions 流式分片，取
// choices[].delta.audio.data 的 base64 音频。无音频字段的 delta（角色帧、
// 收尾帧等）返回 (nil, nil) 由调用方跳过。
func decodeStreamDelta(data string) ([]byte, error) {
	var env struct {
		Choices []struct {
			Delta struct {
				Audio struct {
					Data string `json:"data"`
				} `json:"audio"`
			} `json:"delta"`
		} `json:"choices"`
	}
	if err := json.Unmarshal([]byte(data), &env); err != nil {
		return nil, fmt.Errorf("%w: decode delta: %v", errStreamBroken, err)
	}
	for _, choice := range env.Choices {
		if payload := strings.TrimSpace(choice.Delta.Audio.Data); payload != "" {
			return decodeBase64(payload)
		}
	}
	return nil, nil
}

// WithStreamIdleTimeout 覆盖流式空闲超时（默认 10s）；非正值被忽略。
func (c *Client) WithStreamIdleTimeout(idleFor time.Duration) *Client {
	if idleFor > 0 {
		c.streamIdleTimeout = idleFor
	}
	return c
}
