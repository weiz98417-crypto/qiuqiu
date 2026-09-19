package observation

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"sort"
	"testing"
	"time"

	"qiuqiu/internal/matchstate"
)

// Record-rule conformance (打磨轮 #4): the same operation sequence runs on the
// memory coordinator and the Postgres coordinator and must produce identical
// results. Both stores' Record now share the applyRecordRules reducer, so
// this test is the proof that neither store drifted; the Postgres half
// self-skips without DATABASE_URL, per repo convention.

// conformanceStore adapts the two coordinator storages behind one seam.
type conformanceStore interface {
	Coordinator
	ActiveObservations(ctx context.Context, userID, matchID string) ([]PendingObservation, error)
	get(ctx context.Context, id string) (PendingObservation, bool, error)
}

type memoryConformanceStore struct{ *MemoryCoordinator }

func (m memoryConformanceStore) get(_ context.Context, id string) (PendingObservation, bool, error) {
	pending, ok := m.Get(id)
	return pending, ok, nil
}

type postgresConformanceStore struct{ *PostgresCoordinator }

func (p postgresConformanceStore) get(ctx context.Context, id string) (PendingObservation, bool, error) {
	return p.Get(ctx, id)
}

type conformanceStep struct {
	name string
	// record, when non-nil, runs Record.
	record Input
	// expireAt, when non-zero, runs Expire at that instant.
	expireAt time.Time
	// wantDupOfSignal marks a record step that must dedupe into the
	// observation created for this signal id; "" expects a new row.
	wantDupOfSignal string
}

type conformanceCase struct {
	name       string
	steps      []conformanceStep
	events     []matchstate.MatchEvent
	wantActive []string          // final active signal ids (order-insensitive)
	wantStatus map[string]Status // signal id → final status
}

func conformanceCases() []conformanceCase {
	base := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	claim := func(signal string, at time.Time, player string) Input {
		return Input{
			SignalID: signal, Kind: "event", EventType: "goal",
			ClaimedPlayer: player, ReceivedAt: at,
		}
	}
	ambiguous := func(signal string, at time.Time) Input {
		return Input{SignalID: signal, Kind: "event_reference", EventType: "goal", ReceivedAt: at}
	}
	return []conformanceCase{
		{
			name: "重复观察合并为一条",
			steps: []conformanceStep{
				{name: "first claim", record: claim("turn-1", base, "萨拉赫")},
				{name: "same signal again", record: claim("turn-1", base.Add(time.Second), "萨拉赫"), wantDupOfSignal: "turn-1"},
				{name: "same claim new signal", record: claim("turn-2", base.Add(2*time.Second), "萨拉赫"), wantDupOfSignal: "turn-1"},
			},
			wantActive: []string{"turn-1"},
			wantStatus: map[string]Status{"turn-1": StatusPendingSync},
		},
		{
			name: "窗口过期后同一主张重新记录",
			steps: []conformanceStep{
				{name: "first claim", record: claim("turn-1", base, "萨拉赫")},
				{name: "expire past the reconcile window", expireAt: base.Add(3 * time.Minute)},
				{name: "same claim after expiry", record: claim("turn-2", base.Add(3*time.Minute), "萨拉赫")},
			},
			wantActive: []string{"turn-2"},
			wantStatus: map[string]Status{"turn-1": StatusExpired, "turn-2": StatusPendingSync},
		},
		{
			name: "第六条观察挤出最旧一条",
			steps: []conformanceStep{
				{name: "A", record: claim("turn-1", base, "A")},
				{name: "B", record: claim("turn-2", base.Add(time.Second), "B")},
				{name: "C", record: claim("turn-3", base.Add(2*time.Second), "C")},
				{name: "D", record: claim("turn-4", base.Add(3*time.Second), "D")},
				{name: "E", record: claim("turn-5", base.Add(4*time.Second), "E")},
				{name: "F supersedes the oldest", record: claim("turn-6", base.Add(5*time.Second), "F")},
			},
			wantActive: []string{"turn-2", "turn-3", "turn-4", "turn-5", "turn-6"},
			wantStatus: map[string]Status{
				"turn-1": StatusSuperseded,
				"turn-2": StatusPendingSync,
				"turn-6": StatusPendingSync,
			},
		},
		{
			name: "两个候选事实停在冲突不猜测",
			steps: []conformanceStep{
				{name: "ambiguous reference", record: ambiguous("turn-1", base)},
			},
			events: []matchstate.MatchEvent{
				{MatchID: "", ID: "goal-a", FactID: "fact-a", FactRevision: 1,
					FactStatus: matchstate.FactStatusProvisional, EventType: "goal",
					CreatedAt: base.Add(5 * time.Second).Format(time.RFC3339Nano)},
				{MatchID: "", ID: "goal-b", FactID: "fact-b", FactRevision: 1,
					FactStatus: matchstate.FactStatusProvisional, EventType: "goal",
					CreatedAt: base.Add(7 * time.Second).Format(time.RFC3339Nano)},
				{MatchID: "", ID: "goal-a", FactID: "fact-a", FactRevision: 2,
					FactStatus: matchstate.FactStatusConfirmed, EventType: "goal",
					CreatedAt: base.Add(9 * time.Second).Format(time.RFC3339Nano)},
			},
			wantActive: []string{"turn-1"},
			wantStatus: map[string]Status{"turn-1": StatusConflict},
		},
	}
}

