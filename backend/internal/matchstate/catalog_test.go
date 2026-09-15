package matchstate

import "testing"

func TestStatusRankKeepsLiveMatchesAheadOfScheduledAndFinished(t *testing.T) {
	if got := statusRank("live"); got >= statusRank("scheduled") {
		t.Fatalf("live rank=%d, scheduled rank=%d", got, statusRank("scheduled"))
	}
	if got := statusRank("scheduled"); got >= statusRank("finished") {
		t.Fatalf("scheduled rank=%d, finished rank=%d", got, statusRank("finished"))
	}
}

func TestPublicMatchCatalogPresentsLiveMatchesFirst(t *testing.T) {
	store := NewStore()
	for _, match := range []struct {
		id     string
		period string
	}{
		{id: "finished-match", period: "finished"},
		{id: "scheduled-match", period: "pre_match"},
		{id: "live-match", period: "first_half"},
	} {
		if _, _, err := store.SetConfig(match.id, MatchConfig{HomeTeam: "主队", AwayTeam: "客队"}); err != nil {
			t.Fatalf("SetConfig(%s): %v", match.id, err)
		}
		elapsed := 0
		if _, err := store.SetClock(match.id, ClockCommand{Action: ClockActionSet, Period: match.period, ElapsedSeconds: &elapsed, ExpectedVersion: 0}); err != nil {
			t.Fatalf("SetClock(%s): %v", match.id, err)
		}
	}

	matches, err := store.PublicMatchCatalog()
	if err != nil {
		t.Fatalf("PublicMatchCatalog: %v", err)
	}
	if len(matches) != 3 || matches[0].MatchID != "live-match" || matches[1].MatchID != "scheduled-match" || matches[2].MatchID != "finished-match" {
		t.Fatalf("catalog order=%+v", matches)
	}
}
