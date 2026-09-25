package relationship

import (
	"strings"
	"time"

	"qiuqiu/internal/teamalign"
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
	acts, codes := selectTurnActs(state, signal, now)
	// 显式设置粘性覆盖（ADR-0018）：设置 > cue 推断 > talkativeness 推导；
	// 在状态被读入决策视图前落定，reason code 随决策可解释。
	if signal.User != nil && signal.User.Settings != nil {
		if v := signal.User.Settings.InitiativeMode; v != "" {
			state.Relationship.Preferences.InitiativeMode = v
			codes = append(codes, "policy_user_setting:initiative")
		}
		if v := signal.User.Settings.AnalysisAppetite; v != "" {
			state.Relationship.Preferences.AnalysisAppetite = v
			codes = append(codes, "policy_user_setting:analysis_appetite")
		}
	}
	return acts, codes
}

func selectTurnActs(state *StateBundle, signal Signal, now time.Time) ([]CommunicationAct, []string) {
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
		// ADR-0019 记忆偏置：画像口味命中本场进球事件时放大 affect
		// （±0.2/0.15 封顶），reason code 随决策可解释。
		if !signal.Match.OutputAllowed {
			return []CommunicationAct{ActSilence}, []string{"match_event_observed_output_disabled"}
		}
		if state.Match.Initiative.Mode == "" {
			state.Match.Initiative.Mode = "natural"
		}
		// Quiet tier is L0-safe: it only restricts, never enables — a quiet
		// user gets no proactive turn except critical match events, which
		// policy already allowed before the tier existed.
		if IsQuiet(signal.Match.Talkativeness) && !signal.Match.Critical {
			return []CommunicationAct{ActSilence}, []string{"talkativeness_quiet_limits_initiative"}
		}
		cooldown := 90 * time.Second
		if signal.Match.NormalCooldownSeconds > 0 {
			cooldown = time.Duration(signal.Match.NormalCooldownSeconds) * time.Second
		}
		cooldown = time.Duration(ScaleCooldownForTalkativeness(int(cooldown/time.Second), signal.Match.Talkativeness)) * time.Second
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
		// ADR-0019 记忆偏置：过全部限制门之后施加（队伍命中 0.2 优先，
		// 球员命中 0.15，不叠加），reason code 随决策可解释。上方任一门
		// （输出关闭/安静档/冷却）压成 ActSilence 的事件提前返回、不施加
		// 偏置——被压制的事件只走既有 affect 动力学、不完全放大，系有意
		// 行为：偏置是「这场球值得开口」的加成，不是无条件放大。
		codes := []string{"match_event_affect_updated"}
		codes = append(codes, applyMemoryBias(&state.Match.Affect, signal.Match)...)
		return []CommunicationAct{ActReact}, codes
	}
	if signal.Kind != SignalUserTurn || signal.User == nil {
		return nil, nil
	}
	text := strings.TrimSpace(signal.User.Text)
	// The talkativeness tier rides on every user turn; it feeds
	// InitiativeMode so the relationship view reflects the user's choice
	// instead of the previous permanent "natural" (the C2 drift fix).
	if signal.User.Talkativeness != "" {
		state.Relationship.Preferences.InitiativeMode = InitiativeModeForTalkativeness(signal.User.Talkativeness)
		if state.Match.Initiative.Mode == "" || state.Match.Initiative.Mode == "natural" {
			state.Match.Initiative.Mode = state.Relationship.Preferences.InitiativeMode
		}
	}
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
	// intent-router C2: a user insisting on a claim we already hold gets the
	// warm react — not the fresh-unverified react+disagree pushback, which
	// would read as if we had never heard the first attempt.
	if hasCue(cues, CueClaimPersisted) {
		return []CommunicationAct{ActReact}, []string{"claim_persisted_hold"}
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
	if signal.Grounding.Intent == "personal_share" {
		return []CommunicationAct{ActReact}, []string{"user_personal_share"}
	}
	// intent-router task 1.3: a routed unknown turn chats casually (caps
	// 2 句/60 字, ForbiddenClaims on) instead of the canned acknowledgement.
	// CasualChat is only set when the LLM router actually routed the turn, so
	// the legacy deterministic behaviour is untouched without a router key.
	if signal.Grounding.Intent == "unknown" && signal.Grounding.CasualChat {
		return []CommunicationAct{ActChat}, []string{"unknown_routed_casual"}
	}
	return []CommunicationAct{ActAcknowledge}, []string{"default_acknowledgement"}
}

