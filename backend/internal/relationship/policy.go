package relationship

import (
	"strings"
	"time"
)

const relationshipSchemaVersion = 1

func normalizeStateBundle(state *StateBundle, userID, matchID string) {
	state.Relationship.SchemaVersion = relationshipSchemaVersion
	state.Relationship.UserID = userID
	if state.Relationship.Stage == "" {
		state.Relationship.Stage = StageFirstMeeting
	}
	isNewRelationship := state.Relationship.Version == 0 && state.Relationship.UpdatedAt.IsZero()
	if state.Relationship.Preferences.InitiativeMode == "" {
		state.Relationship.Preferences.InitiativeMode = "natural"
	}
	if state.Relationship.Preferences.AnalysisAppetite == "" {
		state.Relationship.Preferences.AnalysisAppetite = "brief"
	}
	if isNewRelationship {
		state.Relationship.Preferences.ProfanityEnabled = true
		state.Relationship.Preferences.ContinuousDialogue = true
	}
	state.Match.UserID = userID
	state.Match.MatchID = matchID
	if state.Match.Initiative.Mode == "" {
		state.Match.Initiative.Mode = state.Relationship.Preferences.InitiativeMode
	}
}

func recordActions(state *MatchCompanionState, signalID string, actions []CommunicationAct, now time.Time) {
	if len(actions) == 0 {
		return
	}
	state.RecentActions = append(state.RecentActions, ActionRecord{
		SignalID: signalID,
		Actions:  append([]CommunicationAct(nil), actions...),
		At:       now,
	})
	if len(state.RecentActions) > 12 {
		state.RecentActions = state.RecentActions[len(state.RecentActions)-12:]
	}
}

