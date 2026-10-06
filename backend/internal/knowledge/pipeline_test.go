package knowledge

// 检索后处理管道 evals（openspec/changes/knowledge-worldinfo Success
// Criteria）：四机制各一族——预算裁剪次序 / 互斥消歧 / sticky-cooldown
// 状态机 / 概率种子可复现——外加默认值下与 Search 逐字节一致的等价锁、
// 每用户状态隔离锁。全确定性：不依赖时钟与随机源，同输入恒同果。

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// writeEntryFile 落一个 YAML 条目文件（测试目录装配用）。
func writeEntryFile(dir, name, body string) error {
	return os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644)
}

// candidate 造生产形状的候选：过 normalizeWorldInfoParams（p≤0→1 等）——
// Select 的输入契约就是「条目已归一」（策展写入与 YAML 直读两条入口保证）。
func candidate(id, answer string, mutate ...func(*Entry)) Scored {
	entry := Entry{ID: id, Answer: answer, Confidence: 0.9}
	for _, m := range mutate {
		if m != nil {
			m(&entry)
		}
	}
	entry, err := normalizeWorldInfoParams(entry)
	if err != nil {
		panic(err)
	}
	return Scored{Entry: entry, Score: 1}
}

func ids(scores []Scored) []string {
	out := make([]string, 0, len(scores))
	for _, item := range scores {
		out = append(out, item.Entry.ID)
	}
	return out
}

