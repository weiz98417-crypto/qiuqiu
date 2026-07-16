package config

import "testing"

func TestProductionRequiresCredentials(t *testing.T) {
	cfg := &Config{Environment: "production"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("production config without APP_TOKEN should fail")
	}
	cfg.AppToken = "secret"
	if err := cfg.Validate(); err == nil {
		t.Fatal("production config without MIMO_API_KEY should fail")
	}
	cfg.MiMoAPIKey = "mimo-key"
	cfg.SessionSigningKey = "production-session-signing-key-0123456789"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("production config with credentials should pass: %v", err)
	}
}

func TestAuthModeDefaultsAndProductionRequirement(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("AUTH_MODE", "")
	development := Load()
	if development.AuthMode != "dual" || development.SessionAuthRequired() {
		t.Fatalf("development auth defaults = mode %q required=%v", development.AuthMode, development.SessionAuthRequired())
	}

	production := &Config{Environment: "production", AuthMode: "session"}
	if production.LegacyAuthAllowed() || !production.SessionAuthRequired() {
		t.Fatal("production session mode should reject legacy auth")
	}
	if err := production.Validate(); err == nil {
		t.Fatal("production without session signing key should fail")
	}
}

func TestOriginPolicy(t *testing.T) {
	development := &Config{Environment: "development"}
	if !development.OriginAllowed("http://localhost:7357") {
		t.Fatal("development should allow loopback web clients")
	}
	if development.OriginAllowed("https://example.com") {
		t.Fatal("development should not allow arbitrary remote origins")
	}

	production := &Config{
		Environment:    "production",
		AllowedOrigins: []string{"https://app.qiuqiu.example"},
	}
	if !production.OriginAllowed("https://app.qiuqiu.example") {
		t.Fatal("configured production origin should be allowed")
	}
	if production.OriginAllowed("https://attacker.example") {
		t.Fatal("unconfigured production origin should be rejected")
	}
	if !production.OriginAllowed("") {
		t.Fatal("native clients without an Origin header should be allowed")
	}
	if !production.OriginAllowedForHost("http://127.0.0.1:8080", "127.0.0.1:8080") {
		t.Fatal("production should allow pages served from the same origin")
	}
	if production.OriginAllowedForHost("https://attacker.example", "127.0.0.1:8080") {
		t.Fatal("same-origin allowance must not permit a different host")
	}
}

func TestLoadReadsAPISportsKey(t *testing.T) {
	t.Setenv("APISPORTS_API_KEY", "sports-key")
	if got := Load().APISportsAPIKey; got != "sports-key" {
		t.Fatalf("APISportsAPIKey = %q, want configured key", got)
	}
}
