// Package structured 是 openaicompat 之上的结构化输出接缝
// （openspec/changes/structured-tool-seam）：一次强制 function-call 的
// 结构化抽取——JSON Schema 由结果类型反射生成（invopop/jsonschema），
// 模型被迫调用唯一工具，返回实参经 JSON 解码为结构化结果。此前的两处
// 家酿管道（router 自抄 function-call payload、directordraft 大括号截取
// JSON）在此收敛：directordraft 是第一个 adapter，router 已挂待迁标记
// （迁移会字节级改动 payload，须带 eval 重验，单独立项）。
//
// 韧性策略沿袭 ADR-0012：传输零重试，调用方自行决定策略。
package structured

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/invopop/jsonschema"
	"qiuqiu/internal/openaicompat"
)

const (
	defaultHTTPTimeout = 10 * time.Second
	responseByteLimit  = 1 << 20
)

// Client 定位一个 OpenAI 兼容端点并执行结构化抽取调用。
type Client struct {
	httpClient *http.Client
	endpoint   openaicompat.Endpoint
}

// NewClient 与 llm.NewClient 同构：同一组凭据可同时喂两个 client。
func NewClient(baseURL, apiKey, model string) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: defaultHTTPTimeout},
		endpoint:   openaicompat.Endpoint{BaseURL: baseURL, APIKey: apiKey, Model: model},
	}
}

// CallOptions 是一次结构化抽取的语义载荷；工具的参数 schema 由结果类型
// 反射生成，不在这里声明。
type CallOptions struct {
	SystemPrompt    string
	UserContent     string
	ToolName        string
	ToolDescription string
	Temperature     float64
	MaxTokens       int
}

// toolCallEnvelope 只解析唯一需要的响应形状：第一条消息里名为 ToolName
// 的 function call 实参。
type toolCallEnvelope struct {
	Choices []struct {
		Message struct {
			ToolCalls []struct {
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
}

// Extract 强制模型调用唯一工具并解码实参为 T。schema 由 T 反射生成——
// 「schema 与结果类型漂移」这一类 bug 从结构上不存在。
func Extract[T any](ctx context.Context, client *Client, opts CallOptions) (T, error) {
	var zero T
	if client == nil {
		return zero, fmt.Errorf("structured client unavailable")
	}
	schema := jsonschema.Reflect(&zero)
	payload := map[string]any{
		"model": client.endpoint.Model,
		"messages": []map[string]string{
			{"role": "system", "content": opts.SystemPrompt},
			{"role": "user", "content": opts.UserContent},
		},
		"tools": []map[string]any{{
			"type": "function",
			"function": map[string]any{
				"name":        opts.ToolName,
				"description": opts.ToolDescription,
				"parameters":  schema,
			},
		}},
		"tool_choice": map[string]any{
			"type":     "function",
			"function": map[string]string{"name": opts.ToolName},
		},
		"max_tokens":   opts.MaxTokens,
		"temperature":  opts.Temperature,
		"thinking":     map[string]string{"type": "disabled"},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return zero, fmt.Errorf("structured encode: %w", err)
	}
	respBody, err := openaicompat.Post(ctx, client.httpClient, client.endpoint, "/chat/completions", body, responseByteLimit)
	if err != nil {
		return zero, fmt.Errorf("structured call: %w", err)
	}
	var envelope toolCallEnvelope
	if err := json.Unmarshal(respBody, &envelope); err != nil {
		return zero, fmt.Errorf("structured decode: %w", err)
	}
	if len(envelope.Choices) == 0 {
		return zero, fmt.Errorf("structured call returned no choices")
	}
	for _, toolCall := range envelope.Choices[0].Message.ToolCalls {
		if toolCall.Function.Name != opts.ToolName {
			continue
		}
		var result T
		if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &result); err != nil {
			return zero, fmt.Errorf("structured arguments: %w", err)
		}
		return result, nil
	}
	return zero, fmt.Errorf("structured call returned no %s tool call", opts.ToolName)
}
