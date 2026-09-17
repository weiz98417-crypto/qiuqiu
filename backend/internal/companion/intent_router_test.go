package companion

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/memory"
	"qiuqiu/internal/observation"
	"qiuqiu/internal/relationship"
	"qiuqiu/internal/router"
)

// stubRouterServer answers the router client with per-text scripted route_turn
// arguments; texts without a script route to unknown/1.0 with no reply.
func stubRouterServer(t *testing.T, scripts map[string]string) *router.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode router payload: %v", err)
		}
		text := ""
		for _, message := range payload.Messages {
			if message.Role == "user" && text == "" {
				text = message.Content
			}
		}
		arguments, ok := scripts[text]
		if !ok {
			arguments = `{"intent":"unknown","confidence":1}`
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"tool_calls":[{"function":{"name":"route_turn","arguments":` + quoteJSONForRouter(arguments) + `}}]}}]}`))
	}))
	t.Cleanup(server.Close)
	return router.NewClient(router.Config{BaseURL: server.URL, APIKey: "stub", Model: "mimo-v2.5", Timeout: 2 * time.Second})
}

func failingRouterServer(t *testing.T) *router.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"boom"}`, http.StatusBadGateway)
	}))
	t.Cleanup(server.Close)
	return router.NewClient(router.Config{BaseURL: server.URL, APIKey: "stub", Model: "mimo-v2.5", Timeout: 2 * time.Second})
}

func quoteJSONForRouter(value string) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func newRoutedAgent(t *testing.T, routerClient *router.Client) (*Agent, *matchstate.Store) {
	t.Helper()
	store := matchstate.NewStore()
	agent := NewAgent(NewStoreMemoryTools(store)).
		WithDirector(relationship.NewDirector(relationship.NewMemoryRepository())).
		WithRouter(routerClient)
	return agent, store
}

// Golden journey 1: 你在干嘛 (keyword miss) routes to smalltalk and the
// router's natural reply is realized directly — no fact tools, no fact
// tokens, no interrupted (confused) reaction.
func TestGoldenJourneyRoutedSmalltalkRealizesRouterReply(t *testing.T) {
	routerClient := stubRouterServer(t, map[string]string{
		"你在干嘛": `{"intent":"smalltalk","confidence":0.9,"reply":"我在盯着直播呢，陪你一起看。"}`,
	})
	agent, _ := newRoutedAgent(t, routerClient)

	response, err := agent.HandleMessage(context.Background(), MessageRequest{
		SignalID: "golden-smalltalk-1",
		MatchID:  "golden-smalltalk",
		UserID:   "user-1",
		Text:     "你在干嘛",
		Now:      fixedTime(),
	})
	if err != nil {
		t.Fatalf("HandleMessage error: %v", err)
	}
	if response.Intent != IntentSmalltalk {
		t.Fatalf("intent = %q, want smalltalk", response.Intent)
	}
	if response.Reply != "我在盯着直播呢，陪你一起看。" {
		t.Fatalf("reply = %q, want the router suggestion", response.Reply)
	}
	assertToolNotCalled(t, response.Trace, "match.search_events")
	assertToolNotCalled(t, response.Trace, "match.get_player_timeline")
	assertToolCalled(t, response.Trace, "intent.route")
	if response.Trace.Router == nil || !response.Trace.Router.ReplyUsed {
		t.Fatalf("router trace = %+v, want replyUsed", response.Trace.Router)
	}
	if !hasReasonCode(&response.Trace, "router:smalltalk:0.90") {
		t.Fatalf("reason codes = %+v, want router:smalltalk:0.90", reasonCodesOf(&response.Trace))
	}
	if presentationInterrupted(response.Presentation) {
		t.Fatalf("a parsed routed turn must not play the interrupted reaction: %+v", response.Presentation)
	}
}

// Golden journey 2+3 (C2, router-less): 球进了 → deterministic hold; the
// insistence repeat 明明进了 → the warm persisted hold, never 没接明白.
func TestGoldenJourneyClaimPersistedThreePeat(t *testing.T) {
	agent, store := newRoutedAgent(t, nil)
	agent.WithObservationCoordinator(observation.NewMemoryCoordinator())
	matchID := "golden-claim-persist"
	setFactClaimClock(t, store, matchID, "first_half", 31*60+15)
	ctx := context.Background()

	first, err := agent.HandleMessage(ctx, MessageRequest{
		SignalID: "golden-claim-1", MatchID: matchID, UserID: "user-1", Text: "球进了", Now: fixedTime(),
	})
	if err != nil {
		t.Fatalf("first claim error: %v", err)
	}
	if first.Intent != IntentMatchClaim {
		t.Fatalf("first intent = %q, want match_fact_claim", first.Intent)
	}
	assertContains(t, first.Reply, "还没跟上")
	assertNotContains(t, first.Reply, "我知道你看到了")

	insist, err := agent.HandleMessage(ctx, MessageRequest{
		SignalID: "golden-claim-2", MatchID: matchID, UserID: "user-1", Text: "明明进了", Now: fixedTime().Add(time.Second),
	})
	if err != nil {
		t.Fatalf("insistence error: %v", err)
	}
	if insist.Intent != IntentMatchClaim {
		t.Fatalf("insistence intent = %q, want match_fact_claim (C2 generalization)", insist.Intent)
	}
	assertContains(t, insist.Reply, "我知道你看到了")
	assertNotContains(t, insist.Reply, "没接明白")
	if insist.Trace.Reason != "claim_persisted_hold" {
		t.Fatalf("insistence reason = %q, want claim_persisted_hold", insist.Trace.Reason)
	}

	threePeat, err := agent.HandleMessage(ctx, MessageRequest{
		SignalID: "golden-claim-3", MatchID: matchID, UserID: "user-1", Text: "真的进了", Now: fixedTime().Add(2 * time.Second),
	})
	if err != nil {
		t.Fatalf("three-peat error: %v", err)
	}
	assertContains(t, threePeat.Reply, "我知道你看到了")
	assertNotContains(t, threePeat.Reply, "没接明白")
}

