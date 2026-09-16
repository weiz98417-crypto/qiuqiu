package relationship

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The contract tests read the single-source mapping and the model asset
// straight from the client tree (openspec/changes/presentation-mapping 1.6):
// the Go table, presentation-map.json, the client allowlist and the
// female_01Arkit_6.model3.json asset must stay locked to each other.
const (
	presentationMapDir  = "../../../client/assets/live2d/models/qiuqiu"
	presentationMapPath = presentationMapDir + "/presentation-map.json"
	modelAssetPath      = presentationMapDir + "/female_01Arkit_6.model3.json"
)

type presentationMapFile struct {
	Expressions map[string]int `json:"expressions"`
	Motions     map[string]struct {
		Group   string `json:"group"`
		Variant int    `json:"variant"`
	} `json:"motions"`
	Acts     map[string]map[string]json.RawMessage `json:"acts"`
	Events   map[string]string                     `json:"events"`
	Phases   map[string]string                     `json:"phases"`
	Delivery map[string]string                     `json:"delivery"`
}

// actPerformances decodes one "acts" entry: named "expr/motion" strings plus
// the optional energyDelta number.
func (m *presentationMapFile) actPerformances(act string) (map[string]string, float64) {
	performances := map[string]string{}
	energy := 0.0
	for key, raw := range m.Acts[act] {
		if key == "energyDelta" {
			_ = json.Unmarshal(raw, &energy)
			continue
		}
		var performance string
		if err := json.Unmarshal(raw, &performance); err == nil {
			performances[key] = performance
		}
	}
	return performances, energy
}

func (m *presentationMapFile) motionGroup(name string) (string, bool) {
	if entry, ok := m.Motions[name]; ok {
		return entry.Group, true
	}
	return "", false
}

type modelAssetFile struct {
	FileReferences struct {
		Expressions []struct {
			Name string `json:"Name"`
			File string `json:"File"`
		} `json:"Expressions"`
		Motions map[string][]struct {
			File string `json:"File"`
		} `json:"Motions"`
	} `json:"FileReferences"`
}

func loadPresentationMap(t *testing.T) presentationMapFile {
	t.Helper()
	data, err := os.ReadFile(filepath.FromSlash(presentationMapPath))
	if err != nil {
		t.Fatalf("read presentation-map.json: %v", err)
	}
	var mapping presentationMapFile
	if err := json.Unmarshal(data, &mapping); err != nil {
		t.Fatalf("parse presentation-map.json: %v", err)
	}
	return mapping
}

func loadModelAsset(t *testing.T) modelAssetFile {
	t.Helper()
	data, err := os.ReadFile(filepath.FromSlash(modelAssetPath))
	if err != nil {
		t.Fatalf("read model3.json: %v", err)
	}
	var model modelAssetFile
	if err := json.Unmarshal(data, &model); err != nil {
		t.Fatalf("parse model3.json: %v", err)
	}
	return model
}

// jsonActNames maps the presentation-map.json act names (Go-style) onto the
// wire act vocabulary the policy emits. The JSON uses the closed set from
// design.md; a new JSON act name must join this map deliberately.
var jsonActNames = map[string]CommunicationAct{
	"ActReact":       ActReact,
	"ActAsk":         ActAsk,
	"ActAnalyze":     ActAnalyze,
	"ActRecall":      ActRecall,
	"ActRepair":      ActRepair,
	"ActDisagree":    ActDisagree,
	"ActTease":       ActTease,
	"ActAcknowledge": ActAcknowledge,
}

// legacyMotionTargets is the model-group resolution of the legacy synthetic
// motion names (dart motionVariants: 'focus' → ('listen', 1),
// 'cheer' → ('celebrate', 0)).
var legacyMotionTargets = map[string]string{
	"focus": "listen",
	"cheer": "celebrate",
}

