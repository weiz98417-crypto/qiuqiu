package memory

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"qiuqiu/internal/privacy"
	"qiuqiu/internal/structured"
)

// scriptedDecider 是判定器的测试桩（pr tier 脚本 LLM 先例=scriptedRouter）：
// 原样回放夹具判定，落地校验仍由 maintainer 把关。
type scriptedDecider struct {
	decision OpDecision
	err      error
}

func (s scriptedDecider) DecidePortraitOp(context.Context, PortraitClaim, []PortraitOverlay) (OpDecision, error) {
	return s.decision, s.err
}

// noopAudit 收集审计行供断言（Reason 入审计的锁测试）。
type noopAudit struct {
	entries []ExtractionAudit
}

func (a *noopAudit) RecordExtraction(_ context.Context, entry ExtractionAudit) error {
	a.entries = append(a.entries, entry)
	return nil
}

func TestPortraitMaintainerAppliesFourOps(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryPortraitOverlays()
	maintainer := NewPortraitMaintainer(store, nil, nil)

	// ADD：新槽位主张开新条目（valid_from 生效、valid_to 全域）。
	if _, err := maintainer.Consolidate(ctx, "user-1", PortraitClaim{Topic: "preferences", SubTopic: "user_stated", Content: "我最支持的球队是巴萨。"}); err != nil {
		t.Fatalf("ADD consolidate: %v", err)
	}
	current, err := store.CurrentPortrait(ctx, "user-1")
	if err != nil || len(current) != 1 || current[0].Content != "我最支持的球队是巴萨。" {
		t.Fatalf("after ADD current = %+v err=%v, want one live 巴萨 row", current, err)
	}
	if current[0].ID != 1 || current[0].ValidFrom.IsZero() || !current[0].ValidTo.IsZero() {
		t.Fatalf("ADD row = %+v, want id=1 with open-ended validity", current[0])
	}

	// UPDATE：同槽新值取代旧值——旧条目 valid_to 封口、新条目生效。
	decider := scriptedDecider{decision: OpDecision{Op: PortraitOpUpdate, TargetID: 1, Reason: "换主队"}}
	updater := NewPortraitMaintainer(store, decider, nil)
	if _, err := updater.Consolidate(ctx, "user-1", PortraitClaim{Topic: "preferences", SubTopic: "user_stated", Content: "我现在最支持的球队是皇马。"}); err != nil {
		t.Fatalf("UPDATE consolidate: %v", err)
	}
	current, _ = store.CurrentPortrait(ctx, "user-1")
	if len(current) != 1 || current[0].Content != "我现在最支持的球队是皇马。" || current[0].ID != 2 {
		t.Fatalf("after UPDATE current = %+v, want only the 皇马 row", current)
	}
	history, _ := store.PortraitHistory(ctx, "user-1")
	if len(history) != 2 {
		t.Fatalf("after UPDATE history = %+v, want both versions replayable", history)
	}
	if history[0].ID != 1 || history[0].ValidTo.IsZero() {
		t.Fatalf("closed row = %+v, want the 巴萨 row sealed with valid_to", history[0])
	}

	// DELETE：主张明示条目作废——单行封口，墓碑保留、永不物理删。
	decider = scriptedDecider{decision: OpDecision{Op: PortraitOpDelete, TargetID: 2, Reason: "不再成立"}}
	deleter := NewPortraitMaintainer(store, decider, nil)
	if _, err := deleter.Consolidate(ctx, "user-1", PortraitClaim{Topic: "preferences", SubTopic: "user_stated", Content: "我没有主队了。"}); err != nil {
		t.Fatalf("DELETE consolidate: %v", err)
	}
	current, _ = store.CurrentPortrait(ctx, "user-1")
	if len(current) != 0 {
		t.Fatalf("after DELETE current = %+v, want no live row", current)
	}
	history, _ = store.PortraitHistory(ctx, "user-1")
	if len(history) != 2 || history[1].ID != 2 || history[1].ValidTo.IsZero() {
		t.Fatalf("after DELETE history = %+v, want the 皇马 row sealed but preserved", history)
	}

	// NOOP：重复主张不写。
	decider = scriptedDecider{decision: OpDecision{Op: PortraitOpNoop, Reason: "重复"}}
	nooper := NewPortraitMaintainer(store, decider, nil)
	if _, err := nooper.Consolidate(ctx, "user-1", PortraitClaim{Topic: "preferences", SubTopic: "user_stated", Content: "重复的话。"}); err != nil {
		t.Fatalf("NOOP consolidate: %v", err)
	}
	history, _ = store.PortraitHistory(ctx, "user-1")
	if len(history) != 2 {
		t.Fatalf("after NOOP history = %+v, want nothing written", history)
	}
}

