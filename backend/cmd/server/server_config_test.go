package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDevelopmentPagesAreNotRegisteredInProduction(t *testing.T) {
	mux := http.NewServeMux()
	registerDevelopmentPages(mux, "production", t.TempDir())

	for _, path := range []string{"/test-expressions.html", "/director-prototype.html"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		if response.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d, want 404", path, response.Code)
		}
	}
}

func TestDevelopmentPagesAreAvailableOutsideProduction(t *testing.T) {
	assetsDir := t.TempDir()
	for _, name := range []string{"test-expressions.html", "director-prototype.html"} {
		if err := os.WriteFile(filepath.Join(assetsDir, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	mux := http.NewServeMux()
	registerDevelopmentPages(mux, "development", assetsDir)

	for _, path := range []string{"/test-expressions.html", "/director-prototype.html"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200", path, response.Code)
		}
	}
}

func TestHTTPServerHasConnectionSafetyLimits(t *testing.T) {
	server := newHTTPServer(":0", http.NewServeMux())
	if server.ReadHeaderTimeout != 10*time.Second {
		t.Fatalf("ReadHeaderTimeout = %s", server.ReadHeaderTimeout)
	}
	if server.IdleTimeout != 2*time.Minute {
		t.Fatalf("IdleTimeout = %s", server.IdleTimeout)
	}
	if server.MaxHeaderBytes != 1<<20 {
		t.Fatalf("MaxHeaderBytes = %d", server.MaxHeaderBytes)
	}
}