// TestPresentationMapKeysMatchClientVocabularyExactly asserts the exact-set
// invariant in both directions (task 1.6a): the JSON expression key set is
// the Go-side client-expression allowlist (13 names), and the JSON motion key
// set is the canonical 17 motion names (12 groups / 17 motions, ADR-0005 C4).
func TestPresentationMapKeysMatchClientVocabularyExactly(t *testing.T) {
	mapping := loadPresentationMap(t)

	if len(mapping.Expressions) != 13 {
		t.Fatalf("json expression set size = %d, want the 13 whitelisted names", len(mapping.Expressions))
	}
	if len(mapping.Expressions) != len(clientAllowedExpressions) {
		t.Fatalf("expression set size drifted: json=%d go-allowlist=%d", len(mapping.Expressions), len(clientAllowedExpressions))
	}
	for name := range mapping.Expressions {
		if !clientAllowedExpressions[name] {
			t.Errorf("json expression %q is missing from the Go client-expression allowlist", name)
		}
	}
	for name := range clientAllowedExpressions {
		if _, ok := mapping.Expressions[name]; !ok {
			t.Errorf("allowlisted expression %q is missing from presentation-map.json", name)
		}
	}

	// Motions: the JSON carries the canonical 17. The Go allowlist
	// additionally holds group-level names the JSON addresses by variant
	// (idle, listen, speak), celebrate_01 (addressed by its group name), and
	// the legacy synthetic names (cheer, focus); subtracting those must leave
	// exactly the JSON key set — an extra key on either side is a failure.
	canonical := map[string]bool{}
	for name := range clientAllowedMotions {
		canonical[name] = true
	}
	for _, name := range []string{"cheer", "focus", "celebrate_01", "idle", "listen", "speak"} {
		delete(canonical, name)
	}
	if len(mapping.Motions) != 17 {
		t.Fatalf("json motion set size = %d, want the canonical 17 motion names", len(mapping.Motions))
	}
	if len(canonical) != len(mapping.Motions) {
		t.Fatalf("motion set size drifted: json=%d canonical-go=%d", len(mapping.Motions), len(canonical))
	}
	for name := range mapping.Motions {
		if !canonical[name] {
			t.Errorf("json motion %q is missing from the Go canonical motion set", name)
		}
	}
	for name := range canonical {
		if _, ok := mapping.Motions[name]; !ok {
			t.Errorf("canonical motion %q is missing from presentation-map.json", name)
		}
	}
}

// TestPresentationTableMirrorsJSONActsAndEvents locks the table to the JSON
// "events" and "acts" sections 1:1, both directions.
func TestPresentationTableMirrorsJSONActsAndEvents(t *testing.T) {
	mapping := loadPresentationMap(t)

	tableEvents := map[string]presentationRow{}
	for index := range presentationTable {
		row := presentationTable[index]
		if row.eventClass == "" || row.eventClass == watchingEventClass {
			continue
		}
		if _, duplicate := tableEvents[row.eventClass]; duplicate {
			t.Errorf("event class %q has more than one table row", row.eventClass)
		}
		tableEvents[row.eventClass] = row
	}
	for eventClass, performance := range mapping.Events {
		row, ok := tableEvents[eventClass]
		if !ok {
			t.Errorf("json event %q has no table row", eventClass)
			continue
		}
		expression, motion, _ := strings.Cut(performance, "/")
		if row.expression != expression || row.motion != motion {
			t.Errorf("event %q: table row = (%q, %q), json = (%q, %q)", eventClass, row.expression, row.motion, expression, motion)
		}
	}
	for eventClass := range tableEvents {
		if _, ok := mapping.Events[eventClass]; !ok {
			t.Errorf("table event row %q is not in the json events section", eventClass)
		}
	}

	tableActs := map[string]presentationRow{}
	for index := range presentationTable {
		row := presentationTable[index]
		if row.act == "" || row.eventClass != "" {
			continue
		}
		key := string(row.act) + "/" + string(row.quadrant)
		if _, duplicate := tableActs[key]; duplicate {
			t.Errorf("act key %q has more than one table row", key)
		}
		tableActs[key] = row
	}
	for act := range mapping.Acts {
		wireAct, ok := jsonActNames[act]
		if !ok {
			t.Errorf("json act %q is not in the closed act-name map", act)
			continue
		}
		entryPerformances, energyDelta := mapping.actPerformances(act)
		for key, performance := range entryPerformances {
			row, ok := tableActs[string(wireAct)+"/"+key]
			if !ok {
				t.Errorf("json act %q key %q has no table row", act, key)
				continue
			}
			expression, motion, _ := strings.Cut(performance, "/")
			if row.expression != expression || row.motion != motion {
				t.Errorf("act %q key %q: table row = (%q, %q), json = (%q, %q)", act, key, row.expression, row.motion, expression, motion)
			}
			if row.act != wireAct {
				t.Errorf("act %q key %q: table row act = %q, want %q", act, key, row.act, wireAct)
			}
		}
		rowWithDelta := presentationRow{}
		foundDelta := false
		for key, row := range tableActs {
			if strings.HasPrefix(key, string(wireAct)+"/") && row.energyDelta != 0 {
				rowWithDelta = row
				foundDelta = true
			}
		}
		if energyDelta != 0 && (!foundDelta || rowWithDelta.energyDelta != energyDelta) {
			t.Errorf("act %q json energyDelta = %.2f, table row mismatch", act, energyDelta)
		}
		if energyDelta == 0 && foundDelta {
			t.Errorf("act %q table row carries energyDelta %.2f that the json does not define", act, rowWithDelta.energyDelta)
		}
	}
	wireActNames := map[CommunicationAct]string{}
	for jsonName, wireAct := range jsonActNames {
		wireActNames[wireAct] = jsonName
	}
	for key, row := range tableActs {
		wireAct, keyName, _ := strings.Cut(key, "/")
		jsonName, ok := wireActNames[CommunicationAct(wireAct)]
		if !ok {
			t.Errorf("table act row %q is not in the closed act-name map", key)
			continue
		}
		performances, _ := mapping.actPerformances(jsonName)
		if performances[keyName] != row.expression+"/"+row.motion {
			t.Errorf("table act row %q = (%q, %q) is not in the json acts section", key, row.expression, row.motion)
		}
	}
}

