package companion

// Trace 字段名单一源(trace-genai-alignment):ToolCall 的 Name 值曾是散在
// 十个文件的字符串字面量——加一个工具要同步 trace 构造点与 boundary.go 的
// schema 镜像两处。本文件是唯一允许出现这些字面量的地方(由
// tracefields_test.go 守卫,新增裸字面量会红)。
//
// gen_ai 语义映射见 docs/design/trace-genai-alignment.md:toolCalls[].name
// ↔ OTel GenAI 的 gen_ai.tool.name;语音延迟锚点(speech_received/asr_final
// /turn_decided/tts_synthesized/audio_delivered,voice_stages.go)保留领域
// 名不映射——生态语音段约定未 stable,不为对齐而对齐。
const (
	ToolCallCharacterSettingMirror     = "character.setting_mirror"
	ToolCallConversationAppendTurn     = "conversation.append_turn"
	ToolCallConversationReadRecent     = "conversation.read_recent"
	ToolCallIntentRoute                = "intent.route"
	ToolCallKnowledgeAnswer            = "knowledge.answer"
	ToolCallKnowledgeTrigger           = "knowledge.trigger"
	ToolCallKnowledgeTriggerFallback   = "knowledge.trigger_fallback"
	ToolCallMatchGetPlayerTimeline     = "match.get_player_timeline"
	ToolCallMatchReadSnapshot          = "match.read_snapshot"
	ToolCallMatchSearchEvents          = "match.search_events"
	ToolCallMatchVerifyUserClaim       = "match.verify_user_claim"
	ToolCallMemoryAppendThread         = "memory.append_thread"
	ToolCallMemoryPortrait             = "memory.portrait"
	ToolCallMemoryRecall               = "memory.recall"
	ToolCallMemoryRecoverThread        = "memory.recover_thread"
	ToolCallObservationRecord          = "observation.record"
	ToolCallRelationshipApply          = "relationship.apply"
	ToolCallRelationshipGoalComfort    = "relationship.goal_comfort"
	ToolCallReminderCreate             = "reminder.create"
	ToolCallReminderDuplicate          = "reminder.duplicate"
	ToolCallResponseEmitCompanionReply = "response.emit_companion_reply"
	ToolCallScheduleReadToday          = "schedule.read_today"
	ToolCallScheduleSearch             = "schedule.search"
	ToolCallSubscriptionCancel         = "subscription.cancel"
	ToolCallSubscriptionCreate         = "subscription.create"
	ToolCallTraceWriteDecision         = "trace.write_decision"
)
