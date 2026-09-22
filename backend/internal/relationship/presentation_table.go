package relationship

// Presentation mapping table (openspec/changes/presentation-mapping, ADR-0007).
//
// client/assets/live2d/models/qiuqiu/presentation-map.json is the single
// source of the mapping; the table below is its Go-side mirror, locked to the
// JSON, the client allowlist and the model asset by presentation_table_test.go
// the same way presentation_vocabulary.go mirrors the client whitelist.
//
// presentationFor resolves through this table instead of an if-else chain:
//  1. match signals: the event-class row wins (JSON "events"),
//  2. otherwise the first act row whose act is in the policy actions and
//     whose key matches (JSON "acts"; declaration order is the precedence),
//  3. otherwise the user-turn base row (chat/speak),
//  4. otherwise the terminal watching default (focus/focus).

// AffectQuadrant is the act-key vocabulary of the presentation-map.json
// "acts" section (closed set per design.md): "any" fires regardless of mood,
// "positive"/"negative"/"neutral" are affect valence classes, "mild"/"strong"
// only where a policy cue defines intensity (ActDisagree).
type AffectQuadrant string

const (
	QuadrantAny      AffectQuadrant = "any"
	QuadrantPositive AffectQuadrant = "positive"
	QuadrantNeutral  AffectQuadrant = "neutral"
	QuadrantNegative AffectQuadrant = "negative"
	QuadrantMild     AffectQuadrant = "mild"
	QuadrantStrong   AffectQuadrant = "strong"
)

// emptyExpressionFileIndex is the model's expression file index 0
// (expressions/expression1.exp3.json). The emission audit (2026-09-17,
// presentation-mapping proposal.md) found that file carries zero parameters,
// so any non-neutral name bound to it renders no face at all — the old
// `thinking` bug. presentation-map.json therefore binds only the neutral body
// states (focus/idle/listening) to index 0;
// TestPresentationMapNeverBindsNonNeutralNamesToEmptyExpressionFile locks
// that rule against the asset itself.
const emptyExpressionFileIndex = 0

// watchingEventClass keys the terminal watching-default row. It is not a
// model event type; the resolution falls through to it when nothing else
// matched (delivery results, silenced turns, unknown events without acts).
const watchingEventClass = "watching"

// presentationRow is one routed cell: a (act, quadrant, eventClass) key maps
// to an exact performance. Rows are data; presentationFor is a lookup. Every
// row cites the policy cue or match event that triggers it.
type presentationRow struct {
	act         CommunicationAct // policy act; "" keys the user-turn base row
	quadrant    AffectQuadrant   // act key from the JSON "acts" section
	eventClass  string           // match EventType from the JSON "events" section; "" for user turns
	expression  string
	motion      string
	energyDelta float64 // applied to the plan's voice energy (ActRepair)
}

