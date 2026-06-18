package tts

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

var ErrNotConfigured = errors.New("tts provider is not configured")

type Client struct {
	apiKey     string
	baseURL    string
	model      string
	voice      string
	httpClient *http.Client
	mockAudio  []byte
}

func NewClient(apiKey string) *Client {
	return &Client{
		apiKey:  apiKey,
		baseURL: "https://api.xiaomimimo.com/v1",
		model:   "mimo-v2.5-tts",
		voice:   "Chloe",
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func NewMockClient(audio []byte) *Client {
	return &Client{mockAudio: append([]byte(nil), audio...)}
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

func (c *Client) WithVoice(voice string) *Client {
	if strings.TrimSpace(voice) != "" {
		c.voice = strings.TrimSpace(voice)
	}
	return c
}

type SynthesizeResult struct {
	AudioData []byte
	Duration  time.Duration
	MimeType  string
}

// Synthesize calls MiMo Text-to-Speech API.
// voiceID overrides the configured MiMo voice when supplied.
func (c *Client) Synthesize(ctx context.Context, text, voiceID string) (*SynthesizeResult, error) {
	start := time.Now()
	if c == nil {
		return nil, ErrNotConfigured
	}
	if len(c.mockAudio) > 0 {
		return &SynthesizeResult{AudioData: append([]byte(nil), c.mockAudio...), Duration: time.Since(start), MimeType: "audio/mpeg"}, nil
	}
	if strings.TrimSpace(c.apiKey) == "" {
		return nil, ErrNotConfigured
	}
	voice := strings.TrimSpace(voiceID)
	if voice == "" || strings.HasPrefix(voice, "cgSg") {
		voice = c.voice
	}
	payload := map[string]interface{}{
		"model": defaultString(c.model, "mimo-v2.5-tts"),
		"messages": []map[string]string{
			{"role": "assistant", "content": text},
		},
		"audio": map[string]string{
			"format": "wav",
			"voice":  defaultString(voice, "Chloe"),
		},
	}

	body, _ := json.Marshal(payload)
	url := fmt.Sprintf("%s/chat/completions", strings.TrimRight(c.baseURL, "/"))
	httpReq, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	httpReq.Header.Set("api-key", c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("tts request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("tts error %d: %s", resp.StatusCode, string(errBody))
	}

	var raw struct {
		Choices []struct {
			Message struct {
				Audio struct {
					Data string `json:"data"`
				} `json:"audio"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("tts decode: %w", err)
	}
	if len(raw.Choices) == 0 || strings.TrimSpace(raw.Choices[0].Message.Audio.Data) == "" {
		return nil, fmt.Errorf("tts returned no audio")
	}
	audio, err := decodeBase64(raw.Choices[0].Message.Audio.Data)
	if err != nil {
		return nil, fmt.Errorf("tts decode audio: %w", err)
	}

	return &SynthesizeResult{
		AudioData: audio,
		Duration:  time.Since(start),
		MimeType:  "audio/wav",
	}, nil
}

func decodeBase64(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if idx := strings.Index(value, ","); idx >= 0 {
		value = value[idx+1:]
	}
	return base64.StdEncoding.DecodeString(value)
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
