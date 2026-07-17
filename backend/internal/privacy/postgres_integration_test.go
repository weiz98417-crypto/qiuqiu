package privacy

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"qiuqiu/internal/matchstate"
)

func TestPostgresStoreExportsAndDeletesUserData(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	migrationStore, err := matchstate.OpenPostgresStore(ctx, databaseURL, "../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	defer migrationStore.Close()
	store, err := OpenPostgresStore(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	userID := fmt.Sprintf("privacy-test-%d", time.Now().UnixNano())
	matchID := userID + "-match"
	traceID := userID + "-trace"
	defer func() {
		_, _ = store.pool.Exec(context.Background(), `
			DELETE FROM privacy_tombstones WHERE user_id = $1;
			DELETE FROM user_sessions WHERE user_id = $1;
			DELETE FROM pending_match_observations WHERE user_id = $1;
			DELETE FROM conversation_turns WHERE user_id = $1;
			DELETE FROM agent_traces WHERE user_id = $1;
			DELETE FROM relationship_memories WHERE user_id = $1;
			DELETE FROM relationship_states WHERE user_id = $1;
			DELETE FROM match_companion_states WHERE user_id = $1;
			DELETE FROM interaction_decisions WHERE user_id = $1;
			DELETE FROM matches WHERE id = $2
		`, userID, matchID)
	}()

	_, err = store.pool.Exec(ctx, `INSERT INTO matches (id, home_team, away_team, updated_at) VALUES ($1, 'Home', 'Away', now())`, matchID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.pool.Exec(ctx, `
		INSERT INTO user_sessions (id, user_id, device_id, token_hash, scopes, expires_at)
		VALUES ($1, $2, 'privacy-device', decode(md5($2), 'hex'), '{user:read}', now() + interval '1 day')
	`, userID+"-session", userID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.pool.Exec(ctx, `
		INSERT INTO conversation_turns (match_id, user_id, role, text, created_at, expires_at)
		VALUES ($1, $2, 'user', 'hello', now(), now() + interval '1 day')
	`, matchID, userID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.pool.Exec(ctx, `
		INSERT INTO agent_traces (
			id, match_id, user_id, input, intent, output, reason,
			pending_observation, observation_resolution, created_at, expires_at
		)
		VALUES (
			$1, $2, $3, 'hello', 'chat', 'hi', 'test',
			'{"status":"pending_sync"}', '{"status":"confirmed"}', now(), now() + interval '1 day'
		)
	`, traceID, matchID, userID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.pool.Exec(ctx, `
		INSERT INTO relationship_states (user_id, state, version)
		VALUES ($1, '{}', 1)
	`, userID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.pool.Exec(ctx, `
		INSERT INTO relationship_memories (id, user_id, match_id, kind, payload, expires_at)
		VALUES ($1, $2, $3, 'taste_evidence', '{}', now() + interval '1 day')
	`, userID+"-memory", userID, matchID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.pool.Exec(ctx, `
		INSERT INTO match_companion_states (user_id, match_id, state, version)
		VALUES ($1, $2, '{}', 1)
	`, userID, matchID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.pool.Exec(ctx, `
		INSERT INTO interaction_decisions (id, signal_id, user_id, match_id, decision)
		VALUES ($1, 'signal-1', $2, $3, '{}')
	`, userID+"-decision", userID, matchID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.pool.Exec(ctx, `
		INSERT INTO pending_match_observations (
			id, signal_id, trace_id, user_id, match_id, kind, event_type, status,
			received_at, follow_up_deadline, reconcile_until
		) VALUES ($1, 'signal-observation', $2, $3, $4, 'event', 'goal', 'pending_sync', now(), now() + interval '15 seconds', now() + interval '1 minute')
	`, userID+"-observation", traceID, userID, matchID)
	if err != nil {
		t.Fatal(err)
	}

	exported, err := store.Export(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if len(exported.Sessions) != 1 || len(exported.ConversationTurns) != 1 || len(exported.AgentTraces) != 1 || len(exported.RelationshipStates) != 1 || len(exported.RelationshipMemories) != 1 || len(exported.MatchStates) != 1 || len(exported.InteractionDecisions) != 1 || len(exported.PendingObservations) != 1 {
		t.Fatalf("export counts = sessions %d turns %d traces %d states %d memories %d matches %d decisions %d", len(exported.Sessions), len(exported.ConversationTurns), len(exported.AgentTraces), len(exported.RelationshipStates), len(exported.RelationshipMemories), len(exported.MatchStates), len(exported.InteractionDecisions))
	}
	if exported.AgentTraces[0]["pending_observation"] == nil || exported.AgentTraces[0]["observation_resolution"] == nil {
		t.Fatalf("agent trace export omitted observation fields: %+v", exported.AgentTraces[0])
	}
	status, err := store.RequestDeletion(ctx, userID, "user_request")
	if err != nil || status.Status != "pending" || status.JobID == "" {
		t.Fatalf("request status = %+v, err=%v", status, err)
	}
	if _, err := store.Export(ctx, userID); !errors.Is(err, ErrDeletionInProgress) {
		t.Fatalf("export while pending error = %v", err)
	}
	if err := store.ProcessDeletion(ctx, userID); err != nil {
		t.Fatal(err)
	}
	status, err = store.Status(ctx, userID)
	if err != nil || status.Status != "completed" {
		t.Fatalf("completed status = %+v, err=%v", status, err)
	}
	if _, err := store.Export(ctx, userID); !errors.Is(err, ErrDataDeleted) {
		t.Fatalf("export after deletion error = %v", err)
	}

	var remaining int
	if err := store.pool.QueryRow(ctx, `SELECT COUNT(*) FROM conversation_turns WHERE user_id = $1`, userID).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("conversation turns remaining = %d", remaining)
	}
	if err := store.pool.QueryRow(ctx, `SELECT COUNT(*) FROM pending_match_observations WHERE user_id = $1`, userID).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("pending observations remaining = %d", remaining)
	}
}