// presentationTable mirrors presentation-map.json "events" + "acts" 1:1.
// Declaration order is the precedence for act rows: the more specific
// communication act wins (e.g. the ActDisagree inside
// "unverified_fact_requires_reserve" colors the turn even though ActReact
// rides along in the same action list).
var presentationTable = []presentationRow{
	// -- JSON "events": match signals; the event-class row wins over any act row.
	// Trigger: updateAffect "goal" (valence/arousal spike) — the first-class
	// celebrate group ('cheer' stays accepted as a legacy client alias).
	{eventClass: "goal", expression: "excited", motion: "celebrate"},
	// Trigger: a clear chance created (live2d-motion-revert: also the
	// backchannel micro-reaction row — big_chance is a real matchstate event
	// type and shares this single source).
	{eventClass: "big_chance", expression: "excited", motion: "celebrate_02"},
	// Trigger: a save denies the chance — startle then tense respect for the
	// keeper (backchannel row, single source).
	{eventClass: "save", expression: "surprised", motion: "tense"},
	// Trigger: a shot goes wide/miss (live2d-motion-revert: backchannel's
	// "miss" event type; shot_missed below keeps its own row).
	{eventClass: "miss", expression: "sad", motion: "miss"},
	// Trigger: updateAffect "goal_cancelled" (valence crash on a
	// controversial call) — the VAR-overturn startle plus the referee
	// complaint (resurrects the previously never-emitted `surprised` slot).
	{eventClass: "goal_cancelled", expression: "surprised", motion: "complain"},
	// Trigger: a VAR decision overturns a goal against the watched team —
	// provisional route (no updateAffect case emits the class yet); kept in
	// lockstep with the JSON events section so the slot cannot silently die.
	{eventClass: "var_overturn", expression: "surprised", motion: "confused"},
	// Trigger: updateAffect "var_check" (tension spike, confidence drop) —
	// the anxious wait during the review (also the backchannel row; the
	// micro-reaction takes the tense body, the analytical think pose stays
	// with user-turn ActAnalyze).
	{eventClass: "var_check", expression: "tense", motion: "tense"},
	// Trigger: updateAffect "shot_missed" (mild valence dip) — the
	// near-miss gesture instead of the neutral watching default.
	{eventClass: "shot_missed", expression: "sad", motion: "miss"},

	// -- JSON "acts": user turns, first matching row in declaration order wins.
	// Trigger: policy "explicit_analysis_request" (isTacticalQuestion) — a
	// tactical question gets the analysis tableau instead of plain talk.
	{act: ActAnalyze, quadrant: QuadrantAny, expression: "thinking", motion: "analysis"},
	// Triggers: policy "shared_moment_recalled(_with_permission)" and
	// "open_thread_ready_for_recall" — nodding along with a callback.
	{act: ActRecall, quadrant: QuadrantAny, expression: "happy", motion: "agree"},
	// Trigger: policy "stable_opinion_disagreement",
	// "unverified_fact_requires_reserve" and
	// "playful_fact_correction_with_permission" — the mild tier of pushing
	// back on the user reads as the complaint gesture.
	{act: ActDisagree, quadrant: QuadrantMild, expression: "nervous", motion: "complain"},
	// Trigger: policy "personal_insult_rejected" (containsPersonalInsult) —
	// an insult is rejected sharply, so the body shows the angry face while
	// complaining (resurrects the never-emitted `angry` slot).
	{act: ActDisagree, quadrant: QuadrantStrong, expression: "angry", motion: "complain"},
	// Trigger: policy interaction feedback ("banter_boundary",
	// "repetition", "over_analysis") — repairing reads as a subdued agree
	// and deliberately drops the plan's energy by 0.3.
	{act: ActRepair, quadrant: QuadrantAny, expression: "sad", motion: "agree", energyDelta: -0.3},
	// Trigger: policy "stable_preference_worth_following_up" (ActAsk rides
	// with ActAcknowledge) — leaning in with a question gets the think
	// pose (resurrects the dead `think` slot for backend emission).
	{act: ActAsk, quadrant: QuadrantAny, expression: "thinking", motion: "think"},
	// Triggers: policy "banter_invited_with_permission" and
	// "playful_fact_correction_with_permission" — teasing keeps the talking
	// body but borrows the teasing expression.
	{act: ActTease, quadrant: QuadrantAny, expression: "tease", motion: "speak"},
	// Triggers: policy "user_emotion_reaction", "user_personal_share" and
	// "unverified_fact_requires_reserve" (ActReact) — reacting is
	// quadrant-colored: hyped along with the moment, subdued by a low one,
	// plain while serene.
	{act: ActReact, quadrant: QuadrantPositive, expression: "excited", motion: "celebrate"},
	{act: ActReact, quadrant: QuadrantNegative, expression: "nervous", motion: "complain"},
	{act: ActReact, quadrant: QuadrantNeutral, expression: "chat", motion: "speak"},
	// Trigger: policy "default_acknowledgement" plus the boundary and
	// repair-follow-through acks — the plain talking body, unchanged from
	// the pre-table behavior.
	{act: ActAcknowledge, quadrant: QuadrantAny, expression: "chat", motion: "speak"},
	// User-turn base: a turn with no routed act still speaks. Matches the
	// pre-table default so pinned shapes keep holding.
	{act: "", quadrant: QuadrantAny, expression: "chat", motion: "speak"},
	// Terminal watching default: nothing routed (delivery result, silenced
	// turn, unknown event without acts) keeps the focus watching body. The
	// "focus" motion is a legacy synthetic name the client aliases onto the
	// listen group (presentation_vocabulary.go clientMotionAliases).
	{eventClass: watchingEventClass, expression: "focus", motion: "focus"},
}

// presentationEventTuning carries the delivery tuning (voice style + hold
// window) that rides on the match-event rows. It is delivery seasoning, not
// body mapping, so presentation-map.json does not own it; the values are
// carried over verbatim from the pre-table chain.
var presentationEventTuning = map[string]struct {
	voiceStyle string
	holdMS     int
}{
	"goal":           {voiceStyle: "excited", holdMS: 2600},
	"var_check":      {voiceStyle: "tense", holdMS: 2200},
	"goal_cancelled": {voiceStyle: "low_disappointed", holdMS: 2800},
	"shot_missed":    {voiceStyle: "low_disappointed", holdMS: 2200},
}

