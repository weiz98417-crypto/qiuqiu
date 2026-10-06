package knowledge

// Store 双实现一致性（ADR-0006 惯例：每个本地存储至少两套实现，同一把
// 行为测试两边都跑）：内存实现全量跑，Postgres 实现在 postgres_integration_test.go
// 里以同一 harness 回归（无 DATABASE_URL 跳过）。锁的是策展面语义契约：
// Put 幂等 upsert、首建留痕、校验口径、列表确定性、生效窗口与转会窗复查。

import (
	"context"
	"testing"
	"time"
)

func testEntry(id, keyword, answer string, effectiveAt time.Time) Entry {
	return Entry{
		ID:          id,
		Topics:      []string{keyword, " " + keyword + "变体 "},
		Answer:      answer,
		Source:      "IFAB Laws of the Game",
		Confidence:  0.9,
		EffectiveAt: effectiveAt,
	}
}

// exerciseStoreParity 是两套 Store 实现共用的行为契约。
func exerciseStoreParity(t *testing.T, store Store) {
	t.Helper()
	ctx := context.Background()
	effective := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	// Put 新建：归一化 topics（去空白项），created_by 记操作者。
	record, err := store.Put(ctx, testEntry("rule-a", "越位", "越位答案原文。", effective), "阿琴")
	if err != nil {
		t.Fatalf("put new: %v", err)
	}
	if record.ID != "rule-a" || record.CreatedBy != "阿琴" {
		t.Fatalf("record = %+v, want id rule-a created by 阿琴", record)
	}
	if len(record.Topics) != 2 || record.Topics[1] != "越位变体" {
		t.Fatalf("topics = %v, want whitespace-normalized pair", record.Topics)
	}

	// Get 落空 → ErrNotFound。
	if _, err := store.Get(ctx, "missing"); err != ErrNotFound {
		t.Fatalf("get missing = %v, want ErrNotFound", err)
	}

	// Get 回读：字段逐项保真（answer 原文即锚，confidence/effective_at 不走样）。
	got, err := store.Get(ctx, "rule-a")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Answer != "越位答案原文。" || got.Source != "IFAB Laws of the Game" || got.Confidence != 0.9 {
		t.Fatalf("record = %+v, want field-faithful roundtrip", got)
	}
	if !got.EffectiveAt.Equal(effective) {
		t.Fatalf("effective_at = %v, want %v", got.EffectiveAt, effective)
	}

	// Put 已有：内容更新、首建留痕（created_by/created_at 不变）、updated_at 前进。
	sleep := time.Now().UTC().Add(time.Second)
	if memorized, ok := store.(*MemoryStore); ok {
		memorized.mu.Lock()
		memorized.now = func() time.Time { return sleep }
		memorized.mu.Unlock()
	}
	edited := testEntry("rule-a", "越位", "复查后的新答案原文。", effective)
	edited.Confidence = 0.95
	// 后处理参数学五字段（knowledge-worldinfo）随 Put 整条换新，回环保真。
	edited.Priority = 3
	edited.InclusionGroup = " 规则组 "
	edited.StickyTurns = 2
	edited.CooldownTurns = 4
	edited.Probability = 0.5
	updated, err := store.Put(ctx, edited, "小阅")
	if err != nil {
		t.Fatalf("put existing: %v", err)
	}
	if updated.Answer != "复查后的新答案原文。" || updated.Confidence != 0.95 {
		t.Fatalf("updated = %+v, want edited content", updated)
	}
	if updated.Priority != 3 || updated.InclusionGroup != "规则组" || updated.StickyTurns != 2 ||
		updated.CooldownTurns != 4 || updated.Probability != 0.5 {
		t.Fatalf("worldinfo params = %+v, want field-faithful roundtrip (group trimmed)", updated)
	}
	if updated.CreatedBy != "阿琴" {
		t.Fatalf("created_by = %q, want first-creator preserved on update", updated.CreatedBy)
	}
	if !updated.UpdatedAt.After(record.UpdatedAt) {
		t.Fatalf("updated_at = %v, want advanced past %v", updated.UpdatedAt, record.UpdatedAt)
	}

	// List：确定性 id 序。
	if _, err := store.Put(ctx, testEntry("rule-b", "角球", "角球答案原文。", effective), "seed"); err != nil {
		t.Fatalf("put rule-b: %v", err)
	}
	records, err := store.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(records) != 2 || records[0].ID != "rule-a" || records[1].ID != "rule-b" {
		t.Fatalf("list = %+v, want id-sorted [rule-a rule-b]", records)
	}

	// 校验口径：带 triggers 必带 quote、confidence 越界、必填缺失、
	// 参数学负值/越界（knowledge-worldinfo）。
	invalid := []Entry{
		{ID: "bad-triggers", Topics: []string{"var"}, Answer: "答案", Confidence: 0.9,
			EffectiveAt: effective, Triggers: []string{"var_check"}},
		{ID: "bad-confidence", Topics: []string{"玄学"}, Answer: "答案", Confidence: 1.5, EffectiveAt: effective},
		{ID: "bad-topics", Answer: "答案", Confidence: 0.9, EffectiveAt: effective},
		{ID: "", Topics: []string{"越位"}, Answer: "答案", Confidence: 0.9, EffectiveAt: effective},
		{ID: "bad-effective", Topics: []string{"越位"}, Answer: "答案", Confidence: 0.9},
		{ID: "bad-sticky", Topics: []string{"越位"}, Answer: "答案", Confidence: 0.9, EffectiveAt: effective, StickyTurns: -1},
		{ID: "bad-cooldown", Topics: []string{"越位"}, Answer: "答案", Confidence: 0.9, EffectiveAt: effective, CooldownTurns: -2},
		{ID: "bad-probability", Topics: []string{"越位"}, Answer: "答案", Confidence: 0.9, EffectiveAt: effective, Probability: 1.5},
	}
	for _, entry := range invalid {
		if _, err := store.Put(ctx, entry, "阿琴"); err == nil {
			t.Fatalf("put %+v = nil error, want validation failure", entry)
		}
		if _, err := store.PutIfAbsent(ctx, entry, "阿琴"); err == nil {
			t.Fatalf("put-if-absent %+v = nil error, want validation failure", entry)
		}
	}

	// PutIfAbsent：存在即不动（false、内容不被触碰），缺位才插入（true，
	// 留痕同 Put）——seed 幂等的原子面。
	inserted, err := store.PutIfAbsent(ctx, testEntry("rule-a", "越位", "不该落地的答案。", effective), "seed")
	if err != nil {
		t.Fatalf("put-if-absent existing: %v", err)
	}
	if inserted {
		t.Fatal("put-if-absent on existing id must report false")
	}
	if got, err := store.Get(ctx, "rule-a"); err != nil || got.Answer != "复查后的新答案原文。" {
		t.Fatalf("existing entry touched by put-if-absent: answer = %q err = %v", got.Answer, err)
	}
	inserted, err = store.PutIfAbsent(ctx, testEntry("rule-new", "定位球", "定位球答案原文。", effective), "seed")
	if err != nil || !inserted {
		t.Fatalf("put-if-absent missing id = %v, %v; want inserted", inserted, err)
	}
	if got, err := store.Get(ctx, "rule-new"); err != nil || got.CreatedBy != "seed" {
		t.Fatalf("put-if-absent record = %+v err = %v; want created_by seed", got, err)
	}
}

