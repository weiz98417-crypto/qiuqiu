package knowledge

// Postgres 实现的一致性回归：与内存实现共用 exerciseStoreParity / seed
// 幂等 harness（无 DATABASE_URL 跳过）。迁移由 matchstate 迁移器统一执行
// （privacy 集成测试同口径）。

import (
	"context"
	"os"
	"testing"

	"qiuqiu/internal/matchstate"
)

func openTestPostgresStore(t *testing.T) *PostgresStore {
	t.Helper()
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	if _, err := matchstate.OpenPostgresStore(ctx, databaseURL, "../../migrations"); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	store, err := OpenPostgresStore(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open knowledge postgres store: %v", err)
	}
	t.Cleanup(store.Close)
	return store
}

func TestPostgresStoreParity(t *testing.T) {
	exerciseStoreParity(t, openTestPostgresStore(t))
}

func TestPostgresSeedIdempotentAndEditVisible(t *testing.T) {
	ctx := context.Background()
	store := openTestPostgresStore(t)

	seeded, err := SeedDir(ctx, store, "../../../knowledge")
	if err != nil {
		t.Fatalf("seed repo yaml: %v", err)
	}
	reseeded, err := SeedDir(ctx, store, "../../../knowledge")
	if err != nil {
		t.Fatalf("reseed: %v", err)
	}
	if reseeded != 0 {
		t.Fatalf("reseed inserted %d rows, want 0 (idempotent)", reseeded)
	}
	if seeded > 0 {
		t.Logf("first seed imported %d entries (fresh database)", seeded)
	}

	// 双实现同构：PG store 挂进 Library 后保存即刻可检索。
	library, err := NewStoreLibrary(ctx, store, nil)
	if err != nil {
		t.Fatalf("library: %v", err)
	}
	before := library.Size()
	entry, ok := library.Search(ctx, "越位是什么")
	if !ok {
		t.Fatal("seeded rule-offside must be retrievable from postgres store")
	}
	entry.Answer = "集成测试改写的越位答案原文。"
	if _, err := library.Put(ctx, entry, "阿琴"); err != nil {
		t.Fatalf("library put: %v", err)
	}
	got, ok := library.Search(ctx, "越位是什么")
	if !ok || got.Answer != "集成测试改写的越位答案原文。" {
		t.Fatalf("search after edit = %+v ok=%v, want edited answer", got, ok)
	}
	if library.Size() != before {
		t.Fatalf("size = %d, want %d (edit must not duplicate rows)", library.Size(), before)
	}
}