// resolvePresentationRow walks the table for one signal. It returns nil only
// when the caller should keep the watching default.
func resolvePresentationRow(affect AffectState, signal Signal, actions []CommunicationAct) *presentationRow {
	// Match signals: the event-class row wins (JSON "events"), silences
	// included — a muted goal still celebrates in the body.
	if signal.Kind == SignalMatchEvent && signal.Match != nil {
		for index := range presentationTable {
			row := &presentationTable[index]
			if row.eventClass != "" && row.eventClass != watchingEventClass && row.eventClass == signal.Match.EventType {
				return row
			}
		}
	}
	// Silence keeps the watching default; acts never route a muted turn.
	if hasAction(actions, ActSilence) {
		return nil
	}
	var userTurnBase *presentationRow
	for index := range presentationTable {
		row := &presentationTable[index]
		if row.eventClass != "" {
			continue
		}
		if row.act == "" {
			userTurnBase = row
			continue
		}
		if !hasAction(actions, row.act) {
			continue
		}
		// QuadrantAny rows fire regardless of mood; keyed rows match only
		// their resolved act key.
		if key := presentationActKey(row.act, affect, signal); row.quadrant != QuadrantAny && row.quadrant != key {
			continue
		}
		return row
	}
	// The base row only backs user turns; other signal kinds keep the
	// watching default.
	if signal.Kind == SignalUserTurn && userTurnBase != nil {
		return userTurnBase
	}
	return nil
}

// presentationActKey resolves the key a row is matched by. ActDisagree splits
// mild/strong on the existing policy cue — policy.go detects
// "personal_insult_rejected" with containsPersonalInsult, every other
// disagreement path ("stable_opinion_disagreement",
// "unverified_fact_requires_reserve", "playful_fact_correction_with_permission")
// stays mild. Everything else colors by the affect quadrant with the
// thresholds of the client IdleTierPicker.tierFor
// (client/lib/services/idle_tier_picker.dart): deflated / energetic / calm,
// mapped onto the JSON act keys negative / positive / neutral.
func presentationActKey(act CommunicationAct, affect AffectState, signal Signal) AffectQuadrant {
	if act == ActDisagree {
		if signal.User != nil && containsPersonalInsult(signal.User.Text) {
			return QuadrantStrong
		}
		return QuadrantMild
	}
	// Deflated band: arousal ≤ 0.15 || valence ≤ −0.35.
	if affect.Arousal <= 0.15 || affect.Valence <= -0.35 {
		return QuadrantNegative
	}
	// Energetic band: arousal ≥ 0.55 && valence ≥ 0.15.
	if affect.Arousal >= 0.55 && affect.Valence >= 0.15 {
		return QuadrantPositive
	}
	// Serene/calm band (client idle tier "calm").
	return QuadrantNeutral
}

// BackchannelPresentation resolves the micro-reaction performance for a
// backchannel-eligible event type through the same events rows the turn path
// uses (live2d-motion-revert: backchannel's private map was collapsed into
// this single source). ok=false when the event class has no row.
func BackchannelPresentation(eventType string) (expression, motion string, ok bool) {
	for index := range presentationTable {
		row := &presentationTable[index]
		if row.eventClass != "" && row.eventClass != watchingEventClass && row.eventClass == eventType {
			return row.expression, row.motion, true
		}
	}
	return "", "", false
}

// InterruptedDeliveryPresentation builds the one-shot confused/listening
// reaction of presentation-map.json delivery.interrupted: an unparseable user
// turn (companion IntentUnknown) plays it once alongside the deterministic
// clarification — the reply text is never replaced — and the body decays back
// to the watching focus.
func InterruptedDeliveryPresentation(affect AffectState) PresentationPlan {
	return PresentationPlan{
		Affect:      affect,
		Expression:  "confused",
		Motion:      "listening",
		VoiceStyle:  "natural",
		VoiceEnergy: clamp(0.35+affect.Arousal*0.55, 0, 1),
		VoiceSpeed:  clamp(0.9+affect.Arousal*0.15, 0.8, 1.1),
		HoldMS:      1800,
		ReturnMode:  "decay_to_focus",
	}
}
