package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"qiuqiu/internal/companion"
	"qiuqiu/internal/conversation"
	"qiuqiu/internal/relationship"

	"github.com/gorilla/websocket"
)

func TestWebsocketResponseSinkWritesReplyMetadataAndAudioFrames(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	serverDone := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connection, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			serverDone <- err
			return
		}
		defer connection.Close()
		writer := &wsWriter{conn: connection}
		tracker := newReplyDeliveryTracker()
		service := conversation.NewResponseDeliveryService(
			websocketResponseSink{writer: writer},
			responseSynthesizerForTest{},
			tracker,
			nil,
		)
		request := conversation.ResponseDeliveryRequest{
			Reply:  "进球了！",
			Trace:  companion.Trace{ID: "trace-ws", UserID: "user-ws", MatchID: "match-ws"},
			Source: "match_reaction", EventID: "event-ws", DeliveryKey: "goal:ws", Critical: true, TTL: time.Minute,
		}
		_, err = service.Deliver(context.Background(), request, nil)
		if err == nil {
			_, err = service.Deliver(context.Background(), request, nil)
		}
		serverDone <- err
	}))
	defer server.Close()

	url := "ws" + strings.TrimPrefix(server.URL, "http")
	connection, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer connection.Close()

	messageType, payload, err := connection.ReadMessage()
	if err != nil || messageType != websocket.TextMessage {
		t.Fatalf("read reply frame: type=%d err=%v", messageType, err)
	}
	var reply map[string]interface{}
	if err := json.Unmarshal(payload, &reply); err != nil {
		t.Fatalf("decode reply: %v", err)
	}
	data, _ := reply["data"].(map[string]interface{})
	if reply["event"] != "qiuqiu_reply" || data["traceId"] != "trace-ws" || data["deliveryKey"] != "goal:ws" {
		t.Fatalf("unexpected reply frame: %v", reply)
	}

	messageType, payload, err = connection.ReadMessage()
	if err != nil || messageType != websocket.TextMessage {
		t.Fatalf("read audio metadata: type=%d err=%v", messageType, err)
	}
	var metadata map[string]interface{}
	if err := json.Unmarshal(payload, &metadata); err != nil {
		t.Fatalf("decode audio metadata: %v", err)
	}
	if metadata["type"] != "voice_audio" || metadata["traceId"] != "trace-ws" || metadata["deliveryKey"] != "goal:ws" {
		t.Fatalf("unexpected audio metadata: %v", metadata)
	}
	messageType, payload, err = connection.ReadMessage()
	if err != nil || messageType != websocket.BinaryMessage || string(payload) != "audio" {
		t.Fatalf("read audio frame: type=%d payload=%q err=%v", messageType, payload, err)
	}
	if err := <-serverDone; err != nil {
		t.Fatalf("server delivery: %v", err)
	}
	connection.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	if _, _, err := connection.ReadMessage(); err == nil {
		t.Fatal("duplicate delivery emitted an extra websocket frame")
	}
}

func TestWebsocketReconnectRecoversCriticalTextUntilAcknowledged(t *testing.T) {
	now := time.Now().UTC()
	ledger := conversation.NewMemoryDeliveryLedger()
	_, err := ledger.Plan(conversation.DeliveryRecord{
		Key: "trace-recover", TraceID: "trace-recover", UserID: "user-recover", MatchID: "match-recover",
		DeliveryKey: "goal:recover", State: conversation.DeliveryPlanned, Critical: true,
		ExpiresAt: now.Add(time.Minute), UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("plan recovery: %v", err)
	}
	if _, err := ledger.Transition("trace-recover", conversation.DeliveryTextDelivered, now); err != nil {
		t.Fatalf("deliver recovery text: %v", err)
	}
	session := conversation.NewWatchSession(context.Background(), "user-recover", "match-recover", conversation.Config{}, ledger)
	defer session.Close()
	reader := recoveryTraceReader{trace: companion.Trace{
		ID: "trace-recover", UserID: "user-recover", MatchID: "match-recover", Output: "关键进球仍需展示。",
	}}
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connection, upgradeErr := upgrader.Upgrade(w, r, nil)
		if upgradeErr != nil {
			return
		}
		defer connection.Close()
		recoverPendingDeliveries(context.Background(), &wsWriter{conn: connection}, session, reader, "user-recover", "match-recover")
	}))
	defer server.Close()
	url := "ws" + strings.TrimPrefix(server.URL, "http")

	first, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial first connection: %v", err)
	}
	_, payload, err := first.ReadMessage()
	first.Close()
	if err != nil {
		t.Fatalf("read recovered reply: %v", err)
	}
	var recovered map[string]interface{}
	if err := json.Unmarshal(payload, &recovered); err != nil {
		t.Fatalf("decode recovered reply: %v", err)
	}
	data, _ := recovered["data"].(map[string]interface{})
	if data["traceId"] != "trace-recover" || data["source"] != "recovered_delivery" || data["deliveryKey"] != "goal:recover" {
		t.Fatalf("unexpected recovered reply: %v", recovered)
	}

	if _, err := ledger.AcknowledgeText("trace-recover", now.Add(time.Second)); err != nil {
		t.Fatalf("acknowledge text: %v", err)
	}
	second, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial second connection: %v", err)
	}
	defer second.Close()
	second.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	if _, _, err := second.ReadMessage(); err == nil {
		t.Fatal("acknowledged text was recovered again")
	}
}

