package main

import (
	"context"
	"testing"

	"qiuqiu/internal/config"
)

func TestStableUserIDNeverFallsBackToRemoteAddress(t *testing.T) {
	if got := stableUserID("", ""); got != "" {
		t.Fatalf("empty identity = %q, want empty", got)
	}
	if got := stableUserID("127.0.0.1:54321", ""); got != "" {
		t.Fatalf("remote address identity = %q, want empty", got)
	}
	if got := stableUserID("anon_12345678-1234-4123-8123-123456789abc", ""); got == "" {
		t.Fatal("valid anonymous identity was rejected")
	}
}

func TestConnectionUserIDOnlyAcceptsMessageIdentityInLegacyMode(t *testing.T) {
	requested := "anon_12345678-1234-4123-8123-123456789abc"
	secure := newConnectionIdentity("")
	if got := connectionUserID(secure, &config.Config{Environment: "production", AuthMode: "session"}, requested); got != "" {
		t.Fatalf("session mode accepted client identity %q", got)
	}

	legacy := newConnectionIdentity("")
	if got := connectionUserID(legacy, &config.Config{Environment: "development", AuthMode: "dual"}, requested); got != requested {
		t.Fatalf("dual mode compatibility identity = %q", got)
	}

	bound := newConnectionIdentity("usr_server_bound")
	if got := connectionUserID(bound, &config.Config{Environment: "production", AuthMode: "session"}, requested); got != "usr_server_bound" {
		t.Fatalf("server-bound identity changed to %q", got)
	}
}

func TestConnectionIdentityKeepsEstablishedStableIdentity(t *testing.T) {
	identity := newConnectionIdentity("")
	want := "anon_12345678-1234-4123-8123-123456789abc"
	if got := identity.Set(want); got != want {
		t.Fatalf("Set valid identity = %q, want %q", got, want)
	}
	if got := identity.Set("anon_aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"); got != want {
		t.Fatalf("replacement changed identity to %q, want %q", got, want)
	}
}

func TestConnectionIdentityWaitsForStableIdentity(t *testing.T) {
	identity := newConnectionIdentity("")
	want := "anon_12345678-1234-4123-8123-123456789abc"
	done := make(chan string, 1)
	go func() {
		done <- identity.Wait(context.Background())
	}()
	identity.Set(want)
	if got := <-done; got != want {
		t.Fatalf("Wait = %q, want %q", got, want)
	}
}

func TestStableSignalIDUsesClientIdWhenValid(t *testing.T) {
	fallback := "turn_generated"
	if got := stableSignalID("turn_anon_123_1", fallback); got != "turn_anon_123_1" {
		t.Fatalf("signal id = %q", got)
	}
	if got := stableSignalID("bad signal id", fallback); got != fallback {
		t.Fatalf("invalid signal id = %q, want %q", got, fallback)
	}
}
