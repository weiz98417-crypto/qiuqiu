package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"qiuqiu/internal/asr"
)

func TestTranscriptionSessionsEmitProtocolAndCompleteOneVoiceTurn(t *testing.T) {
	messages := make(chan map[string]interface{}, 3)
	completions := make(chan transcriptionCompletion, 1)
	sessions := newTranscriptionSessions(
		context.Background(),
		asr.NewMockClient("现在比分多少"),
		asr.StreamOptions{PartialBytes: 4, MaxBytes: 32},
		func(message map[string]interface{}) { messages <- message },
		func(completion transcriptionCompletion) { completions <- completion },
	)
	defer sessions.Close()

	if err := sessions.Start("utterance-1", "signal-1", "user-1", "Asia/Shanghai", []string{"西班牙", "德国"}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := sessions.Append("utterance-1", 0, []byte{1, 0, 2, 0}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	partial := receiveProtocolMessage(t, messages)
	if partial["type"] != "transcript_partial" || partial["utteranceId"] != "utterance-1" || partial["text"] != "现在比分多少" {
		t.Fatalf("partial message = %#v", partial)
	}

	if err := sessions.Finish("utterance-1"); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	final := receiveProtocolMessage(t, messages)
	if final["type"] != "transcript_final" || final["text"] != "现在比分多少" {
		t.Fatalf("final message = %#v", final)
	}
	select {
	case completion := <-completions:
		if completion.UtteranceID != "utterance-1" || completion.SignalID != "signal-1" || completion.UserID != "user-1" || completion.Timezone != "Asia/Shanghai" || completion.Text != "现在比分多少" {
			t.Fatalf("completion = %+v", completion)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for voice turn completion")
	}

	if err := sessions.Finish("utterance-1"); err == nil {
		t.Fatal("finished transcription session remained active")
	}
}

func TestTranscriptionSessionsDiscardInvalidChunkSession(t *testing.T) {
	sessions := newTranscriptionSessions(
		context.Background(),
		asr.NewMockClient("不会调用"),
		asr.StreamOptions{PartialBytes: 64, MaxBytes: 64},
		nil,
		nil,
	)
	defer sessions.Close()
	if err := sessions.Start("utterance-invalid", "signal-1", "user-1", "", nil); err != nil {
		t.Fatalf("Start: %v", err)
	}

	err := sessions.Append("utterance-invalid", 1, []byte{1, 0})
	if !errors.Is(err, asr.ErrChunkSequence) {
		t.Fatalf("Append error = %v, want %v", err, asr.ErrChunkSequence)
	}
	if err := sessions.Finish("utterance-invalid"); !errors.Is(err, errTranscriptionSessionNotFound) {
		t.Fatalf("Finish after invalid chunk = %v, want session not found", err)
	}
}

func TestTranscriptionSessionsCompleteVoiceTurnsInUtteranceOrder(t *testing.T) {
	recognizer := newControlledRecognizer()
	messages := make(chan map[string]interface{}, 2)
	completions := make(chan transcriptionCompletion, 2)
	sessions := newTranscriptionSessions(
		context.Background(),
		recognizer,
		asr.StreamOptions{PartialBytes: 64, MaxBytes: 64},
		func(message map[string]interface{}) { messages <- message },
		func(completion transcriptionCompletion) { completions <- completion },
	)
	defer sessions.Close()

	startFinishedTranscription(t, sessions, "utterance-1", 1)
	recognizer.waitStarted(t, 1)
	startFinishedTranscription(t, sessions, "utterance-2", 2)
	recognizer.waitStarted(t, 2)

	recognizer.release(2)
	if message := receiveProtocolMessage(t, messages); message["utteranceId"] != "utterance-2" {
		t.Fatalf("first protocol final = %#v, want utterance-2", message)
	}
	select {
	case completion := <-completions:
		t.Fatalf("newer utterance completed before older one: %+v", completion)
	case <-time.After(50 * time.Millisecond):
	}

	recognizer.release(1)
	if message := receiveProtocolMessage(t, messages); message["utteranceId"] != "utterance-1" {
		t.Fatalf("second protocol final = %#v, want utterance-1", message)
	}
	first := receiveCompletion(t, completions)
	second := receiveCompletion(t, completions)
	if first.UtteranceID != "utterance-1" || second.UtteranceID != "utterance-2" {
		t.Fatalf("completion order = %s, %s", first.UtteranceID, second.UtteranceID)
	}
}

func TestValidateTranscriptionStartRequiresPCM16At16kMono(t *testing.T) {
	valid := map[string]interface{}{
		"encoding":   "pcm_s16le",
		"sampleRate": float64(16000),
		"channels":   float64(1),
		"language":   "zh",
	}
	if err := validateTranscriptionStart(valid); err != nil {
		t.Fatalf("valid start: %v", err)
	}
	for field, value := range map[string]interface{}{
		"encoding":   "wav",
		"sampleRate": float64(48000),
		"channels":   float64(2),
		"language":   "en",
	} {
		invalid := make(map[string]interface{}, len(valid))
		for key, current := range valid {
			invalid[key] = current
		}
		invalid[field] = value
		if err := validateTranscriptionStart(invalid); err == nil {
			t.Fatalf("invalid %s was accepted", field)
		}
	}
}

func TestTranscriptionSessionsLimitConcurrentUtterances(t *testing.T) {
	sessions := newTranscriptionSessions(
		context.Background(),
		asr.NewMockClient("不会调用"),
		asr.StreamOptions{PartialBytes: 64, MaxBytes: 64},
		nil,
		nil,
	)
	defer sessions.Close()
	for index := 0; index < maxTranscriptionSessionsPerConnection; index++ {
		utteranceID := fmt.Sprintf("utterance-limit-%d", index)
		if err := sessions.Start(utteranceID, "signal", "user-1", "", nil); err != nil {
			t.Fatalf("Start %d: %v", index, err)
		}
	}
	if err := sessions.Start("utterance-over-limit", "signal", "user-1", "", nil); !errors.Is(err, errTranscriptionSessionLimit) {
		t.Fatalf("over-limit Start error = %v, want %v", err, errTranscriptionSessionLimit)
	}
}

func startFinishedTranscription(t *testing.T, sessions *transcriptionSessions, utteranceID string, marker byte) {
	t.Helper()
	if err := sessions.Start(utteranceID, "signal-"+utteranceID, "user-1", "", nil); err != nil {
		t.Fatalf("Start %s: %v", utteranceID, err)
	}
	if err := sessions.Append(utteranceID, 0, []byte{marker, 0}); err != nil {
		t.Fatalf("Append %s: %v", utteranceID, err)
	}
	if err := sessions.Finish(utteranceID); err != nil {
		t.Fatalf("Finish %s: %v", utteranceID, err)
	}
}

func receiveCompletion(t *testing.T, completions <-chan transcriptionCompletion) transcriptionCompletion {
	t.Helper()
	select {
	case completion := <-completions:
		return completion
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for transcription completion")
		return transcriptionCompletion{}
	}
}

type controlledRecognizer struct {
	started chan byte
	mu      sync.Mutex
	waits   map[byte]chan struct{}
}

func newControlledRecognizer() *controlledRecognizer {
	return &controlledRecognizer{
		started: make(chan byte, 2),
		waits: map[byte]chan struct{}{
			1: make(chan struct{}),
			2: make(chan struct{}),
		},
	}
}

func (r *controlledRecognizer) Transcribe(_ context.Context, audio []byte, _ []string) (*asr.Result, error) {
	marker := audio[len(audio)-2]
	r.started <- marker
	r.mu.Lock()
	wait := r.waits[marker]
	r.mu.Unlock()
	<-wait
	text := "第一句"
	if marker == 2 {
		text = "第二句"
	}
	return &asr.Result{Text: text, Provider: "test"}, nil
}

func (r *controlledRecognizer) waitStarted(t *testing.T, marker byte) {
	t.Helper()
	select {
	case started := <-r.started:
		if started != marker {
			t.Fatalf("recognizer marker = %d, want %d", started, marker)
		}
	case <-time.After(time.Second):
		t.Fatalf("recognizer %d did not start", marker)
	}
}

func (r *controlledRecognizer) release(marker byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	close(r.waits[marker])
}

func receiveProtocolMessage(t *testing.T, messages <-chan map[string]interface{}) map[string]interface{} {
	t.Helper()
	select {
	case message := <-messages:
		return message
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for transcription protocol message")
		return nil
	}
}
