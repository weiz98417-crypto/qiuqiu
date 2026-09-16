package memory

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestFakeRecallOrdersByImportanceThenRecency(t *testing.T) {
	fake := NewFake()
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	moments := []Moment{
		{UserID: "user-1", Kind: MomentUserFact, Content: "我喜欢皇马", Importance: 0.8, OccurredAt: base},
		{UserID: "user-1", Kind: MomentEmotionalExchange, Content: "绝杀那场我破防了", Importance: 0.65, OccurredAt: base.Add(time.Hour)},
		{UserID: "user-1", Kind: MomentPromise, Content: "待会儿告诉你为什么", Importance: 0.9, OccurredAt: base.Add(2 * time.Hour)},
		{UserID: "user-2", Kind: MomentUserFact, Content: "我喜欢米兰", Importance: 0.8, OccurredAt: base},
	}
	for _, moment := range moments {
		if err := fake.Observe(context.Background(), moment); err != nil {
			t.Fatalf("Observe: %v", err)
		}
	}
	recalls := fake.Recall(context.Background(), Query{UserID: "user-1", Focus: "", Limit: 2})
	if len(recalls) != 2 {
		t.Fatalf("recalls = %+v, want top-2 for user-1 only", recalls)
	}
	if recalls[0].Content != "待会儿告诉你为什么" || recalls[1].Content != "我喜欢皇马" {
		t.Fatalf("recall order = %q, %q; want importance ranking", recalls[0].Content, recalls[1].Content)
	}
	for _, recall := range recalls {
		if !strings.HasPrefix(recall.Source, "fake://moment/user-1/") {
			t.Fatalf("recall source = %q, want fake:// provenance", recall.Source)
		}
	}
}

func TestRenderRecallBlockIsBoundedAndCitesProvenance(t *testing.T) {
	recalls := make([]Recall, 0, 40)
	for index := 0; index < 40; index++ {
		recalls = append(recalls, Recall{
			Content:    strings.Repeat("用户喜欢聊中场战术", 10),
			Importance: 0.8,
			OccurredAt: time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC),
			Source:     "memobase://profile/interaction_patterns/recurring_topics",
		})
	}
	block := RenderRecallBlock(recalls)
	if block == "" {
		t.Fatal("recall block should render for non-empty recalls")
	}
	if !strings.Contains(block, "长期记忆") {
		runes := []rune(block)
		t.Fatalf("recall block missing the discipline header: %q", string(runes[:min(60, len(runes))]))
	}
	if len([]rune(block)) > maxRecallBlockRunes {
		t.Fatalf("recall block is %d runes, bounded at %d", len([]rune(block)), maxRecallBlockRunes)
	}
	if !strings.Contains(block, "（memobase://profile/interaction_patterns/recurring_topics）") {
		t.Fatal("every recalled line must cite its provenance")
	}
	if RenderRecallBlock(nil) != "" {
		t.Fatal("empty recall must render an empty block so read_recent stays the fallback")
	}
}

func TestFakePortraitSynthesizesFromFactsAndOverrideWins(t *testing.T) {
	fake := NewFake()
	base := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	if err := fake.Observe(context.Background(), Moment{UserID: "user-1", Kind: MomentUserFact, Content: "我喜欢皇马", Importance: 0.8, OccurredAt: base}); err != nil {
		t.Fatalf("Observe: %v", err)
	}
	portrait, err := fake.Portrait(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("Portrait: %v", err)
	}
	if !strings.Contains(portrait.Block, "我喜欢皇马") {
		t.Fatalf("synthesized portrait = %q, want the observed fact", portrait.Block)
	}
	if !portrait.UpdatedAt.Equal(base) {
		t.Fatalf("portrait timestamp = %v, want the latest moment time %v", portrait.UpdatedAt, base)
	}
	override := Portrait{Block: "【用户画像】手动维护", UpdatedAt: base.Add(time.Hour)}
	fake.SetPortrait("user-1", override)
	portrait, err = fake.Portrait(context.Background(), "user-1")
	if err != nil || portrait.Block != override.Block {
		t.Fatalf("portrait after override = %+v err=%v, want the stored override", portrait, err)
	}
}

func TestFakeThreadsDerivePromisesUntilC2StoreLands(t *testing.T) {
	fake := NewFake()
	base := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	if err := fake.Observe(context.Background(), Moment{UserID: "user-1", Kind: MomentPromise, Content: "待会儿告诉你", Importance: 0.9, OccurredAt: base}); err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if err := fake.Observe(context.Background(), Moment{UserID: "user-1", Kind: MomentUserFact, Content: "我喜欢皇马", Importance: 0.8, OccurredAt: base}); err != nil {
		t.Fatalf("Observe: %v", err)
	}
	threads, err := fake.Threads(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("Threads: %v", err)
	}
	if len(threads) != 1 || threads[0].Kind != ThreadPromise || threads[0].State != "open" {
		t.Fatalf("threads = %+v, want the single open promise", threads)
	}
	if fake.Moments() == nil || len(fake.Moments()) != 2 {
		t.Fatalf("Moments accessor = %+v, want both observations", fake.Moments())
	}
}

func TestRenderPortraitBlockIsBoundedAndLabelled(t *testing.T) {
	entries := make([]profileEntry, 0, 30)
	for index := 0; index < 30; index++ {
		var entry profileEntry
		entry.Content = strings.Repeat("支持皇马二十年", 12)
		entry.Attributes.Topic = "basic_info"
		entry.Attributes.SubTopic = "favorite_team"
		entries = append(entries, entry)
	}
	block := RenderPortraitBlock(entries)
	if block == "" {
		t.Fatal("portrait block should render for non-empty profiles")
	}
	if !strings.Contains(block, "不得据此新增赛况事实") {
		t.Fatal("portrait block must restate the Match Fact discipline")
	}
	if len([]rune(block)) > maxPortraitRunes {
		t.Fatalf("portrait block is %d runes, bounded at %d", len([]rune(block)), maxPortraitRunes)
	}
	if !strings.Contains(block, "基本信息/favorite_team") {
		t.Fatalf("portrait block = %q, want topic labelling", block)
	}
	if RenderPortraitBlock(nil) != "" {
		t.Fatal("empty profile must render an empty block")
	}
}
