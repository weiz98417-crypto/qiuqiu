// Package router implements the LLM intent router (openspec/changes/
// intent-router, ADR-0009): when the keyword table misses, the turn goes to
// one mimo-v2.5 function call that returns intent, slots, confidence and a
// suggested natural reply. The router only classifies — it never writes Match
// Facts; every downstream path stays deterministic or guard-realized.
package router

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"qiuqiu/internal/openaicompat"
)

// Default settings per design.md locked decision 1: the MiMo platform with
// mimo-v2.5 (empirically validated: correct route + 3.7s; -pro misclassifies
// the key persisted-claim sample and is 3x slower). ROUTER_API_KEY falling
// back to MIMO_API_KEY keeps ops to one key; an empty key disables the
// router layer entirely and the agent keeps today's keyword-miss behavior.
const (
	DefaultBaseURL = "https://api.xiaomimimo.com/v1"
	DefaultModel   = "mimo-v2.5"
	DefaultTimeout = 6 * time.Second
	fallbackKeyEnv = "MIMO_API_KEY"
)

// Config carries the router client settings; FromEnv is the production path.
type Config struct {
	BaseURL string
	APIKey  string
	Model   string
	Timeout time.Duration
}

// NewConfig reads the ROUTER_* env trio (defaults per locked decision 1).
func NewConfig(getenv func(string) string) Config {
	timeout := DefaultTimeout
	if raw := strings.TrimSpace(getenv("ROUTER_TIMEOUT_MS")); raw != "" {
		if millis := parseMillis(raw); millis > 0 {
			timeout = time.Duration(millis) * time.Millisecond
		}
	}
	apiKey := strings.TrimSpace(getenv("ROUTER_API_KEY"))
	if apiKey == "" {
		apiKey = strings.TrimSpace(getenv(fallbackKeyEnv))
	}
	baseURL := strings.TrimSpace(getenv("ROUTER_BASE_URL"))
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	model := strings.TrimSpace(getenv("ROUTER_MODEL"))
	if model == "" {
		model = DefaultModel
	}
	return Config{BaseURL: baseURL, APIKey: apiKey, Model: model, Timeout: timeout}
}

func parseMillis(raw string) int64 {
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 || value > 60_000 {
		return 0
	}
	return value
}

// Client is the single-call router. It is deliberately stdlib-only: one
// attempt, one timeout, no retry stacking on the reply latency (locked
// decision 6).
type Client struct {
	baseURL    string
	apiKey     string
	model      string
	httpClient *http.Client
}

func NewClient(config Config) *Client {
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &Client{
		baseURL:    strings.TrimRight(config.BaseURL, "/"),
		apiKey:     config.APIKey,
		model:      config.Model,
		httpClient: &http.Client{Timeout: timeout},
	}
}

// Enabled reports whether the router layer is configured. An unset key (CI,
// evals) keeps the agent on the legacy keyword-miss path (design.md: eval
// compatibility).
func (c *Client) Enabled() bool {
	return c != nil && c.apiKey != ""
}

// Request is one keyword-miss turn to classify.
type Request struct {
	Text    string
	Context string
}

