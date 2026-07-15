package relationship

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"qiuqiu/internal/matchstate"
)

func TestPostgresDirectorPersistsStateAndIdempotency(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	matchStore, err := matchstate.OpenPostgresStore(ctx, databaseURL, "../../migrations")
	if err != nil {
		t.Fatalf("OpenPostgresStore: %v", err)
	}
	defer matchStore.Close()

	matchID := "relationship-pg-" + time.Now().UTC().Format("20060102150405.000000000")
	userID := "relationship-user-" + matchID
	if _, _, err := matchStore.SetConfig(matchID, matchstate.MatchConfig{HomeTeam: "主队", AwayTeam: "客队"}); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}

	repository, err := OpenPostgresRepository(ctx, databaseURL)
	if err != nil {
		t.Fatalf("OpenPostgresRepository: %v", err)
	}
	director := NewDirector(repository)
	signal := Signal{
		ID:         "pg-boundary-1-" + matchID,
		Kind:       SignalUserTurn,
		UserID:     userID,
		MatchID:    matchID,
		OccurredAt: time.Now().UTC(),
		User:       &UserSignal{Text: "别问工作细节"},
	}
	first, err := director.Apply(ctx, signal)
	if err != nil {
		repository.Close()
		t.Fatalf("first Apply: %v", err)
	}
	repository.Close()

	reopened, err := OpenPostgresRepository(ctx, databaseURL)
	if err != nil {
		t.Fatalf("reopen repository: %v", err)
	}
	defer reopened.Close()
	retried, err := NewDirector(reopened).Apply(ctx, signal)
	if err != nil {
		t.Fatalf("retried Apply: %v", err)
	}
	if retried.ID != first.ID || retried.StateVersion != first.StateVersion {
		t.Fatalf("retried decision = %+v, want id=%q version=%d", retried, first.ID, first.StateVersion)
	}

	next := signal
	next.ID = "pg-boundary-2-" + matchID
	next.User = &UserSignal{Text: "继续看球"}
	second, err := NewDirector(reopened).Apply(ctx, next)
	if err != nil {
		t.Fatalf("second Apply: %v", err)
	}
	if second.Relationship.BoundaryCount != 1 {
		t.Fatalf("boundary count = %d, want persisted 1", second.Relationship.BoundaryCount)
	}
	if second.StateVersion != first.StateVersion+1 {
		t.Fatalf("state version = %d, want %d", second.StateVersion, first.StateVersion+1)
	}
}

