package asr

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var ErrNotConfigured = errors.New("asr provider is not configured")

// Client wraps MiMo ASR for speech recognition.
type Client struct {
	apiKey     string
	baseURL    string
	model      string
	httpClient *http.Client
	mockText   string
}

func NewClient(apiKey string) *Client {
	return &Client{
		apiKey:     apiKey,
		baseURL:    "https://api.xiaomimimo.com/v1",
		model:      "mimo-v2.5-asr",
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func NewMockClient(text string) *Client {
	return &Client{mockText: strings.TrimSpace(text)}
}

func (c *Client) WithBaseURL(baseURL string) *Client {
	c.baseURL = strings.TrimRight(baseURL, "/")
	return c
}

func (c *Client) WithHTTPClient(httpClient *http.Client) *Client {
	if httpClient != nil {
		c.httpClient = httpClient
	}
	return c
}

func (c *Client) WithModel(model string) *Client {
	if strings.TrimSpace(model) != "" {
		c.model = strings.TrimSpace(model)
	}
	return c
}

type Result struct {
	Text       string  `json:"text"`
	Confidence float64 `json:"confidence"`
	DurationMs int     `json:"duration_ms"`
	Provider   string  `json:"provider,omitempty"`
}

// Transcribe sends WAV audio to MiMo ASR and returns recognized text.
func (c *Client) Transcribe(ctx context.Context, audio []byte, hints []string) (*Result, error) {
	start := time.Now()
	if c == nil {
		return nil, ErrNotConfigured
	}
	if c.mockText != "" {
		return &Result{Text: c.mockText, Confidence: 1, DurationMs: int(time.Since(start).Milliseconds()), Provider: "mock"}, nil
	}
	if strings.TrimSpace(c.apiKey) == "" {
		return nil, ErrNotConfigured
	}
	payload := map[string]interface{}{
		"model": defaultString(c.model, "mimo-v2.5-asr"),
		"messages": []map[string]interface{}{
			{
				"role": "user",
				"content": []map[string]interface{}{
					{
						"type": "input_audio",
						"input_audio": map[string]string{
							"data": "data:audio/wav;base64," + base64.StdEncoding.EncodeToString(audio),
						},
					},
				},
			},
		},
		"asr_options": map[string]string{"language": "zh"},
	}
	if len(hints) > 0 {
		payload["hints"] = hints
	}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(c.baseURL, "/")+"/chat/completions", bytes.NewReader(body))
	req.Header.Set("api-key", c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("asr request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("asr error %d: %s", resp.StatusCode, string(errBody))
	}

	var raw struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("asr decode: %w", err)
	}
	if len(raw.Choices) == 0 {
		return nil, fmt.Errorf("asr returned no choices")
	}

	return &Result{
		Text:       strings.TrimSpace(raw.Choices[0].Message.Content),
		Confidence: 0,
		DurationMs: int(time.Since(start).Milliseconds()),
		Provider:   "mimo",
	}, nil
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
