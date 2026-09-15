package main

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"qiuqiu/internal/asr"
	"qiuqiu/internal/matchstate"
)

var (
	errTranscriptionSessionExists   = errors.New("transcription session already exists")
	errTranscriptionSessionNotFound = errors.New("transcription session not found")
	errTranscriptionSessionsClosed  = errors.New("transcription sessions are closed")
	errTranscriptionSessionLimit    = errors.New("too many active transcription sessions")
)

const maxTranscriptionSessionsPerConnection = 8

type transcriptionCompletion struct {
	UtteranceID string
	SignalID    string
	UserID      string
	Timezone    string
	Text        string
	Provider    string
}

type activeTranscription struct {
	signalID string
	userID   string
	timezone string
	session  *asr.StreamSession
	order    uint64
	terminal bool
}

type orderedTranscription struct {
	completion *transcriptionCompletion
}

type transcriptionSessions struct {
	ctx            context.Context
	recognizer     asr.Recognizer
	options        asr.StreamOptions
	emit           func(map[string]interface{})
	complete       func(transcriptionCompletion)
	mu             sync.Mutex
	sessions       map[string]*activeTranscription
	terminal       map[uint64]orderedTranscription
	nextOrder      uint64
	nextCompletion uint64
	closed         bool
}

func newTranscriptionSessions(
	ctx context.Context,
	recognizer asr.Recognizer,
	options asr.StreamOptions,
	emit func(map[string]interface{}),
	complete func(transcriptionCompletion),
) *transcriptionSessions {
	return &transcriptionSessions{
		ctx:        ctx,
		recognizer: recognizer,
		options:    options,
		emit:       emit,
		complete:   complete,
		sessions:   make(map[string]*activeTranscription),
		terminal:   make(map[uint64]orderedTranscription),
	}
}

func (s *transcriptionSessions) Start(utteranceID, signalID, userID, timezone string, hints []string) error {
	if s == nil || s.recognizer == nil {
		return asr.ErrNotConfigured
	}
	if utteranceID == "" || userID == "" {
		return errors.New("utterance and user identity are required")
	}
	options := s.options
	options.UtteranceID = utteranceID
	options.Hints = append([]string(nil), hints...)
	active := &activeTranscription{signalID: signalID, userID: userID, timezone: timezone}
	active.session = asr.NewStreamSession(s.ctx, s.recognizer, options, func(event asr.TranscriptEvent) {
		s.handleEvent(active, event)
	})
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		active.session.Cancel()
		return errTranscriptionSessionsClosed
	}
	if len(s.sessions) >= maxTranscriptionSessionsPerConnection {
		s.mu.Unlock()
		active.session.Cancel()
		return errTranscriptionSessionLimit
	}
	if _, loaded := s.sessions[utteranceID]; loaded {
		s.mu.Unlock()
		active.session.Cancel()
		return errTranscriptionSessionExists
	}
	active.order = s.nextOrder
	s.nextOrder++
	s.sessions[utteranceID] = active
	s.mu.Unlock()
	return nil
}

func (s *transcriptionSessions) Append(utteranceID string, sequence int, pcm []byte) error {
	active, err := s.load(utteranceID)
	if err != nil {
		return err
	}
	if err := active.session.Append(sequence, pcm); err != nil {
		active.session.Cancel()
		completions, _ := s.terminate(utteranceID, active, nil)
		s.deliver(completions)
		return err
	}
	return nil
}

func (s *transcriptionSessions) Finish(utteranceID string) error {
	active, err := s.load(utteranceID)
	if err != nil {
		return err
	}
	active.session.Finish()
	return nil
}

func (s *transcriptionSessions) Cancel(utteranceID string) error {
	active, err := s.load(utteranceID)
	if err != nil {
		return err
	}
	active.session.Cancel()
	completions, _ := s.terminate(utteranceID, active, nil)
	s.deliver(completions)
	return nil
}

func (s *transcriptionSessions) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	activeSessions := make([]*activeTranscription, 0, len(s.sessions))
	for _, active := range s.sessions {
		activeSessions = append(activeSessions, active)
	}
	s.sessions = make(map[string]*activeTranscription)
	s.terminal = make(map[uint64]orderedTranscription)
	s.mu.Unlock()
	for _, active := range activeSessions {
		active.session.Cancel()
	}
}

