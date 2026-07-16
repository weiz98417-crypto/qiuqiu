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
	request := operatorwrite.Request{MatchID: matchID, Key: "same-key", PayloadHash: "same-hash", Operation: "events.create"}
	var calls atomic.Int32
	var wait sync.WaitGroup
	errorsSeen := make(chan error, 10)
	for range 10 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			response, _, err := service.Execute(ctx, request, func(context.Context) (operatorwrite.Response, error) {
				calls.Add(1)
				return operatorwrite.JSONResponse(201, map[string]string{"eventId": "event-1"})
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
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err == nil {
		_, _ = pool.Exec(context.Background(), `DELETE FROM idempotency_records WHERE match_id = $1`, matchID)
		pool.Close()
	}
}
