package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"qiuqiu/internal/interaction"
	"qiuqiu/internal/ws"

	"github.com/gorilla/websocket"
)

// ── 运营观测实时流（operations-live-stream，ADR-0021 隐私三层之实时流）──
//
// 事件源是 interaction ledger Append 的旁路（observingLedger 装饰器），
// 广播前在结构体层面裁剪：正文字段（phrase/inputText/outputText/decision/
// presentation/trace 载荷）不进 wire 形状，而非靠约定不填。饱和即丢、
// 只计数——观测永不反伤落库主链路。

// OpsEventWire 是 /ws/ops 下行的事件形状：裁剪发生在序列化之前。
type OpsEventWire struct {
	Type          string `json:"type"`
	Kind          string `json:"kind"`
	MatchID       string `json:"matchId"`
	UserID        string `json:"userId"`
	TraceID       string `json:"traceId,omitempty"`
	DeliveryState string `json:"deliveryState,omitempty"`
	PlaybackState string `json:"playbackState,omitempty"`
	LatencyMS     int    `json:"latencyMs,omitempty"`
	At            string `json:"at"`
}

// trimOpsEvent 把账本事件裁成 wire 形状；turn_planned 事件从 trace 载荷提
// 取 latencyMs（唯一廉价可得延迟的 kind），载荷本身不下发。
func trimOpsEvent(event interaction.Event) OpsEventWire {
	wire := OpsEventWire{
		Type:          "ops_event",
		Kind:          string(event.Kind),
		MatchID:       event.MatchID,
		UserID:        event.UserID,
		TraceID:       event.TraceID,
		DeliveryState: event.DeliveryState,
		PlaybackState: event.PlaybackState,
		At:            event.CreatedAt.UTC().Format(time.RFC3339),
	}
	if event.Kind == interaction.KindTurnPlanned && len(event.TracePayload) > 0 {
		var payload struct {
			LatencyMS int `json:"latencyMs"`
		}
		if json.Unmarshal(event.TracePayload, &payload) == nil {
			wire.LatencyMS = payload.LatencyMS
		}
	}
	return wire
}

// OpsStream 维护 ops 订阅连接并把旁路事件广播出去。零订阅时事件在入队
// 前直接丢弃；队列饱和丢弃并计数（Dropped 暴露给诊断/面板）。
type OpsStream struct {
	queue   chan interaction.Event
	mu      sync.Mutex
	writers map[websocketWriter]struct{}
	dropped atomic.Int64
}

// websocketWriter 是订阅连接的最小写面（真身 *websocket.Conn——指针类型
// 可比较，可直接作 map 键；测试用假件），避免 OpsStream 依赖具体连接类型。
type websocketWriter interface {
	WriteJSON(msg interface{}) error
}

// NewOpsStream 建流；Run 启动泵（测试可先注入假件再 Run，或不起泵直接
// 观察入队/丢弃）。
func NewOpsStream(queueSize int) *OpsStream {
	if queueSize <= 0 {
		queueSize = 256
	}
	return &OpsStream{queue: make(chan interaction.Event, queueSize), writers: map[websocketWriter]struct{}{}}
}

// Attach 注册一个订阅连接的写面。
func (s *OpsStream) Attach(writer websocketWriter) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writers[writer] = struct{}{}
}

// Detach 注销；写失败时由泵注销。
func (s *OpsStream) Detach(writer websocketWriter) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.writers, writer)
}

// Observe 是 observingLedger 的回调入口：非阻塞入队，饱和丢弃计数。
// 零订阅同样丢计入——它就是「这套观测现在没人在看」的量化。
func (s *OpsStream) Observe(event interaction.Event) {
	select {
	case s.queue <- event:
	default:
		s.dropped.Add(1)
	}
}

// Dropped 返回累计丢弃数（诊断/面板留尾用）。
func (s *OpsStream) Dropped() int64 { return s.dropped.Load() }

