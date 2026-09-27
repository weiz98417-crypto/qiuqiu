package main

// 轮次检测转发腿（voice-turn-detection 决策 c）：客户端 model 插槽在静默
// 累计 600ms 起经既有 WS 发 turn_query 提前问，这里把它转发给 turn sidecar
// （LiveKit EOU 多语版 ONNX，backend/cmd/turn-sidecar），结论以 turn_result
// 回同一 utteranceId。纪律与气氛旁路（ambient_relay.go）同族：
//   - sidecar 未配置/超时/熔断一律回 isComplete:null——客户端插槽收到 null
//     下探静默档，降级链天然闭环，客户端零特殊分支；
//   - 转发在独立协程完成，绝不阻塞读循环（ASR 分片同路复用这条连接）；
//   - 失败不报错风暴：真正到达 sidecar 的失败才记日志，熔断开时静默回 null。

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"qiuqiu/internal/companion"
	"qiuqiu/internal/resilience"
)

// defaultTurnSidecarTimeout 是 sidecar 调用的默认预算：提前问的窗口是
// 静默 600ms 到生效阈值（默认 1400ms），500ms 预算保证结论能在阈值帧前
// 赶回（赶不回客户端按超时下探静默档，语义一致）。
const defaultTurnSidecarTimeout = 500 * time.Millisecond

// turnRelayMaxInFlight 是单连接转发在途上限：客户端节流下正常到不了这个
// 数；异常客户端刷查询时饱和即回 null，不排队不堆积。
const turnRelayMaxInFlight = 4

// ErrTurnSidecarNotConfigured 表示未配置 sidecar 端点（接线层据此直接回
// isComplete:null，model 插槽整体降级）。
var ErrTurnSidecarNotConfigured = errors.New("turn sidecar endpoint is not configured")

// turnSidecarClient 调 turn sidecar 的 POST /turn：文本进、轮次概率出。
// 与 internal/ambient.Client 同族：短超时 + 熔断，失败返回错误由转发层兜底。
type turnSidecarClient struct {
	baseURL    string
	httpClient *http.Client
	breaker    *resilience.CircuitBreaker
}

// newTurnSidecarClient 构造指向 sidecar 的客户端；baseURL 为空表示 model
// 插槽降级（Enabled=false，转发层直接回 null）。熔断 3 次失败开 10 秒。
func newTurnSidecarClient(baseURL string, timeout time.Duration) *turnSidecarClient {
	if timeout <= 0 {
		timeout = defaultTurnSidecarTimeout
	}
	return &turnSidecarClient{
		baseURL:    strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		httpClient: &http.Client{Timeout: timeout},
		breaker:    resilience.NewCircuitBreaker(3, 10*time.Second),
	}
}

// Enabled 报告 model 插槽是否配置了 sidecar 端点。
func (c *turnSidecarClient) Enabled() bool {
	return c != nil && c.baseURL != ""
}

// turnPrediction 是 sidecar 的判定结论：probability 透传给客户端作观测。
type turnPrediction struct {
	Probability float64
	IsComplete  bool
}