func applyPolicy(state *StateBundle, signal Signal, now time.Time) ([]CommunicationAct, []string) {
	if signal.Kind == SignalDeliveryResult && signal.Delivery != nil {
		state.Match.PlaybackState = signal.Delivery.State
		if signal.Delivery.Purpose == "first_meeting" && signal.Delivery.State == "text_delivered" {
			deliveredAt := now
			state.Relationship.GreetingDeliveredAt = &deliveredAt
		}
		return nil, []string{"delivery_result_recorded"}
	}
	if signal.Kind == SignalSessionOpened {
		if state.Relationship.FirstMetAt == nil {
			firstMetAt := now
			state.Relationship.FirstMetAt = &firstMetAt
		}
		if state.Relationship.Evidence.SharedMatches == nil {
			state.Relationship.Evidence.SharedMatches = make(map[string]time.Time)
		}
		state.Relationship.Evidence.SharedMatches[signal.MatchID] = now
		if state.Relationship.Stage == StageFirstMeeting || state.Relationship.Stage == "" {
			return []CommunicationAct{ActAcknowledge}, []string{"first_meeting_observed"}
		}
		return nil, []string{"shared_match_recorded"}
	}
	if signal.Kind == SignalMatchEvent && signal.Match != nil {
		updateAffect(&state.Match.Affect, *signal.Match, now)
		if !signal.Match.OutputAllowed {
			return []CommunicationAct{ActSilence}, []string{"match_event_observed_output_disabled"}
		}
		if state.Match.Initiative.Mode == "" {
			state.Match.Initiative.Mode = "natural"
		}
		cooldown := 90 * time.Second
		if signal.Match.NormalCooldownSeconds > 0 {
			cooldown = time.Duration(signal.Match.NormalCooldownSeconds) * time.Second
		}
		if !signal.Match.Critical && state.Match.Initiative.LastNormalAt != nil && now.Sub(*state.Match.Initiative.LastNormalAt) < cooldown {
			return []CommunicationAct{ActSilence}, []string{"natural_initiative_cooldown"}
		}
		if signal.Match.Critical {
			observedAt := now
			state.Match.Initiative.LastCriticalAt = &observedAt
		} else {
			observedAt := now
			state.Match.Initiative.LastNormalAt = &observedAt
		}
		return []CommunicationAct{ActReact}, []string{"match_event_affect_updated"}
	}
	if signal.Kind != SignalUserTurn || signal.User == nil {
		return nil, nil
	}
	text := strings.TrimSpace(signal.User.Text)
	cues := mergeUserCues(signal.User.Cues, inferUserCues(text))
	applyUserCues(&state.Relationship, cues, signal.ID, now)
	if strings.Contains(text, "多说点") && isTacticalQuestion(text) {
		state.Relationship.Preferences.AnalysisAppetite = "detailed"
	}
	if isProfanityBoundary(text) {
		state.Relationship.Preferences.ProfanityEnabled = false
		if !hasBoundary(state.Relationship.Boundaries, "profanity", "disabled") {
			state.Relationship.Boundaries = append(state.Relationship.Boundaries, UserBoundary{
				Scope:          "profanity",
				Rule:           "disabled",
				Explicit:       true,
				SourceSignalID: signal.ID,
				CreatedAt:      now,
			})
		}
		return []CommunicationAct{ActAcknowledge}, []string{"profanity_boundary_recorded"}
	}
	if hasCue(cues, CueNeedsSilence) {
		return []CommunicationAct{ActSilence}, []string{"user_requested_quiet"}
	}
	if repair, ok := interactionFeedback(text, cues); ok {
		repair.TriggerSignalID = signal.ID
		repair.StartedAt = now
		repair.LastObservedAt = now
		state.Relationship.Repair = repair
		return []CommunicationAct{ActRepair}, []string{"interaction_feedback_started_repair"}
	}
	if isExplicitWorkBoundary(text) {
		if !hasBoundary(state.Relationship.Boundaries, "work_details", "do_not_ask") {
			state.Relationship.Boundaries = append(state.Relationship.Boundaries, UserBoundary{
				Scope:          "work_details",
				Rule:           "do_not_ask",
				Explicit:       true,
				SourceSignalID: signal.ID,
				CreatedAt:      now,
			})
		}
		return []CommunicationAct{ActAcknowledge}, []string{"explicit_boundary_recorded"}
	}
	if state.Relationship.Repair.Active {
		state.Relationship.Repair.LastObservedAt = now
		state.Relationship.Repair.SuccessfulTurns++
		if state.Relationship.Repair.SuccessfulTurns >= 3 {
			state.Relationship.Evidence.CompletedRepairs++
			state.Relationship.Repair = RepairState{}
			return []CommunicationAct{ActAcknowledge}, []string{"repair_completed_after_follow_through"}
		}
		return []CommunicationAct{ActAcknowledge}, []string{"repair_follow_through"}
	}
	if hasCue(cues, CueSharedMomentRecalled) {
		if stageAtLeast(state.Relationship.Stage, StageWatchBuddy) && firstAllowedBanterScope(state.Relationship.Banter) != "" {
			return []CommunicationAct{ActRecall, ActTease}, []string{"shared_moment_recalled_with_permission"}
		}
		return []CommunicationAct{ActRecall}, []string{"shared_moment_recalled"}
	}
	if hasCue(cues, CueOpenThreadReady) {
		return []CommunicationAct{ActRecall, ActOpinion}, []string{"open_thread_ready_for_recall"}
	}
	if signal.Grounding.FactMode == FactModeUnverified {
		return []CommunicationAct{ActReact, ActDisagree}, []string{"unverified_fact_requires_reserve"}
	}
	if hasCue(cues, CueOpinionConflict) {
		return []CommunicationAct{ActDisagree, ActOpinion}, []string{"stable_opinion_disagreement"}
	}
	if containsPersonalInsult(text) {
		return []CommunicationAct{ActDisagree, ActOpinion}, []string{"personal_insult_rejected"}
	}
	if signal.Grounding.Intent == "emotion_reaction" {
		return []CommunicationAct{ActReact}, []string{"user_emotion_reaction"}
	}
	if isTacticalQuestion(text) {
		return []CommunicationAct{ActAnalyze}, []string{"explicit_analysis_request"}
	}
	if hasCue(cues, CueBanterAllowed) && stageAtLeast(state.Relationship.Stage, StageFamiliar) {
		if signal.Grounding.FactMode == FactModeDeterministic {
			return []CommunicationAct{ActTease, ActDisagree}, []string{"playful_fact_correction_with_permission"}
		}
		return []CommunicationAct{ActTease}, []string{"banter_invited_with_permission"}
	}
	if hasCue(cues, CueStablePreference) && stageAtLeast(state.Relationship.Stage, StageFamiliar) &&
		state.Match.Affect.Arousal < 0.7 && !hasRecentAction(state.Match.RecentActions, ActAsk, 4) {
		return []CommunicationAct{ActAcknowledge, ActAsk}, []string{"stable_preference_worth_following_up"}
	}
	return []CommunicationAct{ActAcknowledge}, []string{"default_acknowledgement"}
}