// runConformanceCase executes one case on one store and returns the observed
// transcript (returned ids, expiry/event resolution counts scoped to this
// case's user+match, final active set and statuses).
func runConformanceCase(t *testing.T, store conformanceStore, testCase conformanceCase, userID, matchID string) conformanceTranscript {
	t.Helper()
	ctx := context.Background()
	created := make(map[string]string)
	var transcript conformanceTranscript
	for _, step := range testCase.steps {
		switch {
		case !step.expireAt.IsZero():
			resolutions, err := store.Expire(ctx, step.expireAt)
			if err != nil {
				t.Fatalf("%s: Expire: %v", step.name, err)
			}
			// Expire is store-wide; count only this case's rows so the
			// transcript stays stable against other users in the database.
			count := 0
			for _, resolution := range resolutions {
				if resolution.UserID == userID && resolution.MatchID == matchID {
					count++
				}
			}
			transcript.ExpireCounts = append(transcript.ExpireCounts, count)
		default:
			input := step.record
			input.UserID = userID
			input.MatchID = matchID
			returned, err := store.Record(ctx, input)
			if err != nil {
				t.Fatalf("%s: Record: %v", step.name, err)
			}
			if step.wantDupOfSignal == "" {
				if prior, seen := created[input.SignalID]; seen && returned.ID != prior {
					t.Fatalf("%s: expected a new observation for signal %s, got existing %s", step.name, input.SignalID, prior)
				}
				created[input.SignalID] = returned.ID
			} else if returned.ID != created[step.wantDupOfSignal] {
				t.Fatalf("%s: Record returned %s, want the dedupe of signal %s (%s)", step.name, returned.ID, step.wantDupOfSignal, created[step.wantDupOfSignal])
			}
			transcript.ReturnedIDs = append(transcript.ReturnedIDs, returned.ID)
		}
	}
	for index, event := range testCase.events {
		event.MatchID = matchID
		resolutions, err := store.OnFactChanged(ctx, event)
		if err != nil {
			t.Fatalf("event %d (%s): OnFactChanged: %v", index, event.ID, err)
		}
		transcript.EventCounts = append(transcript.EventCounts, len(resolutions))
	}
	for _, signal := range testCase.wantActive {
		pending, ok, err := store.get(ctx, created[signal])
		if err != nil || !ok {
			t.Fatalf("get %s: ok=%t err=%v", signal, ok, err)
		}
		if pending.SignalID != signal || !isActiveStatus(pending.Status) {
			t.Fatalf("active probe %s = %+v, want an active observation", signal, pending)
		}
		transcript.ActiveSignals = append(transcript.ActiveSignals, pending.SignalID)
	}
	sort.Strings(transcript.ActiveSignals)
	transcript.SignalStatus = make(map[string]Status, len(testCase.wantStatus))
	for signal, want := range testCase.wantStatus {
		pending, ok, err := store.get(ctx, created[signal])
		if err != nil || !ok {
			t.Fatalf("get %s: ok=%t err=%v", signal, ok, err)
		}
		if pending.Status != want {
			t.Fatalf("%s status = %s, want %s", signal, pending.Status, want)
		}
		transcript.SignalStatus[signal] = pending.Status
	}
	return transcript
}

type conformanceTranscript struct {
	ReturnedIDs   []string
	ExpireCounts  []int
	EventCounts   []int
	ActiveSignals []string
	SignalStatus  map[string]Status
}

func TestCoordinatorRecordRulesConformance(t *testing.T) {
	cases := conformanceCases()

	memoryTranscripts := make([]conformanceTranscript, len(cases))
	for index, testCase := range cases {
		t.Run("memory/"+testCase.name, func(t *testing.T) {
			userID := fmt.Sprintf("observation-conformance-memory-%d-%d", index, time.Now().UnixNano())
			memoryTranscripts[index] = runConformanceCase(t, memoryConformanceStore{NewMemoryCoordinator()}, testCase, userID, userID+"-match")
		})
	}

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set; Postgres record-rule conformance self-skips (memory side already asserted)")
	}
	postgresTranscripts := make([]conformanceTranscript, len(cases))
	for index, testCase := range cases {
		t.Run("postgres/"+testCase.name, func(t *testing.T) {
			userID := fmt.Sprintf("observation-conformance-pg-%d-%d", index, time.Now().UnixNano())
			store := openConformancePostgresStore(t, databaseURL)
			postgresTranscripts[index] = runConformanceCase(t, store, testCase, userID, userID+"-match")
		})
	}
	for index, testCase := range cases {
		if !reflect.DeepEqual(memoryTranscripts[index], postgresTranscripts[index]) {
			t.Fatalf("case %q: postgres transcript = %+v, want the memory transcript %+v",
				testCase.name, postgresTranscripts[index], memoryTranscripts[index])
		}
	}
}

// openConformancePostgresStore runs the migrations once and opens a
// Postgres-backed coordinator cleaned up with the test.
func openConformancePostgresStore(t *testing.T, databaseURL string) conformanceStore {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	migrations, err := matchstate.OpenPostgresStore(ctx, databaseURL, "../../migrations")
	if err != nil {
		t.Fatalf("migrations: %v", err)
	}
	t.Cleanup(migrations.Close)
	coordinator, err := OpenPostgresCoordinator(ctx, databaseURL)
	if err != nil {
		t.Fatalf("OpenPostgresCoordinator: %v", err)
	}
	t.Cleanup(coordinator.Close)
	return postgresConformanceStore{coordinator}
}
