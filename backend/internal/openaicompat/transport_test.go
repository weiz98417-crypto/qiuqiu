package openaicompat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 鉴权分支表驱动（openspec/changes/llm-transport-seam）：api-key vs Bearer
// 的唯一实现，这里锁定全部分支。
func TestUsesAPIKeyAuth(t *testing.T) {
	cases := []struct {
		name     string
		endpoint Endpoint
		want     bool
	}{
		{"mimo domain", Endpoint{BaseURL: "https://api.xiaomimimo.com/v1", Model: "mimo-v2.5"}, true},
		{"mimo model prefix", Endpoint{BaseURL: "https://other.example.com/v1", Model: "mimo-v2.5-tts"}, true},
		{"non-mimo", Endpoint{BaseURL: "https://api.deepseek.com/v1", Model: "deepseek-flash"}, false},
		{"empty", Endpoint{}, false},
	}
	for _, tc := range cases {
		if got := UsesAPIKeyAuth(tc.endpoint); got != tc.want {
			t.Fatalf("%s: UsesAPIKeyAuth = %t, want %t", tc.name, got, tc.want)
		}
	}
}

func TestSetAuthHeaders(t *testing.T) {
	mimo := Endpoint{BaseURL: "https://api.xiaomimimo.com/v1", APIKey: "k1", Model: "mimo-v2.5"}
	req := httptest.NewRequest(http.MethodPost, "https://api.xiaomimimo.com/v1/chat/completions", nil)
	SetAuthHeaders(req, mimo)
	if req.Header.Get("api-key") != "k1" {
		t.Fatalf("api-key header = %q", req.Header.Get("api-key"))
	}
	if req.Header.Get("Authorization") != "" {
		t.Fatal("mimo endpoints must not carry an Authorization header")
	}

	other := Endpoint{BaseURL: "https://api.deepseek.com/v1", APIKey: "k2", Model: "deepseek-flash"}
	req2 := httptest.NewRequest(http.MethodPost, "https://api.deepseek.com/v1/chat/completions", nil)
	SetAuthHeaders(req2, other)
	if req2.Header.Get("Authorization") != "Bearer k2" {
		t.Fatalf("Authorization = %q, want Bearer k2", req2.Header.Get("Authorization"))
	}
	if req2.Header.Get("api-key") != "" {
		t.Fatal("non-mimo endpoints must not carry an api-key header")
	}
}

func TestPost(t *testing.T) {
	var seenAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuth = r.Header.Get("Authorization")
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("content-type = %q", r.Header.Get("Content-Type"))
		}
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %q", r.URL.Path)
		}
		switch {
		case r.Header.Get("Authorization") == "Bearer bad":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"bad key"}`))
		default:
			_, _ = w.Write([]byte(`{"ok":true}`))
		}
	}))
	t.Cleanup(server.Close)

	endpoint := Endpoint{BaseURL: server.URL, APIKey: "bad", Model: "other-model"}
	if _, err := Post(context.Background(), server.Client(), endpoint, "/v1/chat/completions", []byte(`{}`), 4096); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("non-2xx must fail with status, got %v", err)
	}

	endpoint.APIKey = "good"
	body, err := Post(context.Background(), server.Client(), endpoint, "/v1/chat/completions", []byte(`{}`), 4096)
	if err != nil {
		t.Fatalf("Post error: %v", err)
	}
	if string(body) != `{"ok":true}` {
		t.Fatalf("body = %s", body)
	}
	if seenAuth != "Bearer good" {
		t.Fatalf("server saw auth %q", seenAuth)
	}
}