// TestPresentationTableRoutesFullInventory asserts the inventory-completeness
// invariant (task 1.6b): every expression/motion key in presentation-map.json
// is routed by at least one table row or explicitly owned by the JSON
// phases/delivery sections (client-owned).
func TestPresentationTableRoutesFullInventory(t *testing.T) {
	mapping := loadPresentationMap(t)
	model := loadModelAsset(t)

	// Routed by ≥1 table row. A routed motion covers its whole group: the
	// backend emits group names and the client variant-picker randomizes
	// inside the group (design.md).
	routedExpressions := map[string]bool{}
	routedMotionGroups := map[string]bool{}
	for index := range presentationTable {
		row := &presentationTable[index]
		routedExpressions[row.expression] = true
		if _, isGroup := model.FileReferences.Motions[row.motion]; isGroup {
			routedMotionGroups[row.motion] = true
			continue
		}
		if group, ok := mapping.motionGroup(row.motion); ok {
			routedMotionGroups[group] = true
		}
	}

	// Client-owned: the phases and delivery sections. The idle sentinel
	// ("affect-idle-tier") owns the neutral idle face plus the three idle
	// variants picked by IdleTierPicker (client/lib/services/
	// idle_tier_picker.dart).
	clientExpressions := map[string]bool{}
	clientMotionGroups := map[string]bool{}
	own := func(performance string) {
		if performance == "affect-idle-tier" {
			clientExpressions["idle"] = true
			clientMotionGroups["idle"] = true
			return
		}
		expression, motion, ok := strings.Cut(performance, "/")
		if !ok {
			return
		}
		clientExpressions[expression] = true
		if group, isKey := mapping.motionGroup(motion); isKey {
			clientMotionGroups[group] = true
			return
		}
		clientMotionGroups[motion] = true
	}
	for _, performance := range mapping.Phases {
		own(performance)
	}
	for _, performance := range mapping.Delivery {
		own(performance)
	}
	clientOwnedExpression := func(name string) bool {
		return clientExpressions[name]
	}

	for name := range mapping.Expressions {
		if !routedExpressions[name] && !clientOwnedExpression(name) {
			t.Errorf("expression %q is dead inventory: no table row routes it and no phases/delivery section owns it", name)
		}
	}
	for name := range mapping.Motions {
		group, _ := mapping.motionGroup(name)
		if !routedMotionGroups[group] && !clientMotionGroups[group] {
			t.Errorf("motion %q is dead inventory: no table row routes its group %q and no phases/delivery section owns it", name, group)
		}
	}
}