type recoveryTraceReader struct{ trace companion.Trace }

func (reader recoveryTraceReader) ListTraces(context.Context, string, int) ([]companion.Trace, error) {
	return []companion.Trace{reader.trace}, nil
}

func (reader recoveryTraceReader) GetTrace(_ context.Context, matchID, traceID string) (companion.Trace, error) {
	if reader.trace.MatchID != matchID || reader.trace.ID != traceID {
		return companion.Trace{}, companion.ErrTraceNotFound
	}
	return reader.trace, nil
}

type responseSynthesizerForTest struct{}

func (responseSynthesizerForTest) SynthesizeResponse(context.Context, string, relationship.PresentationPlan, []relationship.CommunicationAct) (conversation.SynthesizedAudio, error) {
	return conversation.SynthesizedAudio{Data: []byte("audio"), MIME: "audio/mpeg"}, nil
}

// memory-surfacing 1.3:reason 字段契约——主动回合(带理由码)的 wire 出
// reason 对象;普通回合(无 decision)reason 缺席,客户端零变化。
func TestReplyReasonWireContract(t *testing.T) {
	if got := replyReasonWire(nil); got != nil {
		t.Fatalf("nil codes = %v, want nil (absent on wire)", got)
	}
	if got := replyReasonWire([]string{}); got != nil {
		t.Fatalf("empty codes = %v, want nil", got)
	}
	reason := replyReasonWire([]string{"reminder_due", "proactive_citation:reminder:rem-1"})
	if reason == nil {
		t.Fatal("proactive codes must produce a reason object")
	}
	if citation, _ := reason["citation"].(string); citation != "reminder:rem-1" {
		t.Fatalf("citation = %v, want reminder:rem-1", reason["citation"])
	}
	codes, _ := reason["codes"].([]string)
	if len(codes) != 2 {
		t.Fatalf("codes = %v, want both", reason["codes"])
	}
}

func TestWebsocketReplyCarriesReasonForProactiveTurn(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	serverDone := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connection, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			serverDone <- err
			return
		}
		defer connection.Close()
		writer := &wsWriter{conn: connection}
		tracker := newReplyDeliveryTracker()
		service := conversation.NewResponseDeliveryService(
			websocketResponseSink{writer: writer},
			responseSynthesizerForTest{},
			tracker,
			nil,
		)
		request := conversation.ResponseDeliveryRequest{
			Reply: "你订的开球提醒,比赛要开始了。",
			Trace: companion.Trace{ID: "trace-reason", UserID: "user-reason", MatchID: "match-reason",
				RelationshipDecision: &relationship.Decision{ReasonCodes: []string{"proactive_citation:reminder:rem-9"}}},
			Source: "reminder", DeliveryKey: "reminder:rem-9", TTL: time.Minute,
		}
		_, err = service.Deliver(context.Background(), request, nil)
		serverDone <- err
	}))
	defer server.Close()

	url := "ws" + strings.TrimPrefix(server.URL, "http")
	connection, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer connection.Close()
	_, payload, err := connection.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var reply map[string]interface{}
	if err := json.Unmarshal(payload, &reply); err != nil {
		t.Fatalf("decode: %v", err)
	}
	data, _ := reply["data"].(map[string]interface{})
	reason, ok := data["reason"].(map[string]interface{})
	if !ok {
		t.Fatalf("reason absent on proactive reply: %v", data)
	}
	if citation, _ := reason["citation"].(string); citation != "reminder:rem-9" {
		t.Fatalf("citation = %v", reason["citation"])
	}
	if err := <-serverDone; err != nil {
		t.Fatalf("server delivery: %v", err)
	}
}
