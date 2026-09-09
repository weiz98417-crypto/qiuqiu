package conversation

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"qiuqiu/internal/companion"
	"qiuqiu/internal/relationship"
)

type responseSinkStub struct {
	mu        sync.Mutex
	replies   []ReplyDelivery
	audio     []AudioDelivery
	statuses  []DeliveryStatus
	audioErr  error
	statusErr error
}

func (sink *responseSinkStub) DeliverReply(_ context.Context, delivery ReplyDelivery) error {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	sink.replies = append(sink.replies, delivery)
	return nil
}

func (sink *responseSinkStub) DeliverAudio(_ context.Context, delivery AudioDelivery) error {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	sink.audio = append(sink.audio, delivery)
	return sink.audioErr
}

func (sink *responseSinkStub) DeliverStatus(_ context.Context, status DeliveryStatus) error {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	sink.statuses = append(sink.statuses, status)
	return sink.statusErr
}

type responseSynthesizerStub struct {
	audio SynthesizedAudio
	err   error
	calls int
}

func (stub *responseSynthesizerStub) SynthesizeResponse(context.Context, string, relationship.PresentationPlan) (SynthesizedAudio, error) {
	stub.calls++
	return stub.audio, stub.err
}

type responseTrackerStub struct{ core *DeliveryTracker }

func newResponseTrackerStub() *responseTrackerStub {
	return &responseTrackerStub{core: NewDeliveryTracker(nil)}
}

func (tracker *responseTrackerStub) TrackWithPolicy(trace companion.Trace, userID, matchID, deliveryKey string, critical bool, ttl time.Duration) error {
	now := time.Date(2026, 9, 8, 10, 0, 0, 0, time.UTC)
	return tracker.core.Plan(DeliveryRecord{Key: trace.ID, TraceID: trace.ID, UserID: userID, MatchID: matchID, DeliveryKey: deliveryKey, Critical: critical, State: DeliveryPlanned, ExpiresAt: now.Add(ttl), UpdatedAt: now}, nil)
}

func (tracker *responseTrackerStub) Transition(key string, state DeliveryState, at time.Time) error {
	return tracker.core.Transition(key, state, at)
}

func (tracker *responseTrackerStub) Lookup(key string) (DeliveryRecord, bool) {
	return tracker.core.Ledger().Get(key)
}

func (tracker *responseTrackerStub) LookupByDeliveryKey(deliveryKey string) (DeliveryRecord, bool) {
	return tracker.core.Ledger().FindByDeliveryKey(deliveryKey)
}

func responseRequest() ResponseDeliveryRequest {
	return ResponseDeliveryRequest{
		Reply:        "这球漂亮。",
		Trace:        companion.Trace{ID: "trace-1", UserID: "user-1", MatchID: "match-1"},
		Presentation: relationship.PresentationPlan{Expression: "happy"},
		Source:       "match_reaction", EventID: "event-1", DeliveryKey: "goal:1", Critical: true, TTL: time.Minute,
	}
}

func TestResponseDeliveryServiceDeliversTextAndAudioOnce(t *testing.T) {
	sink := &responseSinkStub{}
	synthesizer := &responseSynthesizerStub{audio: SynthesizedAudio{Data: []byte{1, 2, 3}, MIME: "audio/mpeg"}}
	tracker := newResponseTrackerStub()
	var media []MediaDeliveryEvent
	service := NewResponseDeliveryService(sink, synthesizer, tracker, MediaDeliveryRecorderFunc(func(_ context.Context, event MediaDeliveryEvent) error {
		media = append(media, event)
		return nil
	}))
	played := 0
	result, err := service.Deliver(context.Background(), responseRequest(), func(string) { played++ })
	if err != nil {
		t.Fatalf("deliver response: %v", err)
	}
	if !result.TextDelivered || !result.AudioDelivered || played != 1 {
		t.Fatalf("unexpected result: %+v, played=%d", result, played)
	}
	if len(sink.replies) != 1 || len(sink.audio) != 1 || len(media) != 1 || media[0].DeliveryState != "audio_started" {
		t.Fatalf("unexpected deliveries: replies=%d audio=%d media=%+v", len(sink.replies), len(sink.audio), media)
	}
	record, ok := tracker.Lookup("trace-1")
	if !ok || record.State != DeliveryAudioStarted {
		t.Fatalf("unexpected delivery record: %+v", record)
	}

	duplicate, err := service.Deliver(context.Background(), responseRequest(), func(string) { played++ })
	if err != nil {
		t.Fatalf("deliver duplicate: %v", err)
	}
	if !duplicate.Duplicate || len(sink.replies) != 1 || len(sink.audio) != 1 || synthesizer.calls != 1 || played != 1 {
		t.Fatalf("duplicate response was delivered: %+v replies=%d audio=%d synth=%d played=%d", duplicate, len(sink.replies), len(sink.audio), synthesizer.calls, played)
	}
}

