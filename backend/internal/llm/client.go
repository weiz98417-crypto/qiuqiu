package llm

import (
	"bufio"
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

var systemPrompt = `你是"球球"，一个陪用户看足球比赛的AI语音助手。你像一个朋友一样聊天，不是专业解说员。每次回复不超过2句话。用自然口语表达。`

var TestEvents = []string{
	"进球了！主队前锋在第78分钟破门，比分变成2-1。请用1-2句话表达你的反应。",
	"客队球员吃到黄牌，第35分钟。请简短反应。",
	"上半场结束，比分0-0。双方都还没有进球。请简短总结上半场。",
	"射门！主队射正了，但被守门员扑出。第60分钟。请简短反应。",
	"比赛开始！对阵双方是皇家马德里对巴塞罗那。请表达期待。",
}

// StreamChunk is a token from streaming LLM output.
type StreamChunk struct {
	Text   string
	Done   bool
	Tokens int
}

// StreamWithMessages sends messages and returns a channel of streaming tokens.
func (c *Client) StreamWithMessages(ctx context.Context, messages []Message, temperature float64) <-chan StreamChunk {
	ch := make(chan StreamChunk, 16)
	go func() {
		defer close(ch)
		if c == nil || c.breaker.Allow(time.Now().UTC()) != nil {
			return
		}
		req := ChatRequest{
			Model: c.model, Messages: messages,
			MaxTokens: 80, Temperature: temperature, Stream: true,
			Thinking: Thinking{Type: "disabled"},
		}
		body, _ := json.Marshal(req)
		httpReq, _ := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/chat/completions", bytes.NewReader(body))
		c.setAuthHeaders(httpReq)
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "text/event-stream")

		resp, err := c.httpClient.Do(httpReq)
		if err != nil {
			c.breaker.Failure(time.Now().UTC())
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			c.breaker.Failure(time.Now().UTC())
			return
		}

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				c.breaker.Success()
				ch <- StreamChunk{Done: true}
				return
			}
			var sse struct {
				Choices []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
				} `json:"choices"`
			}
			if json.Unmarshal([]byte(data), &sse) == nil && len(sse.Choices) > 0 {
				ch <- StreamChunk{Text: sse.Choices[0].Delta.Content}
			}
		}
		c.breaker.Failure(time.Now().UTC())
	}()
	return ch
}

func (c *Client) CircuitState() resilience.State {
	if c == nil {
		return resilience.StateOpen
	}
	return c.breaker.State()
}

// GenerateWithMessages sends a full message list with configurable temperature.
func (c *Client) GenerateWithMessages(ctx context.Context, messages []Message, temperature float64) (*GenerateResult, error) {
	return c.GenerateWithMessagesLimit(ctx, messages, temperature, 80)
}

// GenerateWithMessagesLimit allows structured-output callers to reserve enough room for their schema.
func (c *Client) GenerateWithMessagesLimit(ctx context.Context, messages []Message, temperature float64, maxTokens int) (*GenerateResult, error) {
	if maxTokens <= 0 {
		maxTokens = 80
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

func (c *Client) Generate(ctx context.Context, prompt string) (*GenerateResult, error) {
	req := ChatRequest{
		Model: c.model,
		Messages: []Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: prompt},
		},
		MaxTokens:   80,
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
	body, _ := json.Marshal(req)
	httpReq, _ := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/chat/completions", bytes.NewReader(body))
	c.setAuthHeaders(httpReq)
	httpReq.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		c.breaker.Failure(time.Now().UTC())
		return nil, fmt.Errorf("llm request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		c.breaker.Failure(time.Now().UTC())
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("llm status %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}

	var chatResp ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
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

func (c *Client) setAuthHeaders(req *http.Request) {
	if strings.Contains(c.baseURL, "xiaomimimo.com") || strings.HasPrefix(c.model, "mimo-") {
		req.Header.Set("api-key", c.apiKey)
		return
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
}
