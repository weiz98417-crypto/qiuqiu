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
	SynthesizeResponse(context.Context, string, relationship.PresentationPlan) (SynthesizedAudio, error)
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

	service.mu.Lock()
	if existing, ok := service.tracker.LookupByDeliveryKey(request.DeliveryKey); ok && existing.TraceID != request.Trace.ID {
		service.mu.Unlock()
		result.Duplicate = true
		return result, nil
	}
	record, exists := service.tracker.Lookup(request.Trace.ID)
	if exists && (isTerminalDeliveryState(record.State) || record.State == DeliveryAudioStarted) {
		service.mu.Unlock()
		result.Duplicate = true
		return result, nil
	}
	if !exists {
		if err := service.tracker.TrackWithPolicy(request.Trace, request.Trace.UserID, request.Trace.MatchID, request.DeliveryKey, request.Critical, request.TTL); err != nil {
			if existing, ok := service.tracker.LookupByDeliveryKey(request.DeliveryKey); ok && existing.TraceID != request.Trace.ID {
				service.mu.Unlock()
				result.Duplicate = true
				return result, nil
			}
			service.mu.Unlock()
			return result, fmt.Errorf("plan response delivery: %w", err)
		}
		record = DeliveryRecord{State: DeliveryPlanned}
	}
	if record.State == DeliveryPlanned {
		err := service.sink.DeliverReply(ctx, ReplyDelivery{
			Text: request.Reply, TraceID: request.Trace.ID, Source: request.Source,
			EventID: request.EventID, DeliveryKey: request.DeliveryKey, Presentation: request.Presentation,
		})
		if err != nil {
			_ = service.tracker.Transition(request.Trace.ID, DeliveryFailed, service.timestamp())
			service.mu.Unlock()
			return result, fmt.Errorf("deliver response text: %w", err)
		}
		if err := service.tracker.Transition(request.Trace.ID, DeliveryTextDelivered, service.timestamp()); err != nil {
			service.mu.Unlock()
			return result, fmt.Errorf("record response text delivery: %w", err)
		}
		result.TextDelivered = true
	}
	if request.AfterTextStatus != nil {
		status := *request.AfterTextStatus
		if status.TraceID == "" {
			status.TraceID = request.Trace.ID
		}
		if err := service.sink.DeliverStatus(ctx, status); err != nil {
			service.mu.Unlock()
			return result, fmt.Errorf("deliver response status: %w", err)
		}
	}
	if request.AfterText != nil {
		if err := request.AfterText(ctx); err != nil {
			service.mu.Unlock()
			return result, fmt.Errorf("run post-text delivery action: %w", err)
		}
	}
	service.mu.Unlock()

	if service.synthesizer == nil {
		return service.completeWithFallback(ctx, request, result, "tts unavailable", nil)
	}
	audio, synthErr := service.synthesizer.SynthesizeResponse(ctx, request.Reply, request.Presentation)
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

	service.mu.Lock()
	defer service.mu.Unlock()
	if current, ok := service.tracker.Lookup(request.Trace.ID); ok && (isTerminalDeliveryState(current.State) || current.State == DeliveryAudioStarted) {
		result.Duplicate = true
		return result, nil
	}
	if err := service.tracker.Transition(request.Trace.ID, DeliveryAudioStarted, service.timestamp()); err != nil {
		return result, fmt.Errorf("record response audio start: %w", err)
	}
	if err := service.sink.DeliverAudio(ctx, AudioDelivery{
		Data: audio.Data, MIME: audio.MIME, TraceID: request.Trace.ID, Source: request.Source,
		EventID: request.EventID, DeliveryKey: request.DeliveryKey,
	}); err != nil {
		_ = service.tracker.Transition(request.Trace.ID, DeliveryFailed, service.timestamp())
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
	service.mu.Lock()
	defer service.mu.Unlock()
	if current, ok := service.tracker.Lookup(request.Trace.ID); ok && isTerminalDeliveryState(current.State) {
		result.Duplicate = true
		return result, nil
	}
	result.FallbackReason = reason
	statusErr := service.sink.DeliverStatus(ctx, DeliveryStatus{Kind: "voice", State: "tts_fallback", Reason: reason, TraceID: request.Trace.ID})
	mediaState := "skipped"
	if cause != nil || reason == "empty audio" {
		mediaState = "failed"
	}
	recordErr := service.recordMedia(ctx, request, "", mediaState, reason)
	transitionErr := service.tracker.Transition(request.Trace.ID, DeliveryCompleted, service.timestamp())
	return result, errors.Join(statusErr, recordErr, transitionErr)
}

func (service *ResponseDeliveryService) interrupt(traceID string) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if current, ok := service.tracker.Lookup(traceID); ok && !isTerminalDeliveryState(current.State) {
		_ = service.tracker.Transition(traceID, DeliveryInterrupted, service.timestamp())
	}
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
