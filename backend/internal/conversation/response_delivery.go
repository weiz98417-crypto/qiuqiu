package conversation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"qiuqiu/internal/companion"
	"qiuqiu/internal/relationship"
)

type ReplyDelivery struct {
	Text         string
	TraceID      string
	Source       string
	EventID      string
	DeliveryKey  string
	Presentation relationship.PresentationPlan
}

type AudioDelivery struct {
	Data        []byte
	MIME        string
	TraceID     string
	Source      string
	EventID     string
	DeliveryKey string
}

type DeliveryStatus struct {
	Kind    string
	State   string
	Reason  string
	TraceID string
}

type ResponseSink interface {
	DeliverReply(context.Context, ReplyDelivery) error
	DeliverAudio(context.Context, AudioDelivery) error
	DeliverStatus(context.Context, DeliveryStatus) error
}

type SynthesizedAudio struct {
	Data []byte
	MIME string
}

type ResponseAudioSynthesizer interface {
	// acts 是该回合决策的沟通动作序列：与表演计划内嵌的情绪状态一起，
	// 构成语音表演指令映射（情绪×动作→指令）的两个输入。
	SynthesizeResponse(context.Context, string, relationship.PresentationPlan, []relationship.CommunicationAct) (SynthesizedAudio, error)
}

type MediaDeliveryEvent struct {
	ID            string
	UserID        string
	MatchID       string
	TraceID       string
	DeliveryKey   string
	MediaType     string
	DeliveryState string
	Reason        string
	Source        string
	CreatedAt     time.Time
}

type MediaDeliveryRecorder interface {
	RecordMediaDelivery(context.Context, MediaDeliveryEvent) error
}

type MediaDeliveryRecorderFunc func(context.Context, MediaDeliveryEvent) error

func (record MediaDeliveryRecorderFunc) RecordMediaDelivery(ctx context.Context, event MediaDeliveryEvent) error {
	return record(ctx, event)
}

type ResponseDeliveryTracker interface {
	TrackWithPolicy(companion.Trace, string, string, string, bool, time.Duration) error
	Transition(string, DeliveryState, time.Time) error
	Lookup(string) (DeliveryRecord, bool)
	LookupByDeliveryKey(string) (DeliveryRecord, bool)
}

type ResponseDeliveryRequest struct {
	Reply           string
	Trace           companion.Trace
	Presentation    relationship.PresentationPlan
	Source          string
	EventID         string
	DeliveryKey     string
	Critical        bool
	TTL             time.Duration
	AfterTextStatus *DeliveryStatus
	AfterText       func(context.Context) error
}

type ResponseDeliveryResult struct {
	Duplicate      bool
	TextDelivered  bool
	AudioDelivered bool
	FallbackReason string
}

type ResponseDeliveryService struct {
	sink        ResponseSink
	synthesizer ResponseAudioSynthesizer
	tracker     ResponseDeliveryTracker
	recorder    MediaDeliveryRecorder
	now         func() time.Time
	mu          sync.Mutex
}

func NewResponseDeliveryService(sink ResponseSink, synthesizer ResponseAudioSynthesizer, tracker ResponseDeliveryTracker, recorder MediaDeliveryRecorder) *ResponseDeliveryService {
	return &ResponseDeliveryService{
		sink:        sink,
		synthesizer: synthesizer,
		tracker:     tracker,
		recorder:    recorder,
		now:         func() time.Time { return time.Now().UTC() },
	}
}

// deliveryRoundPlan 是加锁规划半的产物（deep-water-polish 1.2）：一轮投递的
// 全部状态判定与队列操作结果；I/O 半锁外执行——任何网络调用都不持锁。
type deliveryRoundPlan struct {
	duplicate       bool
	sendText        bool
	reply           ReplyDelivery
	afterTextStatus *DeliveryStatus
	afterText       func(context.Context) error
}

// withPlanningLock is the scoped-lock primitive that replaces every manually
// paired Lock/Unlock: a new early return can no longer deadlock or double
// unlock the planning half.
func (service *ResponseDeliveryService) withPlanningLock(fn func() error) error {
	service.mu.Lock()
	defer service.mu.Unlock()
	return fn()
}

