package operatorwrite

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

func TestMemoryServiceExecutesConcurrentDuplicateOnce(t *testing.T) {
	service := NewMemoryService()
	request := Request{MatchID: "match-1", Key: "key-1", PayloadHash: "hash-1", Operation: "events.create"}
	var calls atomic.Int32
	var wait sync.WaitGroup
	errorsSeen := make(chan error, 10)
	for range 10 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			response, _, err := service.Execute(context.Background(), request, func(context.Context) (Response, error) {
				calls.Add(1)
				return JSONResponse(201, map[string]string{"eventId": "event-1"})
			})
			if err != nil {
				errorsSeen <- err
				return
			}
			if response.StatusCode != 201 {
				errorsSeen <- errors.New("unexpected response status")
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
}

func TestMemoryServiceRejectsPayloadConflict(t *testing.T) {
	service := NewMemoryService()
	request := Request{MatchID: "match-1", Key: "key-1", PayloadHash: "hash-1", Operation: "events.create"}
	if _, _, err := service.Execute(context.Background(), request, func(context.Context) (Response, error) {
		return JSONResponse(201, map[string]bool{"ok": true})
	}); err != nil {
		t.Fatal(err)
	}
	request.PayloadHash = "hash-2"
	if _, _, err := service.Execute(context.Background(), request, func(context.Context) (Response, error) {
		return JSONResponse(201, map[string]bool{"ok": false})
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflict error = %v", err)
	}
}

func TestPayloadHashCanonicalizesJSON(t *testing.T) {
	first, err := PayloadHash("POST", "/api/matches/test/config", []byte(`{"away":2,"home":1}`))
	if err != nil {
		t.Fatal(err)
	}
	second, err := PayloadHash("POST", "/api/matches/test/config", []byte(`{ "home": 1, "away": 2 }`))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("canonical hashes differ: %s != %s", first, second)
	}
}