func TestPostgresRepositoryRejectsStaleStateUpdate(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	matchStore, err := matchstate.OpenPostgresStore(ctx, databaseURL, "../../migrations")
	if err != nil {
		t.Fatalf("OpenPostgresStore: %v", err)
	}
	defer matchStore.Close()

	repository, err := OpenPostgresRepository(ctx, databaseURL)
	if err != nil {
		t.Fatalf("OpenPostgresRepository: %v", err)
	}
	defer repository.Close()

	suffix := time.Now().UTC().Format("20060102150405.000000000")
	userID := "cas-user-" + suffix
	matchID := "cas-match-" + suffix
	now := time.Now().UTC()
	first := StateUpdate{
		Relationship: RelationshipState{UserID: userID, Stage: StageFirstMeeting, Version: 1, UpdatedAt: now},
		Match:        MatchCompanionState{UserID: userID, MatchID: matchID, LastSignalID: "cas-first", Version: 1, UpdatedAt: now},
		Decision:     Decision{ID: "decision:cas-first-" + suffix, SignalID: "cas-first-" + suffix, StateVersion: 1, CreatedAt: now},
	}
	if err := repository.CompareAndSwap(ctx, ExpectedVersions{}, first); err != nil {
		t.Fatalf("first CompareAndSwap: %v", err)
	}
	stale := first
	stale.Match.LastSignalID = "cas-stale"
	stale.Decision = Decision{ID: "decision:cas-stale-" + suffix, SignalID: "cas-stale-" + suffix, StateVersion: 1, CreatedAt: now}
	if err := repository.CompareAndSwap(ctx, ExpectedVersions{}, stale); !errors.Is(err, ErrConcurrentUpdate) {
		t.Fatalf("stale CompareAndSwap error = %v, want ErrConcurrentUpdate", err)
	}
	if _, err := repository.pool.Exec(ctx, `UPDATE relationship_states SET state = jsonb_set(state, '{version}', '999'::jsonb) WHERE user_id = $1`, userID); err != nil {
		t.Fatalf("corrupt relationship JSON version: %v", err)
	}
	if _, err := repository.pool.Exec(ctx, `UPDATE match_companion_states SET state = jsonb_set(state, '{version}', '999'::jsonb) WHERE user_id = $1 AND match_id = $2`, userID, matchID); err != nil {
		t.Fatalf("corrupt match JSON version: %v", err)
	}
	loaded, err := repository.Load(ctx, userID, matchID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Relationship.Version != 1 || loaded.Match.Version != 1 || loaded.Match.LastSignalID != "cas-first" {
		t.Fatalf("stale update changed persisted state: %+v", loaded)
	}
}

func TestPostgresSignalIdempotencyIsScopedPerUser(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	matchStore, err := matchstate.OpenPostgresStore(ctx, databaseURL, "../../migrations")
	if err != nil {
		t.Fatalf("OpenPostgresStore: %v", err)
	}
	defer matchStore.Close()
	repository, err := OpenPostgresRepository(ctx, databaseURL)
	if err != nil {
		t.Fatalf("OpenPostgresRepository: %v", err)
	}
	defer repository.Close()

	suffix := time.Now().UTC().Format("20060102150405.000000000")
	director := NewDirector(repository)
	signalID := "shared-signal-" + suffix
	apply := func(userID, matchID string) Decision {
		decision, applyErr := director.Apply(ctx, Signal{
			ID: signalID, Kind: SignalUserTurn, UserID: userID, MatchID: matchID,
			OccurredAt: time.Now().UTC(), User: &UserSignal{Text: "继续"},
		})
		if applyErr != nil {
			t.Fatalf("Apply %s: %v", userID, applyErr)
		}
		return decision
	}
	firstUserID := "scope-user-1-" + suffix
	first := apply(firstUserID, "shared-match-1-"+suffix)
	second := apply("scope-user-2-"+suffix, "shared-match-1-"+suffix)
	if first.ID == second.ID {
		t.Fatalf("cross-user decisions share id %q", first.ID)
	}
	third := apply(firstUserID, "shared-match-2-"+suffix)
	if first.ID == third.ID || third.StateVersion != 1 {
		t.Fatalf("cross-match decision reused old state: %+v", third)
	}
}

func TestPostgresResetMatchKeepsCrossMatchRelationship(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	matchStore, err := matchstate.OpenPostgresStore(ctx, databaseURL, "../../migrations")
	if err != nil {
		t.Fatalf("OpenPostgresStore: %v", err)
	}
	defer matchStore.Close()
	repository, err := OpenPostgresRepository(ctx, databaseURL)
	if err != nil {
		t.Fatalf("OpenPostgresRepository: %v", err)
	}
	defer repository.Close()

	suffix := time.Now().UTC().Format("20060102150405.000000000")
	userID := "reset-user-" + suffix
	matchOneID := "reset-match-1-" + suffix
	matchTwoID := "reset-match-2-" + suffix
	director := NewDirector(repository)
	for _, signal := range []Signal{
		{ID: "reset-signal-1-" + suffix, Kind: SignalUserTurn, UserID: userID, MatchID: matchOneID, OccurredAt: time.Now().UTC(), User: &UserSignal{Text: "继续"}},
		{ID: "reset-signal-2-" + suffix, Kind: SignalUserTurn, UserID: userID, MatchID: matchTwoID, OccurredAt: time.Now().UTC(), User: &UserSignal{Text: "继续"}},
	} {
		if _, err := director.Apply(ctx, signal); err != nil {
			t.Fatalf("Apply %s: %v", signal.ID, err)
		}
	}
	if err := repository.ResetMatch(matchOneID); err != nil {
		t.Fatalf("ResetMatch: %v", err)
	}
	matchOne, err := repository.Load(ctx, userID, matchOneID)
	if err != nil {
		t.Fatalf("Load reset match: %v", err)
	}
	matchTwo, err := repository.Load(ctx, userID, matchTwoID)
	if err != nil {
		t.Fatalf("Load retained match: %v", err)
	}
	if matchOne.Match.Version != 0 || matchTwo.Match.Version != 1 || matchOne.Relationship.Version != 2 {
		t.Fatalf("unexpected states after reset: matchOne=%+v matchTwo=%+v", matchOne, matchTwo)
	}
}

func TestPostgresRelationshipMemoryPersistsAcrossMatches(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	matchStore, err := matchstate.OpenPostgresStore(ctx, databaseURL, "../../migrations")
	if err != nil {
		t.Fatalf("OpenPostgresStore: %v", err)
	}
	defer matchStore.Close()

	suffix := time.Now().UTC().Format("20060102150405.000000000")
	userID := "memory-user-" + suffix
	repository, err := OpenPostgresRepository(ctx, databaseURL)
	if err != nil {
		t.Fatalf("OpenPostgresRepository: %v", err)
	}
	director := NewDirector(repository)
	if _, err := director.Apply(ctx, Signal{
		ID: "memory-write-" + suffix, Kind: SignalUserTurn, UserID: userID, MatchID: "memory-match-1-" + suffix,
		OccurredAt: time.Now().UTC(), User: &UserSignal{Text: "我更吃高位压迫这一套"},
	}); err != nil {
		repository.Close()
		t.Fatalf("write memory: %v", err)
	}
	repository.Close()

	reopened, err := OpenPostgresRepository(ctx, databaseURL)
	if err != nil {
		t.Fatalf("reopen repository: %v", err)
	}
	defer reopened.Close()
	decision, err := NewDirector(reopened).Apply(ctx, Signal{
		ID: "memory-read-" + suffix, Kind: SignalUserTurn, UserID: userID, MatchID: "memory-match-2-" + suffix,
		OccurredAt: time.Now().UTC().Add(7 * 24 * time.Hour), User: &UserSignal{Text: "这场的高位压迫你怎么看？"},
	})
	if err != nil {
		t.Fatalf("read memory: %v", err)
	}
	if len(decision.Memories) != 1 || decision.Memories[0].Kind != MemoryKindTasteEvidence {
		t.Fatalf("decision memories = %+v", decision.Memories)
	}
}

func TestPostgresOpenThreadResolvesOnlyAfterUsedDelivery(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	matchStore, err := matchstate.OpenPostgresStore(ctx, databaseURL, "../../migrations")
	if err != nil {
		t.Fatalf("OpenPostgresStore: %v", err)
	}
	defer matchStore.Close()

	suffix := time.Now().UTC().Format("20060102150405.000000000")
	userID := "thread-user-" + suffix
	matchOne := "thread-match-1-" + suffix
	matchTwo := "thread-match-2-" + suffix
	now := time.Now().UTC()
	repository, err := OpenPostgresRepository(ctx, databaseURL)
	if err != nil {
		t.Fatalf("OpenPostgresRepository: %v", err)
	}
	director := NewDirector(repository)
	if _, err := director.Apply(ctx, Signal{
		ID: "thread-create-" + suffix, Kind: SignalUserTurn, UserID: userID, MatchID: matchOne,
		OccurredAt: now, User: &UserSignal{Text: "高位压迫这个问题，下场接着聊"},
	}); err != nil {
		repository.Close()
		t.Fatalf("create thread: %v", err)
	}
	recall, err := director.Apply(ctx, Signal{
		ID: "thread-recall-" + suffix, Kind: SignalUserTurn, UserID: userID, MatchID: matchTwo,
		OccurredAt: now.Add(7 * 24 * time.Hour), User: &UserSignal{Text: "接着上次说高位压迫"},
	})
	if err != nil {
		repository.Close()
		t.Fatalf("recall thread: %v", err)
	}
	memoryID := recall.Memories[0].ID
	repository.Close()

	reopened, err := OpenPostgresRepository(ctx, databaseURL)
	if err != nil {
		t.Fatalf("reopen repository: %v", err)
	}
	defer reopened.Close()
	beforeDelivery, err := reopened.Load(ctx, userID, matchTwo)
	if err != nil {
		t.Fatalf("load pending thread: %v", err)
	}
	if memoryStatus(beforeDelivery.Memories, memoryID) != "active" || !containsString(beforeDelivery.Memories[0].PendingDecisionIDs, recall.ID) {
		t.Fatalf("memories before delivery = %+v", beforeDelivery.Memories)
	}
	if _, err := NewDirector(reopened).Apply(ctx, Signal{
		ID: "thread-delivery-" + suffix, Kind: SignalDeliveryResult, UserID: userID, MatchID: matchTwo,
		OccurredAt: now.Add(7*24*time.Hour + time.Second),
		Delivery:   &DeliverySignal{DecisionID: recall.ID, State: "text_delivered", UsedMemoryIDs: []string{memoryID}},
	}); err != nil {
		t.Fatalf("deliver thread: %v", err)
	}
	afterDelivery, err := reopened.Load(ctx, userID, matchTwo)
	if err != nil {
		t.Fatalf("load resolved thread: %v", err)
	}
	if memoryStatus(afterDelivery.Memories, memoryID) != "resolved" || len(afterDelivery.Memories[0].PendingDecisionIDs) != 0 {
		t.Fatalf("memories after delivery = %+v", afterDelivery.Memories)
	}
}
