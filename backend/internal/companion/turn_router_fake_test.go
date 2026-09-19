package companion

import (
	"context"
	"errors"
	"testing"

	"qiuqiu/internal/relationship"
	"qiuqiu/internal/router"
)

// relationshipDecisionForGuardTest 构造一个带发言策略的决策（2 句上限 +
// ForbiddenClaims），与生产 policy 的护栏维度一致。
func relationshipDecisionForGuardTest(t *testing.T) relationship.Decision {
	t.Helper()
	return relationship.Decision{Speech: &relationship.SpeechPlan{Content: relationship.ContentPolicy{
		MaxSentences:    2,
		MaxCharacters:   80,
		ProfanityLevel:  "none",
		ForbiddenClaims: []string{"new_score", "new_player", "new_event", "new_penalty_conclusion"},
	}}}
}

// scriptedRouter 是 TurnRouter seam 的进程内 fake——接口声明时承诺的
// 测试替身（openspec/changes/router-trace-durability）。agent 级行为测试
// 用它脚本化路由判定，不耦合 router client 的 HTTP/JSON 信封；信封本身由
// TestRouterClientEnvelope（httptest）单独锁。
type scriptedRouter struct {
	scripts map[string]router.Result
	fail    error
}

func (f *scriptedRouter) Route(_ context.Context, req router.Request) (router.Result, error) {
	if f.fail != nil {
		return router.Result{}, f.fail
	}
	if result, ok := f.scripts[req.Text]; ok {
		return result, nil
	}
	return router.Result{Intent: "unknown", Confidence: 1}, nil
}

func (f *scriptedRouter) Enabled() bool { return true }

// TestRouterClientEnvelope 锁 router client 自身的信封解析（tool_calls →
// Result）；agent 级测试一律用 scriptedRouter，不再各自起 httptest。
func TestRouterClientEnvelope(t *testing.T) {
	client := stubRouterServer(t, map[string]string{
		"你在干嘛": `{"intent":"smalltalk","confidence":0.9,"reply":"陪你看球。"}`,
	})
	result, err := client.Route(context.Background(), router.Request{Text: "你在干嘛", Context: "ctx"})
	if err != nil {
		t.Fatalf("Route error: %v", err)
	}
	if result.Intent != "smalltalk" || result.Confidence != 0.9 || result.Reply != "陪你看球。" {
		t.Fatalf("route result = %+v", result)
	}
	if !client.Enabled() {
		t.Fatal("client with key must be enabled")
	}
}

// ADR-0009 审计承诺补全：有建议但被护栏拦截不再是静默的 ReplyUsed=false——
// RouterTrace.RejectReason 记录 policy，回合照旧走确定性兜底。
func TestRouterRejectionStampedInTrace(t *testing.T) {
	// 三句话的建议超出 MaxSentences（2）护栏。
	agent, _ := newRoutedAgent(t, &scriptedRouter{scripts: map[string]router.Result{
		"你在干嘛": {Intent: "smalltalk", Confidence: 0.9, Reply: "我在看直播。比分还没变。你别急，慢慢看。"},
	}})

	response, err := agent.HandleMessage(context.Background(), MessageRequest{
		SignalID: "guard-reject-1", MatchID: "guard-reject", UserID: "user-1", Text: "你在干嘛", Now: fixedTime(),
	})
	if err != nil {
		t.Fatalf("HandleMessage error: %v", err)
	}
	if response.Trace.Router == nil {
		t.Fatal("router trace missing")
	}
	if response.Trace.Router.ReplyUsed {
		t.Fatal("a guard-rejected suggestion must not be used")
	}
	if response.Trace.Router.RejectReason != ReasonGuardRejected {
		t.Fatalf("rejectReason = %q, want policy", response.Trace.Router.RejectReason)
	}
}

// 空建议与被拒可分：空建议记 empty 而不是 policy。
func TestRouterEmptySuggestionStampedAsEmpty(t *testing.T) {
	agent, _ := newRoutedAgent(t, &scriptedRouter{scripts: map[string]router.Result{
		"你在干嘛": {Intent: "smalltalk", Confidence: 0.9, Reply: "   "},
	}})

	response, err := agent.HandleMessage(context.Background(), MessageRequest{
		SignalID: "guard-empty-1", MatchID: "guard-empty", UserID: "user-1", Text: "你在干嘛", Now: fixedTime(),
	})
	if err != nil {
		t.Fatalf("HandleMessage error: %v", err)
	}
	if response.Trace.Router == nil || response.Trace.Router.RejectReason != ReasonGuardEmpty {
		t.Fatalf("rejectReason = %+v, want empty", response.Trace.Router)
	}
}

// 两条回复路径共用 guardValidateReply：同一违规候选在 realizer 语义与
// router 语义下判得一致，拒绝原因一处定义。
func TestGuardValidateReplySharedVerdict(t *testing.T) {
	decision := relationshipDecisionForGuardTest(t)
	input := "你在干嘛"
	reliable := "我在盯着直播呢。"
	anchors := []string{"国安"}

	valid := "我在盯着直播呢，陪你一起看。"
	got, reason := guardValidateReply(input, IntentSmalltalk, valid, nil, reliable, decision)
	if got != valid || reason != "" {
		t.Fatalf("valid candidate rejected: %q / %q", got, reason)
	}
	if out, reason := guardValidateReply(input, IntentSmalltalk, "   ", nil, reliable, decision); out != "" || reason != ReasonGuardEmpty {
		t.Fatalf("empty candidate = %q / %q, want empty rejection", out, reason)
	}
	// 禁语（比赛事实语言）候选被拒：guard 不得引入未被支持的事实主张。
	if _, reason := guardValidateReply(input, IntentSmalltalk, "var回放还没出结果。", nil, reliable, decision); reason != ReasonGuardRejected {
		t.Fatalf("forbidden candidate reason = %q, want policy", reason)
	}
	// 锚源统一含 requiredAnchors：锚定词只出现在 anchors 里时仍是合法
	// 证据源（路由路径不再比 realizer 路径松）。
	if _, reason := guardValidateReply("国安这球怎么样", IntentEmotionReaction, "国安稳住，别乱。", anchors, reliable, decision); reason != "" {
		t.Fatalf("anchored candidate rejected: %q", reason)
	}
	// router 失败的降级路径不受 guard 影响（scriptedRouter.fail 可用）。
	fake := &scriptedRouter{fail: errors.New("boom")}
	if _, err := fake.Route(context.Background(), router.Request{Text: "x"}); err == nil {
		t.Fatal("failing fake must surface the error")
	}
}
