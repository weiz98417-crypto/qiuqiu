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
	"reflect"
	"strings"
	"time"

	"github.com/invopop/jsonschema"
	"qiuqiu/internal/openaicompat"
)

// reflectSchema 反射结果类型为根内联的 JSON Schema——ExpandedStruct 把
// 定义放根上（否则根是 $ref，部分平台不认作合法工具参数）。
func reflectSchema(v any) *jsonschema.Schema {
	reflector := jsonschema.Reflector{ExpandedStruct: true, DoNotReference: true}
	return reflector.ReflectFromType(reflect.TypeOf(v))
}

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
	return NewClientWithTimeout(baseURL, apiKey, model, defaultHTTPTimeout)
}

// NewClientWithTimeout 让调用方声明自己的延迟预算（router 的 6s 单次
// 调用是 ADR-0009 锁定决策，不能吃默认 10s）。
func NewClientWithTimeout(baseURL, apiKey, model string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = defaultHTTPTimeout
	}
	return &Client{
		httpClient: &http.Client{Timeout: timeout},
		endpoint:   openaicompat.Endpoint{BaseURL: baseURL, APIKey: apiKey, Model: model},
	}
}

// CallOptions 是一次结构化抽取的语义载荷；工具的参数 schema 由结果类型
// 反射生成，不在这里声明。ContextMessage 是可选的附加 user 消息（如
// router 的比赛上下文摘要），插在 UserContent 之后。
type CallOptions struct {
	SystemPrompt    string
	UserContent     string
	ContextMessage  string
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
	schema := reflectSchema(&zero)
	messages := []map[string]string{
		{"role": "system", "content": opts.SystemPrompt},
		{"role": "user", "content": opts.UserContent},
	}
	if strings.TrimSpace(opts.ContextMessage) != "" {
		messages = append(messages, map[string]string{"role": "user", "content": opts.ContextMessage})
	}
	toolFunction := map[string]any{
		"name":       opts.ToolName,
		"parameters": schema,
	}
	if strings.TrimSpace(opts.ToolDescription) != "" {
		toolFunction["description"] = opts.ToolDescription
	}
	payload := map[string]any{
		"model":    client.endpoint.Model,
		"messages": messages,
		"tools": []map[string]any{{
			"type":     "function",
			"function": toolFunction,
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
