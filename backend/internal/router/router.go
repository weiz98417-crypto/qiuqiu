// Package router implements the LLM intent router (openspec/changes/
// intent-router, ADR-0009): when the keyword table misses, the turn goes to
// one mimo-v2.5 function call that returns intent, slots, confidence and a
// suggested natural reply. The router only classifies — it never writes Match
// Facts; every downstream path stays deterministic or guard-realized.
package router

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"qiuqiu/internal/structured"
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
// decision 6). 传输与解析经 structured 深模块（留尾 3.6 兑现）。
type Client struct {
	apiKey     string
	structured *structured.Client
}

func NewClient(config Config) *Client {
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &Client{
		apiKey:     config.APIKey,
		structured: structured.NewClientWithTimeout(config.BaseURL, config.APIKey, config.Model, timeout),
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

// Result mirrors the route_turn function-call arguments. jsonschema tag 由
// structured.Extract 反射成工具参数 schema——invopop 的枚举语法是重复的
// `enum=值` 指令，顺序与迁移前手写 schema 逐项一致（router_schema_test.go
// 锁定），description 保留原中文文案。
type Result struct {
	Intent string `json:"intent" jsonschema:"description=用户这回合的意图，必须从这个枚举里选,required,enum=smalltalk,enum=schedule_question,enum=match_status_question,enum=recent_event_question,enum=follow_up_question,enum=player_question,enum=match_fact_claim,enum=emotion_reaction,enum=personal_share,enum=control_command,enum=reminder_request,enum=knowledge_question,enum=unknown"`
	Player string `json:"player,omitempty" jsonschema_description:"提到的球员名，没有则空串"`
	Team   string `json:"team,omitempty" jsonschema_description:"提到的球队名，没有则空串"`
	Score  string `json:"score,omitempty" jsonschema_description:"提到的比分（如 2-1），没有则空串"`
	Confidence float64 `json:"confidence" jsonschema:"description=意图判断置信度 0 到 1,required"`
	Reply      string  `json:"reply,omitempty" jsonschema_description:"仅闲聊类意图给一句自然回复建议（最多两句60字），事实类与控制类留空"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// RouteTurnToolName is the single function the router model must call.
const RouteTurnToolName = "route_turn"

// RoutableIntents exposes the router intent vocabulary——单一来源是 Result.
// Intent 字段的 jsonschema tag（重复 enum= 指令即枚举，schema 反射与词汇
// 锁测试同源）。match_reaction is proactive-only and deliberately absent
// from user turns.
func RoutableIntents() []string {
	field, ok := reflect.TypeOf(Result{}).FieldByName("Intent")
	if !ok {
		return nil
	}
	values := []string{}
	for _, part := range strings.Split(field.Tag.Get("jsonschema"), ",") {
		if strings.HasPrefix(part, "enum=") {
			values = append(values, strings.TrimPrefix(part, "enum="))
		}
	}
	if len(values) == 0 {
		return nil
	}
	return values
}

// SystemPrompt exposes the hardcoded routing prompt read-only: the intent
// registry (openspec/changes/intent-registry) mirrors each intent definition
// line and asserts at startup/CI that the mirror still matches — the prompt
// itself stays byte-identical (ADR-0009 behavior lock).
func SystemPrompt() string {
	return routeSystemPrompt
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
- reminder_request：让我在开球前提醒你（开球前叫我、赛前提醒我）。
- knowledge_question：问足球规则或赛制知识（越位是什么、积分怎么算）。
- unknown：以上都不适合。
置信度低于 0.7 时直接给 unknown。闲聊类（smalltalk/emotion_reaction/personal_share）额外给一句自然回复建议 reply，口语、最多两句60字、绝不提具体比分球员等比赛事实；其余意图 reply 留空。
示例：
用户说"球进了" → match_fact_claim（confidence 0.9，reply 空）
用户说"你在干嘛" → smalltalk（confidence 0.9，reply "我在盯着比分呢，陪你一起看。"）
用户说"明明进了，裁判瞎了吗" → match_fact_claim（confidence 0.85，reply 空）`

// Route classifies one turn. Exactly one attempt: any transport/parse failure
// returns an error and the caller degrades to the legacy keyword-miss path
// (locked decision 6 — never a console error). 迁移说明（留尾 3.6 兑现）：
// payload/响应解析全部走 structured.Extract——schema 由 Result 的
// jsonschema tag 反射生成（router_schema_test.go 锁形状），单次调用、6s
// 超时、thinking disabled、MaxTokens 200、温度 0.1 均不变。
func (c *Client) Route(ctx context.Context, req Request) (Result, error) {
	if !c.Enabled() {
		return Result{}, fmt.Errorf("router disabled")
	}
	text := strings.TrimSpace(req.Text)
	if text == "" {
		return Result{}, fmt.Errorf("router: empty text")
	}
	result, err := structured.Extract[Result](ctx, c.structured, structured.CallOptions{
		SystemPrompt:   routeSystemPrompt,
		UserContent:    text,
		ContextMessage: strings.TrimSpace(req.Context),
		ToolName:       RouteTurnToolName,
		Temperature:    0.1,
		MaxTokens:      200,
	})
	if err != nil {
		return Result{}, err
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

// 迁移完成（semantic-memory 兑现 structured-tool-seam 留尾 3.6）：自抄的
// function-call payload/响应解析（routeRequestPayload/routeResponsePayload/
// parseRouteResult）已删，Route 走 structured.Extract；schema 形状由
// router_schema_test.go 锁定，6s 超时/单次调用/thinking disabled 不变。