// transition queues a ledger state change under the planning lock.
func (service *ResponseDeliveryService) transition(traceID string, state DeliveryState) {
	_ = service.withPlanningLock(func() error {
		return service.tracker.Transition(traceID, state, service.timestamp())
	})
}

// deliveryIsTerminal reports terminal state under the planning lock.
func (service *ResponseDeliveryService) deliveryIsTerminal(traceID string) bool {
	terminal := false
	_ = service.withPlanningLock(func() error {
		if current, ok := service.tracker.Lookup(traceID); ok && isTerminalDeliveryState(current.State) {
			terminal = true
		}
		return nil
	})
	return terminal
}

// planResponseRound is the locked planning half of a text round: duplicate
// decisions, the TrackWithPolicy queue op and the TextDelivered claim
// transition all happen under the lock; nothing here performs I/O.
func (service *ResponseDeliveryService) planResponseRound(request ResponseDeliveryRequest) (deliveryRoundPlan, error) {
	plan := deliveryRoundPlan{}
	err := service.withPlanningLock(func() error {
		if existing, ok := service.tracker.LookupByDeliveryKey(request.DeliveryKey); ok && existing.TraceID != request.Trace.ID {
			plan.duplicate = true
			return nil
		}
		record, exists := service.tracker.Lookup(request.Trace.ID)
		if exists && (isTerminalDeliveryState(record.State) || record.State == DeliveryAudioStarted) {
			plan.duplicate = true
			return nil
		}
		if !exists {
			if err := service.tracker.TrackWithPolicy(request.Trace, request.Trace.UserID, request.Trace.MatchID, request.DeliveryKey, request.Critical, request.TTL); err != nil {
				if existing, ok := service.tracker.LookupByDeliveryKey(request.DeliveryKey); ok && existing.TraceID != request.Trace.ID {
					plan.duplicate = true
					return nil
				}
				return fmt.Errorf("plan response delivery: %w", err)
			}
			record = DeliveryRecord{State: DeliveryPlanned}
		}
		if record.State == DeliveryPlanned {
			// The TextDelivered transition is queued inside the planning
			// half as this round's claim: concurrent rounds observe it and
			// skip the text while the I/O half runs unlocked.
			if err := service.tracker.Transition(request.Trace.ID, DeliveryTextDelivered, service.timestamp()); err != nil {
				return fmt.Errorf("record response text delivery: %w", err)
			}
			plan.sendText = true
		}
		plan.reply = ReplyDelivery{
			Text: request.Reply, TraceID: request.Trace.ID, Source: request.Source,
			EventID: request.EventID, DeliveryKey: request.DeliveryKey, Presentation: request.Presentation,
		}
		if request.AfterTextStatus != nil {
			status := *request.AfterTextStatus
			if status.TraceID == "" {
				status.TraceID = request.Trace.ID
			}
			plan.afterTextStatus = &status
		}
		plan.afterText = request.AfterText
		return nil
	})
	return plan, err
}

// planAudioRound is the locked planning half of the audio round: the terminal/
// duplicate decision plus the AudioStarted claim transition.
func (service *ResponseDeliveryService) planAudioRound(request ResponseDeliveryRequest) (bool, error) {
	send := false
	err := service.withPlanningLock(func() error {
		if current, ok := service.tracker.Lookup(request.Trace.ID); ok && (isTerminalDeliveryState(current.State) || current.State == DeliveryAudioStarted) {
			return nil
		}
		if err := service.tracker.Transition(request.Trace.ID, DeliveryAudioStarted, service.timestamp()); err != nil {
			return fmt.Errorf("record response audio start: %w", err)
		}
		send = true
		return nil
	})
	return send, err
}