func inferUserCues(text string) []UserCue {
	var cues []UserCue
	if strings.Contains(text, "我喜欢") || strings.Contains(text, "我支持") || strings.Contains(text, "我不喜欢") || strings.Contains(text, "我更吃") {
		cues = append(cues, UserCue{Kind: CueStablePreference})
	}
	if strings.Contains(text, "刚才说的") || strings.Contains(text, "接着上次") || strings.Contains(text, "上次说") {
		cues = append(cues, UserCue{Kind: CueOpenThreadReady})
	} else if strings.Contains(text, "接着") || strings.Contains(text, "上次") {
		cues = append(cues, UserCue{Kind: CueContinuedThread})
	}
	if strings.Contains(text, "别拿") && strings.Contains(text, "开我玩笑") {
		cues = append(cues, UserCue{Kind: CueBanterDenied})
	} else if strings.Contains(text, "毒奶") {
		cues = append(cues, UserCue{Kind: CueBanterAllowed, Scope: "prediction"})
	}
	if strings.Contains(text, "不想分析") || strings.Contains(text, "缓会儿") || strings.Contains(text, "先别说话") {
		cues = append(cues, UserCue{Kind: CueNeedsSilence})
	}
	if containsAnyPhrase(text, "判断挺准", "判断很准", "你说准了", "被你说中了", "你上次说得对") {
		cues = append(cues, UserCue{Kind: CueAcceptedJudgment})
	}
	if containsAnyPhrase(text, "接着来", "继续提醒我", "继续这样", "就这么来", "有变化提醒我") {
		cues = append(cues, UserCue{Kind: CueAcceptedInitiative})
	}
	if (containsAnyPhrase(text, "想起", "记得") && containsAnyPhrase(text, "上次那场", "那场", "上次那个球")) ||
		containsAnyPhrase(text, "那个被吹掉的球", "上次那个绝杀") {
		cues = append(cues, UserCue{Kind: CueSharedMomentRecalled})
	}
	return cues
}

func mergeUserCues(primary, inferred []UserCue) []UserCue {
	merged := append([]UserCue(nil), primary...)
	for _, candidate := range inferred {
		duplicate := false
		for _, existing := range merged {
			if existing.Kind == candidate.Kind && existing.Scope == candidate.Scope {
				duplicate = true
				break
			}
		}
		if !duplicate {
			merged = append(merged, candidate)
		}
	}
	return merged
}

func applyUserCues(state *RelationshipState, cues []UserCue, signalID string, now time.Time) {
	for _, cue := range cues {
		switch cue.Kind {
		case CueStablePreference:
			state.Evidence.StablePreferences++
		case CueContinuedThread:
			state.Evidence.ContinuedThreads++
		case CueOpenThreadReady:
			state.Evidence.ContinuedThreads++
			acceptedAt := now
			state.Trust.CallbackAcceptedAt = &acceptedAt
		case CueAcceptedJudgment:
			state.Evidence.AcceptedJudgments++
			acceptedAt := now
			state.Trust.JudgmentAcceptedAt = &acceptedAt
		case CueAcceptedInitiative:
			state.Evidence.AcceptedInitiatives++
			acceptedAt := now
			state.Trust.InitiativeAcceptedAt = &acceptedAt
		case CueBanterAllowed:
			if cue.Scope == "" {
				continue
			}
			if state.Banter == nil {
				state.Banter = make(map[string]Permission)
			}
			permission := state.Banter[cue.Scope]
			if permission.Status != "allowed" {
				state.Evidence.AllowedBanterScopes++
			}
			permission.Status = "allowed"
			permission.EvidenceCount++
			permission.SourceSignalID = signalID
			state.Banter[cue.Scope] = permission
		case CueBanterDenied:
			if cue.Scope == "" {
				for scope, permission := range state.Banter {
					if permission.Status != "allowed" {
						continue
					}
					permission.Status = "denied"
					permission.EvidenceCount++
					permission.SourceSignalID = signalID
					state.Banter[scope] = permission
				}
				continue
			}
			if state.Banter == nil {
				state.Banter = make(map[string]Permission)
			}
			permission := state.Banter[cue.Scope]
			permission.Status = "denied"
			permission.EvidenceCount++
			permission.SourceSignalID = signalID
			state.Banter[cue.Scope] = permission
		case CueSharedMomentRecalled:
			state.Evidence.SharedMomentsRecalled++
		case CueContinuedDisagreement:
			state.Evidence.ContinuedDisagreements++
			continuedAt := now
			state.Trust.CorrectionContinuedAt = &continuedAt
		}
	}
}