func equalIDs(a []string, b ...string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestSelectBudgetClipsByPriorityOrder 预算裁剪次序：priority 决定谁先占
// 预算，预算耗尽即停（不是跳过续装）；同优先级保持候选序（得分降序+声明序）。
func TestSelectBudgetClipsByPriorityOrder(t *testing.T) {
	candidates := []Scored{
		candidate("low", "一二三四五六七八九十", nil),
		candidate("mid", "一二三四五六七八九十", func(e *Entry) { e.Priority = 5 }),
		candidate("top", "一二三四五六七八九十", func(e *Entry) { e.Priority = 9 }),
	}
	// 答案 10 个汉字 = 30 字节（len 按字节）：预算 65 → top+mid 装下（60），
	// low 溢出即停。
	kept := Select(candidates, NewLifecycle(), SelectOptions{BudgetBytes: 65})
	if !equalIDs(ids(kept), "top", "mid") {
		t.Fatalf("budget survivors = %v, want [top mid] (priority order, exhausted stop)", ids(kept))
	}
	// 全默认（priority 同 0）：候选序前两名存活。
	plain := []Scored{candidate("a", "第一"), candidate("b", "第二"), candidate("c", "第三")}
	kept = Select(plain, NewLifecycle(), SelectOptions{BudgetBytes: 12})
	if !equalIDs(ids(kept), "a", "b") {
		t.Fatalf("default priority survivors = %v, want [a b] (candidate order)", ids(kept))
	}
}

// TestSelectBudgetStickyExempt sticky 活跃条目豁免预算裁剪（连续话题保位）：
// 高优先级条目装满预算后，sticky 活跃者不因溢出被停掉。
func TestSelectBudgetStickyExempt(t *testing.T) {
	lc := NewLifecycle()
	lc.Turn = 2 // sticky 命中@1，窗口（1, 1+3] 覆盖 T2-T4
	lc.Fired = map[string]int{"sticky": 1}
	candidates := []Scored{
		candidate("fat", "三个字", func(e *Entry) { e.Priority = 9 }),
		candidate("sticky", "短", func(e *Entry) { e.StickyTurns = 3 }),
	}
	// fat 9 字节装满预算 9；sticky 3 字节本会溢出被停，豁免保位——
	// 且 sticky 活跃者按语义提到队首。
	kept := Select(candidates, lc, SelectOptions{BudgetBytes: 9})
	if !equalIDs(ids(kept), "sticky", "fat") {
		t.Fatalf("survivors = %v, want sticky exempt from budget clip and promoted", ids(kept))
	}
	// 停机点之后的 sticky 活跃者同样豁免（豁免与停机点位置无关）。
	lc2 := NewLifecycle()
	lc2.Turn = 2
	lc2.Fired = map[string]int{"late": 1}
	spread := []Scored{
		candidate("hog", "九个字九个字九个字", func(e *Entry) { e.Priority = 9 }),
		candidate("mid", "三个字", func(e *Entry) { e.Priority = 5 }),
		candidate("late", "短", func(e *Entry) { e.StickyTurns = 3 }),
	}
	// hog 27 字节装下预算 30，mid 9 字节溢出即停（mid 位处停机点出局），
	// late 豁免存活——豁免与停机点位置无关。
	kept = Select(spread, lc2, SelectOptions{BudgetBytes: 30})
	if !equalIDs(ids(kept), "late", "hog") {
		t.Fatalf("survivors = %v, want sticky exempt even past the stop point", ids(kept))
	}
}

// TestSelectInclusionGroupKeepsHighest 互斥消歧：非空组内按当前序只留
// 第一个——当前序在预算段后已是优先级序，组内「取最高」= 优先级高者胜；
// 无组条目不受牵连。
func TestSelectInclusionGroupKeepsHighest(t *testing.T) {
	candidates := []Scored{
		candidate("g-first", "一", func(e *Entry) { e.InclusionGroup = "规则组" }),
		candidate("g-second", "二", func(e *Entry) { e.InclusionGroup = "规则组"; e.Priority = 5 }),
		candidate("solo", "三"),
	}
	kept := Select(candidates, NewLifecycle(), SelectOptions{})
	if !equalIDs(ids(kept), "g-second", "solo") {
		t.Fatalf("survivors = %v, want [g-second solo] (higher priority wins the group)", ids(kept))
	}
	// 同优先级：候选序（得分降序+声明序）在先者胜。
	tied := []Scored{
		candidate("t-second", "甲", func(e *Entry) { e.InclusionGroup = "组" }),
		candidate("t-first", "乙", func(e *Entry) { e.InclusionGroup = "组" }),
	}
	kept = Select(tied, NewLifecycle(), SelectOptions{})
	if !equalIDs(ids(kept), "t-second") {
		t.Fatalf("tied survivors = %v, want [t-second] (candidate order breaks the tie)", ids(kept))
	}
}

// TestSelectCooldownSuppressesThenRecovers 冷却状态机：命中后冷却 N 轮内
// 剔除（次优顶上），窗口过后恢复。
func TestSelectCooldownSuppressesThenRecovers(t *testing.T) {
	lc := NewLifecycle()
	candidates := func() []Scored {
		return []Scored{
			candidate("rule", "规则答案。", func(e *Entry) { e.CooldownTurns = 2 }),
			candidate("fallback", "备选答案。"),
		}
	}
	lc.Advance() // T1
	kept := Select(candidates(), lc, SelectOptions{})
	if !equalIDs(ids(kept), "rule", "fallback") {
		t.Fatalf("T1 survivors = %v, want [rule fallback]", ids(kept))
	}
	for turn := 2; turn <= 3; turn++ {
		lc.Advance()
		kept = Select(candidates(), lc, SelectOptions{})
		if !equalIDs(ids(kept), "fallback") {
			t.Fatalf("T%d survivors = %v, want [fallback] (cooldown %d)", turn, ids(kept), turn-1)
		}
	}
	lc.Advance() // T4：turn-last=3 > 2，冷却解除
	kept = Select(candidates(), lc, SelectOptions{})
	if !equalIDs(ids(kept), "rule", "fallback") {
		t.Fatalf("T4 survivors = %v, want cooldown expired", ids(kept))
	}
}

// TestSelectStickyPromotesWithinWindow sticky 状态机：命中后 N 轮内存活即
// 提到队首（连续话题优先）且免概率掷骰，窗口过后恢复候选序、掷骰重新生效。
func TestSelectStickyPromotesWithinWindow(t *testing.T) {
	// 找确定性种子：T1 骰中（首命中建立 sticky 状态），T2-T5 骰皆落空——
	// T2-T4 sticky 存活即证豁免；T5 窗口过期后落空即证掷骰恢复。
	seed := uint64(0)
	for s := uint64(1); s < 100000; s++ {
		if probabilityRoll(s, 1, "sticky", 0.5) && !probabilityRoll(s, 2, "sticky", 0.5) &&
			!probabilityRoll(s, 3, "sticky", 0.5) && !probabilityRoll(s, 4, "sticky", 0.5) &&
			!probabilityRoll(s, 5, "sticky", 0.5) {
			seed = s
			break
		}
	}
	if seed == 0 {
		t.Fatal("no deterministic seed satisfies the scenario")
	}
	lc := NewLifecycle()
	newCandidates := func() []Scored {
		return []Scored{
			candidate("fresh", "新话题答案。"),
			candidate("sticky", "连续话题答案。", func(e *Entry) { e.StickyTurns = 3; e.Probability = 0.5 }),
		}
	}
	lc.Advance() // T1：sticky 骰中以候选序存活并登记
	kept := Select(newCandidates(), lc, SelectOptions{Seed: seed})
	if !equalIDs(ids(kept), "fresh", "sticky") {
		t.Fatalf("T1 survivors = %v, want candidate order", ids(kept))
	}
	for turn := 2; turn <= 4; turn++ {
		lc.Advance()
		kept = Select(newCandidates(), lc, SelectOptions{Seed: seed})
		if !equalIDs(ids(kept), "sticky", "fresh") {
			t.Fatalf("T%d survivors = %v, want sticky promoted (dice-exempt; roll would miss)", turn, ids(kept))
		}
	}
	lc.Advance() // T5：turn-last=4 > 3，窗口过期，掷骰恢复且该轮落空
	kept = Select(newCandidates(), lc, SelectOptions{Seed: seed})
	if !equalIDs(ids(kept), "fresh") {
		t.Fatalf("T5 survivors = %v, want sticky window expired (dice applies again)", ids(kept))
	}
}

// TestSelectProbabilitySeedReproducible 概率掷骰确定性：同种子同轮同条目
// 恒同果；边界 p≥1 恒真、p≤0 恒假。
func TestSelectProbabilitySeedReproducible(t *testing.T) {
	// p=1 恒真；p≤0 恒假（归一化在入口，此处只锁骰函数边界）。
	if !probabilityRoll(42, 7, "rule-x", 1) || probabilityRoll(42, 7, "rule-x", 0) {
		t.Fatal("probability bounds: p>=1 must always fire, p<=0 never")
	}
	// 同输入两次执行结果一致（两次独立 Lifecycle）。
	run := func(seed uint64) []string {
		candidates := []Scored{
			candidate("rule-a", "答案甲。", func(e *Entry) { e.Probability = 0.5 }),
			candidate("rule-b", "答案乙。", func(e *Entry) { e.Probability = 0.5 }),
		}
		lc := NewLifecycle()
		lc.Advance()
		return ids(Select(candidates, lc, SelectOptions{Seed: seed}))
	}
	for seed := uint64(1); seed <= 8; seed++ {
		first, second := run(seed), run(seed)
		if !equalIDs(first, second...) {
			t.Fatalf("seed %d: run1 = %v run2 = %v, want reproducible", seed, first, second)
		}
	}
	// 落点健康度：p=0.5 固定 64 个 (seed,turn,entry) 组合，命中数显著偏离
	// 32 才算坏（确定性计算，非随机摆）。
	hits := 0
	for seed := uint64(1); seed <= 16; seed++ {
		for turn := 1; turn <= 4; turn++ {
			if probabilityRoll(seed, turn, "rule-half", 0.5) {
				hits++
			}
		}
	}
	if hits < 16 || hits > 48 {
		t.Fatalf("p=0.5 hits = %d/64, want near half", hits)
	}
}

// TestSelectDefaultsMatchSearchByteForByte 等价锁（Success Criteria）：全
// 默认参数条目下，管道存活者首元素与 Search 冠军逐条目一致（含答案原文字
// 节），无命中两侧一致——管道是可选层。
func TestSelectDefaultsMatchSearchByteForByte(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"rule-offside.yaml":  "id: rule-offside\ntopics: [\"越位\", \"offside\"]\nanswer: \"越位看传球出脚瞬间。\"\nconfidence: 0.9\neffective_at: 2026-07-01\n",
		"rule-corner.yaml":   "id: rule-corner\ntopics: [\"角球\"]\nanswer: \"角球直接进球有效。\"\nconfidence: 0.9\neffective_at: 2026-07-01\n",
		"rule-sub.yaml":      "id: rule-sub\ntopics: [\"换人\", \"名额\"]\nanswer: \"每队最多五个换人名额。\"\nconfidence: 0.85\neffective_at: 2026-07-01\n",
		"rule-stoppage.yaml": "id: rule-stoppage\ntopics: [\"补时\"]\nanswer: \"补时由裁判酌情给出。\"\nconfidence: 0.8\neffective_at: 2026-07-01\n",
	}
	writeAll := func(dir string) error {
		for name, body := range files {
			if err := writeEntryFile(dir, name, body); err != nil {
				return err
			}
		}
		return nil
	}
	if err := writeAll(dir); err != nil {
		t.Fatalf("seed yaml: %v", err)
	}
	library, err := Load(dir, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	queries := []string{"越位是什么", "角球直接进球算吗", "换人名额还有几个", "补时给几分钟", "完全不相关的提问xyz", "越位 角球", ""}
	for _, query := range queries {
		winner, ok := library.Search(context.Background(), query)
		candidates := library.SearchTopN(context.Background(), query, 8)
		kept := Select(candidates, NewLifecycle(), SelectOptions{})
		if !ok {
			if len(kept) != 0 {
				t.Fatalf("query %q: search miss but pipeline returned %v", query, ids(kept))
			}
			continue
		}
		if len(kept) == 0 {
			t.Fatalf("query %q: search hit %s but pipeline empty", query, winner.ID)
		}
		top := kept[0].Entry
		if top.ID != winner.ID || top.Answer != winner.Answer || top.Confidence != winner.Confidence {
			t.Fatalf("query %q: pipeline first = %+v, search winner = %+v (byte parity broken)", query, top, winner)
		}
	}
}

// TestSelectPerUserIsolation 状态隔离锁：同一份候选喂两个用户各自的
// Lifecycle，互不串味——甲的命中不影响乙的队序。
func TestSelectPerUserIsolation(t *testing.T) {
	newCandidates := func() []Scored {
		return []Scored{
			candidate("rule", "规则答案。", func(e *Entry) { e.CooldownTurns = 2; e.StickyTurns = 3 }),
			candidate("fallback", "备选答案。"),
		}
	}
	alice := NewLifecycle()
	bob := NewLifecycle()
	alice.Advance()
	Select(newCandidates(), alice, SelectOptions{}) // 甲 T1 命中 rule
	alice.Advance()
	bob.Advance()
	aliceKept := Select(newCandidates(), alice, SelectOptions{})
	bobKept := Select(newCandidates(), bob, SelectOptions{})
	if !equalIDs(ids(aliceKept), "fallback") {
		t.Fatalf("alice = %v, want cooldown-suppressed", ids(aliceKept))
	}
	if !equalIDs(ids(bobKept), "rule", "fallback") {
		t.Fatalf("bob = %v, want untouched by alice (per-user state)", ids(bobKept))
	}
}

// TestSearchTopNOrdering SearchTopN 形状锁：得分降序、并列声明序、n 截断、
// 零分不入——Select 的候选源契约。
func TestSearchTopNOrdering(t *testing.T) {
	dir := t.TempDir()
	bodies := []string{
		"id: a-corner\ntopics: [\"角球\"]\nanswer: \"甲\"\nconfidence: 0.9\neffective_at: 2026-07-01\n",
		"id: b-corner\ntopics: [\"角球\", \"角球附加\"]\nanswer: \"乙\"\nconfidence: 0.9\neffective_at: 2026-07-01\n",
		"id: c-offside\ntopics: [\"越位\"]\nanswer: \"丙\"\nconfidence: 0.9\neffective_at: 2026-07-01\n",
	}
	for name, body := range map[string]string{"a.yaml": bodies[0], "b.yaml": bodies[1], "c.yaml": bodies[2]} {
		if err := writeEntryFile(dir, name, body); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	library, err := Load(dir, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	all := library.SearchTopN(context.Background(), "角球", 8)
	if len(all) != 2 {
		t.Fatalf("SearchTopN 角球 = %d candidates, want 2 (offside scores zero)", len(all))
	}
	// 多 topic 条目按设计被摊薄（双向 contains 归一：1/2 < 1/1）——
	// a-corner（单 topic 全中 1.0）压 b-corner（双 topic 半中 0.5）。
	if all[0].Entry.ID != "a-corner" || all[1].Entry.ID != "b-corner" {
		t.Fatalf("order = %v, want a-corner first (1/1 keyword hit beats 1/2 diluted)", ids(all))
	}
	one := library.SearchTopN(context.Background(), "角球", 1)
	if len(one) != 1 || one[0].Entry.ID != "a-corner" {
		t.Fatalf("SearchTopN n=1 = %v, want [a-corner]", ids(one))
	}
}
