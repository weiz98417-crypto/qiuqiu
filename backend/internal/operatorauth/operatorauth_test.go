package operatorauth

import (
	"context"
	"strings"
	"testing"

	"qiuqiu/internal/auth"
)

func TestScopesForRoles(t *testing.T) {
	director := ScopesFor(RoleDirector)
	if len(director) != 4 {
		t.Fatalf("director scopes = %v, want the four declared operator scopes", director)
	}
	for _, scope := range []string{
		auth.ScopeOperatorMatchWrite,
		auth.ScopeOperatorFactConfirm,
		auth.ScopeOperatorFactCorrect,
		auth.ScopeOperatorTraceRead,
	} {
		if !contains(director, scope) {
			t.Fatalf("director scopes %v missing %q", director, scope)
		}
	}
	auditor := ScopesFor(RoleAuditor)
	if len(auditor) != 1 || auditor[0] != auth.ScopeOperatorTraceRead {
		t.Fatalf("auditor scopes = %v, want only %q", auditor, auth.ScopeOperatorTraceRead)
	}
	if scopes := ScopesFor(Role("intern")); scopes != nil {
		t.Fatalf("unknown role scopes = %v, want none", scopes)
	}
}

func TestSeedStoresHashAndLookupResolves(t *testing.T) {
	store := NewMemoryStore()
	operator, err := store.Seed(context.Background(), "阿琴", "op-token-1", RoleDirector)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if operator.Name != "阿琴" || operator.Role != RoleDirector {
		t.Fatalf("operator = %+v", operator)
	}

	// The store must only hold the SHA-256 hex hash, never the plaintext.
	store.mu.Lock()
	hashes := make([]string, 0, len(store.byToken))
	for hash := range store.byToken {
		hashes = append(hashes, hash)
	}
	store.mu.Unlock()
	if len(hashes) != 1 || hashes[0] != HashToken("op-token-1") {
		t.Fatalf("stored hashes = %v, want exactly sha256 hex of the token", hashes)
	}
	if len(hashes[0]) != 64 || strings.Trim(hashes[0], "0123456789abcdef") != "" {
		t.Fatalf("stored hash %q is not sha256 hex", hashes[0])
	}

	ctx := context.Background()
	resolved, ok := store.Lookup(ctx, "op-token-1")
	if !ok || resolved.Name != "阿琴" || resolved.Role != RoleDirector {
		t.Fatalf("lookup = %+v ok=%v, want the seeded director", resolved, ok)
	}
	if _, ok := store.Lookup(ctx, "wrong-token"); ok {
		t.Fatal("wrong token resolved, want miss")
	}
	if _, ok := store.Lookup(ctx, ""); ok {
		t.Fatal("empty token resolved, want miss")
	}
}

func TestRevocationIsImmediate(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	if _, err := store.Seed(ctx, "阿伦", "op-token-2", RoleAuditor); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, ok := store.Lookup(ctx, "op-token-2"); !ok {
		t.Fatal("lookup before revocation failed")
	}
	if !store.Delete(ctx, "阿伦") {
		t.Fatal("delete reported no row")
	}
	if _, ok := store.Lookup(ctx, "op-token-2"); ok {
		t.Fatal("lookup succeeded after revocation; row deletion must fail the next request immediately")
	}
	if store.Count(ctx) != 0 {
		t.Fatalf("count after revocation = %d, want 0", store.Count(ctx))
	}
	if _, err := store.Seed(ctx, "阿伦", "op-token-2", RoleAuditor); err != nil {
		t.Fatalf("re-seed after revocation: %v", err)
	}
	if store.Count(ctx) != 1 {
		t.Fatalf("count after re-seed = %d, want 1", store.Count(ctx))
	}
}

func TestBootstrapSeedsFirstDirectorWhenEmpty(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	logs := 0
	if err := Bootstrap(ctx, store, "阿琴:bootstrap-token", func(string, ...any) { logs++ }); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if logs != 1 {
		t.Fatalf("bootstrap logged %d times, want exactly once", logs)
	}
	if store.Count(ctx) != 1 {
		t.Fatalf("count after bootstrap = %d, want 1", store.Count(ctx))
	}
	resolved, ok := store.Lookup(ctx, "bootstrap-token")
	if !ok || resolved.Role != RoleDirector || resolved.Name != "阿琴" {
		t.Fatalf("bootstrap lookup = %+v ok=%v, want seeded director", resolved, ok)
	}

	// A second bootstrap run (or an existing operator population) is a no-op.
	if err := Bootstrap(ctx, store, "别人:other-token", func(string, ...any) { logs++ }); err != nil {
		t.Fatalf("second bootstrap: %v", err)
	}
	if logs != 1 {
		t.Fatalf("bootstrap ran twice (logs=%d); must seed only when the table is empty", logs)
	}
	if store.Count(ctx) != 1 {
		t.Fatalf("count after second bootstrap = %d, want 1", store.Count(ctx))
	}
	if _, ok := store.Lookup(ctx, "other-token"); ok {
		t.Fatal("second bootstrap token accepted; bootstrap must not add rows when operators exist")
	}
}

func TestBootstrapValidatesSpecAndEmptySpec(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	if err := Bootstrap(ctx, store, "", func(string, ...any) {}); err != nil {
		t.Fatalf("empty spec should be a silent no-op, got %v", err)
	}
	if err := Bootstrap(ctx, store, "no-separator", func(string, ...any) {}); err == nil {
		t.Fatal("malformed spec should error")
	}
	if err := Bootstrap(ctx, store, "阿琴:", func(string, ...any) {}); err == nil {
		t.Fatal("empty token should error")
	}
	if store.Count(ctx) != 0 {
		t.Fatalf("failed bootstraps must not seed rows, count = %d", store.Count(ctx))
	}
}

func TestAuditTrailAppendAndRecent(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	_ = store.AppendAudit(ctx, "阿琴", "thread.address", "thread:12")
	_ = store.AppendAudit(ctx, "阿琴", "portrait.delete", "user:u1 slot basic_info/favorite_team")
	_ = store.AppendAudit(ctx, "阿伦", "thread.expire", "thread:9")

	recent, err := store.RecentAudit(ctx, 2)
	if err != nil {
		t.Fatalf("recent audit: %v", err)
	}
	if len(recent) != 2 {
		t.Fatalf("recent audit = %+v, want the 2 newest rows", recent)
	}
	if recent[0].Action != "thread.expire" || recent[0].OperatorName != "阿伦" {
		t.Fatalf("newest audit row = %+v, want the last append", recent[0])
	}
	if recent[1].Action != "portrait.delete" {
		t.Fatalf("second audit row = %+v", recent[1])
	}
}

func contains(haystack []string, needle string) bool {
	for _, candidate := range haystack {
		if candidate == needle {
			return true
		}
	}
	return false
}
