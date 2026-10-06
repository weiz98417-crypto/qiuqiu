package memory

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

// 8.1 排序方向性（policy-bits…不对——memory-scoring 8.1）：importance 参与
// 后排序变化仅发生在「低相关高重要」记忆上位。三个确定性场景：
//  1. focus 空 = 纯 importance×recency 排序（旧算法 importance 不参与，
//     类目条目无差异）；
//  2. 衰减窗口内同类目新条目压旧条目（exp 衰减对齐向量腿）；
//  3. 命中优先于类目（relevance 是第一因子，类目不能把不相关条目抬过
//     直接命中的）。
func TestRecallWeightDirectionalOrdering(t *testing.T) {
	now := time.Now().UTC()
	entry := func(topic, subTopic, content string, age time.Duration) profileEntry {
		var entry profileEntry
		entry.Attributes.Topic = topic
		entry.Attributes.SubTopic = subTopic
		entry.Content = content
		entry.UpdatedAt = now.Add(-age).UTC().Format(time.RFC3339)
		return entry
	}

	favorite := entry("basic_info", "favorite_team", "皇马", 24*time.Hour)
	generic := entry("preferences", "reply_style", "喜欢简洁回复", 24*time.Hour)

	// 场景 1：focus 空——高类目在前（旧算法两路同分，类目不参与）。
	if got := recallWeight("", favorite, 0); got <= recallWeight("", generic, 0) {
		t.Fatalf("no-focus ordering: favorite=%v must outrank generic=%v", got, recallWeight("", generic, 0))
	}

	// 场景 2：同类目，衰减窗内新的在前（τ=14 天）。
	oldFavorite := entry("basic_info", "favorite_team", "皇马", 20*24*time.Hour)
	newFavorite := entry("basic_info", "favorite_team", "皇马", 24*time.Hour)
	if got := recallWeight("", newFavorite, 14); got <= recallWeight("", oldFavorite, 14) {
		t.Fatalf("decay ordering: new=%v must outrank old=%v", got, recallWeight("", oldFavorite, 14))
	}
	// 衰减关（DecayDays=0）时新旧同权——回归现状。
	if recallWeight("", newFavorite, 0) != recallWeight("", oldFavorite, 0) {
		t.Fatal("decay disabled must keep recency factor at 1")
	}

	// 场景 3：命中优先——不相关的高类目不能抬过直接命中的低类目。
	if got := recallWeight("皇马", favorite, 0); got <= recallWeight(" TotallyUnrelated ", generic, 0) {
		t.Fatal("relevance must be the first factor: a hit always outranks a miss")
	}
}

// 8.2 触发语义：Observe 累计重要性；阈值判定用；ReflectNow 消费（取出即
// 清）；adapter 未配置不消费（reflect 没跑成，账不清）。
func TestPendingImportanceAccruesAndClearsOnReflect(t *testing.T) {
	server, _ := stubMemobaseServer(func(r recordedRequest) (int, []byte) {
		switch {
		case r.method == http.MethodGet && strings.HasPrefix(r.path, "/api/v1/users/profile/"):
			return http.StatusOK, memobaseOK(`{"profiles":[]}`)
		case r.method == http.MethodGet && strings.HasPrefix(r.path, "/api/v1/users/"):
			return http.StatusOK, memobaseOK(`{"data":{},"errmsg":"","errno":0}`)
		default:
			return http.StatusOK, memobaseOK(`{}`)
		}
	})
	defer server.Close()

	adapter := NewMemobase(MemobaseConfig{BaseURL: server.URL, Token: "test-token"})
	queue := NewQueue(adapter, nil, nil)
	ctx := context.Background()

	if got := queue.PendingImportance("user-1"); got != 0 {
		t.Fatalf("initial pending = %v, want 0", got)
	}
	for _, moment := range []Moment{
		{UserID: "user-1", Kind: MomentUserFact, Content: "我支持皇马", Importance: 0.8},
		{UserID: "user-1", Kind: MomentMatchEvent, Content: "绝杀了", Importance: 0.8},
	} {
		if err := queue.Observe(ctx, moment); err != nil {
			t.Fatalf("Observe: %v", err)
		}
	}
	if got := queue.PendingImportance("user-1"); got < ReflectImportanceThreshold {
		t.Fatalf("pending = %v, want >= threshold %v after two 0.8 moments", got, ReflectImportanceThreshold)
	}
	if other := queue.PendingImportance("user-2"); other != 0 {
		t.Fatalf("other user pending = %v, want 0 (per-user ledger)", other)
	}

	// adapter 未配置：reflect 不发生，账不清。
	broken := NewQueue(NewMemobase(MemobaseConfig{}), nil, nil)
	if err := broken.Observe(ctx, Moment{UserID: "user-1", Kind: MomentUserFact, Content: "x", Importance: 0.9}); err != nil {
		t.Fatalf("Observe broken: %v", err)
	}
	if _, err := broken.ReflectNow(ctx, "user-1", "", "idle"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unconfigured reflect = %v, want ErrUnavailable", err)
	}
	if got := broken.PendingImportance("user-1"); got <= 0 {
		t.Fatal("unconsumed importance must stay on the ledger when the beat cannot run")
	}

	// configured：beat 消费累计。
	if _, err := queue.ReflectNow(ctx, "user-1", "", "idle"); err != nil {
		t.Fatalf("ReflectNow: %v", err)
	}
	if got := queue.PendingImportance("user-1"); got != 0 {
		t.Fatalf("pending after reflect = %v, want 0 (beat consumed the ledger)", got)
	}
}