// Predict 把一段话轮文本送 sidecar 判定。调用失败返回错误，转发层回 null。
func (c *turnSidecarClient) Predict(ctx context.Context, text string) (turnPrediction, error) {
	if !c.Enabled() {
		return turnPrediction{}, ErrTurnSidecarNotConfigured
	}
	now := time.Now().UTC()
	if err := c.breaker.Allow(now); err != nil {
		return turnPrediction{}, fmt.Errorf("turn sidecar unavailable: %w", err)
	}
	body, err := json.Marshal(map[string]interface{}{"text": text})
	if err != nil {
		return turnPrediction{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/turn", bytes.NewReader(body))
	if err != nil {
		return turnPrediction{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.breaker.Failure(now)
		return turnPrediction{}, fmt.Errorf("turn sidecar request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		// 4xx 是输入侧错误（如归一化后空文本），sidecar 服务本身健康——
		// 不计熔断：否则一条连接发 3 条空白文本就能让全服轮次检测降级
		// 10 秒。只有服务性故障（5xx）才计入。
		if resp.StatusCode >= http.StatusInternalServerError {
			c.breaker.Failure(now)
			return turnPrediction{}, fmt.Errorf("turn sidecar error %d", resp.StatusCode)
		}
		return turnPrediction{}, fmt.Errorf("turn sidecar rejected input %d", resp.StatusCode)
	}
	var payload struct {
		Probability float64 `json:"probability"`
		IsComplete  bool    `json:"isComplete"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		c.breaker.Failure(now)
		return turnPrediction{}, fmt.Errorf("turn sidecar decode: %w", err)
	}
	c.breaker.Success()
	return turnPrediction{Probability: payload.Probability, IsComplete: payload.IsComplete}, nil
}

// handleTurnQuery 处理 turn_query 上行：校验后即返回，转发与下行在独立
// 协程完成（读循环不等 sidecar）。utteranceId 为空的坏消息直接丢弃。
func (c *watchConnection) handleTurnQuery(utteranceID, text string) {
	if utteranceID == "" {
		return
	}
	sidecar := c.deps.turnSidecar
	if sidecar == nil || !sidecar.Enabled() {
		// 未配置端点：同步快回 null——model 插槽整体降级，静默档自决。
		c.sendTurnResult(utteranceID, turnPrediction{}, false, 0)
		return
	}
	select {
	case c.turnInFlight <- struct{}{}:
	default:
		// 在途饱和：不反压读循环，按不可用回 null（客户端下探静默档）。
		c.sendTurnResult(utteranceID, turnPrediction{}, false, 0)
		return
	}
	go c.forwardTurnQuery(sidecar, utteranceID, text)
}

// forwardTurnQuery 在转发协程内完成 sidecar 调用与 turn_result 下行：任何
// 失败（超时/熔断/坏响应）都回 isComplete:null，与降级语义同形。用独立
// 预算的 background context：连接拆除不打断在途短调用，写失败随连接消亡。
func (c *watchConnection) forwardTurnQuery(sidecar *turnSidecarClient, utteranceID, text string) {
	defer func() { <-c.turnInFlight }()
	startedAt := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), defaultTurnSidecarTimeout)
	defer cancel()
	prediction, err := sidecar.Predict(ctx, text)
	if err != nil {
		if !errors.Is(err, ErrTurnSidecarNotConfigured) {
			// 只有真到达 sidecar 的失败才记日志（熔断开的快速失败不刷屏）。
			log.Printf("turn sidecar query error: user=%q match=%q utterance=%q err=%v",
				c.identity.Get(), c.matchID, utteranceID, err)
		}
		c.sendTurnResult(utteranceID, turnPrediction{}, false, time.Since(startedAt))
		return
	}
	c.sendTurnResult(utteranceID, prediction, true, time.Since(startedAt))
}

// sendTurnResult 下行 turn_result；resolved=false 时 isComplete 固定 null
// （客户端插槽 null 语义=下探静默档）。结论同时进 utterance 级缓冲
// （operations-turn-replay）：同 utterance 多问以最后一问为准，trace 诞生
// 后由 turn_decided attach。转写文本不入日志（与 duplex_event 同纪律）。
func (c *watchConnection) sendTurnResult(utteranceID string, prediction turnPrediction, resolved bool, elapsed time.Duration) {
	decision := &companion.VoiceTurnDecision{Source: "unavailable", QueryLatencyMS: int(elapsed.Milliseconds())}
	if resolved {
		complete := prediction.IsComplete
		decision.IsComplete = &complete
		decision.Source = "model"
	}
	c.recordTurnVerdict(utteranceID, decision)
	payload := map[string]interface{}{
		"type":        "turn_result",
		"utteranceId": utteranceID,
		"isComplete":  nil,
	}
	if resolved {
		payload["isComplete"] = prediction.IsComplete
		payload["probability"] = prediction.Probability
	}
	_ = c.writer.SendJSON(payload)
}
