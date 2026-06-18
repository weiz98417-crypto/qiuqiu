package main

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"qiuqiu/internal/asr"
	"qiuqiu/internal/companion"
	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/tts"
)

func TestEvalVoiceSessionTextAndMockASRReachCompanion(t *testing.T) {
	agent := seededVoiceAgent(t, "voice-session-eval")

	textResult, err := handleVoiceSession(context.Background(), agent, nil, nil, "voice-session-eval", "user-1", "现在几比几？", "", fixedVoiceTime())
	if err != nil {
		t.Fatalf("text handleVoiceSession error: %v", err)
	}
	if !strings.Contains(textResult.Reply, "1-0") {
		t.Fatalf("expected text path to reach companion status answer, got %q", textResult.Reply)
	}

	audio := base64.StdEncoding.EncodeToString([]byte{1, 2, 3, 4})
	voiceResult, err := handleVoiceSession(context.Background(), agent, asr.NewMockClient("刚才谁助攻？"), tts.NewMockClient([]byte("mp3")), "voice-session-eval", "user-1", "", audio, fixedVoiceTime())
	if err != nil {
		t.Fatalf("voice handleVoiceSession error: %v", err)
	}
	if !strings.Contains(voiceResult.Reply, "Fabian") || !strings.Contains(voiceResult.Reply, "Yamal") {
		t.Fatalf("expected mock voice path to answer assist facts, got %q", voiceResult.Reply)
	}
	if string(voiceResult.AudioData) != "mp3" || voiceResult.AudioMIME != "audio/mpeg" {
		t.Fatalf("expected TTS audio result, got %+v", voiceResult)
	}
	if voiceResult.Trace.Voice == nil || voiceResult.Trace.Voice.ASRStatus != "ok" || voiceResult.Trace.Voice.TTSStatus != "ok" {
		t.Fatalf("expected voice trace metadata, got %+v", voiceResult.Trace.Voice)
	}
	if voiceResult.Trace.Voice.TTSByteCount != 3 || voiceResult.Trace.Voice.ASRText != "刚才谁助攻？" {
		t.Fatalf("unexpected voice trace values: %+v", voiceResult.Trace.Voice)
	}
}

func TestEvalVoiceSessionASRFallbackAndFailuresDoNotBreakText(t *testing.T) {
	agent := seededVoiceAgent(t, "voice-fallback-eval")
	audio := base64.StdEncoding.EncodeToString([]byte{1, 2, 3, 4})

	fallback, err := handleVoiceSession(context.Background(), agent, failingASR{}, nil, "voice-fallback-eval", "user-1", "刚才谁助攻？", audio, fixedVoiceTime())
	if err != nil {
		t.Fatalf("text fallback should still answer after ASR error: %v", err)
	}
	if fallback.ASRError == "" || !strings.Contains(fallback.Reply, "Fabian") {
		t.Fatalf("expected ASR error plus text reply, got %+v", fallback)
	}

	_, err = handleVoiceSession(context.Background(), agent, failingASR{}, nil, "voice-fallback-eval", "user-1", "", audio, fixedVoiceTime())
	if err == nil {
		t.Fatalf("pure audio should fail when ASR fails and no text fallback exists")
	}

	ttsFallback, err := handleVoiceSession(context.Background(), agent, nil, failingTTS{}, "voice-fallback-eval", "user-1", "刚才谁助攻？", "", fixedVoiceTime())
	if err != nil {
		t.Fatalf("TTS failure should not break text reply: %v", err)
	}
	if ttsFallback.TTSError == "" || !strings.Contains(ttsFallback.Reply, "Fabian") {
		t.Fatalf("expected TTS error plus text reply, got %+v", ttsFallback)
	}
	if ttsFallback.Trace.Voice == nil || ttsFallback.Trace.Voice.TTSStatus != "failed" {
		t.Fatalf("expected failed TTS metadata, got %+v", ttsFallback.Trace.Voice)
	}
}

func TestEvalVoicePlaybackStatusUpdatesTrace(t *testing.T) {
	store := matchstate.NewStore()
	matchID := "voice-playback-eval"
	if _, _, err := store.SetConfig(matchID, matchstate.MatchConfig{HomeTeam: "Spain", AwayTeam: "Germany"}); err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}
	if _, _, err := store.Create(matchID, matchstate.MatchEvent{
		EventType:   "goal",
		Clock:       "24:10",
		TeamID:      "home",
		TeamName:    "Spain",
		PlayerName:  "Pedri",
		Score:       matchstate.Score{Home: 1, Away: 0},
		Description: "Pedri scores.",
		Participants: []matchstate.Participant{
			{Role: "assist", Name: "Fabian"},
			{Role: "pre_assist", Name: "Yamal"},
		},
	}); err != nil {
		t.Fatalf("Create goal error: %v", err)
	}
	tools := companion.NewStoreMemoryTools(store)
	agent := companion.NewAgent(tools)
	result, err := handleVoiceSession(context.Background(), agent, nil, tts.NewMockClient([]byte("mp3")), matchID, "user-1", "刚才谁助攻？", "", fixedVoiceTime())
	if err != nil {
		t.Fatalf("handleVoiceSession error: %v", err)
	}
	if err := recordPlaybackStatus(context.Background(), tools, agent, matchID, result.Trace.ID, "ok"); err != nil {
		t.Fatalf("recordPlaybackStatus error: %v", err)
	}
	trace, err := tools.GetTrace(context.Background(), matchID, result.Trace.ID)
	if err != nil {
		t.Fatalf("GetTrace error: %v", err)
	}
	if trace.Voice == nil || trace.Voice.TTSStatus != "ok" || trace.Voice.PlaybackStatus != "ok" {
		t.Fatalf("expected playback metadata on trace, got %+v", trace.Voice)
	}
}

func seededVoiceAgent(t *testing.T, matchID string) *companion.Agent {
	t.Helper()
	store := matchstate.NewStore()
	if _, _, err := store.SetConfig(matchID, matchstate.MatchConfig{HomeTeam: "Spain", AwayTeam: "Germany"}); err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}
	if _, _, err := store.Create(matchID, matchstate.MatchEvent{
		EventType:   "goal",
		Period:      "first_half",
		Clock:       "24:10",
		TeamID:      "home",
		TeamName:    "Spain",
		PlayerName:  "Pedri",
		Score:       matchstate.Score{Home: 1, Away: 0},
		Description: "Pedri scores.",
		Participants: []matchstate.Participant{
			{Role: "scorer", Name: "Pedri", TeamID: "home", TeamName: "Spain"},
			{Role: "assist", Name: "Fabian", TeamID: "home", TeamName: "Spain"},
			{Role: "pre_assist", Name: "Yamal", TeamID: "home", TeamName: "Spain"},
		},
	}); err != nil {
		t.Fatalf("Create goal error: %v", err)
	}
	return companion.NewAgent(companion.NewStoreMemoryTools(store))
}

type failingASR struct{}

func (failingASR) Transcribe(ctx context.Context, audio []byte, hints []string) (*asr.Result, error) {
	_ = ctx
	_ = audio
	_ = hints
	return nil, errors.New("asr unavailable")
}

type failingTTS struct{}

func (failingTTS) Synthesize(ctx context.Context, text, voiceID string) (*tts.SynthesizeResult, error) {
	_ = ctx
	_ = text
	_ = voiceID
	return nil, errors.New("tts unavailable")
}

func fixedVoiceTime() time.Time {
	return time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
}