func advanceStage(state *RelationshipState) {
	matchCount := len(state.Evidence.SharedMatches)
	if state.Stage == "" {
		state.Stage = StageFirstMeeting
	}
	if state.Repair.Active {
		return
	}
	if state.Stage == StageFirstMeeting && matchCount >= 2 && familiarEvidenceClasses(state.Evidence) >= 2 {
		state.Stage = StageFamiliar
	}
	if state.Stage == StageFamiliar && matchCount >= 3 && watchBuddyEvidenceClasses(state.Evidence) >= 3 {
		state.Stage = StageWatchBuddy
	}
	if state.Stage == StageWatchBuddy && matchCount >= 6 && state.Evidence.SharedMomentsRecalled > 0 &&
		(state.Evidence.ContinuedDisagreements > 0 || state.Evidence.CompletedRepairs > 0) {
		state.Stage = StageOldBallmate
	}
}

func familiarEvidenceClasses(evidence StageEvidence) int {
	return nonZeroCount(
		evidence.StablePreferences,
		evidence.ExplicitBoundaries,
		evidence.ContinuedThreads,
		evidence.AcceptedJudgments,
		evidence.AcceptedInitiatives,
		evidence.AllowedBanterScopes,
		evidence.SharedMomentsRecalled,
	)
}

func watchBuddyEvidenceClasses(evidence StageEvidence) int {
	return nonZeroCount(
		evidence.ContinuedThreads,
		evidence.AcceptedJudgments,
		evidence.AcceptedInitiatives,
		evidence.AllowedBanterScopes,
		evidence.SharedMomentsRecalled,
	)
}

func nonZeroCount(values ...int) int {
	count := 0
	for _, value := range values {
		if value > 0 {
			count++
		}
	}
	return count
}

func speechFor(signal Signal, actions []CommunicationAct, relationshipState RelationshipState, matchState MatchCompanionState) *SpeechPlan {
	if len(actions) == 0 || (len(actions) == 1 && actions[0] == ActSilence) || signal.Kind == SignalDeliveryResult {
		return nil
	}
	policy := DeliveryPolicy{Urgency: "normal", InterruptMode: "never", TTLSeconds: 30}
	if signal.Match != nil {
		policy.DedupeKey = signal.Match.EventID
		if signal.Match.Critical {
			policy.Urgency = "critical"
			policy.TTLSeconds = 90
		}
		if signal.Match.UserSpeaking {
			policy.InterruptMode = "after_user"
		}
	}
	return &SpeechPlan{
		Actions:  append([]CommunicationAct(nil), actions...),
		Content:  contentPolicyFor(signal, actions, relationshipState, matchState),
		Delivery: policy,
	}
}

func contentPolicyFor(signal Signal, actions []CommunicationAct, relationshipState RelationshipState, matchState MatchCompanionState) ContentPolicy {
	actionNames := make([]string, 0, len(actions))
	for _, action := range actions {
		actionNames = append(actionNames, string(action))
	}
	policy := ContentPolicy{
		Goal:               strings.Join(actionNames, "+"),
		RequiredAnchors:    append([]string(nil), signal.Grounding.RequiredAnchors...),
		ForbiddenClaims:    []string{"new_score", "new_player", "new_event", "new_penalty_conclusion"},
		ForbiddenTopics:    forbiddenTopics(relationshipState.Boundaries),
		MaxSentences:       2,
		MaxCharacters:      80,
		QuestionAllowed:    hasAction(actions, ActAsk),
		AnalysisDepth:      relationshipState.Preferences.AnalysisAppetite,
		ProfanityLevel:     "none",
		Addressing:         relationshipState.Preferences.PreferredName,
		RecentPhraseHashes: append(append([]uint64(nil), matchState.RecentPhraseHashes...), signal.Grounding.RecentPhraseHashes...),
	}
	if hasAction(actions, ActRepair) {
		policy.MaxCharacters = 60
		policy.AnalysisDepth = "none"
	}
	if relationshipState.Repair.Active {
		policy.MaxSentences = 2
		policy.MaxCharacters = 40
		policy.AnalysisDepth = "none"
		policy.BanterScope = ""
		policy.ProfanityLevel = "none"
	}
	if hasAction(actions, ActAnalyze) {
		policy.MaxSentences = 3
		policy.MaxCharacters = 160
	}
	if hasAction(actions, ActTease) {
		policy.BanterScope = firstAllowedBanterScope(relationshipState.Banter)
	}
	if !relationshipState.Repair.Active && relationshipState.Preferences.ProfanityEnabled && matchState.Affect.Arousal >= 0.7 {
		if stageAtLeast(relationshipState.Stage, StageWatchBuddy) && matchState.Affect.Arousal >= 0.9 {
			policy.ProfanityLevel = "strong_non_directed"
		} else if stageAtLeast(relationshipState.Stage, StageFamiliar) {
			policy.ProfanityLevel = "mild_non_directed"
		}
	}
	return policy
}

