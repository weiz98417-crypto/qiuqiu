package main

// 语音链路延迟分解日志的单测（voice-transport-upgrade 1.1）：
// 只锁「日志点存在且字段完整」——handler 层面能锁的边界。真实 p90 由
// 后续真机会话按这些日志行采集，本测试不制造延迟数据。

import (
	"bytes"
	"context"
	"encoding/base64"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"qiuqiu/internal/asr"
	"qiuqiu/internal/auth"
	"qiuqiu/internal/companion"
	"qiuqiu/internal/config"
	"qiuqiu/internal/conversation"
	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/tts"

	"github.com/gorilla/websocket"
)

// syncBuffer 线程安全的日志缓冲：延迟日志行来自读循环/调度器多个协程。
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// waitFor 轮询日志缓冲直到出现目标子串（话轮在调度器协程异步执行）。
func (b *syncBuffer) waitFor(t *testing.T, substr string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if output := b.String(); strings.Contains(output, substr) {
			return output
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("log line %q did not appear within deadline; got:\n%s", substr, b.String())
	return ""
}

func captureLogs(t *testing.T) *syncBuffer {
	t.Helper()
	buffer := &syncBuffer{}
	previous := log.Writer()
	log.SetOutput(buffer)
	t.Cleanup(func() { log.SetOutput(previous) })
	return buffer
}

// newLatencyMatchStore 装配带配置与进球事件的比赛仓库（伴答最简数据面）。
func newLatencyMatchStore(t *testing.T) *matchstate.Store {
	t.Helper()
	store := matchstate.NewStore()
	if _, _, err := store.SetConfig("match-lat", matchstate.MatchConfig{HomeTeam: "Spain", AwayTeam: "Germany"}); err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}
	if _, _, err := store.Create("match-lat", matchstate.MatchEvent{
		EventType:   "goal",
		Period:      "first_half",
		Clock:       "24:10",
		TeamID:      "home",
		TeamName:    "Spain",
		PlayerName:  "Pedri",
		Score:       matchstate.Score{Home: 1, Away: 0},
		Description: "Pedri scores.",
		Participants: []matchstate.Participant{
			{Role: "scorer", Name: "Pedri", TeamID: "home", TeamName: "Spain"},
			{Role: "assist", Name: "Fabian", TeamID: "home", TeamName: "Spain"},
		},
	}); err != nil {
		t.Fatalf("Create goal error: %v", err)
	}
	return store
}

// startLatencyWSConnection 起一条真实 WS 连接并装配最小可用的
// watchConnection；返回连接与其读循环的启动函数。
func startLatencyWSConnection(t *testing.T, deps watchDeps) (*watchConnection, *websocket.Conn, func()) {
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

	connection := newWatchConnection(deps, serverConn, auth.Claims{Subject: "user-lat"})
	ctx, cancel := context.WithCancel(context.Background())
	connection.connectionCtx = ctx
	connection.connectionCancel = cancel
	connection.matchID = "match-lat"
	// 客户端侧排空下行帧，避免写入背压。
	go func() {
		for {
			if _, _, err := clientConn.ReadMessage(); err != nil {
				return
			}
		}
	}()
	cleanup := func() {
		cancel()
		_ = clientConn.Close()
	}
	t.Cleanup(cleanup)
	return connection, clientConn, cleanup
}

func TestVoiceLatencyLogLineFieldsComplete(t *testing.T) {
	buffer := captureLogs(t)
	connection := &watchConnection{matchID: "match-x"}
	connection.identity = newConnectionIdentity("user-x")
	connection.logVoiceLatency("turn_decided", "signal-x", time.Now().Add(-30*time.Millisecond))
	line := buffer.waitFor(t, `stage="turn_decided"`)
	for _, field := range []string{
		"voice latency event:",
		`user="user-x"`,
		`match="match-x"`,
		`signal="signal-x"`,
		`stage="turn_decided"`,
		"elapsed_ms=",
	} {
		if !strings.Contains(line, field) {
			t.Fatalf("latency log line missing field %q: %s", field, line)
		}
	}
}

func TestVoiceLatencyMissingAnchorSkipsLog(t *testing.T) {
	buffer := captureLogs(t)
	connection := &watchConnection{matchID: "match-x"}
	connection.identity = newConnectionIdentity("user-x")
	connection.logVoiceLatency("turn_decided", "signal-x", time.Time{})
	if output := buffer.String(); strings.Contains(output, "voice latency event") {
		t.Fatalf("zero anchor must skip the log line, got: %s", output)
	}
}

func TestVoiceLatencyAnchorCapEviction(t *testing.T) {
	connection := newWatchConnection(watchDeps{}, nil, auth.Claims{})
	now := time.Now()
	for i := 0; i < maxVoiceLatencyAnchors; i++ {
		connection.storeVoiceLatencyAnchor(voiceLatencyAnchorKey("utt", string(rune('a'+i))), now)
	}
	firstKey := voiceLatencyAnchorKey("utt", "a")
	if connection.voiceLatencyAnchor(firstKey).IsZero() {
		t.Fatal("anchor should be present before cap overflow")
	}
	connection.storeVoiceLatencyAnchor("overflow", now)
	if !connection.voiceLatencyAnchor(firstKey).IsZero() {
		t.Fatal("anchors must be evicted once the cap overflows")
	}
	if connection.voiceLatencyAnchor("overflow").IsZero() {
		t.Fatal("latest anchor must survive eviction")
	}
}