// TestPortraitMaintainerBlindAddWithoutDecider 是 success criteria 的删除测
// 试：删操作集（不挂判定器），Reflection 回到盲 ADD——两条矛盾主张并存、
// 无消解，冲突知识集中在操作集一处。
func TestPortraitMaintainerBlindAddWithoutDecider(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryPortraitOverlays()
	maintainer := NewPortraitMaintainer(store, nil, nil)
	for _, content := range []string{"我最支持的球队是巴萨。", "我现在最支持的球队是皇马。"} {
		if _, err := maintainer.Consolidate(ctx, "user-1", PortraitClaim{Topic: "preferences", SubTopic: "user_stated", Content: content}); err != nil {
			t.Fatalf("blind ADD consolidate: %v", err)
		}
	}
	current, _ := store.CurrentPortrait(ctx, "user-1")
	if len(current) != 2 {
		t.Fatalf("blind ADD current = %+v, want both contradictory rows kept", current)
	}
}

func TestPortraitMaintainerSkipsInvalidDecisions(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryPortraitOverlays()
	audit := &noopAudit{}
	seeder := NewPortraitMaintainer(store, nil, audit)
	if _, err := seeder.Consolidate(ctx, "user-1", PortraitClaim{Topic: "preferences", SubTopic: "user_stated", Content: "种子主张。"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// UPDATE 指向不存在的条目（幻觉 TargetID）必须整体跳过，不落库。
	hallucinating := NewPortraitMaintainer(store, scriptedDecider{decision: OpDecision{Op: PortraitOpUpdate, TargetID: 99, Reason: "幻觉"}}, audit)
	if _, err := hallucinating.Consolidate(ctx, "user-1", PortraitClaim{Topic: "preferences", SubTopic: "user_stated", Content: "新主张。"}); err == nil {
		t.Fatal("UPDATE with a hallucinated target must error")
	}
	// ADD 不该带 target。
	mistargeted := NewPortraitMaintainer(store, scriptedDecider{decision: OpDecision{Op: PortraitOpAdd, TargetID: 1, Reason: "画蛇添足"}}, audit)
	if _, err := mistargeted.Consolidate(ctx, "user-1", PortraitClaim{Topic: "preferences", SubTopic: "user_stated", Content: "新主张二。"}); err == nil {
		t.Fatal("ADD carrying a targetId must error")
	}
	// 未知动作同样拒收。
	unknown := NewPortraitMaintainer(store, scriptedDecider{decision: OpDecision{Op: "MERGE", Reason: "不存在"}}, audit)
	if _, err := unknown.Consolidate(ctx, "user-1", PortraitClaim{Topic: "preferences", SubTopic: "user_stated", Content: "新主张三。"}); err == nil {
		t.Fatal("unknown op must error")
	}

	history, _ := store.PortraitHistory(ctx, "user-1")
	if len(history) != 1 {
		t.Fatalf("history after invalid decisions = %+v, want only the seed row", history)
	}
	skips := 0
	for _, entry := range audit.entries {
		if entry.ReasonCode == ReasonPortraitOpSkipped {
			skips++
		}
	}
	if skips != 3 {
		t.Fatalf("audit = %+v, want three skipped decisions recorded", audit.entries)
	}
}

// hiddenStore 模拟隐私生命周期在存储缝上生效：Check 报活跃用户墓碑。
type hiddenStore struct {
	MemoryPortraitOverlays
}

func (h *hiddenStore) Check(context.Context, string) error {
	return privacy.ErrDataDeleted
}

// TestPortraitMaintainerHonorsPrivacyGate 锁 privacy 前置纪律：删除用户
// （已完成或进行中）的画像在判定与落地之前就拒绝，绝不写行。
func TestPortraitMaintainerHonorsPrivacyGate(t *testing.T) {
	ctx := context.Background()
	store := &hiddenStore{MemoryPortraitOverlays: *NewMemoryPortraitOverlays()}
	maintainer := NewPortraitMaintainer(store, scriptedDecider{decision: OpDecision{Op: PortraitOpAdd, Reason: "不顾纪律"}}, nil)
	if _, err := maintainer.Consolidate(ctx, "user-1", PortraitClaim{Topic: "preferences", SubTopic: "user_stated", Content: "偷偷写。"}); !errors.Is(err, privacy.ErrDataDeleted) {
		t.Fatalf("consolidate under a privacy tombstone = %v, want ErrDataDeleted", err)
	}
	history, err := store.PortraitHistory(ctx, "user-1")
	if err != nil || len(history) != 0 {
		t.Fatalf("history under a privacy tombstone = %+v err=%v, want zero rows written", history, err)
	}
}

// TestMemoryPortraitOverlaysTimeWindow 锁时间窗语义（③）：NULL=全域有效；
// valid_from 未到/valid_to 已过都取不到；全历史读法不过滤任何行。
func TestMemoryPortraitOverlaysTimeWindow(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	store := NewMemoryPortraitOverlays()
	store.now = func() time.Time { return now }

	// NULL=全域：valid_from/valid_to 双零的存量行任何时刻都取得到。
	if _, err := store.Put(ctx, "user-1", "preferences", "reply_style", "简短直接"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	// 窗外行：直接改账本行（等价 SQL 侧 UPDATE valid_to/valid_from）。
	store.mu.Lock()
	store.rows["user-1"] = append(store.rows["user-1"],
		PortraitOverlay{ID: 99, Topic: "preferences", SubTopic: "expired", Content: "已过期", ValidFrom: now.Add(-2 * time.Hour), ValidTo: now.Add(-time.Hour), UpdatedAt: now},
		PortraitOverlay{ID: 100, Topic: "preferences", SubTopic: "future", Content: "还没生效", ValidFrom: now.Add(time.Hour), UpdatedAt: now},
		PortraitOverlay{ID: 101, Topic: "preferences", SubTopic: "edge", Content: "封口时刻", ValidTo: now, UpdatedAt: now},
	)
	store.mu.Unlock()

	current, err := store.CurrentPortrait(ctx, "user-1")
	if err != nil {
		t.Fatalf("CurrentPortrait: %v", err)
	}
	if len(current) != 1 || current[0].SubTopic != "reply_style" {
		t.Fatalf("current = %+v, want only the all-time row (窗外不取、封口即失效)", current)
	}
	history, err := store.PortraitHistory(ctx, "user-1")
	if err != nil || len(history) != 4 {
		t.Fatalf("history = %+v err=%v, want every row regardless of window", history, err)
	}
}

// TestLLMPortraitOpsJudgesViaStructuredSeam 照抄 structured_test 的
// httptest fake：锁强制单工具调用、schema 枚举形状、判定解码与落地闭环。
func TestLLMPortraitOpsJudgesViaStructuredSeam(t *testing.T) {
	var capturedPayload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&capturedPayload)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{
					"tool_calls": []map[string]any{{
						"function": map[string]any{
							"name":      portraitOpToolName,
							"arguments": `{"op":"UPDATE","targetId":7,"reason":"换主队，旧条目作废"}`,
						},
					}},
				},
			}},
		})
	}))
	defer server.Close()

	decider := NewLLMPortraitOpDecider(structured.NewClient(server.URL, "test-key", "test-model"))
	claim := PortraitClaim{Topic: "preferences", SubTopic: "user_stated", Content: "我现在最支持的球队是皇马。"}
	existing := []PortraitOverlay{{ID: 7, Topic: "preferences", SubTopic: "user_stated", Content: "我最支持的球队是巴萨。"}}
	decision, err := decider.DecidePortraitOp(context.Background(), claim, existing)
	if err != nil {
		t.Fatalf("DecidePortraitOp: %v", err)
	}
	if decision.Op != PortraitOpUpdate || decision.TargetID != 7 || decision.Reason == "" {
		t.Fatalf("decision = %+v, want UPDATE@7 with a reason", decision)
	}

	tools, ok := capturedPayload["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("payload tools = %+v, want exactly one tool", capturedPayload["tools"])
	}
	tool := tools[0].(map[string]any)["function"].(map[string]any)
	if tool["name"] != portraitOpToolName {
		t.Fatalf("tool name = %+v", tool["name"])
	}
	// schema 由 OpDecision 反射生成：op 必须是四操作枚举（structured seam
	// 的核心承诺——schema 与结果类型同源）。
	properties := tool["parameters"].(map[string]any)["properties"].(map[string]any)
	opSchema := properties["op"].(map[string]any)
	enums, ok := opSchema["enum"].([]any)
	if !ok || len(enums) != 4 {
		t.Fatalf("op schema = %+v, want the four-op enum", opSchema)
	}
	choice := capturedPayload["tool_choice"].(map[string]any)
	if choice["type"] != "function" {
		t.Fatalf("tool_choice = %+v, want forced function", choice)
	}
	// 判定输入必须带新主张与现存条目（含 id），判定器才有得比。
	userContent, _ := capturedPayload["messages"].([]any)
	var prompt strings.Builder
	for _, message := range userContent {
		content := message.(map[string]any)["content"].(string)
		prompt.WriteString(content)
	}
	if !strings.Contains(prompt.String(), "我现在最支持的球队是皇马。") || !strings.Contains(prompt.String(), "id=7") {
		t.Fatalf("prompt = %s, want the claim and the listed entry", prompt.String())
	}

	// 判定→落地闭环：maintainer 接真判定器，UPDATE 封口 id=7。
	store := NewMemoryPortraitOverlays()
	if _, err := store.ApplyPortraitOp(context.Background(), "user-1", OpDecision{Op: PortraitOpAdd}, PortraitClaim{Topic: "preferences", SubTopic: "user_stated", Content: "我最支持的球队是巴萨。"}); err != nil {
		t.Fatalf("seed ADD: %v", err)
	}
	store.mu.Lock()
	store.rows["user-1"][0].ID = 7
	store.mu.Unlock()
	maintainer := NewPortraitMaintainer(store, decider, nil)
	if _, err := maintainer.Consolidate(context.Background(), "user-1", claim); err != nil {
		t.Fatalf("consolidate with the structured decider: %v", err)
	}
	current, _ := store.CurrentPortrait(context.Background(), "user-1")
	if len(current) != 1 || current[0].Content != "我现在最支持的球队是皇马。" {
		t.Fatalf("current after structured UPDATE = %+v, want the new claim live", current)
	}
}