func (service *ResponseDeliveryService) Deliver(ctx context.Context, request ResponseDeliveryRequest, playback Playback) (ResponseDeliveryResult, error) {
	result := ResponseDeliveryResult{}
	if service == nil || service.sink == nil || service.tracker == nil {
		return result, errors.New("response delivery service is not configured")
	}
	if ctx == nil || ctx.Err() != nil {
		return result, context.Canceled
	}
	request.Reply = strings.TrimSpace(request.Reply)
	if request.Trace.ID == "" || request.Trace.UserID == "" || request.Trace.MatchID == "" || request.Reply == "" {
		return result, errors.New("response delivery request is incomplete")
	}
	if request.TTL <= 0 {
		request.TTL = 30 * time.Second
	}
	if request.DeliveryKey == "" {
		request.DeliveryKey = request.Trace.ID
	}

	// ── 加锁规划半：状态判定 + 队列操作，产出投递计划。──
	plan, err := service.planResponseRound(request)
	if err != nil {
		return result, err
	}
	if plan.duplicate {
		result.Duplicate = true
		return result, nil
	}

	// ── 无锁 I/O 半：sink/回调全部在锁外执行。──
	if plan.sendText {
		if err := service.sink.DeliverReply(ctx, plan.reply); err != nil {
			service.transition(request.Trace.ID, DeliveryFailed)
			return result, fmt.Errorf("deliver response text: %w", err)
		}
		result.TextDelivered = true
	}
	if plan.afterTextStatus != nil {
		if err := service.sink.DeliverStatus(ctx, *plan.afterTextStatus); err != nil {
			return result, fmt.Errorf("deliver response status: %w", err)
		}
	}
	if plan.afterText != nil {
		if err := plan.afterText(ctx); err != nil {
			return result, fmt.Errorf("run post-text delivery action: %w", err)
		}
	}

	if service.synthesizer == nil {
		return service.completeWithFallback(ctx, request, result, "tts unavailable", nil)
	}
	var acts []relationship.CommunicationAct
	if request.Trace.RelationshipDecision != nil {
		acts = request.Trace.RelationshipDecision.Actions
	}
	audio, synthErr := service.synthesizer.SynthesizeResponse(ctx, request.Reply, request.Presentation, acts)
	if synthErr != nil {
		if ctx.Err() != nil {
			service.interrupt(request.Trace.ID)
			return result, ctx.Err()
		}
		return service.completeWithFallback(ctx, request, result, synthErr.Error(), synthErr)
	}
	if len(audio.Data) == 0 {
		return service.completeWithFallback(ctx, request, result, "empty audio", nil)
	}
	if ctx.Err() != nil {
		service.interrupt(request.Trace.ID)
		return result, ctx.Err()
	}
	if strings.TrimSpace(audio.MIME) == "" {
		audio.MIME = "audio/mpeg"
	}

	// ── 加锁规划半（音频）：终态/重复判定 + AudioStarted 占位迁移。──
	sendAudio, err := service.planAudioRound(request)
	if err != nil {
		return result, err
	}
	if !sendAudio {
		result.Duplicate = true
		return result, nil
	}

	// ── 无锁 I/O 半（音频）。──
	if err := service.sink.DeliverAudio(ctx, AudioDelivery{
		Data: audio.Data, MIME: audio.MIME, TraceID: request.Trace.ID, Source: request.Source,
		EventID: request.EventID, DeliveryKey: request.DeliveryKey,
	}); err != nil {
		service.transition(request.Trace.ID, DeliveryFailed)
		_ = service.recordMedia(ctx, request, audio.MIME, "failed", err.Error())
		return result, fmt.Errorf("deliver response audio: %w", err)
	}
	if playback != nil {
		playback(request.Trace.ID)
	}
	result.AudioDelivered = true
	if err := service.recordMedia(ctx, request, audio.MIME, "audio_started", ""); err != nil {
		return result, fmt.Errorf("record response audio delivery: %w", err)
	}
	return result, nil
}

