package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"qiuqiu/internal/openaicompat"
	"qiuqiu/internal/resilience"
)

// defaultMaxTokens 是闲聊措辞类调用的回复预算：realizer 的句数上限决定了
// 80 token 足够。需要更大预算的调用方走 GenerateWithMessagesLimit 显式
// 声明（如 directordraft 的结构化抽取 320）。
const defaultMaxTokens = 80

type Client struct {
	baseURL    string
	apiKey     string
	model      string
	httpClient *http.Client
	breaker    *resilience.CircuitBreaker
}

func NewClient(baseURL, apiKey, model string) *Client {
	if model == "" {
		model = "mimo-v2.5-pro"
	}
	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		model:   model,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		breaker: resilience.NewCircuitBreaker(3, 10*time.Second),
	}
}

func (c *Client) DebugString() string {
	if c == nil {
		return "<nil>"
	}
	return fmt.Sprintf("llm.Client{baseURL:%s,model:%s}", c.baseURL, c.model)
}

type ChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	MaxTokens   int       `json:"max_tokens"`
	Temperature float64   `json:"temperature"`
	Stream      bool      `json:"stream"`
	Thinking    Thinking  `json:"thinking"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Thinking struct {
	Type string `json:"type"`
}

type ChatResponse struct {
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

type Choice struct {
	Message Message `json:"message"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type GenerateResult struct {
	Text     string
	Duration time.Duration
	Tokens   int
}

func (c *Client) CircuitState() resilience.State {
	if c == nil {
		return resilience.StateOpen
	}
	return c.breaker.State()
}

// GenerateWithMessages sends a full message list with configurable temperature.
func (c *Client) GenerateWithMessages(ctx context.Context, messages []Message, temperature float64) (*GenerateResult, error) {
	return c.GenerateWithMessagesLimit(ctx, messages, temperature, defaultMaxTokens)
}

// GenerateWithMessagesLimit allows structured-output callers to reserve enough room for their schema.
func (c *Client) GenerateWithMessagesLimit(ctx context.Context, messages []Message, temperature float64, maxTokens int) (*GenerateResult, error) {
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}
	req := ChatRequest{
		Model:       c.model,
		Messages:    messages,
		MaxTokens:   maxTokens,
		Temperature: temperature,
		Stream:      false,
		Thinking:    Thinking{Type: "disabled"},
	}

	return c.doChat(ctx, req)
}

func (c *Client) Generate(ctx context.Context, system, prompt string) (*GenerateResult, error) {
	req := ChatRequest{
		Model: c.model,
		Messages: []Message{
			{Role: "system", Content: system},
			{Role: "user", Content: prompt},
		},
		MaxTokens:   defaultMaxTokens,
		Temperature: 0.7,
		Stream:      false,
		Thinking:    Thinking{Type: "disabled"},
	}

	return c.doChat(ctx, req)
}

func (c *Client) doChat(ctx context.Context, req ChatRequest) (*GenerateResult, error) {
	if err := c.breaker.Allow(time.Now().UTC()); err != nil {
		return nil, fmt.Errorf("llm unavailable: %w", err)
	}
	body, err := json.Marshal(req)
	if err != nil {
		c.breaker.Failure(time.Now().UTC())
		return nil, fmt.Errorf("llm encode: %w", err)
	}

	start := time.Now()
	// HTTP 执行走共享传输层；熔断语义（Allow/Failure/Success）留在本 client。
	respBody, err := openaicompat.Post(ctx, c.httpClient, c.endpoint(), "/chat/completions", body, 1<<20)
	if err != nil {
		c.breaker.Failure(time.Now().UTC())
		return nil, fmt.Errorf("llm call: %w", err)
	}

	var chatResp ChatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		c.breaker.Failure(time.Now().UTC())
		return nil, fmt.Errorf("llm decode: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		c.breaker.Failure(time.Now().UTC())
		return nil, fmt.Errorf("llm returned no choices")
	}

	c.breaker.Success()
	return &GenerateResult{
		Text:     chatResp.Choices[0].Message.Content,
		Duration: time.Since(start),
		Tokens:   chatResp.Usage.TotalTokens,
	}, nil
}

func (c *Client) endpoint() openaicompat.Endpoint {
	return openaicompat.Endpoint{BaseURL: c.baseURL, APIKey: c.apiKey, Model: c.model}
}