// motionResolvesInModel reports whether a table row's motion name resolves in
// the model asset: a JSON motion key with a valid group/variant, a model
// group name, or a legacy synthetic resolved the way the client resolves it
// (alias table + dart motionVariants targets).
func motionResolvesInModel(t *testing.T, mapping presentationMapFile, model modelAssetFile, motion string) bool {
	t.Helper()
	if entry, ok := mapping.Motions[motion]; ok {
		variants, isGroup := model.FileReferences.Motions[entry.Group]
		return isGroup && entry.Variant >= 0 && entry.Variant < len(variants)
	}
	if _, isGroup := model.FileReferences.Motions[motion]; isGroup {
		return true
	}
	if !ClientAcceptsMotion(motion) {
		return false
	}
	target := NormalizeClientMotion(motion)
	if legacy, ok := legacyMotionTargets[target]; ok {
		target = legacy
	}
	if legacy, ok := legacyMotionTargets[motion]; ok {
		target = legacy
	}
	_, isGroup := model.FileReferences.Motions[target]
	return isGroup
}

// TestPresentationTableRowsResolveInModelAsset asserts every table row's
// motion group/variant and expression file exist in the model (task 1.6c).
// Names resolve through the client alias tables, mirroring how the surfaces
// actually bind them (e.g. the legacy "tense"/"low" expression aliases and
// the legacy "focus" motion).
func TestPresentationTableRowsResolveInModelAsset(t *testing.T) {
	mapping := loadPresentationMap(t)
	model := loadModelAsset(t)
	expressionFiles := len(model.FileReferences.Expressions)
	for index := range presentationTable {
		row := &presentationTable[index]
		resolvedExpression := NormalizeClientExpression(row.expression)
		file, ok := mapping.Expressions[resolvedExpression]
		if !ok {
			t.Errorf("row %+v: expression %q is not in presentation-map.json", *row, resolvedExpression)
		} else if file < emptyExpressionFileIndex || file >= expressionFiles {
			t.Errorf("row %+v: expression %q binds file %d outside the model's %d expressions", *row, resolvedExpression, file, expressionFiles)
		}
		if !motionResolvesInModel(t, mapping, model, row.motion) {
			t.Errorf("row %+v: motion %q does not resolve in the model asset", *row, row.motion)
		}
		if row.eventClass != "" && row.eventClass != watchingEventClass {
			if _, ok := mapping.Events[row.eventClass]; !ok {
				t.Errorf("row %+v: eventClass %q is not in the json events section", *row, row.eventClass)
			}
		}
	}
}

// TestPresentationMapNeverBindsNonNeutralNamesToEmptyExpressionFile locks the
// non-empty binding rule (task 1.4): index 0 resolves to the EMPTY expression
// file (expressions/expression1.exp3.json, zero parameters per the emission
// audit), so only the neutral body states may bind it — and `thinking` stays
// re-bound to expression file 3.
func TestPresentationMapNeverBindsNonNeutralNamesToEmptyExpressionFile(t *testing.T) {
	mapping := loadPresentationMap(t)
	// Neutral names = body states, not emotional faces; extend deliberately.
	neutral := map[string]bool{"focus": true, "idle": true, "listening": true}
	for name, file := range mapping.Expressions {
		if file == emptyExpressionFileIndex && !neutral[name] {
			t.Errorf("expression %q binds the empty expression file (index %d); re-bind it in presentation-map.json or extend the neutral set deliberately", name, emptyExpressionFileIndex)
		}
	}
	if mapping.Expressions["thinking"] != 3 {
		t.Fatalf("thinking binds expression file %d, want 3 (the empty-face fix)", mapping.Expressions["thinking"])
	}

	model := loadModelAsset(t)
	if len(model.FileReferences.Expressions) == 0 {
		t.Fatal("model asset declares no expressions")
	}
	expressionOne := model.FileReferences.Expressions[emptyExpressionFileIndex]
	if expressionOne.Name != "expression1" {
		t.Fatalf("expression index %d is %q, want expression1", emptyExpressionFileIndex, expressionOne.Name)
	}
	data, err := os.ReadFile(filepath.Join(filepath.FromSlash(presentationMapDir), filepath.FromSlash(expressionOne.File)))
	if err != nil {
		t.Fatalf("read %s: %v", expressionOne.File, err)
	}
	var exp3 struct {
		Parameters []json.RawMessage `json:"Parameters"`
	}
	if err := json.Unmarshal(data, &exp3); err != nil {
		t.Fatalf("parse %s: %v", expressionOne.File, err)
	}
	if len(exp3.Parameters) != 0 {
		t.Fatalf("%s now carries %d parameters; the empty-file audit no longer holds — re-audit the neutral bindings", expressionOne.File, len(exp3.Parameters))
	}
}

