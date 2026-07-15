package companion

import (
	"context"
	"strings"
	"testing"
	"time"

	"qiuqiu/internal/matchstate"
)

func TestFirstMeetingGreetingIsPersonalAndTraceable(t *testing.T) {
	tools := NewStoreMemoryTools(matchstate.NewStore())
	agent := NewAgent(tools)

	response, err := agent.HandleFirstMeeting(context.Background(), FirstMeetingRequest{
		MatchID:      "welcome-match",
		UserID:       "new-user",
		Nickname:     "小林",
		FavoriteTeam: "利物浦",
		Now:          time.Date(2026, 7, 11, 20, 0, 0, 0, time.Local),
	})
	if err != nil {
		t.Fatalf("HandleFirstMeeting error: %v", err)
	}
	for _, expected := range []string{"小林", "球球", "利物浦"} {
		if !strings.Contains(response.Reply, expected) {
			t.Fatalf("greeting %q missing %q", response.Reply, expected)
		}
	}
	if response.Trace.Reason != "first_meeting_welcome" {
		t.Fatalf("unexpected trace reason: %q", response.Trace.Reason)
	}
	if len(tools.Traces()) != 1 {
		t.Fatalf("expected one greeting trace, got %d", len(tools.Traces()))
	}
}

func TestFirstMeetingGreetingLimitsProfileLabels(t *testing.T) {
	greeting := firstMeetingGreeting(strings.Repeat("名", 40), strings.Repeat("队", 40))
	if strings.Contains(greeting, strings.Repeat("名", 21)) {
		t.Fatalf("nickname was not limited: %q", greeting)
	}
	if strings.Contains(greeting, strings.Repeat("队", 25)) {
		t.Fatalf("team name was not limited: %q", greeting)
	}
}