// Result mirrors the route_turn function-call arguments.
type Result struct {
	Intent     string  `json:"intent"`
	Player     string  `json:"player,omitempty"`
	Team       string  `json:"team,omitempty"`
	Score      string  `json:"score,omitempty"`
	Confidence float64 `json:"confidence"`
	Reply      string  `json:"reply,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type toolFunction struct {
	Name       string         `json:"name"`
	Parameters map[string]any `json:"parameters"`
}

type toolSpec struct {
	Type     string       `json:"type"`
	Function toolFunction `json:"function"`
}

type routeRequestPayload struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Tools       []toolSpec    `json:"tools"`
	ToolChoice  any           `json:"tool_choice"`
	MaxTokens   int           `json:"max_tokens"`
	Temperature float64       `json:"temperature"`
	Thinking    struct {
		Type string `json:"type"`
	} `json:"thinking"`
}

type routeResponsePayload struct {
	Choices []struct {
		Message struct {
			ToolCalls []struct {
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
}

// RouteTurnToolName is the single function the router model must call.
const RouteTurnToolName = "route_turn"

// routableIntents is the intent vocabulary the router schema offers — the
// single source both the tool schema and the vocabulary lock test read.
// match_reaction is proactive-only and deliberately absent from user turns.
var routableIntents = []string{
	"smalltalk",
	"schedule_question",
	"match_status_question",
	"recent_event_question",
	"follow_up_question",
	"player_question",
	"match_fact_claim",
	"emotion_reaction",
	"personal_share",
	"control_command",
	"unknown",
}

// RoutableIntents exposes the router intent vocabulary (the companion
// package's vocabulary lock test reads it to keep routedTurnIntent 1:1).
func RoutableIntents() []string {
	return append([]string(nil), routableIntents...)
}

// SystemPrompt exposes the hardcoded routing prompt read-only: the intent
// registry (openspec/changes/intent-registry) mirrors each intent definition
// line and asserts at startup/CI that the mirror still matches — the prompt
// itself stays byte-identical (ADR-0009 behavior lock).
func SystemPrompt() string {
	return routeSystemPrompt
}

// routeTurnParameters is the tool schema: the 12 backend intents (the router
// prompt documents 坚持主张 as match_fact_claim), slots, confidence and the
// reply suggestion used only for non-fact intents.
func routeTurnParameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"intent": map[string]any{
				"type":        "string",
				"enum":        routableIntents,
				"description": "用户这回合的意图，必须从这个枚举里选",
			},
			"player":     map[string]any{"type": "string", "description": "提到的球员名，没有则空串"},
			"team":       map[string]any{"type": "string", "description": "提到的球队名，没有则空串"},
			"score":      map[string]any{"type": "string", "description": "提到的比分（如 2-1），没有则空串"},
			"confidence": map[string]any{"type": "number", "description": "意图判断置信度 0 到 1"},
			"reply":      map[string]any{"type": "string", "description": "仅闲聊类意图给一句自然回复建议（最多两句60字），事实类与控制类留空"},
		},
		"required": []string{"intent", "confidence"},
	}
}

// routeSystemPrompt: 12 intent definitions (match_reaction is proactive-only
// and unreachable from user turns), fact discipline, three few-shot rows and
// the 分类不回答 constraint from design.md.
const routeSystemPrompt = `你是陪看足球助手"球球"的意图路由器。你的唯一任务是分类用户这句话，不得虚构任何比赛事实，只分类不回答。
意图定义：
- smalltalk：陪你聊天、打招呼、问你在干嘛、让你陪着看球。
- schedule_question：问赛程、今天/明天有什么比赛。
- match_status_question：问当前比分或比赛进行情况。
- recent_event_question：问刚才发生了什么（谁进的、上一球）。
- follow_up_question：顺着上一条追问（那球呢、然后呢、谁策动）。
- player_question：问某个球员的表现或进球。
- match_fact_claim：用户主张一个比赛事实（进球/比分/破门）。用户坚持主张（明明进了、真的进了、确实进了）也算 match_fact_claim。
- emotion_reaction：看球情绪反应（漂亮、牛、紧张、离谱、真的假的）。
- personal_share：用户分享自己的生活（我赢了、我累了、我喜欢某队）。
- control_command：让球球别说/少说/安静。
- unknown：以上都不适合。
置信度低于 0.7 时直接给 unknown。闲聊类（smalltalk/emotion_reaction/personal_share）额外给一句自然回复建议 reply，口语、最多两句60字、绝不提具体比分球员等比赛事实；其余意图 reply 留空。
示例：
用户说"球进了" → match_fact_claim（confidence 0.9，reply 空）
用户说"你在干嘛" → smalltalk（confidence 0.9，reply "我在盯着比分呢，陪你一起看。"）
用户说"明明进了，裁判瞎了吗" → match_fact_claim（confidence 0.85，reply 空）`

// Route classifies one turn. Exactly one attempt: any transport/parse failure
// returns an error and the caller degrades to the legacy keyword-miss path
// (locked decision 6 — never a console error).
func (c *Client) Route(ctx context.Context, req Request) (Result, error) {
	if !c.Enabled() {
		return Result{}, fmt.Errorf("router disabled")
	}
	text := strings.TrimSpace(req.Text)
	if text == "" {
		return Result{}, fmt.Errorf("router: empty text")
	}
	messages := []chatMessage{
		{Role: "system", Content: routeSystemPrompt},
		{Role: "user", Content: text},
	}
	if contextSummary := strings.TrimSpace(req.Context); contextSummary != "" {
		messages = append(messages, chatMessage{Role: "user", Content: contextSummary})
	}
	payload := routeRequestPayload{
		Model:    c.model,
		Messages: messages,
		Tools: []toolSpec{{
			Type: "function",
			Function: toolFunction{
				Name:       RouteTurnToolName,
				Parameters: routeTurnParameters(),
			},
		}},
		ToolChoice: map[string]any{
			"type":     "function",
			"function": map[string]any{"name": RouteTurnToolName},
		},
		MaxTokens:   200,
		Temperature: 0.1,
	}
	payload.Thinking.Type = "disabled"

	body, err := json.Marshal(payload)
	if err != nil {
		return Result{}, fmt.Errorf("router encode: %w", err)
	}
	// 单次调用（ADR-0009）：传输层零重试，6s 超时由 ctx 承载。
	startedAt := time.Now()
	respBody, err := openaicompat.Post(ctx, c.httpClient, c.endpoint(), "/chat/completions", body, 1<<20)
	if err != nil {
		return Result{}, fmt.Errorf("router call: %w", err)
	}
	var parsed routeResponsePayload
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return Result{}, fmt.Errorf("router decode: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return Result{}, fmt.Errorf("router returned no choices")
	}
	for _, toolCall := range parsed.Choices[0].Message.ToolCalls {
		if toolCall.Function.Name != RouteTurnToolName {
			continue
		}
		result, err := parseRouteResult(toolCall.Function.Arguments)
		if err != nil {
			return Result{}, err
		}
		return result, nil
	}
	return Result{}, fmt.Errorf("router returned no %s tool call (elapsed %s)", RouteTurnToolName, time.Since(startedAt).Round(time.Millisecond))
}

func parseRouteResult(arguments string) (Result, error) {
	var result Result
	if err := json.Unmarshal([]byte(arguments), &result); err != nil {
		return Result{}, fmt.Errorf("router arguments: %w", err)
	}
	result.Intent = strings.TrimSpace(result.Intent)
	if result.Intent == "" {
		return Result{}, fmt.Errorf("router arguments: empty intent")
	}
	if result.Confidence < 0 {
		result.Confidence = 0
	}
	if result.Confidence > 1 {
		result.Confidence = 1
	}
	return result, nil
}

func (c *Client) endpoint() openaicompat.Endpoint {
	return openaicompat.Endpoint{BaseURL: c.baseURL, APIKey: c.apiKey, Model: c.model}
}

// 待迁标记（openspec/changes/structured-tool-seam）：本文件自抄的
// function-call payload/响应解析（routeRequestPayload/routeResponsePayload/
// parseRouteResult）是 internal/structured 深模块的迁移候选——迁移会字节级
// 改动调用载荷，须带 boundary 路由 eval 重验，单独立项，本轮不动。