func (service *ResponseDeliveryService) completeWithFallback(ctx context.Context, request ResponseDeliveryRequest, result ResponseDeliveryResult, reason string, cause error) (ResponseDeliveryResult, error) {
	// 加锁判定半：终态去重与非 Critical 的 Completed 声明必须原子完成——
	// 否则两个并发 fallback 都会通过检查、双发 tts_fallback 并重复记录
	// 媒体。注意 mu 不可重入：终态检查必须内联，不能调 deliveryIsTerminal。
	claimed := true
	duplicate := false
	_ = service.withPlanningLock(func() error {
		if current, ok := service.tracker.Lookup(request.Trace.ID); ok && isTerminalDeliveryState(current.State) {
			claimed = false
			duplicate = true
			return nil
		}
		if !request.Critical {
			// 非 Critical：在此声明 Completed 终态，后续并发 fallback 与
			// interrupt 都被终态挡住。Critical 保持不迁移（既有语义）。
			_ = service.tracker.Transition(request.Trace.ID, DeliveryCompleted, service.timestamp())
		}
		return nil
	})
	if !claimed {
		result.Duplicate = duplicate
		return result, nil
	}
	// 无锁 I/O 半：fallback 状态与媒体记录。
	result.FallbackReason = reason
	statusErr := service.sink.DeliverStatus(ctx, DeliveryStatus{Kind: "voice", State: "tts_fallback", Reason: reason, TraceID: request.Trace.ID})
	mediaState := "skipped"
	if cause != nil || reason == "empty audio" {
		mediaState = "failed"
	}
	recordErr := service.recordMedia(ctx, request, "", mediaState, reason)
	return result, errors.Join(statusErr, recordErr)
}

func (service *ResponseDeliveryService) interrupt(traceID string) {
	_ = service.withPlanningLock(func() error {
		if current, ok := service.tracker.Lookup(traceID); ok && !isTerminalDeliveryState(current.State) {
			_ = service.tracker.Transition(traceID, DeliveryInterrupted, service.timestamp())
		}
		return nil
	})
}

func (service *ResponseDeliveryService) recordMedia(ctx context.Context, request ResponseDeliveryRequest, mediaType, state, reason string) error {
	if service.recorder == nil {
		return nil
	}
	return service.recorder.RecordMediaDelivery(ctx, MediaDeliveryEvent{
		ID:     "media:" + request.Trace.UserID + ":" + request.Trace.MatchID + ":" + request.Trace.ID + ":" + state,
		UserID: request.Trace.UserID, MatchID: request.Trace.MatchID, TraceID: request.Trace.ID,
		DeliveryKey: request.DeliveryKey, MediaType: mediaType, DeliveryState: state,
		Reason: reason, Source: request.Source, CreatedAt: service.timestamp(),
	})
}

func (service *ResponseDeliveryService) timestamp() time.Time {
	if service.now == nil {
		return time.Now().UTC()
	}
	return service.now().UTC()
}

type FirstMeetingPlanner interface {
	HandleFirstMeeting(context.Context, companion.FirstMeetingRequest) (companion.ProactiveResponse, error)
}

type FirstMeetingCoordinator struct {
	planner  FirstMeetingPlanner
	delivery *ResponseDeliveryService
}

func NewFirstMeetingCoordinator(planner FirstMeetingPlanner, delivery *ResponseDeliveryService) *FirstMeetingCoordinator {
	return &FirstMeetingCoordinator{planner: planner, delivery: delivery}
}

func (coordinator *FirstMeetingCoordinator) Handle(ctx context.Context, request companion.FirstMeetingRequest, playback Playback) (ResponseDeliveryResult, error) {
	if coordinator == nil || coordinator.planner == nil || coordinator.delivery == nil {
		return ResponseDeliveryResult{}, errors.New("first meeting coordinator is not configured")
	}
	response, err := coordinator.planner.HandleFirstMeeting(ctx, request)
	if err != nil {
		if ctx != nil && ctx.Err() == nil {
			_ = coordinator.delivery.sink.DeliverStatus(ctx, DeliveryStatus{Kind: "voice", State: "failed", Reason: "greeting unavailable"})
		}
		return ResponseDeliveryResult{}, err
	}
	if strings.TrimSpace(response.Reply) == "" {
		if ctx == nil || ctx.Err() != nil {
			return ResponseDeliveryResult{}, context.Canceled
		}
		return ResponseDeliveryResult{}, coordinator.delivery.sink.DeliverStatus(ctx, DeliveryStatus{Kind: "first_meeting", State: "skipped", Reason: response.Trace.Reason})
	}
	return coordinator.delivery.Deliver(ctx, ResponseDeliveryRequest{
		Reply: response.Reply, Trace: response.Trace, Presentation: response.Presentation,
		Source: "first_meeting", DeliveryKey: response.Trace.ID, TTL: 30 * time.Second,
		AfterTextStatus: &DeliveryStatus{Kind: "first_meeting", State: "delivered", TraceID: response.Trace.ID},
	}, playback)
}