// TestActDisagreeCueSelectsTier pins the ActDisagree mild/strong resolution
// (task 1.8): the existing policy cue (containsPersonalInsult, reason
// "personal_insult_rejected") escalates to angry/complain, every other
// disagreement stays at nervous/complain.
func TestActDisagreeCueSelectsTier(t *testing.T) {
	userTurn := func(text string) Signal {
		return Signal{Kind: SignalUserTurn, User: &UserSignal{Text: text}}
	}
	// policy.go "stable_opinion_disagreement" — mild tier.
	mild := presentationFor(AffectState{}, userTurn("你说得不对吧"), []CommunicationAct{ActDisagree})
	if mild.Expression != "nervous" || mild.Motion != "complain" {
		t.Fatalf("mild disagreement = (%q, %q), want (nervous, complain)", mild.Expression, mild.Motion)
	}
	// policy.go "personal_insult_rejected" — strong tier.
	strong := presentationFor(AffectState{}, userTurn("这人就是个废物"), []CommunicationAct{ActDisagree})
	if strong.Expression != "angry" || strong.Motion != "complain" {
		t.Fatalf("strong disagreement = (%q, %q), want (angry, complain)", strong.Expression, strong.Motion)
	}
}

// TestMuteActsGetTableRows pins the four previously mute acts plus the
// var_overturn event row (task 1.3 / JSON events).
func TestMuteActsGetTableRows(t *testing.T) {
	userTurn := Signal{Kind: SignalUserTurn, User: &UserSignal{Text: "这球你怎么看"}}
	// ActAsk — policy "stable_preference_worth_following_up" rides
	// [ActAcknowledge, ActAsk]; the ask wins over the plain ack.
	ask := presentationFor(AffectState{}, userTurn, []CommunicationAct{ActAcknowledge, ActAsk})
	if ask.Expression != "thinking" || ask.Motion != "think" {
		t.Fatalf("ActAsk = (%q, %q), want (thinking, think)", ask.Expression, ask.Motion)
	}
	// ActRepair — sad/agree with the -0.3 energy delta applied.
	repair := presentationFor(AffectState{Arousal: 0.2}, userTurn, []CommunicationAct{ActRepair})
	if repair.Expression != "sad" || repair.Motion != "agree" {
		t.Fatalf("ActRepair = (%q, %q), want (sad, agree)", repair.Expression, repair.Motion)
	}
	wantEnergy := clamp(0.35+0.2*0.55, 0, 1) - 0.3
	if repair.VoiceEnergy < wantEnergy-1e-9 || repair.VoiceEnergy > wantEnergy+1e-9 {
		t.Fatalf("ActRepair voice energy = %.17f, want %.17f (energyDelta -0.3)", repair.VoiceEnergy, wantEnergy)
	}
	// ActReact — quadrant-colored.
	positive := presentationFor(AffectState{Valence: 0.7, Arousal: 0.8}, userTurn, []CommunicationAct{ActReact})
	if positive.Expression != "excited" || positive.Motion != "celebrate" {
		t.Fatalf("positive ActReact = (%q, %q), want (excited, celebrate)", positive.Expression, positive.Motion)
	}
	negative := presentationFor(AffectState{Valence: -0.8, Arousal: 0.1}, userTurn, []CommunicationAct{ActReact})
	if negative.Expression != "nervous" || negative.Motion != "complain" {
		t.Fatalf("negative ActReact = (%q, %q), want (nervous, complain)", negative.Expression, negative.Motion)
	}
	neutral := presentationFor(AffectState{Valence: 0, Arousal: 0.3}, userTurn, []CommunicationAct{ActReact})
	if neutral.Expression != "chat" || neutral.Motion != "speak" {
		t.Fatalf("neutral ActReact = (%q, %q), want (chat, speak)", neutral.Expression, neutral.Motion)
	}
	// ActAcknowledge — chat/speak unchanged.
	ack := presentationFor(AffectState{}, userTurn, []CommunicationAct{ActAcknowledge})
	if ack.Expression != "chat" || ack.Motion != "speak" {
		t.Fatalf("ActAcknowledge = (%q, %q), want (chat, speak)", ack.Expression, ack.Motion)
	}
	// JSON events: var_overturn — surprised/confused.
	overturn := presentationFor(AffectState{}, Signal{Kind: SignalMatchEvent, Match: &MatchSignal{EventID: "evt-1", EventType: "var_overturn", OutputAllowed: true}}, []CommunicationAct{ActReact})
	if overturn.Expression != "surprised" || overturn.Motion != "confused" {
		t.Fatalf("var_overturn = (%q, %q), want (surprised, confused)", overturn.Expression, overturn.Motion)
	}
}

