package main

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"strings"
	"testing"
	"time"

	"qiuqiu/internal/asr"
	"qiuqiu/internal/companion"
	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/relationship"
	"qiuqiu/internal/tts"
)

func TestPCMToWavUsesRecorderSampleRate(t *testing.T) {
	wav := pcmToWav([]byte{0, 0, 1, 0})
	if got := binary.LittleEndian.Uint32(wav[24:28]); got != 16000 {
		t.Fatalf("sample rate = %d, want 16000", got)
	}
	if got := binary.LittleEndian.Uint32(wav[28:32]); got != 32000 {
		t.Fatalf("byte rate = %d, want 32000", got)
	}
}

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
	if err := recordPlaybackStatus(context.Background(), tools, agent, matchID, result.Trace.ID, "user-1", "ok"); err != nil {
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

func TestPlaybackTraceStatusKeepsMutedSkipDistinctFromFailure(t *testing.T) {
	if got := playbackTraceStatus("skipped"); got != "skipped" {
		t.Fatalf("skipped playback status = %q", got)
	}
	if got := playbackTraceStatus("blocked"); got != "blocked:autoplay" {
		t.Fatalf("blocked playback status = %q", got)
	}
	if got := playbackTraceStatus("error"); got != "failed:error" {
		t.Fatalf("error playback status = %q", got)
	}
}

func TestPlaybackStatusRejectsDifferentUser(t *testing.T) {
	store := matchstate.NewStore()
	tools := companion.NewStoreMemoryTools(store)
	agent := companion.NewAgent(tools)
	trace := companion.Trace{
		ID: "trace-owner-test", MatchID: "match-owner-test", UserID: "user-1", Input: "hello", Output: "hi", CreatedAt: fixedVoiceTime(),
	}
	if err := tools.WriteTrace(context.Background(), trace); err != nil {
		t.Fatalf("WriteTrace: %v", err)
	}
	if err := recordPlaybackStatus(context.Background(), tools, agent, trace.MatchID, trace.ID, "user-2", "ok"); err == nil {
		t.Fatal("playback update from a different user was accepted")
	}
	stored, err := tools.GetTrace(context.Background(), trace.MatchID, trace.ID)
	if err != nil {
		t.Fatalf("GetTrace: %v", err)
	}
	if stored.Voice != nil {
		t.Fatalf("mismatched playback changed trace: %+v", stored.Voice)
	}
}

func TestDisplayedFirstMeetingReplyMarksGreetingDelivered(t *testing.T) {
	store := matchstate.NewStore()
	tools := companion.NewStoreMemoryTools(store)
	repository := relationship.NewMemoryRepository()
	agent := companion.NewAgent(tools).WithDirector(relationship.NewDirector(repository))
	trace := companion.Trace{
		ID: "trace-first-meeting-display", MatchID: "match-1", UserID: "user-1", Input: "first_meeting", Output: "hello",
		Reason: "first_meeting_welcome", CreatedAt: fixedVoiceTime(),
	}
	if err := tools.WriteTrace(context.Background(), trace); err != nil {
		t.Fatalf("WriteTrace: %v", err)
	}
	if err := recordDisplayedReply(context.Background(), tools, agent, trace.MatchID, trace.ID, trace.UserID, fixedVoiceTime()); err != nil {
		t.Fatalf("recordDisplayedReply: %v", err)
	}
	state, err := repository.Load(context.Background(), trace.UserID, trace.MatchID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if state.Relationship.GreetingDeliveredAt == nil || state.Match.PlaybackState != "text_delivered" {
		t.Fatalf("display acknowledgement was not persisted: %+v", state)
	}
}

func TestReplyContextActiveRejectsCanceledTurns(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	if !replyContextActive(ctx) {
		t.Fatal("active context rejected")
	}
	cancel()
	if replyContextActive(ctx) {
		t.Fatal("canceled context accepted")
	}
}

func TestVoiceTurnRefreshesOnceWhenCriticalFactChanges(t *testing.T) {
	snapshots := []matchstate.Snapshot{
		{},
		{KeyEvents: []matchstate.MatchEvent{{ID: "goal-1", EventType: "goal"}}},
	}
	snapshotCall := 0
	generationCall := 0
	result, err := handleVoiceTurnWithFactRefresh(
		func() matchstate.Snapshot {
			index := snapshotCall
			if index >= len(snapshots) {
				index = len(snapshots) - 1
			}
			snapshotCall++
			return snapshots[index]
		},
		func(text, audio string) (voiceSessionResult, error) {
			generationCall++
			if generationCall == 1 {
				return voiceSessionResult{Text: "刚才谁进球？", Reply: "还没有进球。"}, nil
			}
			if text != "刚才谁进球？" || audio != "" {
				t.Fatalf("refresh input = %q/%q, want recognized text without audio", text, audio)
			}
			return voiceSessionResult{Text: text, Reply: "萨拉赫刚刚进球了。"}, nil
		},
		"",
		"encoded-audio",
	)
	if err != nil {
		t.Fatalf("handleVoiceTurnWithFactRefresh error: %v", err)
	}
	if generationCall != 2 || result.Reply != "萨拉赫刚刚进球了。" {
		t.Fatalf("expected one refreshed answer, calls=%d result=%+v", generationCall, result)
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