func TestResponseDeliveryServiceFallsBackToTextWhenTTSFails(t *testing.T) {
	sink := &responseSinkStub{}
	synthesizer := &responseSynthesizerStub{err: errors.New("provider unavailable")}
	tracker := newResponseTrackerStub()
	var media []MediaDeliveryEvent
	service := NewResponseDeliveryService(sink, synthesizer, tracker, MediaDeliveryRecorderFunc(func(_ context.Context, event MediaDeliveryEvent) error {
		media = append(media, event)
		return nil
	}))
	result, err := service.Deliver(context.Background(), responseRequest(), nil)
	if err != nil {
		t.Fatalf("deliver fallback: %v", err)
	}
	if result.FallbackReason != "provider unavailable" || len(sink.replies) != 1 || len(sink.audio) != 0 {
		t.Fatalf("unexpected fallback result: %+v", result)
	}
	if len(sink.statuses) != 1 || sink.statuses[0].State != "tts_fallback" || len(media) != 1 || media[0].DeliveryState != "failed" || media[0].Reason != "provider unavailable" {
		t.Fatalf("fallback evidence missing: statuses=%+v media=%+v", sink.statuses, media)
	}
	record, _ := tracker.Lookup("trace-1")
	if record.State != DeliveryCompleted {
		t.Fatalf("text fallback should complete delivery, got %+v", record)
	}
}

func TestResponseDeliveryServiceMarksFailedAudioTransport(t *testing.T) {
	sink := &responseSinkStub{audioErr: errors.New("socket closed")}
	synthesizer := &responseSynthesizerStub{audio: SynthesizedAudio{Data: []byte{1}, MIME: "audio/mpeg"}}
	tracker := newResponseTrackerStub()
	service := NewResponseDeliveryService(sink, synthesizer, tracker, nil)
	if _, err := service.Deliver(context.Background(), responseRequest(), nil); err == nil {
		t.Fatal("expected audio transport failure")
	}
	record, _ := tracker.Lookup("trace-1")
	if record.State != DeliveryFailed {
		t.Fatalf("audio transport failure should be terminal, got %+v", record)
	}
}

func TestResponseDeliveryServicePreventsConcurrentAudibleDuplicates(t *testing.T) {
	sink := &responseSinkStub{}
	synthesizer := &responseSynthesizerStub{audio: SynthesizedAudio{Data: []byte{1}, MIME: "audio/mpeg"}}
	tracker := newResponseTrackerStub()
	service := NewResponseDeliveryService(sink, synthesizer, tracker, nil)
	var wait sync.WaitGroup
	wait.Add(2)
	for range 2 {
		go func() {
			defer wait.Done()
			_, _ = service.Deliver(context.Background(), responseRequest(), nil)
		}()
	}
	wait.Wait()
	if len(sink.replies) != 1 || len(sink.audio) != 1 {
		t.Fatalf("concurrent duplicate escaped: replies=%d audio=%d", len(sink.replies), len(sink.audio))
	}
}

func TestResponseDeliveryServiceRetriesStatusWithoutRepeatingText(t *testing.T) {
	sink := &responseSinkStub{statusErr: errors.New("socket closed")}
	tracker := newResponseTrackerStub()
	service := NewResponseDeliveryService(sink, nil, tracker, nil)
	request := responseRequest()
	request.AfterTextStatus = &DeliveryStatus{Kind: "first_meeting", State: "delivered"}

	if _, err := service.Deliver(context.Background(), request, nil); err == nil {
		t.Fatal("expected status transport failure")
	}
	record, _ := tracker.Lookup(request.Trace.ID)
	if record.State != DeliveryTextDelivered || len(sink.replies) != 1 {
		t.Fatalf("text delivery was not preserved: record=%+v replies=%d", record, len(sink.replies))
	}

	sink.statusErr = nil
	result, err := service.Deliver(context.Background(), request, nil)
	if err != nil {
		t.Fatalf("retry status: %v", err)
	}
	if result.TextDelivered || len(sink.replies) != 1 || len(sink.statuses) != 3 {
		t.Fatalf("retry repeated text or omitted status: result=%+v replies=%d statuses=%d", result, len(sink.replies), len(sink.statuses))
	}
}