// inferUserCues 只查 policy_vocabulary.go 的触发词表（deep-water-polish 1.1：
// 词表收敛单文件，此处保留判定结构）。
func inferUserCues(text string) []UserCue {
	var cues []UserCue
	if containsAnyTriggers(text, stablePreferenceTriggers) {
		cues = append(cues, UserCue{Kind: CueStablePreference})
	}
	if containsAnyTriggers(text, openThreadReadyTriggers) {
		cues = append(cues, UserCue{Kind: CueOpenThreadReady})
	} else if containsAnyTriggers(text, continuedThreadTriggers) {
		cues = append(cues, UserCue{Kind: CueContinuedThread})
	}
	if containsAllTriggers(text, banterDeniedRequiredTriggers[:]) {
		cues = append(cues, UserCue{Kind: CueBanterDenied})
	} else if containsAnyTriggers(text, banterAllowedTriggers) {
		cues = append(cues, UserCue{Kind: CueBanterAllowed, Scope: "prediction"})
	}
	if containsAnyTriggers(text, needsSilenceTriggers) {
		cues = append(cues, UserCue{Kind: CueNeedsSilence})
	}
	if containsAnyTriggers(text, acceptedJudgmentTriggers) {
		cues = append(cues, UserCue{Kind: CueAcceptedJudgment})
	}
	if containsAnyTriggers(text, acceptedInitiativeTriggers) {
		cues = append(cues, UserCue{Kind: CueAcceptedInitiative})
	}
	if (containsAnyTriggers(text, sharedMomentVerbTriggers) && containsAnyTriggers(text, sharedMomentObjectTriggers)) ||
		containsAnyTriggers(text, sharedMomentDirectTriggers) {
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
	// intent-router task 1.3: the casual chat act keeps the 2-sentence cap
	// and tightens the 80-char budget to 60; ForbiddenClaims stay on from the
	// defaults above.
	if hasAction(actions, ActChat) {
		policy.MaxCharacters = 60
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

// interactionFeedback 查 policy_vocabulary.go 的修复类别触发词。
func interactionFeedback(text string, cues []UserCue) (RepairState, bool) {
	if hasCue(cues, CueBanterDenied) || containsAnyTriggers(text, repairBanterBoundaryTriggers) {
		return RepairState{
			Active:          true,
			Category:        "banter_boundary",
			BehaviorChanges: []string{"disable_banter", "reduce_teasing", "reduce_initiative"},
		}, true
	}
	if containsAnyTriggers(text, repairRepetitionTriggers) {
		return RepairState{
			Active:          true,
			Category:        "repetition",
			BehaviorChanges: []string{"avoid_recent_phrases", "reduce_reassurance_templates", "reduce_questions"},
		}, true
	}
	if containsAnyTriggers(text, repairOverAnalysisTriggers) {
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
	return containsAnyTriggers(text, profanityBoundaryTriggers)
}

// isTacticalQuestion 查 policy_vocabulary.go：疑问引导词 + 战术主题词。
func isTacticalQuestion(text string) bool {
	if !containsAnyTriggers(text, tacticalQuestionTriggers) {
		return false
	}
	return containsAnyTriggers(text, tacticalSubjectTriggers)
}

func containsPersonalInsult(text string) bool {
	return containsAnyTriggers(text, personalInsultTriggers)
}

func hasBoundary(boundaries []UserBoundary, scope, rule string) bool {
	for _, boundary := range boundaries {
		if boundary.Scope == scope && boundary.Rule == rule {
			return true
		}
	}
	return false
}

// InferUserCues exposes the policy-table cue vocabulary to consumers that must
// classify user text with the exact same phrases (e.g. memory observation in
// backend/internal/memory) without duplicating the marker lists.
func InferUserCues(text string) []UserCue {
	return inferUserCues(text)
}

// applyMemoryBias（ADR-0019）：画像口味命中本场进球事件时放大 affect——
// 支持的队进球 +0.2、支持球员进球再 +0.15；未命中返回空 codes。偏置只动
// affect 与优先级，沉默/边界/引用码纪律原样。
func applyMemoryBias(affect *AffectState, match *MatchSignal) []string {
	if match == nil || match.EventType != "goal" {
		return nil
	}
	codes := []string{}
	eventTeam := strings.TrimSpace(match.TeamName)
	favorite := strings.TrimSpace(match.Memory.FavoriteTeam)
	if eventTeam != "" && favorite != "" && teamalign.Aligns(eventTeam, favorite) {
		affect.Valence = clamp(affect.Valence+0.2, -1, 1)
		codes = append(codes, "policy_memory:favorite_team_goal")
	}
	player := strings.TrimSpace(match.Memory.FavoritePlayer)
	eventPlayer := strings.TrimSpace(match.PlayerName)
	if eventPlayer != "" && player != "" && teamalign.Aligns(eventPlayer, player) {
		affect.Valence = clamp(affect.Valence+0.15, -1, 1)
		codes = append(codes, "policy_memory:favorite_player_scored")
	}
	return codes
}
