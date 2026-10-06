package knowledge

import (
	"context"
	"path/filepath"
	"testing"
)

// knowledge-worldinfo 11.0 门判的常驻守卫（2026-10-07 记录：52 条全解析导入，
// 门开）：条目库跌破 50 即红——防止删条目悄悄破门（ADR-0023 门判的回退面）。
func TestKnowledgeDirCountsOverGate(t *testing.T) {
	dir := filepath.Join("..", "..", "knowledge")
	store := NewMemoryStore()
	// 与生产同路径：对 KNOWLEDGE_DIR 根一次递归导入（Open 同款 WalkDir）。
	seeded, err := SeedDir(context.Background(), store, dir)
	if err != nil {
		t.Fatalf("SeedDir: %v", err)
	}
	if seeded < 50 {
		t.Fatalf("knowledge entries = %d, want >= 50 (knowledge-worldinfo 门判)", seeded)
	}
	t.Logf("knowledge gate: %d entries parse and import cleanly", seeded)
}