// TestLLMPortraitOpsRejectsMalformedVerdict 锁判定契约：模型答非所选或返回
// 未知动作时报错，maintainer 跳过写库而不是猜。
func TestLLMPortraitOpsRejectsMalformedVerdict(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{
					"tool_calls": []map[string]any{{
						"function": map[string]any{
							"name":      portraitOpToolName,
							"arguments": `{"op":"MERGE","targetId":0,"reason":"不存在"}`,
						},
					}},
				},
			}},
		})
	}))
	defer server.Close()

	decider := NewLLMPortraitOpDecider(structured.NewClient(server.URL, "k", "m"))
	if _, err := decider.DecidePortraitOp(context.Background(), PortraitClaim{Topic: "preferences", SubTopic: "user_stated", Content: "任意"}, nil); err == nil {
		t.Fatal("unknown op must error")
	}
	if _, err := NewLLMPortraitOpDecider(nil).DecidePortraitOp(context.Background(), PortraitClaim{}, nil); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("nil client = %v, want ErrNotSupported", err)
	}
}

// TestQueueReflectNowConsolidatesClaimsThroughOpSet 锁 Reflection 写路径：
// user_fact 主张进待合并账本，beat 内经冲突操作集并入权威层——带判定器走
// UPDATE 消解，不带判定器回到盲 ADD。
func TestQueueReflectNowConsolidatesClaimsThroughOpSet(t *testing.T) {
	server, _ := stubMemobaseServer(func(r recordedRequest) (int, []byte) {
		switch {
		case r.method == http.MethodGet && strings.HasPrefix(r.path, "/api/v1/users/profile/"):
			return http.StatusOK, memobaseOK(`{"profiles":[]}`)
		case r.method == http.MethodGet && strings.HasPrefix(r.path, "/api/v1/users/"):
			return http.StatusOK, memobaseOK(`{}`)
		default:
			return http.StatusOK, memobaseOK(`null`)
		}
	})
	defer server.Close()
	ctx := context.Background()

	newQueueWith := func(decider PortraitOpDecider) (*Queue, *MemoryPortraitOverlays) {
		overlays := NewMemoryPortraitOverlays()
		options := []QueueOption{WithPortraitOverlays(overlays)}
		if decider != nil {
			options = append(options, WithPortraitOps(decider))
		}
		return NewQueue(NewMemobase(MemobaseConfig{BaseURL: server.URL, Token: "test-token"}), nil, nil, options...), overlays
	}

	// 带判定器：第二拍 UPDATE 消解第一拍的旧主张。
	decider := &scriptedDeciderSequence{decisions: []OpDecision{
		{Op: PortraitOpAdd, Reason: "新槽位"},
		{Op: PortraitOpUpdate, TargetID: 1, Reason: "换主队"},
	}}
	queue, overlays := newQueueWith(decider)
	if err := queue.Observe(ctx, Moment{UserID: "user-1", Kind: MomentUserFact, Content: "我最支持的球队是巴萨。", Importance: 0.7}); err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if _, err := queue.ReflectNow(ctx, "user-1", "match-1", "post_match"); err != nil {
		t.Fatalf("ReflectNow 1: %v", err)
	}
	if err := queue.Observe(ctx, Moment{UserID: "user-1", Kind: MomentUserFact, Content: "我现在最支持的球队是皇马。", Importance: 0.7}); err != nil {
		t.Fatalf("Observe 2: %v", err)
	}
	if _, err := queue.ReflectNow(ctx, "user-1", "match-1", "post_match"); err != nil {
		t.Fatalf("ReflectNow 2: %v", err)
	}
	current, _ := overlays.CurrentPortrait(ctx, "user-1")
	if len(current) != 1 || current[0].Content != "我现在最支持的球队是皇马。" || current[0].Topic != PortraitClaimTopic || current[0].SubTopic != PortraitClaimSubTopic {
		t.Fatalf("current after two beats = %+v, want only the new claim in %s/%s", current, PortraitClaimTopic, PortraitClaimSubTopic)
	}
	history, _ := overlays.PortraitHistory(ctx, "user-1")
	if len(history) != 2 || history[0].ValidTo.IsZero() {
		t.Fatalf("history after two beats = %+v, want the old claim sealed and preserved", history)
	}

	// 不带判定器（删操作集）：回到盲 ADD，两条矛盾主张并存。
	blindQueue, blindOverlays := newQueueWith(nil)
	for _, content := range []string{"我最支持的球队是巴萨。", "我现在最支持的球队是皇马。"} {
		if err := blindQueue.Observe(ctx, Moment{UserID: "user-1", Kind: MomentUserFact, Content: content, Importance: 0.7}); err != nil {
			t.Fatalf("blind Observe: %v", err)
		}
	}
	if _, err := blindQueue.ReflectNow(ctx, "user-1", "match-1", "post_match"); err != nil {
		t.Fatalf("blind ReflectNow: %v", err)
	}
	blindCurrent, _ := blindOverlays.CurrentPortrait(ctx, "user-1")
	if len(blindCurrent) != 2 {
		t.Fatalf("blind ADD current = %+v, want both contradictory rows", blindCurrent)
	}
}

// scriptedDeciderSequence 按序回放判定（一个 beat 一条主张）。
type scriptedDeciderSequence struct {
	decisions []OpDecision
	calls     int
}

func (s *scriptedDeciderSequence) DecidePortraitOp(context.Context, PortraitClaim, []PortraitOverlay) (OpDecision, error) {
	if s.calls >= len(s.decisions) {
		return OpDecision{Op: PortraitOpNoop, Reason: "脚本用尽"}, nil
	}
	decision := s.decisions[s.calls]
	s.calls++
	return decision, nil
}