func TestMemoryStoreParity(t *testing.T) {
	exerciseStoreParity(t, NewMemoryStore())
}

// nil 库安全：companion 未挂知识库时（WithKnowledge 缺席），检索与触发查找
// 必须照旧降级为「无命中」而不是 panic。
func TestNilLibraryDegradesSilently(t *testing.T) {
	var library *Library
	if _, ok := library.Search(context.Background(), "越位"); ok {
		t.Fatal("nil library search must miss")
	}
	if _, ok := library.TriggerLookup("var_check"); ok {
		t.Fatal("nil library trigger lookup must miss")
	}
	if library.Size() != 0 || library.Store() != nil {
		t.Fatal("nil library must report empty size and no store")
	}
	if err := library.Reload(context.Background()); err == nil {
		t.Fatal("nil library reload must fail explicitly")
	}
}

func TestLastTransferWindowClose(t *testing.T) {
	cases := []struct {
		now  time.Time
		want time.Time
	}{
		{time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC), time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)},
		{time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)},
		{time.Date(2026, 6, 30, 23, 59, 0, 0, time.UTC), time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
		{time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC), time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
		{time.Date(2025, 12, 31, 8, 0, 0, 0, time.UTC), time.Date(2025, 7, 1, 0, 0, 0, 0, time.UTC)},
	}
	for _, tc := range cases {
		if got := LastTransferWindowClose(tc.now); !got.Equal(tc.want) {
			t.Fatalf("LastTransferWindowClose(%s) = %s, want %s", tc.now, got, tc.want)
		}
	}
}

func TestDueReviewAndEntryStatus(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	// 生效窗口早于最近窗闭（2026-07-01）→ 待复查；窗后生效 → 不用复查。
	if !DueReview(now, time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC)) {
		t.Fatal("entry effective before last window close must be due for review")
	}
	if DueReview(now, time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatal("entry effective at window close is freshly reviewed")
	}
	if DueReview(now, time.Time{}) {
		t.Fatal("zero effective_at must not be flagged due")
	}
	// 生效二态：active / pending。
	if got := EntryStatus(now, now.Add(-time.Second)); got != "active" {
		t.Fatalf("past entry status = %q, want active", got)
	}
	if got := EntryStatus(now, now.Add(time.Second)); got != "pending" {
		t.Fatalf("future entry status = %q, want pending", got)
	}
}

// TestWorldInfoParamsNormalize 锁参数学五字段的归一口径（knowledge-worldinfo）：
// probability ≤0 视同未策展归一为 1（存量 YAML 零值=现状行为），>1 拒绝；
// 策展写入（preparePut）与 YAML 直读（parseEntryFile）两条入口同口径。
func TestWorldInfoParamsNormalize(t *testing.T) {
	effective := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	entry, err := preparePut(Entry{ID: "rule-p0", Topics: []string{"越位"}, Answer: "答案。",
		Confidence: 0.9, EffectiveAt: effective, InclusionGroup: " 组 ", Priority: -7})
	if err != nil {
		t.Fatalf("preparePut: %v", err)
	}
	if entry.Probability != 1 {
		t.Fatalf("probability = %v, want normalized 1 (zero means uncurated = always)", entry.Probability)
	}
	if entry.InclusionGroup != "组" {
		t.Fatalf("inclusion_group = %q, want trimmed", entry.InclusionGroup)
	}
	if entry.Priority != -7 {
		t.Fatalf("priority = %d, want passthrough (any int legal)", entry.Priority)
	}

	raw := []byte("id: rule-yaml\ntopics: [\"越位\"]\nanswer: \"答案。\"\nconfidence: 0.9\neffective_at: 2026-07-01\n")
	parsed, err := parseEntryFile("rule-yaml.yaml", raw)
	if err != nil {
		t.Fatalf("parseEntryFile: %v", err)
	}
	if parsed.Probability != 1 {
		t.Fatalf("yaml probability = %v, want normalized 1 (defaults = current behavior)", parsed.Probability)
	}
}
