package memory

import (
	"context"
	"testing"
)

// reflection-attribution：post_match 审计标签用用户实际看过的那场终场。
func TestRecentEndedMatchForAttributesUserMatch(t *testing.T) {
	q := NewQueue(NewMemobase(MemobaseConfig{}), nil, nil)

	q.Observe(context.Background(), Moment{UserID: "usr_a", Content: "看 A 场进球", Importance: 0.8, MatchID: "match_a"})
	q.Observe(context.Background(), Moment{UserID: "usr_b", Content: "看 B 场红牌", Importance: 0.8, MatchID: "match_b"})

	ended := []string{"match_a", "match_b"}
	if got := q.RecentEndedMatchFor("usr_a", ended); got != "match_a" {
		t.Fatalf("usr_a attributed %q, want match_a", got)
	}
	if got := q.RecentEndedMatchFor("usr_b", ended); got != "match_b" {
		t.Fatalf("usr_b attributed %q, want match_b", got)
	}
}

func TestRecentEndedMatchForPrefersMostRecent(t *testing.T) {
	q := NewQueue(NewMemobase(MemobaseConfig{}), nil, nil)

	q.Observe(context.Background(), Moment{UserID: "usr_a", Content: "先看 A", Importance: 0.8, MatchID: "match_a"})
	q.Observe(context.Background(), Moment{UserID: "usr_a", Content: "再看 B", Importance: 0.8, MatchID: "match_b"})

	if got := q.RecentEndedMatchFor("usr_a", []string{"match_a", "match_b"}); got != "match_b" {
		t.Fatalf("attributed %q, want most recent match_b", got)
	}
}

func TestRecentEndedMatchForSkipsUnwatched(t *testing.T) {
	q := NewQueue(NewMemobase(MemobaseConfig{}), nil, nil)

	q.Observe(context.Background(), Moment{UserID: "usr_a", Content: "只看 A", Importance: 0.8, MatchID: "match_a"})

	if got := q.RecentEndedMatchFor("usr_a", []string{"match_b", "match_c"}); got != "" {
		t.Fatalf("attributed %q, want empty for unwatched matches", got)
	}
	if got := q.RecentEndedMatchFor("usr_nobody", []string{"match_a"}); got != "" {
		t.Fatalf("attributed %q, want empty for unknown user", got)
	}
}

func TestRecentEndedMatchForBoundedWindow(t *testing.T) {
	q := NewQueue(NewMemobase(MemobaseConfig{}), nil, nil)

	for index := 0; index < maxUserMatches+4; index++ {
		q.Observe(context.Background(), Moment{
			UserID: "usr_a", Content: "看很多场", Importance: 0.8,
			MatchID: "match_old_" + string(rune('a'+index)),
		})
	}
	q.Observe(context.Background(), Moment{UserID: "usr_a", Content: "最新一场", Importance: 0.8, MatchID: "match_final"})

	ended := []string{"match_old_a", "match_final"}
	if got := q.RecentEndedMatchFor("usr_a", ended); got != "match_final" {
		t.Fatalf("attributed %q, want match_final", got)
	}
	// 最老的窗口外比赛已被淘汰。
	ended = []string{"match_old_a"}
	if got := q.RecentEndedMatchFor("usr_a", ended); got != "" {
		t.Fatalf("attributed %q, want empty (evicted from window)", got)
	}
}

func TestObserveIgnoresEmptyMatchAttribution(t *testing.T) {
	q := NewQueue(NewMemobase(MemobaseConfig{}), nil, nil)

	q.Observe(context.Background(), Moment{UserID: "usr_a", Content: "纯闲聊", Importance: 0.8})
	q.Observe(context.Background(), Moment{UserID: "", Content: "无用户", Importance: 0.8, MatchID: "match_x"})

	if got := q.RecentEndedMatchFor("usr_a", []string{"match_x"}); got != "" {
		t.Fatalf("attributed %q, want empty (no MatchID observed)", got)
	}
}