// Run 驱动广播泵直到 ctx 取消：取出事件、裁剪、扇出到全部订阅者；写失
// 败的订阅者即注销。
func (s *OpsStream) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-s.queue:
			wire := trimOpsEvent(event)
			s.mu.Lock()
			writers := make([]websocketWriter, 0, len(s.writers))
			for writer := range s.writers {
				writers = append(writers, writer)
			}
			s.mu.Unlock()
			for _, writer := range writers {
				if err := writer.WriteJSON(wire); err != nil {
					s.Detach(writer)
					log.Printf("ops stream write error: %v", err)
				}
			}
		}
	}
}

// observingLedger 是 interaction.Ledger 的观察者装饰器：Append 成功后把
// 事件喂给旁路观察者；失败（冲突/校验）不喂——观测只看真实发生的事。
// 删除测试：摘掉这层装饰器，ledger 原样，无悬空依赖。
type observingLedger struct {
	inner    interaction.Ledger
	observer func(interaction.Event)
}

func newObservingLedger(inner interaction.Ledger, observer func(interaction.Event)) *observingLedger {
	return &observingLedger{inner: inner, observer: observer}
}

func (l *observingLedger) Append(ctx context.Context, event interaction.Event) (interaction.Event, error) {
	appended, err := l.inner.Append(ctx, event)
	if err != nil {
		return appended, err
	}
	if l.observer != nil {
		l.observer(appended)
	}
	return appended, nil
}

func (l *observingLedger) List(ctx context.Context, userID, matchID string, limit int) ([]interaction.Event, error) {
	return l.inner.List(ctx, userID, matchID, limit)
}

func (l *observingLedger) ListUser(ctx context.Context, userID string, limit int) ([]interaction.Event, error) {
	return l.inner.ListUser(ctx, userID, limit)
}

// 可选接口转发：调用方对 ledger 做 PageableLedger/SnapshotLedger/
// MatchSnapshotLedger 类型断言（交互分页与 trace 投影），包装器若不转发
// 会把这些能力藏掉（session-isolation eval 实测 500）。内层不支持时返回
// 不支持，与直接使用内层的断言语义一致。
func (l *observingLedger) ListPage(ctx context.Context, query interaction.PageQuery) (interaction.Page, error) {
	pageable, ok := l.inner.(interaction.PageableLedger)
	if !ok {
		return interaction.Page{}, interaction.ErrInvalidCursor
	}
	return pageable.ListPage(ctx, query)
}

func (l *observingLedger) ListSnapshot(ctx context.Context, userID, matchID string) ([]interaction.Event, error) {
	snapshot, ok := l.inner.(interaction.SnapshotLedger)
	if !ok {
		return nil, interaction.ErrSnapshotUnavailable
	}
	return snapshot.ListSnapshot(ctx, userID, matchID)
}

func (l *observingLedger) ListMatchSnapshot(ctx context.Context, matchID string) ([]interaction.Event, error) {
	snapshot, ok := l.inner.(interaction.MatchSnapshotLedger)
	if !ok {
		return nil, interaction.ErrSnapshotUnavailable
	}
	return snapshot.ListMatchSnapshot(ctx, matchID)
}

// handleOpsStream 是 /ws/ops 的 HTTP 入口：UpgradeOps 放行后进入只读监听
// 循环——上行不收任何业务消息（ping 回 pong 保持代理活跃），下行由泵扇出。
func handleOpsStream(hub *ws.Hub, stream *OpsStream) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, release, err := hub.UpgradeOps(w, r)
		if err != nil {
			return
		}
		defer release()
		defer conn.Close()

		if err := conn.WriteJSON(map[string]string{"type": "welcome", "stream": "ops"}); err != nil {
			return
		}
		stream.Attach(conn)
		defer stream.Detach(conn)

		readDone := make(chan struct{})
		go func() {
			defer close(readDone)
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
			}
		}()

		pingTicker := time.NewTicker(30 * time.Second)
		defer pingTicker.Stop()
		for {
			select {
			case <-readDone:
				return
			case <-pingTicker.C:
				_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					return
				}
			}
		}
	}
}