func forbiddenTopics(boundaries []UserBoundary) []string {
	topics := make([]string, 0)
	for _, boundary := range boundaries {
		if boundary.RevokedAt != nil {
			continue
		}
		if boundary.Scope == "work_details" && boundary.Rule == "do_not_ask" {
			topics = append(topics, "工作细节")
		}
	}
	return topics
}

func hasRecentAction(records []ActionRecord, wanted CommunicationAct, limit int) bool {
	if limit <= 0 || limit > len(records) {
		limit = len(records)
	}
	for index := len(records) - limit; index < len(records); index++ {
		if index >= 0 && hasAction(records[index].Actions, wanted) {
			return true
		}
	}
	return false
}

func hasAction(actions []CommunicationAct, wanted CommunicationAct) bool {
	for _, action := range actions {
		if action == wanted {
			return true
		}
	}
	return false
}

func firstAllowedBanterScope(permissions map[string]Permission) string {
	for _, scope := range []string{"match_judgment", "favorite_team", "favorite_player", "prediction", "watching_habit", "real_life"} {
		if permissions[scope].Status == "allowed" {
			return scope
		}
	}
	return ""
}

func hasCue(cues []UserCue, wanted UserCueKind) bool {
	for _, cue := range cues {
		if cue.Kind == wanted {
			return true
		}
	}
	return false
}

func stageAtLeast(stage, minimum RelationshipStage) bool {
	order := map[RelationshipStage]int{
		StageFirstMeeting: 0,
		StageFamiliar:     1,
		StageWatchBuddy:   2,
		StageOldBallmate:  3,
	}
	return order[stage] >= order[minimum]
}

func allowedBanterScopes(permissions map[string]Permission) int {
	count := 0
	for _, permission := range permissions {
		if permission.Status == "allowed" {
			count++
		}
	}
	return count
}

func interactionFeedback(text string, cues []UserCue) (RepairState, bool) {
	if hasCue(cues, CueBanterDenied) || strings.Contains(text, "开我玩笑") {
		return RepairState{
			Active:          true,
			Category:        "banter_boundary",
			BehaviorChanges: []string{"disable_banter", "reduce_teasing", "reduce_initiative"},
		}, true
	}
	if strings.Contains(text, "你怎么老说") || strings.Contains(text, "又重复") {
		return RepairState{
			Active:          true,
			Category:        "repetition",
			BehaviorChanges: []string{"avoid_recent_phrases", "reduce_reassurance_templates", "reduce_questions"},
		}, true
	}
	if strings.Contains(text, "大道理") || strings.Contains(text, "分析太多") || strings.Contains(text, "太啰嗦") {
		return RepairState{
			Active:          true,
			Category:        "over_analysis",
			BehaviorChanges: []string{"reduce_analysis", "reduce_questions", "reduce_initiative"},
		}, true
	}
	return RepairState{}, false
}

func isExplicitWorkBoundary(text string) bool {
	if !strings.Contains(text, "工作") {
		return false
	}
	return strings.Contains(text, "别问") || strings.Contains(text, "不想说") || strings.Contains(text, "不碰")
}

func isProfanityBoundary(text string) bool {
	return containsAnyPhrase(text, "别说脏话", "别爆粗", "不要爆粗", "别说卧槽", "不喜欢你说脏话")
}

func containsAnyPhrase(text string, phrases ...string) bool {
	for _, phrase := range phrases {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}

func isTacticalQuestion(text string) bool {
	question := strings.Contains(text, "为什么") || strings.Contains(text, "怎么") || strings.Contains(text, "换人")
	if !question {
		return false
	}
	for _, subject := range []string{"右路", "左路", "站位", "防线", "中场", "边后卫", "回收", "压迫"} {
		if strings.Contains(text, subject) {
			return true
		}
	}
	return false
}

func containsPersonalInsult(text string) bool {
	for _, insult := range []string{"废物", "垃圾", "蠢货", "裁判瞎", "人没了"} {
		if strings.Contains(text, insult) {
			return true
		}
	}
	return false
}

func hasBoundary(boundaries []UserBoundary, scope, rule string) bool {
	for _, boundary := range boundaries {
		if boundary.Scope == scope && boundary.Rule == rule {
			return true
		}
	}
	return false
}
