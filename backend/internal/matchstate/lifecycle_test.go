package matchstate

import "testing"

func TestMatchLifecycleTransitions(t *testing.T) {
	store := NewStore()
	if _, err := store.SetLifecycle("missing", LifecycleLive); err != ErrNotFound {
		t.Fatalf("missing match error=%v", err)
	}
	if _, _, err := store.SetConfig("lifecycle", MatchConfig{HomeTeam: "A", AwayTeam: "B"}); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{LifecycleScheduled, LifecycleLive, LifecycleFinished, LifecycleArchived} {
		if _, err := store.SetLifecycle("lifecycle", state); err != nil {
			t.Fatalf("set %s: %v", state, err)
		}
	}
	if got := store.Lifecycle("lifecycle"); got != LifecycleArchived {
		t.Fatalf("lifecycle=%q", got)
	}
	if _, err := store.SetLifecycle("lifecycle", LifecycleLive); err == nil {
		t.Fatal("archived match must not transition back to live")
	}
}

func TestPublicMatchCatalogHidesUnpublishedLifecycle(t *testing.T) {
	store := NewStore()
	for _, state := range []string{LifecycleScheduled, LifecycleCancelled, LifecycleArchived} {
		if _, _, err := store.SetConfig(state, MatchConfig{HomeTeam: "A", AwayTeam: "B", Lifecycle: state}); err != nil {
			t.Fatal(err)
		}
	}
	matches, err := store.PublicMatchCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || matches[0].MatchID != LifecycleScheduled {
		t.Fatalf("matches=%+v", matches)
	}
}