// recordingAudit 是审计断言替身（noopAudit 同形）。
type recordingAudit struct {
	entries []ExtractionAudit
}

func (a *recordingAudit) RecordExtraction(_ context.Context, entry ExtractionAudit) error {
	a.entries = append(a.entries, entry)
	return nil
}

// mustCurrentEntries 读当前有效条目数（测试断言落库面）。
func mustCurrentEntries(t *testing.T, store *MemoryPortraitOverlays, userID string) []PortraitOverlay {
	t.Helper()
	entries, err := store.CurrentPortrait(context.Background(), userID)
	if err != nil {
		t.Fatalf("CurrentPortrait: %v", err)
	}
	return entries
}

// scriptedReviewer 是 dry-run 审查的测试替身。
type scriptedReviewer struct {
	verdict WritebackVerdict
	err     error
	calls   int
}

func (r *scriptedReviewer) ReviewWriteback(ctx context.Context, userID string, claim PortraitClaim, existing []PortraitOverlay) (WritebackVerdict, error) {
	r.calls++
	return r.verdict, r.err
}

// 8.3 dry-run：审查拒绝→claim 不落库+审计带 writeback_rejected；审查故障
// →直落（reviewer 是质量闸不是可用性闸）；缺席→现状直落；比分红线硬门
// 不依赖 reviewer（确定性，零 LLM 成本）。
func TestWritebackDryRunGatesAndDegrades(t *testing.T) {
	ctx := context.Background()
	claim := PortraitClaim{Topic: "preferences", SubTopic: "user_stated", Content: "我最支持巴萨。"}

	t.Run("score_redline_blocks_without_reviewer", func(t *testing.T) {
		store := NewMemoryPortraitOverlays()
		audit := &recordingAudit{}
		maintainer := NewPortraitMaintainer(store, nil, audit, nil)
		decision, err := maintainer.Consolidate(ctx, "user-1", PortraitClaim{
			Topic: "preferences", SubTopic: "user_stated", Content: "刚那场 2-1 太刺激了",
		})
		if err != nil {
			t.Fatalf("Consolidate: %v", err)
		}
		if decision.Op != PortraitOpNoop || !strings.Contains(decision.Reason, "writeback_rejected") {
			t.Fatalf("score redline decision = %+v, want noop writeback_rejected", decision)
		}
		if entries := mustCurrentEntries(t, store, "user-1"); len(entries) != 0 {
			t.Fatalf("score-redlined claim must not land, got %d entries", len(entries))
		}
		if len(audit.entries) != 1 {
			t.Fatalf("audit rows = %d, want 1 (审计链可查)", len(audit.entries))
		}
	})

	// 三段式数字（阵型/时间戳类）豁免。
	for _, allowed := range []string{"我喜欢看 3-4-3 阵型的球队"} {
		if reason := claimScoreRedline(allowed); reason != "" {
			t.Fatalf("claimScoreRedline(%q) = %q, want exemption", allowed, reason)
		}
	}

	t.Run("rejected_claim_never_lands", func(t *testing.T) {
		store := NewMemoryPortraitOverlays()
		audit := &recordingAudit{}
		reviewer := &scriptedReviewer{verdict: WritebackVerdict{OK: false, Reason: "与画像矛盾"}}
		maintainer := NewPortraitMaintainer(store, nil, audit, reviewer)
		decision, err := maintainer.Consolidate(ctx, "user-1", claim)
		if err != nil {
			t.Fatalf("Consolidate: %v", err)
		}
		if decision.Op != PortraitOpNoop || !strings.Contains(decision.Reason, "writeback_rejected") {
			t.Fatalf("decision = %+v, want noop with writeback_rejected", decision)
		}
		if entries := mustCurrentEntries(t, store, "user-1"); len(entries) != 0 {
			t.Fatalf("rejected claim must not land, got %d entries", len(entries))
		}
		if reviewer.calls != 1 {
			t.Fatalf("reviewer calls = %d, want 1", reviewer.calls)
		}
		// 审计链可查（spec success criteria）。
		if len(audit.entries) != 1 {
			t.Fatalf("audit rows = %d, want 1", len(audit.entries))
		}
	})

	t.Run("reviewer_failure_falls_through", func(t *testing.T) {
		store := NewMemoryPortraitOverlays()
		reviewer := &scriptedReviewer{err: errors.New("sidecar down")}
		maintainer := NewPortraitMaintainer(store, nil, nil, reviewer)
		if _, err := maintainer.Consolidate(ctx, "user-1", claim); err != nil {
			t.Fatalf("Consolidate: %v", err)
		}
		if entries := mustCurrentEntries(t, store, "user-1"); len(entries) != 1 {
			t.Fatalf("reviewer failure must fall through to landing, got %d entries", len(entries))
		}
	})

	t.Run("absent_reviewer_is_status_quo", func(t *testing.T) {
		store := NewMemoryPortraitOverlays()
		maintainer := NewPortraitMaintainer(store, nil, nil, nil)
		if _, err := maintainer.Consolidate(ctx, "user-1", claim); err != nil {
			t.Fatalf("Consolidate: %v", err)
		}
		if entries := mustCurrentEntries(t, store, "user-1"); len(entries) != 1 {
			t.Fatalf("absent reviewer must land directly, got %d entries", len(entries))
		}
	})
}
