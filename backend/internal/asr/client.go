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

// Client wraps OpenAI Whisper API for speech recognition.
type Client struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

func NewClient(apiKey string) *Client {
	return &Client{
		apiKey:  apiKey,
		baseURL: "https://api.openai.com/v1",
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

type Result struct {
	Text       string  `json:"text"`
	Confidence float64 `json:"confidence"`
	DurationMs int     `json:"duration_ms"`
}

// Transcribe sends PCM audio to Whisper and returns recognized text.
func (c *Client) Transcribe(ctx context.Context, audio []byte, hints []string) (*Result, error) {
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)

	// Audio file part
	part, _ := w.CreateFormFile("file", "audio.wav")
	part.Write(audio)

	w.WriteField("model", "whisper-1")
	w.WriteField("language", "zh")
	w.WriteField("response_format", "verbose_json")

	// Inject context hints as prompt
	if len(hints) > 0 {
		prompt := ""
		for _, h := range hints {
			prompt += h + " "
		}
		w.WriteField("prompt", prompt)
	}

	w.Close()

	req, _ := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/audio/transcriptions", body)
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", w.FormDataContentType())

	start := time.Now()
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
		Text     string `json:"text"`
		Segments []struct {
			Confidence float64 `json:"confidence"`
		} `json:"segments"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("asr decode: %w", err)
	}

	confidence := 0.0
	if len(raw.Segments) > 0 {
		for _, s := range raw.Segments {
			confidence += s.Confidence
		}
		confidence /= float64(len(raw.Segments))
	}

	return &Result{
		Text:       raw.Text,
		Confidence: confidence,
		DurationMs: int(time.Since(start).Milliseconds()),
	}, nil
}