// TestMatchEventTuningRidesOnEventRows keeps the delivery tuning (voice
// style + hold window) that rode on the pre-table chain.
func TestMatchEventTuningRidesOnEventRows(t *testing.T) {
	goal := presentationFor(AffectState{}, Signal{Kind: SignalMatchEvent, Match: &MatchSignal{EventID: "evt-goal", EventType: "goal", OutputAllowed: true}}, []CommunicationAct{ActReact})
	if goal.VoiceStyle != "excited" || goal.HoldMS != 2600 {
		t.Fatalf("goal tuning = (%q, %d), want (excited, 2600)", goal.VoiceStyle, goal.HoldMS)
	}
	cancelled := presentationFor(AffectState{}, Signal{Kind: SignalMatchEvent, Match: &MatchSignal{EventID: "evt-cancel", EventType: "goal_cancelled", OutputAllowed: true}}, []CommunicationAct{ActReact})
	if cancelled.VoiceStyle != "low_disappointed" || cancelled.HoldMS != 2800 {
		t.Fatalf("goal_cancelled tuning = (%q, %d), want (low_disappointed, 2800)", cancelled.VoiceStyle, cancelled.HoldMS)
	}
	if cancelled.Expression != "surprised" || cancelled.Motion != "complain" {
		t.Fatalf("goal_cancelled = (%q, %q), want (surprised, complain)", cancelled.Expression, cancelled.Motion)
	}
	missed := presentationFor(AffectState{}, Signal{Kind: SignalMatchEvent, Match: &MatchSignal{EventID: "evt-miss", EventType: "shot_missed", OutputAllowed: true}}, []CommunicationAct{ActReact})
	if missed.Expression != "sad" || missed.Motion != "miss" {
		t.Fatalf("shot_missed = (%q, %q), want (sad, miss)", missed.Expression, missed.Motion)
	}
}

// TestInterruptedDeliveryPresentationIsOneShotShotReaction pins the
// delivery.interrupted binding (task 1.8) and keeps it inside the strict
// client whitelist.
func TestInterruptedDeliveryPresentationIsOneShotReaction(t *testing.T) {
	plan := InterruptedDeliveryPresentation(AffectState{Arousal: 0.2})
	if plan.Expression != "confused" || plan.Motion != "listening" {
		t.Fatalf("interrupted = (%q, %q), want (confused, listening)", plan.Expression, plan.Motion)
	}
	if plan.HoldMS <= 0 || plan.ReturnMode != "decay_to_focus" {
		t.Fatalf("one-shot decay missing: %+v", plan)
	}
	if !ClientAcceptsExpression(plan.Expression) || !ClientAcceptsMotion(plan.Motion) {
		t.Fatalf("interrupted presentation outside the client whitelist: %+v", plan)
	}
}
