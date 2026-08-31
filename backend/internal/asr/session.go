package asr

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

const (
	defaultPartialBytes = pcmSampleRate * pcmBitsPerSample / 8 * 3 / 2
	defaultOverlapBytes = pcmSampleRate * pcmBitsPerSample / 8 / 4
	defaultMaxBytes     = pcmSampleRate * pcmBitsPerSample / 8 * 30
)

var (
	ErrChunkSequence = errors.New("audio chunk sequence is out of order")
	ErrAudioTooLarge = errors.New("streaming audio exceeds the session limit")
)

type Recognizer interface {
	Transcribe(ctx context.Context, audio []byte, hints []string) (*Result, error)
}

type TranscriptKind string

const (
	TranscriptPartial TranscriptKind = "partial"
	TranscriptFinal   TranscriptKind = "final"
	TranscriptError   TranscriptKind = "error"
)

type TranscriptEvent struct {
	UtteranceID string
	Kind        TranscriptKind
	Revision    int
	Text        string
	Provider    string
	Error       string
	Recoverable bool
}

type StreamOptions struct {
	UtteranceID  string
	Hints        []string
	PartialBytes int
	OverlapBytes int
	MaxBytes     int
}

type StreamSession struct {
	mu              sync.Mutex
	emitMu          sync.Mutex
	ctx             context.Context
	cancel          context.CancelFunc
	recognizer      Recognizer
	emit            func(TranscriptEvent)
	options         StreamOptions
	pcm             []byte
	nextSequence    int
	partialCursor   int
	partialInFlight bool
	partialCancel   context.CancelFunc
	partialText     string
	revision        int
	finished        bool
	canceled        bool
}

func NewStreamSession(ctx context.Context, recognizer Recognizer, options StreamOptions, emit func(TranscriptEvent)) *StreamSession {
	if ctx == nil {
		ctx = context.Background()
	}
	if options.PartialBytes <= 0 {
		options.PartialBytes = defaultPartialBytes
	}
	if options.OverlapBytes < 0 {
		options.OverlapBytes = 0
	} else if options.OverlapBytes == 0 {
		options.OverlapBytes = defaultOverlapBytes
	}
	if options.MaxBytes <= 0 {
		options.MaxBytes = defaultMaxBytes
	}
	sessionCtx, cancel := context.WithCancel(ctx)
	return &StreamSession{
		ctx:        sessionCtx,
		cancel:     cancel,
		recognizer: recognizer,
		emit:       emit,
		options:    options,
		pcm:        make([]byte, 0, options.PartialBytes*2),
	}
}

func (s *StreamSession) Append(sequence int, pcm []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.finished {
		return errors.New("streaming session is already finished")
	}
	if sequence != s.nextSequence {
		return fmt.Errorf("%w: got %d want %d", ErrChunkSequence, sequence, s.nextSequence)
	}
	if len(s.pcm)+len(pcm) > s.options.MaxBytes {
		return ErrAudioTooLarge
	}
	s.nextSequence++
	s.pcm = append(s.pcm, pcm...)
	s.startPartialLocked()
	return nil
}

func (s *StreamSession) Finish() {
	s.mu.Lock()
	if s.finished {
		s.mu.Unlock()
		return
	}
	s.finished = true
	if s.partialCancel != nil {
		s.partialCancel()
	}
	pcm := append([]byte(nil), s.pcm...)
	s.mu.Unlock()

	if len(pcm) == 0 {
		s.cancel()
		s.emitEvent(TranscriptEvent{Kind: TranscriptError, Error: "empty voice input"})
		return
	}
	go s.transcribeFinal(pcm)
}

func (s *StreamSession) Cancel() {
	s.mu.Lock()
	if s.canceled {
		s.mu.Unlock()
		return
	}
	s.emitMu.Lock()
	s.finished = true
	s.canceled = true
	s.mu.Unlock()
	s.cancel()
	s.emitMu.Unlock()
}

