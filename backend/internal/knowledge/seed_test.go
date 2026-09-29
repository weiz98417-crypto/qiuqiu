package knowledge

// Seed 幂等与「编辑 → 运行时可见」回归（openspec/changes/
// knowledge-curation-console 7.1）：YAML 降级 seed 后重跑零新增、运营编辑
// 不被 seed 覆盖；Library 挂 store 后保存的条目立刻进入检索双路（确定性
// 拼装用上新条目），触发索引同步重建。Postgres 侧同一套 harness 见
// postgres_integration_test.go。

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeSeedDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	write("rule-offside.yaml", `id: rule-offside
topics: ["越位"]
answer: "传球一瞬间比对方最后一名防守球员更靠近球门线就算越位。"
source: "IFAB Law 11"
confidence: 0.95
effective_at: 2026-07-01
`)
	// 子目录模拟 players/ 组织层：WalkDir 递归覆盖。
	if err := os.MkdirAll(filepath.Join(dir, "players"), 0o755); err != nil {
		t.Fatalf("mkdir players: %v", err)
	}
	write(filepath.Join("players", "player-haaland.yaml"), `id: player-haaland
topics: ["哈兰德", "Haaland"]
answer: "哈兰德是曼城中锋，进球机器。"
source: "zh.wikipedia.org 埃尔林·哈兰德"
confidence: 0.95
effective_at: 2026-09-01
`)
	return dir
}

func TestSeedDirIdempotent(t *testing.T) {
	ctx := context.Background()
	dir := writeSeedDir(t)
	store := NewMemoryStore()

	seeded, err := SeedDir(ctx, store, dir)
	if err != nil || seeded != 2 {
		t.Fatalf("first seed = %d, %v; want 2 entries", seeded, err)
	}
	// 重跑零新增（幂等纪律）。
	reseeded, err := SeedDir(ctx, store, dir)
	if err != nil || reseeded != 0 {
		t.Fatalf("second seed = %d, %v; want 0 (idempotent)", reseeded, err)
	}
	records, err := store.List(ctx)
	if err != nil || len(records) != 2 {
		t.Fatalf("list = %d entries, %v; want 2", len(records), err)
	}
	for _, record := range records {
		if record.CreatedBy != "seed" {
			t.Fatalf("created_by = %q, want seed", record.CreatedBy)
		}
	}
}

func TestSeedDirPreservesOperatorEdits(t *testing.T) {
	ctx := context.Background()
	dir := writeSeedDir(t)
	store := NewMemoryStore()
	if _, err := SeedDir(ctx, store, dir); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// 运营改写越位答案（保存即生效路径）。
	library, err := NewStoreLibrary(ctx, store, nil)
	if err != nil {
		t.Fatalf("library: %v", err)
	}
	entry, ok := library.Search(ctx, "越位是什么")
	if !ok || entry.ID != "rule-offside" {
		t.Fatalf("pre-edit search hit = %+v ok=%v", entry, ok)
	}
	edited := entry
	edited.Answer = "运营台改写后的越位解释原文。"
	// 推进内存时钟：Windows 时钟粒度下同拍写入分不出先后。
	later := time.Now().UTC().Add(time.Second)
	store.mu.Lock()
	store.now = func() time.Time { return later }
	store.mu.Unlock()
	if _, err := store.Put(ctx, edited, "阿琴"); err != nil {
		t.Fatalf("operator put: %v", err)
	}
	// 再 seed：编辑不被 YAML 覆盖，也不产生新行。
	if reseeded, err := SeedDir(ctx, store, dir); err != nil || reseeded != 0 {
		t.Fatalf("reseed after edit = %d, %v; want 0", reseeded, err)
	}
	got, err := store.Get(ctx, "rule-offside")
	if err != nil {
		t.Fatalf("get after reseed: %v", err)
	}
	if got.Answer != "运营台改写后的越位解释原文。" {
		t.Fatalf("record = %+v, want operator edit preserved", got)
	}
	// created_by 保留首建者（seed）；编辑者归属走 operator_audit（写路径纪律），
	// 行内只留 created_at/updated_at 时间线。
	if got.CreatedBy != "seed" {
		t.Fatalf("created_by = %q, want first-creator preserved", got.CreatedBy)
	}
	if !got.UpdatedAt.After(got.CreatedAt) {
		t.Fatalf("updated_at = %v, want advanced past created_at %v", got.UpdatedAt, got.CreatedAt)
	}
}