// C2 2.3: a distinct-content insistence claim opens a second observation —
// the warm hold fires (generic goal observation matches) but the coordinator
// ledger still grows.
func TestClaimPersistedDistinctContentOpensSecondObservation(t *testing.T) {
	coordinator := observation.NewMemoryCoordinator()
	agent, store := newRoutedAgent(t, nil)
	agent.WithObservationCoordinator(coordinator)
	matchID := "golden-claim-second-obs"
	if _, _, err := store.SetConfig(matchID, matchstate.MatchConfig{
		HomeTeam: "西班牙", AwayTeam: "德国",
		AwayPlayers: []matchstate.Player{{Number: "10", Name: "穆西亚拉", Position: "AM"}},
	}); err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}
	setFactClaimClock(t, store, matchID, "first_half", 31*60+15)
	ctx := context.Background()
	if _, err := agent.HandleMessage(ctx, MessageRequest{
		SignalID: "second-obs-1", MatchID: matchID, UserID: "user-1", Text: "球进了", Now: fixedTime(),
	}); err != nil {
		t.Fatalf("first claim error: %v", err)
	}
	if _, err := agent.HandleMessage(ctx, MessageRequest{
		SignalID: "second-obs-2", MatchID: matchID, UserID: "user-1", Text: "穆西亚拉明明也进了", Now: fixedTime().Add(time.Second),
	}); err != nil {
		t.Fatalf("distinct claim error: %v", err)
	}
	active, err := coordinator.ActiveObservations(ctx, "user-1", matchID)
	if err != nil {
		t.Fatalf("ActiveObservations error: %v", err)
	}
	if len(active) != 2 {
		t.Fatalf("active observations = %d, want 2 (generic + claimed player)", len(active))
	}
}

func TestIsEventClaimColloquialVariants(t *testing.T) {
	claims := []string{"明明进了", "真的进了", "确实得分了", "千真万确破门了", "就是进了", "球进了", "明明是穆西亚拉进了"}
	for _, text := range claims {
		if !isEventClaim(text) {
			t.Fatalf("isEventClaim(%q) = false, want true", text)
		}
		if Classify(text) != IntentMatchClaim {
			t.Fatalf("Classify(%q) = %s, want match_fact_claim", text, Classify(text))
		}
	}
	nonClaims := []string{
		"你在干嘛",    // no event token
		"明明进了？",   // question
		"谁进了",     // scorer question
		"要是进了就好了", // hypothetical
		"真的假的",    // disbelief, no event token
		"我打进了一个",  // first-person achievement
		"进球了吗",    // question
		"梦到梅西进球了", // non-literal
	}
	for _, text := range nonClaims {
		if isEventClaim(text) {
			t.Fatalf("isEventClaim(%q) = true, want false", text)
		}
	}
}

// Task 1.4: a routed chat suggestion cannot realize when the user text
// carries match-fact language — the deterministic backstop wins.
func TestFactLanguageBackstopForcesDeterministicOnRoutedTurns(t *testing.T) {
	routerClient := stubRouterServer(t, map[string]string{
		"var你在干嘛": `{"intent":"smalltalk","confidence":0.9,"reply":"我在看var回放呢，主裁还没定。"}`,
	})
	agent, _ := newRoutedAgent(t, routerClient)

	response, err := agent.HandleMessage(context.Background(), MessageRequest{
		SignalID: "backstop-1",
		MatchID:  "backstop",
		UserID:   "user-1",
		Text:     "var你在干嘛",
		Now:      fixedTime(),
	})
	if err != nil {
		t.Fatalf("HandleMessage error: %v", err)
	}
	// allowRealize is revoked by fact_language_policy, so the router reply is
	// dropped and the deterministic smalltalk fallback answers.
	if response.Reply == "我在看var回放呢，主裁还没定。" {
		t.Fatalf("router reply must be dropped by the fact-language backstop")
	}
	assertNotContains(t, response.Reply, "var回放")
}

