package datasource

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
