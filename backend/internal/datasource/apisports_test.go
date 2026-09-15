package datasource

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientRejectsHTTPErrorResponses(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusTooManyRequests} {
		t.Run(fmt.Sprintf("status_%d", status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte(strings.Repeat("quota exceeded ", 500)))
			}))
			defer server.Close()

			client := NewClient("test-key").WithBaseURL(server.URL)
			_, err := client.GetLiveFixtures()
			if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("status %d", status)) {
				t.Fatalf("error = %v, want status %d", err, status)
			}
			if len(err.Error()) > maxAPIErrorMessageBytes+100 {
				t.Fatalf("error body was not bounded: %d bytes", len(err.Error()))
			}
		})
	}
}

func TestGetFixturesUsesUTCOffsetDateAndPreservesUnknownScore(t *testing.T) {
	requestedDate := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedDate = r.URL.Query().Get("date")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"get":"fixtures","results":1,"errors":[],"response":[{"fixture":{"id":42,"date":"2026-07-25T12:00:00Z","status":{"short":"NS"}},"league":{"name":"Friendly"},"teams":{"home":{"name":"Spain"},"away":{"name":"Germany"}},"goals":{"home":null,"away":null}}]}`))
	}))
	defer server.Close()

	client := NewClient("test-key").WithBaseURL(server.URL)
	from := time.Date(2026, 7, 24, 16, 0, 0, 0, time.UTC)
	fixtures, err := client.GetFixtures(from, from.Add(24*time.Hour), "UTC+08:00")
	if err != nil {
		t.Fatalf("GetFixtures: %v", err)
	}
	if requestedDate != "2026-07-25" {
		t.Fatalf("requested date = %q, want 2026-07-25", requestedDate)
	}
	if len(fixtures) != 1 || fixtures[0].HomeGoalKnown || fixtures[0].AwayGoalKnown {
		t.Fatalf("unknown score was normalized as known: %+v", fixtures)
	}
	if zone := fixtures[0].KickoffAt.Location().String(); zone != "UTC+08:00" {
		t.Fatalf("kickoff timezone = %q, want UTC+08:00", zone)
	}
}

func TestGetFixturesContextCancelsProviderRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	client := NewClient("test-key").WithBaseURL(server.URL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := client.GetFixturesContext(ctx, time.Now(), time.Now().Add(24*time.Hour), "UTC")
	if err == nil {
		t.Fatal("canceled schedule request returned no error")
	}
}

func TestClientRejectsSuccessfulResponseWithAPIErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"get":"fixtures","results":0,"errors":{"requests":"daily quota reached"},"response":[]}`))
	}))
	defer server.Close()

	client := NewClient("test-key").WithBaseURL(server.URL)
	_, err := client.GetLiveFixtures()
	if err == nil || !strings.Contains(err.Error(), "daily quota reached") {
		t.Fatalf("error = %v, want API quota error", err)
	}
}
