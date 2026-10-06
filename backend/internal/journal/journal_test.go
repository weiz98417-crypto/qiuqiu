package journal

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// stubGenerator 是织写的测试替身（返回预置正文或错误）。
type stubGenerator struct {
	body string
	err  error
}

func (g *stubGenerator) GenerateJournal(ctx context.Context, facts MatchFacts, portrait []PortraitLine) (string, error) {
	return g.body, g.err
}

func testFacts() MatchFacts {
	return MatchFacts{
		MatchID:  "m1",
		HomeTeam: "西班牙",
		AwayTeam: "德国",
		Score:    "1-0",
		Goals:    []string{"佩德里 25:00"},
		Season:   "2026",
	}
}

// 10.1 零编造：LLM 织写的比分与账本不符 → 降级确定性底稿；一致 → 保留。
// 无 generator / generator 失败 → 底稿。手记面永远有内容且零编造。
func TestGenerateNeverFabricatesScore(t *testing.T) {
	ctx := context.Background()

	t.Run("llm_wrong_score_degrades_to_draft", func(t *testing.T) {
		store := &MemoryStore{}
		entry, err := Generate(ctx, store, &stubGenerator{body: "看完这场西班牙对德国，2-0！佩德里太神了。"}, "user-1", testFacts(), nil)
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		if strings.Contains(entry.Body, "2-0") {
			t.Fatalf("fabricated score leaked: %q", entry.Body)
		}
		if !strings.Contains(entry.Body, "1-0") {
			t.Fatalf("draft must carry the ledger score: %q", entry.Body)
		}
	})

	t.Run("llm_correct_score_kept", func(t *testing.T) {
		store := &MemoryStore{}
		entry, err := Generate(ctx, store, &stubGenerator{body: "看完这场西班牙对德国，1-0，佩德里那下真漂亮。"}, "user-1", testFacts(), nil)
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		if !strings.Contains(entry.Body, "佩德里那下真漂亮") {
			t.Fatalf("woven body lost: %q", entry.Body)
		}
	})

	t.Run("no_generator_falls_to_draft", func(t *testing.T) {
		store := &MemoryStore{}
		entry, err := Generate(ctx, store, nil, "user-1", testFacts(), nil)
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		if !strings.Contains(entry.Body, "1-0") || !strings.Contains(entry.Body, "佩德里 25:00") {
			t.Fatalf("draft body = %q", entry.Body)
		}
	})

	t.Run("generator_error_falls_to_draft", func(t *testing.T) {
		store := &MemoryStore{}
		entry, err := Generate(ctx, store, &stubGenerator{err: errors.New("sidecar down")}, "user-1", testFacts(), nil)
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		if !strings.Contains(entry.Body, "1-0") {
			t.Fatalf("draft body = %q", entry.Body)
		}
	})
}

// 溯源：每篇手记的 sources 引账本与画像条目（抽查断言）。
func TestGenerateCitesSources(t *testing.T) {
	store := &MemoryStore{}
	entry, err := Generate(context.Background(), store, &stubGenerator{body: "看完这场西班牙对德国，1-0。"}, "user-1", testFacts(), []PortraitLine{{SubTopic: "favorite_team", Content: "皇马"}})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	joined := strings.Join(entry.Sources, ",")
	if !strings.Contains(joined, "match:m1") || !strings.Contains(joined, "goal:佩德里 25:00") || !strings.Contains(joined, "portrait:favorite_team") {
		t.Fatalf("sources = %v, want ledger + goal + portrait citations", entry.Sources)
	}
}

// 幂等：同 user+match 重复 beat 更新正文，不堆叠。
func TestGenerateIsIdempotentPerMatch(t *testing.T) {
	store := &MemoryStore{}
	ctx := context.Background()
	if _, err := Generate(ctx, store, nil, "user-1", testFacts(), nil); err != nil {
		t.Fatalf("Generate 1: %v", err)
	}
	second, err := Generate(ctx, store, &stubGenerator{body: "看完这场西班牙对德国，1-0，重温还是感动。"}, "user-1", testFacts(), nil)
	if err != nil {
		t.Fatalf("Generate 2: %v", err)
	}
	entries, listErr := store.List(ctx, "user-1", 10)
	if listErr != nil {
		t.Fatalf("List: %v", listErr)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1 (same match updates in place)", len(entries))
	}
	if entries[0].Body != second.Body {
		t.Fatal("second generate must replace the body")
	}
}

// 10.3 装订：赛季册每场一条（比分+手记摘要+共同瞬间），season 过滤，
// 摘要=首句截断。
func TestBuildAlbumBindsSeasonPages(t *testing.T) {
	store := &MemoryStore{}
	ctx := context.Background()
	facts := testFacts()
	if _, err := Generate(ctx, store, nil, "user-1", facts, nil); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	other := facts
	other.MatchID = "m2"
	other.Season = "2025"
	other.Score = "3-2"
	if _, err := Generate(ctx, store, nil, "user-1", other, nil); err != nil {
		t.Fatalf("Generate other: %v", err)
	}
	moments := &stubMoments{byMatch: map[string][]string{"m1": {"佩德里的进球瞬间"}}}

	album, err := BuildAlbum(ctx, store, moments, "user-1", "2026")
	if err != nil {
		t.Fatalf("BuildAlbum: %v", err)
	}
	if len(album) != 1 {
		t.Fatalf("2026 album pages = %d, want 1", len(album))
	}
	page := album[0]
	if page.MatchID != "m1" || page.Score != "1-0" {
		t.Fatalf("page = %+v", page)
	}
	if len(page.Moments) != 1 || page.Moments[0] != "佩德里的进球瞬间" {
		t.Fatalf("moments = %v", page.Moments)
	}
	if !strings.Contains(page.Journal, "1-0") {
		t.Fatalf("journal summary = %q", page.Journal)
	}

	all, err := BuildAlbum(ctx, store, moments, "user-1", "")
	if err != nil {
		t.Fatalf("BuildAlbum all: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("full album pages = %d, want 2", len(all))
	}
}

type stubMoments struct {
	byMatch map[string][]string
}

func (s *stubMoments) MomentsForMatch(ctx context.Context, userID, matchID string) []string {
	return s.byMatch[matchID]
}
