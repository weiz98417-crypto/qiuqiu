package asr

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestStreamSessionEmitsPartialThenAuthoritativeFinalTranscript(t *testing.T) {
	recognizer := &sequenceRecognizer{results: []string{"现在比分", "现在比分多少"}}
	events := make(chan TranscriptEvent, 2)
	session := NewStreamSession(
		context.Background(),
		recognizer,
		StreamOptions{
			UtteranceID:  "utterance-1",
			PartialBytes: 4,
			MaxBytes:     32,
		},
		func(event TranscriptEvent) { events <- event },
	)

	if err := session.Append(0, []byte{1, 0}); err != nil {
		t.Fatalf("Append first chunk: %v", err)
	}
	if err := session.Append(1, []byte{2, 0}); err != nil {
		t.Fatalf("Append second chunk: %v", err)
	}
	partial := receiveTranscriptEvent(t, events)
	if partial.Kind != TranscriptPartial || partial.Text != "现在比分" || partial.Revision != 1 {
		t.Fatalf("partial event = %+v", partial)
	}

	if err := session.Append(2, []byte{3, 0}); err != nil {
		t.Fatalf("Append final chunk: %v", err)
	}
	session.Finish()
	final := receiveTranscriptEvent(t, events)
	if final.Kind != TranscriptFinal || final.Text != "现在比分多少" || final.Revision != 2 {
		t.Fatalf("final event = %+v", final)
	}
	if final.UtteranceID != "utterance-1" || final.Provider != "test" {
		t.Fatalf("final identity/provider = %+v", final)
	}

	requests := recognizer.Requests()
	if len(requests) != 2 {
		t.Fatalf("recognizer requests = %d, want 2", len(requests))
	}
	for _, request := range requests {
		if !strings.HasPrefix(string(request), "RIFF") {
			t.Fatalf("request is not WAV: %q", request)
		}
	}
}

func TestStreamSessionRejectsOutOfOrderAndOverLimitAudio(t *testing.T) {
	session := NewStreamSession(
		context.Background(),
		NewMockClient("ignored"),
		StreamOptions{PartialBytes: defaultMaxBytes + 1},
		nil,
	)

	if err := session.Append(1, []byte{1, 0}); !errors.Is(err, ErrChunkSequence) {
		t.Fatalf("out-of-order Append error = %v, want %v", err, ErrChunkSequence)
	}
	if err := session.Append(0, make([]byte, defaultMaxBytes+1)); !errors.Is(err, ErrAudioTooLarge) {
		t.Fatalf("oversized Append error = %v, want %v", err, ErrAudioTooLarge)
	}
}

func TestStreamSessionFinishIsIdempotent(t *testing.T) {
	recognizer := &sequenceRecognizer{results: []string{"最终结果"}}
	events := make(chan TranscriptEvent, 2)
	session := NewStreamSession(
		context.Background(),
		recognizer,
		StreamOptions{PartialBytes: 64, MaxBytes: 64},
		func(event TranscriptEvent) { events <- event },
	)
	if err := session.Append(0, []byte{1, 0}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	session.Finish()
	session.Finish()
	if event := receiveTranscriptEvent(t, events); event.Kind != TranscriptFinal {
		t.Fatalf("event = %+v, want final", event)
	}
	select {
	case duplicate := <-events:
		t.Fatalf("duplicate finish event = %+v", duplicate)
	case <-time.After(50 * time.Millisecond):
	}
	if requests := recognizer.Requests(); len(requests) != 1 {
		t.Fatalf("recognizer requests = %d, want 1", len(requests))
	}
}

func TestStreamSessionCancelSuppressesLateFinalTranscript(t *testing.T) {
	recognizer := &blockingRecognizer{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	events := make(chan TranscriptEvent, 1)
	session := NewStreamSession(
		context.Background(),
		recognizer,
		StreamOptions{PartialBytes: 64, MaxBytes: 64},
		func(event TranscriptEvent) { events <- event },
	)
	if err := session.Append(0, []byte{1, 0}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	session.Finish()
	select {
	case <-recognizer.started:
	case <-time.After(time.Second):
		t.Fatal("recognizer did not start")
	}

	session.Cancel()
	close(recognizer.release)
	select {
	case event := <-events:
		t.Fatalf("event after cancel = %+v", event)
	case <-time.After(50 * time.Millisecond):
	}
}

func receiveTranscriptEvent(t *testing.T, events <-chan TranscriptEvent) TranscriptEvent {
	t.Helper()
	select {
	case event := <-events:
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for transcript event")
		return TranscriptEvent{}
	}
}

type sequenceRecognizer struct {
	mu       sync.Mutex
	results  []string
	requests [][]byte
}

type blockingRecognizer struct {
	started chan struct{}
	release chan struct{}
}

func (r *blockingRecognizer) Transcribe(context.Context, []byte, []string) (*Result, error) {
	close(r.started)
	<-r.release
	return &Result{Text: "不应发送", Provider: "test"}, nil
}

func (r *sequenceRecognizer) Transcribe(_ context.Context, audio []byte, _ []string) (*Result, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests = append(r.requests, append([]byte(nil), audio...))
	text := r.results[0]
	r.results = r.results[1:]
	return &Result{Text: text, Provider: "test"}, nil
}

func (r *sequenceRecognizer) Requests() [][]byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([][]byte(nil), r.requests...)
}
