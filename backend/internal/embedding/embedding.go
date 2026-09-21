// Package embedding 是 OpenAI 兼容 /embeddings 端点的最小客户端
// （openspec/changes/semantic-memory）：本地 Ollama bge-m3 是首个生产端点。
// 零外部依赖；超时与重试策略归调用方（ADR-0012 同哲学）。
package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const defaultTimeout = 2 * time.Second

type Client struct {
	baseURL    string
	model      string
	apiKey     string
	httpClient *http.Client
}

func NewClient(baseURL, model, apiKey string) *Client {
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		model:      model,
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: defaultTimeout},
	}
}

type request struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

type response struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

// Embed 把一段文本嵌为向量。文本超长时按 rune 截断到 2000——bge-m3 上限
// 8192 token，2000 rune 对记忆时刻/检索词绰绰有余，防止病态输入拖垮本地
// 推理。
func (c *Client) Embed(ctx context.Context, text string) ([]float32, error) {
	if c == nil {
		return nil, fmt.Errorf("embedding client unavailable")
	}
	runes := []rune(strings.TrimSpace(text))
	if len(runes) == 0 {
		return nil, fmt.Errorf("embedding empty text")
	}
	if len(runes) > 2000 {
		runes = runes[:2000]
	}
	body, err := json.Marshal(request{Model: c.model, Input: string(runes)})
	if err != nil {
		return nil, fmt.Errorf("embedding encode: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("embedding build: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding call: %w", err)
	}
	defer resp.Body.Close()
	var parsed response
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("embedding decode: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("embedding status %d", resp.StatusCode)
	}
	if len(parsed.Data) == 0 || len(parsed.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("embedding returned no vector")
	}
	return parsed.Data[0].Embedding, nil
}
