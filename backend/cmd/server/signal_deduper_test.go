package main

import (
	"testing"
	"time"
)

func TestSignalDeduperScopesClaimsByKeyAndExpiresThem(t *testing.T) {
	deduper := newSignalDeduper(time.Minute, 2)
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)

	if !deduper.Claim("user\x00match\x00signal", now) {
		t.Fatal("first claim was rejected")
	}
	if deduper.Claim("user\x00match\x00signal", now.Add(30*time.Second)) {
		t.Fatal("duplicate claim was accepted")
	}
	if !deduper.Claim("other-user\x00match\x00signal", now.Add(30*time.Second)) {
		t.Fatal("scoped claim was rejected")
	}
	if !deduper.Claim("user\x00match\x00signal", now.Add(time.Minute)) {
		t.Fatal("expired claim was not released")
	}
}

func TestSignalDeduperEvictsOldestWhenBounded(t *testing.T) {
	deduper := newSignalDeduper(time.Hour, 2)
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)

	if !deduper.Claim("first", now) || !deduper.Claim("second", now.Add(time.Second)) || !deduper.Claim("third", now.Add(2*time.Second)) {
		t.Fatal("bounded claims were not accepted")
	}
	if deduper.Claim("second", now.Add(3*time.Second)) {
		t.Fatal("second claim should remain in the bounded set")
	}
	if !deduper.Claim("first", now.Add(3*time.Second)) {
		t.Fatal("oldest claim should have been evicted")
	}
}
