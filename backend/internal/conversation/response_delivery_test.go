package conversation

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
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
	// calls 由并发 Deliver 路径读写(-race 猎获过裸 int),必须原子。
	calls atomic.Int64
}

func (stub *responseSynthesizerStub) SynthesizeResponse(context.Context, string, relationship.PresentationPlan, []relationship.CommunicationAct) (SynthesizedAudio, error) {
	stub.calls.Add(1)
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
	if !duplicate.Duplicate || len(sink.replies) != 1 || len(sink.audio) != 1 || synthesizer.calls.Load() != 1 || played != 1 {
		t.Fatalf("duplicate response was delivered: %+v replies=%d audio=%d synth=%d played=%d", duplicate, len(sink.replies), len(sink.audio), synthesizer.calls.Load(), played)
	}
}

func TestResponseDeliveryServiceKeepsCriticalTextFallbackRecoverableUntilAcknowledged(t *testing.T) {
	sink := &responseSinkStub{}
	synthesizer := &responseSynthesizerStub{err: errors.New("provider unavailable")}
	tracker := newResponseTrackerStub()
	var media []MediaDeliveryEvent
	service := NewResponseDeliveryService(sink, synthesizer, tracker, MediaDeliveryRecorderFunc(func(_ context.Context, event MediaDeliveryEvent) error {
		media = append(media, event)
		return nil
	}))
	now := time.Date(2026, 9, 8, 10, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
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
	if record.State != DeliveryTextDelivered {
		t.Fatalf("critical text fallback should remain recoverable, got %+v", record)
	}
	if pending := tracker.core.Ledger().Pending(now.Add(time.Second)); len(pending) != 1 || pending[0].Key != record.Key {
		t.Fatalf("critical text fallback missing from recovery ledger: %+v", pending)
	}
}

func TestResponseDeliveryServiceCompletesNonCriticalTextFallback(t *testing.T) {
	sink := &responseSinkStub{}
	tracker := newResponseTrackerStub()
	service := NewResponseDeliveryService(sink, nil, tracker, nil)
	request := responseRequest()
	request.Critical = false

	if _, err := service.Deliver(context.Background(), request, nil); err != nil {
		t.Fatalf("deliver fallback: %v", err)
	}
	record, _ := tracker.Lookup(request.Trace.ID)
	if record.State != DeliveryCompleted {
		t.Fatalf("non-critical text fallback should complete delivery, got %+v", record)
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

// deep-water-polish 1.2：Deliver 拆「加锁规划半 + 无锁 I/O 半」后，规划半
// 纯函数化的可测面——只做状态判定与队列操作，全程零 sink I/O。
func TestResponseDeliveryPlanningHalfDecidesAndClaims(t *testing.T) {
	sink := &responseSinkStub{}
	tracker := newResponseTrackerStub()
	service := NewResponseDeliveryService(sink, nil, tracker, nil)

	// 新投递：判定发送文本，并在锁内完成 TextDelivered 占位迁移；不产生 I/O。
	plan, err := service.planResponseRound(responseRequest())
	if err != nil {
		t.Fatalf("plan response round: %v", err)
	}
	if plan.duplicate || !plan.sendText {
		t.Fatalf("plan = %+v", plan)
	}
	if plan.reply.TraceID != "trace-1" || plan.reply.DeliveryKey != "goal:1" || plan.reply.Text != "这球漂亮。" {
		t.Fatalf("plan reply = %+v", plan.reply)
	}
	if len(sink.replies) != 0 {
		t.Fatalf("planning half performed I/O: %+v", sink.replies)
	}
	if record, _ := tracker.Lookup("trace-1"); record.State != DeliveryTextDelivered {
		t.Fatalf("claim transition missing, record = %+v", record)
	}

	// 同 trace 重放：既非重复也不重发文本。
	replay, err := service.planResponseRound(responseRequest())
	if err != nil {
		t.Fatalf("replay plan: %v", err)
	}
	if replay.duplicate || replay.sendText {
		t.Fatalf("replay plan = %+v", replay)
	}

	// 终态之后：重复。
	if err := tracker.Transition("trace-1", DeliveryCompleted, time.Now()); err != nil {
		t.Fatalf("complete: %v", err)
	}
	terminal, err := service.planResponseRound(responseRequest())
	if err != nil {
		t.Fatalf("terminal plan: %v", err)
	}
	if !terminal.duplicate {
		t.Fatalf("terminal plan = %+v", terminal)
	}

	// 同 deliveryKey 不同 trace：重复。
	keyDuplicate := responseRequest()
	keyDuplicate.Trace.ID = "trace-2"
	duplicate, err := service.planResponseRound(keyDuplicate)
	if err != nil {
		t.Fatalf("key duplicate plan: %v", err)
	}
	if !duplicate.duplicate {
		t.Fatalf("key duplicate plan = %+v", duplicate)
	}
	if len(sink.replies) != 0 || len(sink.statuses) != 0 {
		t.Fatalf("planning half performed I/O: replies=%d statuses=%d", len(sink.replies), len(sink.statuses))
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

// ── 句粒度投递（voice-streaming-delivery 3.4/3.5）─────────────────────────

type streamingSynthesizerStub struct {
	sentences []StreamedSentence
	err       error
	cancelAt  int // 第 N 帧回调时取消 ctx（打断语义）；-1 = 不取消
}

func (stub *streamingSynthesizerStub) SynthesizeResponse(context.Context, string, relationship.PresentationPlan, []relationship.CommunicationAct) (SynthesizedAudio, error) {
	return SynthesizedAudio{}, errors.New("streaming stub must not take the whole-shot path")
}

func (stub *streamingSynthesizerStub) SynthesizeResponseStream(ctx context.Context, _ string, _ relationship.PresentationPlan, _ []relationship.CommunicationAct, onChunk func(StreamedSentence) error) error {
	for index, sentence := range stub.sentences {
		if stub.cancelAt == index {
			// 等取消落地（模拟第 N 句合成期间被打断）；等不到 = 测试没取消。
			deadline := time.Now().Add(time.Second)
			for ctx.Err() == nil && time.Now().Before(deadline) {
				time.Sleep(2 * time.Millisecond)
			}
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		if err := onChunk(sentence); err != nil {
			return err
		}
	}
	return stub.err
}

func newStreamingService(sink *responseSinkStub, streamer *streamingSynthesizerStub) *ResponseDeliveryService {
	return NewResponseDeliveryService(sink, streamer, newResponseTrackerStub(), nil)
}

func TestResponseDeliveryStreamsSentenceBySentence(t *testing.T) {
	sink := &responseSinkStub{}
	streamer := &streamingSynthesizerStub{sentences: []StreamedSentence{
		{Data: []byte("s1"), MIME: "audio/wav", SentenceIndex: 0, SentenceCount: 3, Final: false},
		{Data: []byte("s2"), MIME: "audio/wav", SentenceIndex: 1, SentenceCount: 3, Final: false},
		{Data: []byte("s3"), MIME: "audio/wav", SentenceIndex: 2, SentenceCount: 3, Final: true},
	}}
	service := newStreamingService(sink, streamer)
	result, err := service.Deliver(context.Background(), responseRequest(), nil)
	if err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if len(sink.audio) != 3 {
		t.Fatalf("expected 3 sentence frames, got %d", len(sink.audio))
	}
	for index, delivery := range sink.audio {
		if delivery.SentenceIndex != index {
			t.Fatalf("frame %d out of order: %+v", index, delivery)
		}
	}
	if !result.AudioDelivered || result.FirstAudioMS < 0 || result.SentenceCount != 3 || result.TTSByteCount != 6 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if record, ok := trackerLookup(t, service, "trace-1"); !ok || record.State != DeliveryAudioStarted {
		t.Fatalf("expected audio_started, got %+v", record)
	}
}

func TestResponseDeliveryStreamInterruptDropsRemainingSentences(t *testing.T) {
	sink := &responseSinkStub{}
	streamer := &streamingSynthesizerStub{sentences: []StreamedSentence{
		{Data: []byte("s1"), MIME: "audio/wav", SentenceIndex: 0, SentenceCount: 3},
		{Data: []byte("s2"), MIME: "audio/wav", SentenceIndex: 1, SentenceCount: 3},
		{Data: []byte("s3"), MIME: "audio/wav", SentenceIndex: 2, SentenceCount: 3},
	}, cancelAt: 1}
	service := newStreamingService(sink, streamer)
	ctx, cancel := context.WithCancel(context.Background())
	deliveryDone := make(chan struct{})
	go func() {
		defer close(deliveryDone)
		_, _ = service.Deliver(ctx, responseRequest(), nil)
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	<-deliveryDone
	if len(sink.audio) == 0 || len(sink.audio) == 3 {
		t.Fatalf("expected partial delivery on interrupt, got %d frames", len(sink.audio))
	}
	if record, ok := trackerLookup(t, service, "trace-1"); !ok || record.State != DeliveryInterrupted {
		t.Fatalf("expected interrupted, got %+v", record)
	}
}

func TestResponseDeliveryStreamMidwayFailureFallsBackWithoutResending(t *testing.T) {
	sink := &responseSinkStub{}
	streamer := &streamingSynthesizerStub{sentences: []StreamedSentence{
		{Data: []byte("s1"), MIME: "audio/wav", SentenceIndex: 0, SentenceCount: 2},
	}, err: errors.New("sentence 2 synth exploded")}
	service := newStreamingService(sink, streamer)
	result, err := service.Deliver(context.Background(), responseRequest(), nil)
	if err != nil {
		t.Fatalf("fallback must swallow synth error: %v", err)
	}
	if result.FallbackReason == "" {
		t.Fatalf("expected fallback reason, got %+v", result)
	}
	if len(sink.audio) != 1 {
		t.Fatalf("delivered sentence must not be resent: %d frames", len(sink.audio))
	}
	// 已有句下发：不再发失真的 tts_fallback，改发 truncated 截断信号；
	// FallbackReason 保留观测。
	if len(sink.statuses) != 1 || sink.statuses[0].State != "truncated" {
		t.Fatalf("partial delivery must send exactly one truncated status, got %+v", sink.statuses)
	}
	if result.FallbackReason == "" {
		t.Fatalf("partial delivery must keep FallbackReason for observability, got %+v", result)
	}
}

func trackerLookup(t *testing.T, service *ResponseDeliveryService, traceID string) (DeliveryRecord, bool) {
	t.Helper()
	return service.tracker.Lookup(traceID)
}
