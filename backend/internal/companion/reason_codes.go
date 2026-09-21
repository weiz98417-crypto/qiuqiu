package companion

// trace Reason 词汇表（openspec/changes/router-trace-durability）：每一条
// trace.Reason 的码值在这里定义一次，赋值点引用常量——码值不变（历史 trace
// 仍可读），新增码先进这张表。引用审计与引用审计前端按码值展示。

const (
	// 用户回合：比赛事实主张 / 事件引用路径。
	ReasonUserMatchClaimPrefix   = "user_match_claim_"
	ReasonUserEventReferencePrfx = "user_event_reference_"
	ReasonClaimPersistedHold     = "claim_persisted_hold"
	ReasonMatchSnapshotInc       = "match_snapshot_inconsistent"
	ReasonMatchNotStarted        = "match has not started"
	ReasonActiveMatchContext     = "active_match_context"
	ReasonNoRecentConfirmedEvent = "no recent confirmed match event"
	ReasonMatchedLatestEvent     = "matched latest confirmed match event"

	// 赛程查询路径。
	ReasonScheduleUnavailable      = "schedule_unavailable"
	ReasonScheduleLookupAck        = "schedule_lookup_acknowledgement"
	ReasonScheduleLookupCtxUpdated = "schedule_lookup_context_updated"
	ReasonScheduleLookupUnavail    = "schedule_lookup_unavailable"
	ReasonScheduleLookupResult     = "schedule_lookup_result"

	// 主动回合路径。
	ReasonRelationshipMatchReaction = "relationship_match_reaction"
	ReasonCriticalFactRefreshLimit  = "critical_fact_refresh_limit"
	ReasonOperatorEventProactive    = "operator_event_proactive_line"
	ReasonMatchObservedSilent       = "relationship_match_observed_silent"
	ReasonFirstMeetingDelivered     = "first_meeting_already_delivered"

	// 回合收尾：用户回合管线（HandleBoundaryRequest）。
	ReasonRelationshipChosenSilence = "relationship_chosen_silence"
	ReasonRouterReplyRealized       = "router_reply_realized"
	ReasonRealizeFallbackError      = "realize_fallback_error"
	ReasonRealizeFallbackPolicy     = "realize_fallback_policy"
	ReasonRealizeFallbackUnavail    = "realize_fallback_unavailable"
	ReasonDeterministicCompanion    = "deterministic_companion_policy"
	ReasonDeterministicPrefix       = "deterministic_"

	// realizeReply 通过全部护栏后盖的章（router 自然化判定依赖它）。
	ReasonRelationshipPlanRealized = "relationship_plan_realized"

	// 记忆进措辞层（openspec/changes/memory-into-turns）：带记忆重措辞的
	// 主动回合盖的章（仅 recall 材料非空时可能触发）。
	ReasonProactiveMemoryRealized = "proactive_memory_realized"

	// 提醒簿落账（openspec/changes/proactive-scheduler，ADR-0015）。
	ReasonReminderScheduled = "reminder_scheduled"
)

// ReasonGuardRejected / ReasonGuardEmpty 是护栏拒绝的两类原因，落在
// RouterTrace.RejectReason（ADR-0009：分得清「模型没给建议」和「被护栏
// 拦截」）。
const (
	ReasonGuardRejected = "policy"
	ReasonGuardEmpty    = "empty"
)
