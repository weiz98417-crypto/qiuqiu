package operatorwrite_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/operatorwrite"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresServicePersistsAndSerializesIdempotency(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	migrations, err := matchstate.OpenPostgresStore(ctx, databaseURL, "../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	defer migrations.Close()
	service, err := operatorwrite.OpenPostgresService(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	matchID := fmt.Sprintf("idempotency-test-%d", time.Now().UnixNano())
	request := operatorwrite.Request{MatchID: matchID, Key: "same-key", PayloadHash: "same-hash", Operation: "events.create", Atomic: true}
	var calls atomic.Int32
	var wait sync.WaitGroup
	errorsSeen := make(chan error, 10)
	for range 10 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			response, _, err := service.Execute(ctx, request, func(operationCtx context.Context) (operatorwrite.Response, error) {
				if _, ok := operatorwrite.Transaction(operationCtx); !ok {
					return operatorwrite.Response{}, errors.New("atomic operation did not receive transaction")
				}
				calls.Add(1)
				return operatorwrite.JSONResponse(201, map[string]any{"event": map[string]string{"id": "event-1"}})
			})
			if err != nil {
				errorsSeen <- err
				return
			}
			if response.StatusCode != 201 {
				errorsSeen <- errors.New("unexpected status")
			}
		}()
	}
	wait.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("operation calls = %d, want 1", calls.Load())
	}
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	var resultEventID string
	if err := pool.QueryRow(ctx, `SELECT result_event_id FROM idempotency_records WHERE match_id = $1 AND idempotency_key = $2`, matchID, request.Key).Scan(&resultEventID); err != nil {
		t.Fatal(err)
	}
	if resultEventID != "event-1" {
		t.Fatalf("result event id = %q, want event-1", resultEventID)
	}
	service.Close()

	reopened, err := operatorwrite.OpenPostgresService(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	response, replayed, err := reopened.Execute(ctx, request, func(context.Context) (operatorwrite.Response, error) {
		return operatorwrite.Response{}, errors.New("should not execute")
	})
	if err != nil || !replayed || response.StatusCode != 201 {
		t.Fatalf("reopened replay = status %d replayed %v err %v", response.StatusCode, replayed, err)
	}
	request.PayloadHash = "different"
	if _, _, err := reopened.Execute(ctx, request, func(context.Context) (operatorwrite.Response, error) { return operatorwrite.Response{}, nil }); !errors.Is(err, operatorwrite.ErrConflict) {
		t.Fatalf("payload conflict error = %v", err)
	}
	_, _ = pool.Exec(context.Background(), `DELETE FROM idempotency_records WHERE match_id = $1`, matchID)
	pool.Close()
}

func TestPostgresServiceReservesNonAtomicWritesBeforeSideEffects(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	migrations, err := matchstate.OpenPostgresStore(ctx, databaseURL, "../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	defer migrations.Close()
	service, err := operatorwrite.OpenPostgresService(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	matchID := fmt.Sprintf("idempotency-reserved-test-%d", time.Now().UnixNano())
	request := operatorwrite.Request{MatchID: matchID, Key: "same-key", PayloadHash: "same-hash", Operation: "config.set"}
	started := make(chan struct{})
	release := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		_, _, executeErr := service.Execute(ctx, request, func(operationCtx context.Context) (operatorwrite.Response, error) {
			if _, ok := operatorwrite.Transaction(operationCtx); ok {
				return operatorwrite.Response{}, errors.New("reserved operation unexpectedly received transaction")
			}
			close(started)
			<-release
			return operatorwrite.JSONResponse(200, map[string]bool{"ok": true})
		})
		firstDone <- executeErr
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if _, _, err := service.Execute(ctx, request, func(context.Context) (operatorwrite.Response, error) {
		return operatorwrite.Response{}, errors.New("duplicate operation should not execute")
	}); !errors.Is(err, operatorwrite.ErrInProgress) {
		t.Fatalf("concurrent duplicate error = %v, want ErrInProgress", err)
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	response, replayed, err := service.Execute(ctx, request, func(context.Context) (operatorwrite.Response, error) {
		return operatorwrite.Response{}, errors.New("completed operation should replay")
	})
	if err != nil || !replayed || response.StatusCode != 200 {
		t.Fatalf("completed replay status=%d replayed=%v err=%v", response.StatusCode, replayed, err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err == nil {
		_, _ = pool.Exec(ctx, `DELETE FROM idempotency_records WHERE match_id = $1`, matchID)
		pool.Close()
	}
}

func TestPostgresServiceRecoversFailedAndStaleReservations(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	migrations, err := matchstate.OpenPostgresStore(ctx, databaseURL, "../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	defer migrations.Close()
	service, err := operatorwrite.OpenPostgresService(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	failedMatchID := fmt.Sprintf("idempotency-failed-test-%d", time.Now().UnixNano())
	failedRequest := operatorwrite.Request{MatchID: failedMatchID, Key: "same-key", PayloadHash: "same-hash", Operation: "config.set"}
	if _, _, err := service.Execute(ctx, failedRequest, func(context.Context) (operatorwrite.Response, error) {
		return operatorwrite.Response{}, errors.New("transient failure")
	}); err == nil {
		t.Fatal("expected first attempt to fail")
	}
	var status, lastError string
	if err := pool.QueryRow(ctx, `SELECT status, last_error FROM idempotency_records WHERE match_id = $1 AND idempotency_key = $2`, failedMatchID, failedRequest.Key).Scan(&status, &lastError); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || lastError == "" {
		t.Fatalf("failed reservation status=%q last_error=%q", status, lastError)
	}
	if response, replayed, err := service.Execute(ctx, failedRequest, func(context.Context) (operatorwrite.Response, error) {
		return operatorwrite.JSONResponse(200, map[string]bool{"ok": true})
	}); err != nil || replayed || response.StatusCode != 200 {
		t.Fatalf("failed reservation retry status=%d replayed=%v err=%v", response.StatusCode, replayed, err)
	}

	staleMatchID := fmt.Sprintf("idempotency-stale-test-%d", time.Now().UnixNano())
	staleRequest := operatorwrite.Request{MatchID: staleMatchID, Key: "same-key", PayloadHash: "same-hash", Operation: "automation.set"}
	if _, err := pool.Exec(ctx, `
		INSERT INTO idempotency_records (
			match_id, idempotency_key, payload_hash, operation, status, created_at, updated_at, expires_at
		) VALUES ($1, $2, $3, $4, 'pending', now() - interval '10 minutes', now() - interval '10 minutes', now() + interval '1 hour')
	`, staleMatchID, staleRequest.Key, staleRequest.PayloadHash, staleRequest.Operation); err != nil {
		t.Fatal(err)
	}
	if response, replayed, err := service.Execute(ctx, staleRequest, func(context.Context) (operatorwrite.Response, error) {
		return operatorwrite.JSONResponse(200, map[string]bool{"ok": true})
	}); err != nil || replayed || response.StatusCode != 200 {
		t.Fatalf("stale reservation retry status=%d replayed=%v err=%v", response.StatusCode, replayed, err)
	}

	_, _ = pool.Exec(ctx, `DELETE FROM idempotency_records WHERE match_id = ANY($1)`, []string{failedMatchID, staleMatchID})
}