// Task 1.5: a router failure degrades to the legacy canned reply plus the
// one-shot interrupted (confused/listening) reaction — never a console error.
func TestRouterErrorDegradesToLegacyCanned(t *testing.T) {
	agent, _ := newRoutedAgent(t, failingRouterServer(t))

	response, err := agent.HandleMessage(context.Background(), MessageRequest{
		SignalID: "degrade-1",
		MatchID:  "degrade",
		UserID:   "user-1",
		Text:     "你在干嘛",
		Now:      fixedTime(),
	})
	if err != nil {
		t.Fatalf("router failure must not fail the turn: %v", err)
	}
	if response.Intent != IntentUnknown {
		t.Fatalf("intent = %q, want unknown", response.Intent)
	}
	assertContains(t, response.Reply, "没接明白")
	assertToolCalled(t, response.Trace, "intent.route")
	if !presentationInterrupted(response.Presentation) {
		t.Fatalf("degraded unknown turn must keep the interrupted reaction: %+v", response.Presentation)
	}
}

// Locked decision 5: a fact-class route below 0.7 degrades to the casual
// realization with the router reply — the deterministic fact path is not
// taken, and the C3 funnel thread opens.
func TestLowConfidenceFactRouteDegradesToCasual(t *testing.T) {
	routerClient := stubRouterServer(t, map[string]string{
		"咋回事这是": `{"intent":"match_fact_claim","confidence":0.4,"reply":"哈哈，这句我先收下了。"}`,
	})
	agent, _ := newRoutedAgent(t, routerClient)
	seam := memory.NewFake()
	agent.WithMemories(seam)

	response, err := agent.HandleMessage(context.Background(), MessageRequest{
		SignalID: "lowconf-1",
		MatchID:  "lowconf",
		UserID:   "user-1",
		Text:     "咋回事这是",
		Now:      fixedTime(),
	})
	if err != nil {
		t.Fatalf("HandleMessage error: %v", err)
	}
	if response.Intent != IntentUnknown {
		t.Fatalf("intent = %q, want unknown (low-confidence fact degrades)", response.Intent)
	}
	if response.Reply != "哈哈，这句我先收下了。" {
		t.Fatalf("reply = %q, want the casual router reply", response.Reply)
	}
	if response.Trace.Claim != nil {
		t.Fatalf("a degraded fact route must not take the claim path: %+v", response.Trace.Claim)
	}
	threads := seam.ThreadsAll()
	unroutable := 0
	for _, thread := range threads {
		if thread.Kind == memory.ThreadUnroutable {
			unroutable++
		}
	}
	if unroutable == 0 {
		t.Fatalf("degraded turn must open an unroutable funnel thread: %+v", threads)
	}
}

// Task 4.3 router fixture path: a routed claim at high confidence takes the
// deterministic claim path and ignores the router's reply field (locked
// decision 4).
func TestHighConfidenceRoutedClaimIgnoresRouterReply(t *testing.T) {
	routerClient := stubRouterServer(t, map[string]string{
		"明明进了，裁判瞎了吗": `{"intent":"match_fact_claim","confidence":0.85,"team":"德国","reply":"裁判是瞎了。"}`,
	})
	agent, store := newRoutedAgent(t, routerClient)
	agent.WithObservationCoordinator(observation.NewMemoryCoordinator())
	matchID := "routed-claim"
	if _, _, err := store.SetConfig(matchID, matchstate.MatchConfig{HomeTeam: "西班牙", AwayTeam: "德国"}); err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}

	response, err := agent.HandleMessage(context.Background(), MessageRequest{
		SignalID: "routed-claim-1", MatchID: matchID, UserID: "user-1", Text: "明明进了，裁判瞎了吗", Now: fixedTime(),
	})
	if err != nil {
		t.Fatalf("HandleMessage error: %v", err)
	}
	if response.Intent != IntentMatchClaim {
		t.Fatalf("intent = %q, want match_fact_claim", response.Intent)
	}
	assertNotContains(t, response.Reply, "裁判是瞎了")
	assertContains(t, response.Reply, "还没跟上")
	if response.Trace.Claim == nil || response.Trace.Claim.Status != ClaimStatusUnverified {
		t.Fatalf("routed claim must land on the deterministic unverified claim path: %+v", response.Trace.Claim)
	}
	if response.Trace.Router == nil || response.Trace.Router.ReplyUsed {
		t.Fatalf("fact intents ignore the router reply field: %+v", response.Trace.Router)
	}
}

func reasonCodesOf(trace *Trace) []string {
	if trace == nil || trace.RelationshipDecision == nil {
		return nil
	}
	return trace.RelationshipDecision.ReasonCodes
}

func hasReasonCode(trace *Trace, want string) bool {
	for _, code := range reasonCodesOf(trace) {
		if code == want {
			return true
		}
	}
	return false
}

func presentationInterrupted(plan relationship.PresentationPlan) bool {
	return plan.Expression == "confused" && plan.Motion == "listening"
}