func TestStoreLibraryEditVisibleToSearchAndTriggers(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	if _, err := store.Put(ctx, Entry{
		ID: "rule-corner", Topics: []string{"角球"}, Answer: "角球是攻方从角旗区开的定位球。",
		Source: "IFAB Law 17", Confidence: 0.9,
		EffectiveAt: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
	}, "seed"); err != nil {
		t.Fatalf("seed corner: %v", err)
	}
	library, err := NewStoreLibrary(ctx, store, nil)
	if err != nil {
		t.Fatalf("library: %v", err)
	}
	// 编辑前：新条目不可见。
	if _, ok := library.Search(ctx, "什么叫越位啊"); ok {
		t.Fatal("entry must not be visible before put")
	}
	// 经 Library.Put（保存即生效收口）：无需手工 Reload，检索立即命中新条目。
	if _, err := library.Put(ctx, Entry{
		ID: "rule-offside", Topics: []string{"越位", "offside"},
		Answer: "传球一瞬间比对方最后一名防守球员更靠近球门线就算越位。",
		Source: "IFAB Law 11", Confidence: 0.95,
		EffectiveAt: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
	}, "阿琴"); err != nil {
		t.Fatalf("library put: %v", err)
	}
	entry, ok := library.Search(ctx, "给我讲讲越位")
	if !ok || entry.ID != "rule-offside" {
		t.Fatalf("post-edit search = %+v ok=%v, want rule-offside", entry, ok)
	}
	if entry.Answer != "传球一瞬间比对方最后一名防守球员更靠近球门线就算越位。" {
		t.Fatalf("answer = %q, want curated verbatim anchor", entry.Answer)
	}
	if library.Size() != 2 {
		t.Fatalf("size = %d, want 2", library.Size())
	}
	// 触发索引随换血重建：带 triggers 的新条目可被 TriggerLookup 命中，
	// 且 quote 织写锚原样保留。
	if _, err := library.Put(ctx, Entry{
		ID: "judgment-var", Topics: []string{"var", "视频助理裁判"},
		Answer: "VAR 只介入进球、点球、直接红牌和认错球员四类情况。",
		Source: "IFAB VAR Protocol", Confidence: 0.9,
		EffectiveAt: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		Triggers:    []string{"var_check"}, Quote: "VAR 只介入进球、点球、直接红牌和认错球员四类情况",
	}, "阿琴"); err != nil {
		t.Fatalf("library put var: %v", err)
	}
	entry, ok = library.TriggerLookup("var_check")
	if !ok || entry.ID != "judgment-var" || entry.Quote == "" {
		t.Fatalf("trigger lookup = %+v ok=%v, want judgment-var with quote", entry, ok)
	}
	// 低确信条目照旧被 guard 挡在答案外（MinConfidence 语义不变）。
	if _, err := library.Put(ctx, Entry{
		ID: "low-conf", Topics: []string{"玄学"}, Answer: "说不清。",
		Source: "道听途说", Confidence: 0.3,
		EffectiveAt: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
	}, "seed"); err != nil {
		t.Fatalf("library put low: %v", err)
	}
	if _, ok := library.Search(ctx, "玄学"); ok {
		t.Fatal("low-confidence entry must stay guarded after edit")
	}
}

func TestReloadRequiresStore(t *testing.T) {
	library, err := Load(writeSeedDir(t), nil)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if library.Store() != nil {
		t.Fatal("file-loaded library must carry no store")
	}
	if err := library.Reload(context.Background()); err == nil {
		t.Fatal("reload on file-loaded library must fail (no store)")
	}
}
