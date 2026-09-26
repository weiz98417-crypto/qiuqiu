package main

// 轮次检测转发腿的单测（voice-turn-detection 决策 c）：对齐
// voice_latency_test 的真实 WS 风格——turn_query 上行走 readMessages 的
// 真实 case 分发，下行用确定性 fake sidecar（httptest）回填。锁四条边界：
// 未配置端点回 isComplete:null（model 插槽降级闭环）、正常转发回结论、
// sidecar 失败回 isComplete:null、utteranceId 缺失的坏消息静默丢弃。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"qiuqiu/internal/auth"

	"github.com/gorilla/websocket"
)

// startTurnWSConnection 起一条真实 WS 连接并装配最小可用的
// watchConnection；下行帧由测试自己读（不装排空协程），返回连接与读帧函数。
func startTurnWSConnection(t *testing.T, deps watchDeps) (*watchConnection, *websocket.Conn, func()) {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	serverConnCh := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		serverConnCh <- conn
		<-r.Context().Done()
		_ = conn.Close()
	}))
	t.Cleanup(server.Close)

	clientConn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	t.Cleanup(func() { _ = clientConn.Close() })

	var serverConn *websocket.Conn
	select {
	case serverConn = <-serverConnCh:
	case <-time.After(5 * time.Second):
		t.Fatal("server websocket upgrade timed out")
	}

	connection := newWatchConnection(deps, serverConn, auth.Claims{Subject: "user-turn"})
	connection.matchID = "match-turn"
	// connectionCancel 供 readMessages 的 defer 调用（nil 函数会在连接
	// 拆除时 panic，对齐 voice_latency_test 的装配）。
	ctx, cancel := context.WithCancel(context.Background())
	connection.connectionCtx = ctx
	connection.connectionCancel = cancel
	go connection.readMessages()
	cleanup := func() {
		cancel()
		_ = clientConn.Close()
	}
	t.Cleanup(cleanup)
	return connection, clientConn, cleanup
}

// readDownlink 带超时读一条下行 JSON 消息。
func readDownlink(t *testing.T, conn *websocket.Conn) map[string]interface{} {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	defer func() { _ = conn.SetReadDeadline(time.Time{}) }()
	var message map[string]interface{}
	if err := conn.ReadJSON(&message); err != nil {
		t.Fatalf("read downlink message: %v", err)
	}
	return message
}

// waitForDownlink 持续读下行直到出现指定 type 的消息（转发在协程里，
// 前面可能插着别的帧）。
func waitForDownlink(t *testing.T, conn *websocket.Conn, messageType string) map[string]interface{} {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		message := readDownlink(t, conn)
		if message["type"] == messageType {
			return message
		}
	}
	t.Fatalf("downlink message %q did not arrive within deadline", messageType)
	return nil
}

// fakeTurnSidecar 起一个确定性 fake sidecar：按返回码/响应体计数回包。
func fakeTurnSidecar(t *testing.T, status int, body string) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server, &calls
}

func TestTurnQueryUnconfiguredRepliesNull(t *testing.T) {
	// QIUQIU_TURN_SIDECAR_URL 留空（Enabled=false）：model 插槽整体降级，
	// turn_query 仍必须回 turn_result{isComplete:null}——客户端插槽收到
	// null 下探静默档，降级链闭环。
	_, clientConn, cleanup := startTurnWSConnection(t, watchDeps{
		turnSidecar: newTurnSidecarClient("", 0),
	})
	defer cleanup()

	if err := clientConn.WriteJSON(map[string]interface{}{
		"type":        "turn_query",
		"utteranceId": "utt-turn-1",
		"text":        "我觉得裁判这次吹得",
	}); err != nil {
		t.Fatalf("write turn_query: %v", err)
	}
	result := waitForDownlink(t, clientConn, "turn_result")
	if result["utteranceId"] != "utt-turn-1" {
		t.Fatalf("turn_result utteranceId = %v, want utt-turn-1", result["utteranceId"])
	}
	isComplete, exists := result["isComplete"]
	if exists && isComplete != nil {
		t.Fatalf("unconfigured sidecar must reply isComplete:null, got %v", isComplete)
	}
	if _, hasProbability := result["probability"]; hasProbability {
		t.Fatalf("unconfigured sidecar must not carry probability: %v", result)
	}
}

func TestTurnQueryForwardsSidecarVerdict(t *testing.T) {
	// fake sidecar 正常应答：turn_result 透传结论与概率。
	sidecar, calls := fakeTurnSidecar(t, http.StatusOK, `{"probability":0.3555,"isComplete":true}`)
	_, clientConn, cleanup := startTurnWSConnection(t, watchDeps{
		turnSidecar: newTurnSidecarClient(sidecar.URL, 0),
	})
	defer cleanup()

	if err := clientConn.WriteJSON(map[string]interface{}{
		"type":        "turn_query",
		"utteranceId": "utt-turn-2",
		"text":        "我觉得裁判这次吹得没问题",
	}); err != nil {
		t.Fatalf("write turn_query: %v", err)
	}
	result := waitForDownlink(t, clientConn, "turn_result")
	if result["utteranceId"] != "utt-turn-2" {
		t.Fatalf("turn_result utteranceId = %v, want utt-turn-2", result["utteranceId"])
	}
	if result["isComplete"] != true {
		t.Fatalf("turn_result isComplete = %v, want true", result["isComplete"])
	}
	if probability, ok := result["probability"].(float64); !ok || probability != 0.3555 {
		t.Fatalf("turn_result probability = %v, want 0.3555", result["probability"])
	}
	waitForReplication(t, func() bool { return calls.Load() == 1 })
}

func TestTurnQuerySidecarFailureRepliesNull(t *testing.T) {
	// sidecar 500：转发失败同样回 isComplete:null，不重试不报错风暴。
	sidecar, calls := fakeTurnSidecar(t, http.StatusInternalServerError, `{}`)
	_, clientConn, cleanup := startTurnWSConnection(t, watchDeps{
		turnSidecar: newTurnSidecarClient(sidecar.URL, 0),
	})
	defer cleanup()

	if err := clientConn.WriteJSON(map[string]interface{}{
		"type":        "turn_query",
		"utteranceId": "utt-turn-3",
		"text":        "下半场刚开始",
	}); err != nil {
		t.Fatalf("write turn_query: %v", err)
	}
	result := waitForDownlink(t, clientConn, "turn_result")
	if isComplete, exists := result["isComplete"]; exists && isComplete != nil {
		t.Fatalf("failed sidecar must reply isComplete:null, got %v", isComplete)
	}
	waitForReplication(t, func() bool { return calls.Load() == 1 })
}

func TestTurnQueryMissingUtteranceIDIsDropped(t *testing.T) {
	// utteranceId 缺失：坏消息直接丢弃，不回包（客户端超时路径自会兜底）。
	_, clientConn, cleanup := startTurnWSConnection(t, watchDeps{
		turnSidecar: newTurnSidecarClient("", 0),
	})
	defer cleanup()

	if err := clientConn.WriteJSON(map[string]interface{}{
		"type": "turn_query",
		"text": "没有 utteranceId 的查询",
	}); err != nil {
		t.Fatalf("write turn_query: %v", err)
	}
	_ = clientConn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	var message map[string]interface{}
	if err := clientConn.ReadJSON(&message); err == nil {
		t.Fatalf("missing utteranceId must not be answered, got %v", message)
	}
}
