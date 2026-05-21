package asr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"
)

// Client wraps SiliconFlow SenseVoice API for speech recognition.
type Client struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

func NewClient(apiKey string) *Client {
	return &Client{
		apiKey:  apiKey,
		baseURL: "https://api.siliconflow.cn/v1",
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

type Result struct {
	Text       string  `json:"text"`
	Confidence float64 `json:"confidence"`
	DurationMs int     `json:"duration_ms"`
}

// Transcribe sends PCM audio to SenseVoice and returns recognized text.
func (c *Client) Transcribe(ctx context.Context, audio []byte, hints []string) (*Result, error) {
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)

	part, _ := w.CreateFormFile("file", "audio.wav")
	part.Write(audio)

	w.WriteField("model", "FunAudioLLM/SenseVoiceSmall")

	w.Close()

	start := time.Now()
	req, _ := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/audio/transcriptions", body)
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", w.FormDataContentType())
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
		Text string `json:"text"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("asr decode: %w", err)
	}

	return &Result{
		Text:       raw.Text,
		Confidence: 0,
		DurationMs: int(time.Since(start).Milliseconds()),
	}, nil
}
