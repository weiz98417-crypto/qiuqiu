package main

// 轮次转发熔断红测（bug 猎手）：turnSidecarClient.Predict 把 sidecar 的
// 一切非 200 都计入共享熔断器（main.go 装配的单例，全连接共用）。sidecar
// 对空文本回 400（输入类错误，服务本身健康），于是一个连接发三条坏文本
// turn_query 就能把熔断器打开 10 秒——期间所有连接的轮次检测静默降级
// 为 isComplete:null。输入类 4xx 不是服务故障，不得计入熔断失败数。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestTurnQueryClientInputErrorsDoNotPoisonSharedBreaker(t *testing.T) {
	// fake sidecar：空/纯空白文本回 400（与真 sidecar 契约一致），正常
	// 文本回判定结论。
	var calls atomic.Int64
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var body struct {
			Text string `json:"text"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if strings.TrimSpace(body.Text) == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"detail":"text is required"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"probability":0.3555,"isComplete":true}`))
	}))
	t.Cleanup(sidecar.Close)

	deps := watchDeps{turnSidecar: newTurnSidecarClient(sidecar.URL, 0)}

	// 连接 1：三条坏文本查询（每条都等回包，确保失败已计入熔断器）。
	_, client1, cleanup1 := startTurnWSConnection(t, deps)
	defer cleanup1()
	for i := 0; i < 3; i++ {
		if err := client1.WriteJSON(map[string]interface{}{
			"type":        "turn_query",
			"utteranceId": "utt-bad-1",
			"text":        "   ",
		}); err != nil {
			t.Fatalf("write bad turn_query: %v", err)
		}
		result := waitForDownlink(t, client1, "turn_result")
		if isComplete, exists := result["isComplete"]; exists && isComplete != nil {
			t.Fatalf("bad text must reply isComplete:null, got %v", isComplete)
		}
	}

	// 连接 2（共享同一 sidecar 客户端与熔断器）：正常文本查询必须照常
	// 转发并拿到结论——别人家的输入错误不得让本连接降级。
	_, client2, cleanup2 := startTurnWSConnection(t, deps)
	defer cleanup2()
	if err := client2.WriteJSON(map[string]interface{}{
		"type":        "turn_query",
		"utteranceId": "utt-good-1",
		"text":        "我觉得裁判这次吹得没问题",
	}); err != nil {
		t.Fatalf("write good turn_query: %v", err)
	}
	result := waitForDownlink(t, client2, "turn_result")
	if result["utteranceId"] != "utt-good-1" {
		t.Fatalf("turn_result utteranceId = %v, want utt-good-1", result["utteranceId"])
	}
	if result["isComplete"] != true {
		t.Fatalf("client-induced 4xx must not open the shared breaker: isComplete = %v (calls=%d)",
			result["isComplete"], calls.Load())
	}
	if calls.Load() != 4 {
		t.Fatalf("good query must still reach the sidecar, calls = %d, want 4", calls.Load())
	}
}
