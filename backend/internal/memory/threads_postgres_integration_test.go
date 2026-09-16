package memory

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/privacy"
	"qiuqiu/internal/relationship"
)

func TestPostgresThreadStoreIntegration(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	migrated, err := matchstate.OpenPostgresStore(ctx, databaseURL, "../../migrations")
	if err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	defer migrated.Close()
	store, err := OpenThreadStore(ctx, databaseURL)
	if err != nil {
		t.Fatalf("OpenThreadStore: %v", err)
	}
	defer store.Close()
	records, err := OpenRecords(ctx, databaseURL)
	if err != nil {
		t.Fatalf("OpenRecords: %v", err)
	}
	defer records.Close()

	userID := "pg-thread-user-" + time.Now().UTC().Format("150405")
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = records.pool.Exec(cleanupCtx, `DELETE FROM privacy_tombstones WHERE user_id = $1`, userID)
		_, _ = records.pool.Exec(cleanupCtx, `DELETE FROM open_threads WHERE user_id = $1`, userID)
	}()
	question, err := store.AppendThread(ctx, Thread{UserID: userID, Kind: ThreadUnansweredQuestion, Content: "穆西亚拉进球了吗", SourceTurn: "turn_1"})
	if err != nil {
		t.Fatalf("AppendThread: %v", err)
	}
	if question.ID == "" || question.State != "open" || question.CreatedAt.IsZero() {
		t.Fatalf("appended thread = %+v, want ledger id, open state and timestamp", question)
	}
	promise, err := store.AppendThread(ctx, Thread{UserID: userID, Kind: ThreadPromise, Content: "待会儿告诉你"})
	if err != nil {
		t.Fatalf("AppendThread promise: %v", err)
	}

	open, err := store.OpenThreads(ctx, userID)
	if err != nil || len(open) != 2 {
		t.Fatalf("OpenThreads = %+v err=%v, want both threads", open, err)
	}

	if err := store.MarkThreadAddressed(ctx, promise.ID); err != nil {
		t.Fatalf("MarkThreadAddressed: %v", err)
	}
	open, err = store.OpenThreads(ctx, userID)
	if err != nil || len(open) != 1 || open[0].ID != question.ID {
		t.Fatalf("OpenThreads after address = %+v err=%v, want only the question", open, err)
	}

	// Expiry is visible in the audit: one thread_expired row per aged thread.
	expired, err := store.ExpireStaleThreads(ctx, time.Now().UTC().Add(2*DefaultThreadTTL), DefaultThreadTTL)
	if err != nil || len(expired) != 1 || expired[0].ID != question.ID {
		t.Fatalf("ExpireStaleThreads = %+v err=%v, want the stale question", expired, err)
	}
	var reasonCode string
	if err := records.pool.QueryRow(ctx, `
		SELECT reason_code FROM memory_extraction_audit
		WHERE moment_id = $1 AND reason_code = $2
		ORDER BY id DESC LIMIT 1
	`, "thread:"+question.ID, ReasonThreadExpired).Scan(&reasonCode); err != nil {
		t.Fatalf("thread expiry audit row missing: %v", err)
	}

	// Privacy lifecycle: a completed tombstone blocks every thread read/write.
	if err := records.pool.QueryRow(ctx, `
		INSERT INTO privacy_tombstones (user_id, status) VALUES ($1, 'completed')
		ON CONFLICT (user_id) DO UPDATE SET status = 'completed'
		RETURNING user_id
	`, userID).Scan(&reasonCode); err != nil {
		t.Fatalf("seed tombstone: %v", err)
	}
	if _, err := store.AppendThread(ctx, Thread{UserID: userID, Kind: ThreadPromise, Content: "again"}); !errors.Is(err, privacy.ErrDataDeleted) {
		t.Fatalf("AppendThread after deletion = %v, want ErrDataDeleted", err)
	}
	if _, err := store.OpenThreads(ctx, userID); !errors.Is(err, privacy.ErrDataDeleted) {
		t.Fatalf("OpenThreads after deletion = %v, want ErrDataDeleted", err)
	}

	// Talkativeness preference persists per user and honors the lifecycle.
	if err := records.RecordTalkativeness(ctx, userID, "loud"); err != nil {
		t.Fatalf("RecordTalkativeness: %v", err)
	}
	tier, err := records.Talkativeness(ctx, userID)
	if err != nil || tier != relationship.TalkativenessNormal {
		t.Fatalf("Talkativeness = %q err=%v, want unknown values normalized to normal", tier, err)
	}
	if err := records.RecordTalkativeness(ctx, userID, relationship.TalkativenessActive); err != nil {
		t.Fatalf("RecordTalkativeness active: %v", err)
	}
	if tier, err = records.Talkativeness(ctx, userID); err != nil || tier != relationship.TalkativenessActive {
		t.Fatalf("Talkativeness = %q err=%v, want the persisted active tier", tier, err)
	}
	if _, err := records.Talkativeness(ctx, userID); !errors.Is(err, privacy.ErrDataDeleted) {
		t.Fatalf("Talkativeness after deletion = %v, want ErrDataDeleted", err)
	}
}