func (s *transcriptionSessions) load(utteranceID string) (*activeTranscription, error) {
	if s == nil {
		return nil, errTranscriptionSessionNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	active, ok := s.sessions[utteranceID]
	if !ok {
		return nil, fmt.Errorf("%w: %s", errTranscriptionSessionNotFound, utteranceID)
	}
	return active, nil
}

func (s *transcriptionSessions) handleEvent(active *activeTranscription, event asr.TranscriptEvent) {
	message := map[string]interface{}{
		"utteranceId": event.UtteranceID,
		"revision":    event.Revision,
	}
	var completion *transcriptionCompletion
	terminal := false
	switch event.Kind {
	case asr.TranscriptPartial:
		message["type"] = "transcript_partial"
		message["text"] = event.Text
		message["provider"] = event.Provider
	case asr.TranscriptFinal:
		message["type"] = "transcript_final"
		message["text"] = event.Text
		message["provider"] = event.Provider
		terminal = true
		completion = &transcriptionCompletion{
			UtteranceID: event.UtteranceID,
			SignalID:    active.signalID,
			UserID:      active.userID,
			Timezone:    active.timezone,
			Text:        event.Text,
			Provider:    event.Provider,
		}
	case asr.TranscriptError:
		message["type"] = "transcript_error"
		message["reason"] = event.Error
		message["recoverable"] = event.Recoverable
		if !event.Recoverable {
			terminal = true
		}
	default:
		return
	}
	var completions []transcriptionCompletion
	if terminal {
		var claimed bool
		completions, claimed = s.terminate(event.UtteranceID, active, completion)
		if !claimed {
			return
		}
	} else if !s.isActive(event.UtteranceID, active) {
		return
	}
	if s.emit != nil {
		s.emit(message)
	}
	s.deliver(completions)
}

func (s *transcriptionSessions) isActive(utteranceID string, active *activeTranscription) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.closed && !active.terminal && s.sessions[utteranceID] == active
}

func (s *transcriptionSessions) terminate(utteranceID string, active *activeTranscription, completion *transcriptionCompletion) ([]transcriptionCompletion, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || active.terminal {
		return nil, false
	}
	active.terminal = true
	if s.sessions[utteranceID] == active {
		delete(s.sessions, utteranceID)
	}
	s.terminal[active.order] = orderedTranscription{completion: completion}
	completions := make([]transcriptionCompletion, 0, 1)
	for {
		ordered, ok := s.terminal[s.nextCompletion]
		if !ok {
			break
		}
		delete(s.terminal, s.nextCompletion)
		s.nextCompletion++
		if ordered.completion != nil {
			completions = append(completions, *ordered.completion)
		}
	}
	return completions, true
}

func (s *transcriptionSessions) deliver(completions []transcriptionCompletion) {
	if s.complete == nil {
		return
	}
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return
	}
	for _, completion := range completions {
		s.complete(completion)
	}
}

func transcriptErrorMessage(utteranceID string, err error, recoverable bool) map[string]interface{} {
	reason := "transcription failed"
	if err != nil {
		reason = err.Error()
	}
	return map[string]interface{}{
		"type":        "transcript_error",
		"utteranceId": utteranceID,
		"reason":      reason,
		"recoverable": recoverable,
	}
}

func voiceRecognitionHints(config matchstate.MatchConfig) []string {
	hints := make([]string, 0, 2+len(config.HomePlayers)+len(config.AwayPlayers))
	seen := make(map[string]struct{})
	add := func(value string) {
		if value == "" {
			return
		}
		if _, duplicate := seen[value]; duplicate {
			return
		}
		seen[value] = struct{}{}
		hints = append(hints, value)
	}
	add(config.HomeTeam)
	add(config.AwayTeam)
	for _, player := range config.HomePlayers {
		add(player.Name)
	}
	for _, player := range config.AwayPlayers {
		add(player.Name)
	}
	return hints
}

func intField(message map[string]interface{}, key string) (int, bool) {
	value, ok := message[key].(float64)
	if !ok || value < 0 || value != float64(int(value)) {
		return 0, false
	}
	return int(value), true
}

func validateTranscriptionStart(message map[string]interface{}) error {
	encoding, _ := message["encoding"].(string)
	sampleRate, sampleRateOK := intField(message, "sampleRate")
	channels, channelsOK := intField(message, "channels")
	language, _ := message["language"].(string)
	if encoding != "pcm_s16le" || !sampleRateOK || sampleRate != 16000 || !channelsOK || channels != 1 || language != "zh" {
		return errors.New("streaming transcription requires pcm_s16le 16kHz mono Chinese audio")
	}
	return nil
}
