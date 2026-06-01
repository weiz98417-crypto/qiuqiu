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
)

type Client struct {
	baseURL    string
	apiKey     string
	model      string
	httpClient *http.Client
}

func NewClient(baseURL, apiKey, model string) *Client {
	if model == "" {
		model = "deepseek-v4-flash"
	}
	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		model:   model,
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

// StreamChunk is a token from streaming LLM output.
type StreamChunk struct {
	Text    string
	Done    bool
	Tokens  int
}

// StreamWithMessages sends messages and returns a channel of streaming tokens.
func (c *Client) StreamWithMessages(ctx context.Context, messages []Message, temperature float64) <-chan StreamChunk {
	ch := make(chan StreamChunk, 16)
	go func() {
		defer close(ch)
		req := ChatRequest{
			Model: c.model, Messages: messages,
			MaxTokens: 80, Temperature: temperature, Stream: true,
			Thinking: Thinking{Type: "disabled"},
		}
		body, _ := json.Marshal(req)
		httpReq, _ := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/chat/completions", bytes.NewReader(body))
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "text/event-stream")

		resp, err := c.httpClient.Do(httpReq)
		if err != nil {
			return
		}
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") { continue }
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" { ch <- StreamChunk{Done: true}; return }
			var sse struct {
				Choices []struct {
					Delta struct{ Content string `json:"content"` } `json:"delta"`
				} `json:"choices"`
			}
			if json.Unmarshal([]byte(data), &sse) == nil && len(sse.Choices) > 0 {
				ch <- StreamChunk{Text: sse.Choices[0].Delta.Content}
			}
		}
	}()
	return ch
}

// GenerateWithMessages sends a full message list with configurable temperature.
func (c *Client) GenerateWithMessages(ctx context.Context, messages []Message, temperature float64) (*GenerateResult, error) {
	req := ChatRequest{
		Model:       c.model,
		Messages:    messages,
		MaxTokens:   80,
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

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("llm status %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}

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
