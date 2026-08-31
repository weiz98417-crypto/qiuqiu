package config

import (
	"testing"
	"time"
)

func TestCompanionRealizerTimeoutDefaultsToFiveSecondsAndCanBeConfigured(t *testing.T) {
	t.Setenv("COMPANION_REALIZER_TIMEOUT_MS", "")
	if got := Load().CompanionRealizerTimeout(); got != 5*time.Second {
		t.Fatalf("default companion realizer timeout = %s, want 5s", got)
	}

	t.Setenv("COMPANION_REALIZER_TIMEOUT_MS", "7500")
	if got := Load().CompanionRealizerTimeout(); got != 7500*time.Millisecond {
		t.Fatalf("configured companion realizer timeout = %s, want 7.5s", got)
	}
}

func TestCompanionRealizerTimeoutCannotExceedHTTPClientTimeout(t *testing.T) {
	cfg := &Config{CompanionRealizerTimeoutMS: 10001}
	if err := cfg.Validate(); err == nil {
		t.Fatal("companion realizer timeout above the HTTP client limit should fail validation")
	}
}

func TestPendingObservationCoordinationDefaultsOnAndCanBeDisabled(t *testing.T) {
	t.Setenv("PENDING_OBSERVATION_COORDINATION", "")
	if cfg := Load(); !cfg.PendingObservationCoordination {
		t.Fatal("pending observation coordination should default to enabled")
	}
	t.Setenv("PENDING_OBSERVATION_COORDINATION", "false")
	if cfg := Load(); cfg.PendingObservationCoordination {
		t.Fatal("pending observation coordination should be disabled by feature flag")
	}
}

func TestFactLedgerPublicReadsDefaultsOnAndCanBeDisabled(t *testing.T) {
	t.Setenv("FACT_LEDGER_PUBLIC_READS", "")
	if cfg := Load(); !cfg.FactLedgerPublicReads {
		t.Fatal("fact ledger public reads should default to enabled")
	}
	t.Setenv("FACT_LEDGER_PUBLIC_READS", "false")
	if cfg := Load(); cfg.FactLedgerPublicReads {
		t.Fatal("fact ledger public reads should be disabled by rollback flag")
	}
}

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

	production := &Config{Environment: "production", AuthMode: "dual"}
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

func TestLoadReadsAPISportsBaseURL(t *testing.T) {
	t.Setenv("APISPORTS_BASE_URL", "")
	if got := Load().APISportsBaseURL; got != "https://v3.football.api-sports.io" {
		t.Fatalf("default APISportsBaseURL = %q", got)
	}

	t.Setenv("APISPORTS_BASE_URL", "http://127.0.0.1:19090")
	if got := Load().APISportsBaseURL; got != "http://127.0.0.1:19090" {
		t.Fatalf("configured APISportsBaseURL = %q", got)
	}
}

func TestLoadReadsPrivacyRetentionDays(t *testing.T) {
	t.Setenv("PRIVACY_RETENTION_DAYS", "45")
	if got := Load().PrivacyRetentionDays; got != 45 {
		t.Fatalf("PrivacyRetentionDays = %d, want 45", got)
	}
	if err := (&Config{PrivacyRetentionDays: -1}).Validate(); err == nil {
		t.Fatal("negative privacy retention should fail validation")
	}
}