func (s *StreamSession) startPartialLocked() {
	if s.finished || s.partialInFlight || s.recognizer == nil {
		return
	}
	if len(s.pcm)-s.partialCursor < s.options.PartialBytes {
		return
	}
	end := s.partialCursor + s.options.PartialBytes
	start := s.partialCursor - s.options.OverlapBytes
	if start < 0 {
		start = 0
	}
	window := append([]byte(nil), s.pcm[start:end]...)
	s.partialCursor = end
	s.partialInFlight = true
	partialCtx, cancel := context.WithCancel(s.ctx)
	s.partialCancel = cancel
	go s.transcribePartial(partialCtx, cancel, window)
}

func (s *StreamSession) transcribePartial(ctx context.Context, cancel context.CancelFunc, pcm []byte) {
	result, err := s.recognizer.Transcribe(ctx, PCM16ToWAV(pcm), s.options.Hints)
	wasCanceled := ctx.Err() != nil
	cancel()

	s.mu.Lock()
	s.partialInFlight = false
	s.partialCancel = nil
	if s.finished || s.canceled || wasCanceled {
		s.mu.Unlock()
		return
	}
	var event TranscriptEvent
	if err != nil {
		event = TranscriptEvent{Kind: TranscriptError, Error: err.Error(), Recoverable: true}
	} else if result != nil && strings.TrimSpace(result.Text) != "" {
		s.partialText = mergeTranscript(s.partialText, result.Text)
		s.revision++
		event = TranscriptEvent{Kind: TranscriptPartial, Revision: s.revision, Text: s.partialText, Provider: result.Provider}
	}
	s.startPartialLocked()
	if event.Kind != "" {
		s.emitMu.Lock()
	}
	s.mu.Unlock()

	if event.Kind != "" {
		s.emitEventLocked(event)
		s.emitMu.Unlock()
	}
}

func (s *StreamSession) transcribeFinal(pcm []byte) {
	defer s.cancel()
	s.mu.Lock()
	if s.canceled {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	if s.recognizer == nil {
		s.emitEvent(TranscriptEvent{Kind: TranscriptError, Error: ErrNotConfigured.Error()})
		return
	}
	result, err := s.recognizer.Transcribe(s.ctx, PCM16ToWAV(pcm), s.options.Hints)
	s.mu.Lock()
	if s.canceled {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	if err != nil {
		s.emitEvent(TranscriptEvent{Kind: TranscriptError, Error: err.Error()})
		return
	}
	if result == nil || strings.TrimSpace(result.Text) == "" {
		s.emitEvent(TranscriptEvent{Kind: TranscriptError, Error: "empty voice input"})
		return
	}
	s.mu.Lock()
	if s.canceled {
		s.mu.Unlock()
		return
	}
	s.revision++
	revision := s.revision
	s.emitMu.Lock()
	s.mu.Unlock()
	s.emitEventLocked(TranscriptEvent{
		Kind:     TranscriptFinal,
		Revision: revision,
		Text:     strings.TrimSpace(result.Text),
		Provider: result.Provider,
	})
	s.emitMu.Unlock()
}

func (s *StreamSession) emitEvent(event TranscriptEvent) {
	s.mu.Lock()
	if s.canceled {
		s.mu.Unlock()
		return
	}
	s.emitMu.Lock()
	s.mu.Unlock()
	s.emitEventLocked(event)
	s.emitMu.Unlock()
}

func (s *StreamSession) emitEventLocked(event TranscriptEvent) {
	if s.emit == nil {
		return
	}
	event.UtteranceID = s.options.UtteranceID
	s.emit(event)
}

func mergeTranscript(existing, incoming string) string {
	existing = strings.TrimSpace(existing)
	incoming = strings.TrimSpace(incoming)
	if existing == "" {
		return incoming
	}
	if incoming == "" || strings.HasSuffix(existing, incoming) {
		return existing
	}
	if strings.HasPrefix(incoming, existing) {
		return incoming
	}
	existingRunes := []rune(existing)
	incomingRunes := []rune(incoming)
	limit := len(existingRunes)
	if len(incomingRunes) < limit {
		limit = len(incomingRunes)
	}
	for overlap := limit; overlap > 0; overlap-- {
		if string(existingRunes[len(existingRunes)-overlap:]) == string(incomingRunes[:overlap]) {
			return existing + string(incomingRunes[overlap:])
		}
	}
	return existing + incoming
}
