package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

type ChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	MaxTokens   int       `json:"max_tokens"`
	Temperature float64   `json:"temperature"`
	Stream      bool      `json:"stream"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
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
	Text      string
	Duration  time.Duration
	Tokens    int
}

var systemPrompt = `你是"球球"，一个陪用户看足球比赛的AI语音助手。你像一个朋友一样聊天，不是专业解说员。每次回复不超过2句话。用自然口语表达。`

var TestEvents = []string{
	"进球了！主队前锋在第78分钟破门，比分变成2-1。请用1-2句话表达你的反应。",
	"客队球员吃到黄牌，第35分钟。请简短反应。",
	"上半场结束，比分0-0。双方都还没有进球。请简短总结上半场。",
	"射门！主队射正了，但被守门员扑出。第60分钟。请简短反应。",
	"比赛开始！对阵双方是皇家马德里对巴塞罗那。请表达期待。",
}

// GenerateWithMessages sends a full message list with configurable temperature.
func (c *Client) GenerateWithMessages(ctx context.Context, messages []Message, temperature float64) (*GenerateResult, error) {
	req := ChatRequest{
		Model:       "deepseek-chat",
		Messages:    messages,
		MaxTokens:   80,
		Temperature: temperature,
		Stream:      false,
	}

	return c.doChat(ctx, req)
}

func (c *Client) Generate(ctx context.Context, prompt string) (*GenerateResult, error) {
	req := ChatRequest{
		Model: "deepseek-chat",
		Messages: []Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: prompt},
		},
		MaxTokens:   80,
		Temperature: 0.7,
		Stream:      false,
	}

	return c.doChat(ctx, req)
}

func (c *Client) doChat(ctx context.Context, req ChatRequest) (*GenerateResult, error) {
	body, _ := json.Marshal(req)
	httpReq, _ := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/chat/completions", bytes.NewReader(body))
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("llm request: %w", err)
	}
	defer resp.Body.Close()

	var chatResp ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return nil, fmt.Errorf("llm decode: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return nil, fmt.Errorf("llm returned no choices")
	}

	return &GenerateResult{
		Text:     chatResp.Choices[0].Message.Content,
		Duration: time.Since(start),
		Tokens:   chatResp.Usage.TotalTokens,
	}, nil
}