func TestAsrHandlersLogLatencyStages(t *testing.T) {
	buffer := captureLogs(t)
	connection, clientConn, cleanup := startLatencyWSConnection(t, watchDeps{
		cfg:              &config.Config{},
		matchStore:       newLatencyMatchStore(t),
		submittedSignals: newSignalDeduper(time.Minute, 16),
	})
	go connection.readMessages()
	defer cleanup()

	startMessage := map[string]interface{}{
		"type":        "asr_start",
		"utteranceId": "utt-lat-1",
		"signalId":    "signal-lat-asr",
		"userId":      "user-lat",
		"encoding":    "pcm_s16le",
		"sampleRate":  16000,
		"channels":    1,
		"language":    "zh",
	}
	if err := clientConn.WriteJSON(startMessage); err != nil {
		t.Fatalf("write asr_start: %v", err)
	}
	if err := clientConn.WriteJSON(map[string]interface{}{
		"type":        "asr_finish",
		"utteranceId": "utt-lat-1",
	}); err != nil {
		t.Fatalf("write asr_finish: %v", err)
	}

	line := buffer.waitFor(t, `stage="speech_received"`)
	for _, field := range []string{
		`user="user-lat"`, `match="match-lat"`, `signal="signal-lat-asr"`, "elapsed_ms=",
	} {
		if !strings.Contains(line, field) {
			t.Fatalf("speech_received log missing field %q: %s", field, line)
		}
	}
	finishLine := buffer.waitFor(t, `stage="asr_finish"`)
	for _, field := range []string{`signal="utt-lat-1"`, "elapsed_ms="} {
		if !strings.Contains(finishLine, field) {
			t.Fatalf("asr_finish log missing field %q: %s", field, finishLine)
		}
	}
}

func TestUserSpeechLogsTurnDecidedAndAudioDelivered(t *testing.T) {
	buffer := captureLogs(t)
	store := newLatencyMatchStore(t)
	agent := companion.NewAgent(companion.NewStoreMemoryTools(store))
	connection, clientConn, cleanup := startLatencyWSConnection(t, watchDeps{
		cfg:              &config.Config{},
		agent:            agent,
		matchStore:       store,
		tts:              tts.NewMockClient([]byte("mp3")),
		submittedSignals: newSignalDeduper(time.Minute, 16),
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	connection.scheduler = conversation.NewScheduler(ctx, conversation.Config{})
	connection.responseDelivery = newResponseDeliveryService(
		connection.writer, agent, connection.deps.tts, newReplyDeliveryTracker())
	go connection.readMessages()
	defer cleanup()

	if err := clientConn.WriteJSON(map[string]interface{}{
		"type":     "user_speech",
		"text":     "现在几比几？",
		"userId":   "user-lat",
		"signalId": "signal-lat-turn",
	}); err != nil {
		t.Fatalf("write user_speech: %v", err)
	}

	line := buffer.waitFor(t, `stage="speech_received"`)
	if !strings.Contains(line, `signal="signal-lat-turn"`) {
		t.Fatalf("speech_received log missing signal: %s", line)
	}
	buffer.waitFor(t, `stage="turn_decided"`)
	buffer.waitFor(t, `stage="audio_delivered"`)
}

func TestVoiceSessionLogsTTSSynthesizedStage(t *testing.T) {
	store := matchstate.NewStore()
	if _, _, err := store.SetConfig("match-lat", matchstate.MatchConfig{HomeTeam: "Spain", AwayTeam: "Germany"}); err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}
	agent := companion.NewAgent(companion.NewStoreMemoryTools(store))
	buffer := captureLogs(t)
	audio := base64.StdEncoding.EncodeToString([]byte{1, 2, 3, 4})
	result, err := handleVoiceSessionWithSignalIDOptions(
		context.Background(), agent, asr.NewMockClient("刚才谁助攻？"), tts.NewMockClient([]byte("mp3")),
		"match-lat", "user-lat", "", audio, time.Now(), "signal-lat-tts", voiceSessionOptions{},
	)
	if err != nil {
		t.Fatalf("handleVoiceSessionWithSignalIDOptions: %v", err)
	}
	if len(result.AudioData) == 0 {
		t.Fatalf("expected tts audio, got %+v", result)
	}
	line := buffer.waitFor(t, `stage="tts_synthesized"`)
	for _, field := range []string{
		`user="user-lat"`, `match="match-lat"`, `signal="signal-lat-tts"`, "elapsed_ms=",
	} {
		if !strings.Contains(line, field) {
			t.Fatalf("tts_synthesized log missing field %q: %s", field, line)
		}
	}
}