func TestResponseDeliveryServiceDeduplicatesByDeliveryKeyAcrossTraces(t *testing.T) {
	sink := &responseSinkStub{}
	synthesizer := &responseSynthesizerStub{audio: SynthesizedAudio{Data: []byte{1}, MIME: "audio/mpeg"}}
	tracker := newResponseTrackerStub()
	service := NewResponseDeliveryService(sink, synthesizer, tracker, nil)
	if _, err := service.Deliver(context.Background(), responseRequest(), nil); err != nil {
		t.Fatalf("first delivery: %v", err)
	}
	duplicateRequest := responseRequest()
	duplicateRequest.Trace.ID = "trace-2"
	result, err := service.Deliver(context.Background(), duplicateRequest, nil)
	if err != nil {
		t.Fatalf("duplicate delivery: %v", err)
	}
	if !result.Duplicate || len(sink.replies) != 1 || len(sink.audio) != 1 {
		t.Fatalf("delivery key duplicate escaped: result=%+v replies=%d audio=%d", result, len(sink.replies), len(sink.audio))
	}
}

func TestResponseDeliveryServiceDeduplicatesDeliveryKeyAcrossServices(t *testing.T) {
	sink := &responseSinkStub{}
	ledger := NewMemoryDeliveryLedger()
	firstTracker := &responseTrackerStub{core: NewDeliveryTracker(ledger)}
	secondTracker := &responseTrackerStub{core: NewDeliveryTracker(ledger)}
	services := []*ResponseDeliveryService{
		NewResponseDeliveryService(sink, nil, firstTracker, nil),
		NewResponseDeliveryService(sink, nil, secondTracker, nil),
	}
	requests := []ResponseDeliveryRequest{responseRequest(), responseRequest()}
	requests[1].Trace.ID = "trace-2"

	var wait sync.WaitGroup
	results := make([]ResponseDeliveryResult, len(services))
	errorsByService := make([]error, len(services))
	for index := range services {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			results[index], errorsByService[index] = services[index].Deliver(context.Background(), requests[index], nil)
		}(index)
	}
	wait.Wait()
	for _, err := range errorsByService {
		if err != nil {
			t.Fatalf("concurrent delivery: %v", err)
		}
	}
	duplicates := 0
	for _, result := range results {
		if result.Duplicate {
			duplicates++
		}
	}
	if duplicates != 1 || len(sink.replies) != 1 {
		t.Fatalf("cross-service duplicate escaped: results=%+v replies=%d", results, len(sink.replies))
	}
}

type firstMeetingPlannerStub struct{ response companion.ProactiveResponse }

func (stub firstMeetingPlannerStub) HandleFirstMeeting(context.Context, companion.FirstMeetingRequest) (companion.ProactiveResponse, error) {
	return stub.response, nil
}

func TestFirstMeetingCoordinatorUsesSharedDeliveryService(t *testing.T) {
	sink := &responseSinkStub{}
	tracker := newResponseTrackerStub()
	service := NewResponseDeliveryService(sink, nil, tracker, nil)
	coordinator := NewFirstMeetingCoordinator(firstMeetingPlannerStub{response: companion.ProactiveResponse{
		Reply: "第一次一起看球。",
		Trace: companion.Trace{ID: "first-1", UserID: "user-1", MatchID: "match-1"},
	}}, service)
	result, err := coordinator.Handle(context.Background(), companion.FirstMeetingRequest{UserID: "user-1", MatchID: "match-1"}, nil)
	if err != nil {
		t.Fatalf("first meeting: %v", err)
	}
	if !result.TextDelivered || result.FallbackReason != "tts unavailable" || len(sink.replies) != 1 {
		t.Fatalf("unexpected first meeting result: %+v replies=%+v", result, sink.replies)
	}
	if len(sink.statuses) != 2 || sink.statuses[0].Kind != "first_meeting" || sink.statuses[0].State != "delivered" || sink.statuses[1].State != "tts_fallback" {
		t.Fatalf("unexpected first meeting statuses: %+v", sink.statuses)
	}
}
