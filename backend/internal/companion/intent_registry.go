package companion

// 意图注册表（openspec/changes/intent-registry）：一个意图在一处声明全部
// 五件套——有序关键词管道步骤、router 枚举字符串、router prompt 意图定义行、
// 意图标记（fact/置信门/回复资格）、确定性 handler。此前散在 7+ 处靠人肉
// 同步（classify 词表、router 枚举与 prompt、schema、routedTurnIntent 映射、
// 意图大 switch、isFactIntent、词汇锁测试），漂移在启动校验与词汇锁即红。
//
// 校验式边界（ADR-0009 行为锁定）：router prompt 与枚举保持 router 包内
// 硬编码字节不动，注册表持镜像；ValidateIntentRegistry 在启动与 CI 断言
// 镜像一致。

import (
	"context"
	"fmt"
	"strings"

	"qiuqiu/internal/router"
)

// classifierStep 是关键词路径的一个有序判定步骤：Classify 按声明顺序遍历
// 管道，第一个命中的步骤拥有该回合。步骤顺序本身是行为（优先级交错跨
// 意图），与 classify.go 原判定序列逐字对应。
type classifierStep struct {
	Intent Intent
	Name   string
	Match  func(text string) bool
}

// IntentSpec 汇总一个意图在四个易漂移面上的声明。
type IntentSpec struct {
	Intent Intent

	// RouterIntent 是 router schema 枚举里的原始字符串；空 = 不可路由
	//（当前无此类用户回合意图，match_reaction 走别名表）。
	RouterIntent string

	// RouterPromptLine 是 router system prompt 里的意图定义行（不含
	// "- " 前缀）的镜像，供漂移校验；router prompt 本体字节不动。
	RouterPromptLine string

	// IsFact：置信门控的确定性事实类。
	IsFact bool

	// ReplyEligible：闲聊类，允许消费 router 的回复建议（design decision 4）。
	ReplyEligible bool

	// ConfidenceGated：确定性路径仅在置信 >= 0.7 时执行（locked decisions 4/5）。
	ConfidenceGated bool

	// Handle：用户回合的确定性 handler；每个用户回合意图必备。
	Handle userTurnHandler
}

// userTurn 承载原意图大 switch 各 case 闭包引用的回合内状态。
type userTurn struct {
	ctx            context.Context
	req            AgentBoundaryRequest
	requestTraceID string
	trace          *Trace
}

// intentHandling 是 handler 产出的确定性回复与后续策略标记——原 switch
// 各 case 写入的共享局部变量的结构化版本（默认值与原声明一致）。
type intentHandling struct {
	reply               string
	requiredAnchors     []string
	scheduleLookup      *ScheduleLookup
	allowRealize        bool
	deterministicReason string
	claimPersisted      bool
}

func newIntentHandling() intentHandling {
	return intentHandling{allowRealize: true, deterministicReason: "policy"}
}

// userTurnHandler 是统一的意图处理函数签名。原 switch 各 case 中的 `break`
// 一律改为携带已产出值的早返回。
type userTurnHandler func(a *Agent, t *userTurn) (intentHandling, error)

// routerIntentAliases：router 侧存在、但不作为用户回合意图直达的原始枚举。
// match_reaction 是主动回合专属（HandleMatchEvent），路由命中时落情绪路径
//（locked decision 4）。
var routerIntentAliases = map[string]Intent{
	"match_reaction": IntentEmotionReaction,
}

// intentRegistry 是 companion 包的意图单一声明点。
var intentRegistry = IntentRegistry{
	pipeline: []classifierStep{
		{IntentUnknown, "operator_edit_guard", matchesOperatorEditGuard},
		{IntentSmalltalk, "presence_check", isPresenceCheck},
		{IntentSmalltalk, "non_literal_match_aside", isNonLiteralMatchAside},
		{IntentMatchClaim, "fact_claim", isMatchFactClaimText},
		{IntentMatchStatus, "question_shaped_score", isQuestionShapedScoreText},
		{IntentControlCommand, "silence_request", matchesSilenceRequestCue},
		{IntentReminderRequest, "pre_match_reminder_cue", matchesPreMatchReminderCue},
		{IntentMatchStatus, "match_status_question", isMatchStatusQuestion},
		{IntentSchedule, "schedule_question", isScheduleQuestion},
		{IntentSmalltalk, "companion_directed_smalltalk", isCompanionDirectedSmalltalk},
		{IntentEmotionReaction, "disbelief_reaction", isDisbeliefReaction},
		{IntentEmotionReaction, "raw_emotion_cue", matchesRawEmotionCue},
		{IntentRecentEvent, "recent_goal_scorer_question", isRecentGoalScorerQuestion},
		{IntentPersonalShare, "personal_share", isPersonalShare},
		{IntentPlayerQuestion, "player_question_cue", matchesPlayerQuestionCue},
		{IntentRecentEvent, "recent_event_cue", matchesRecentEventCue},
		{IntentRecentEvent, "recent_event_combo_cue", matchesRecentEventComboCue},
		{IntentFollowUp, "follow_up_cue", matchesFollowUpCue},
		{IntentSmalltalk, "greeting", isGreeting},
		{IntentSmalltalk, "simple_social_turn", isSimpleSocialTurn},
	},
	specs: []IntentSpec{
		{
			Intent:           IntentSmalltalk,
			RouterIntent:     "smalltalk",
			RouterPromptLine: "smalltalk：陪你聊天、打招呼、问你在干嘛、让你陪着看球。",
			ReplyEligible:    true,
			Handle:           (*Agent).handleSmalltalk,
		},
		{
			Intent:           IntentSchedule,
			RouterIntent:     "schedule_question",
			RouterPromptLine: "schedule_question：问赛程、今天/明天有什么比赛。",
			ConfidenceGated:  true,
			Handle:           (*Agent).handleSchedule,
		},
		{
			Intent:           IntentMatchStatus,
			RouterIntent:     "match_status_question",
			RouterPromptLine: "match_status_question：问当前比分或比赛进行情况。",
			IsFact:           true,
			ConfidenceGated:  true,
			Handle:           (*Agent).handleMatchStatus,
		},
		{
			Intent:           IntentRecentEvent,
			RouterIntent:     "recent_event_question",
			RouterPromptLine: "recent_event_question：问刚才发生了什么（谁进的、上一球）。",
			IsFact:           true,
			ConfidenceGated:  true,
			Handle:           (*Agent).handleRecentEvent,
		},
		{
			Intent:           IntentFollowUp,
			RouterIntent:     "follow_up_question",
			RouterPromptLine: "follow_up_question：顺着上一条追问（那球呢、然后呢、谁策动）。",
			IsFact:           true,
			ConfidenceGated:  true,
			Handle:           (*Agent).handleFollowUp,
		},
		{
			Intent:           IntentPlayerQuestion,
			RouterIntent:     "player_question",
			RouterPromptLine: "player_question：问某个球员的表现或进球。",
			IsFact:           true,
			ConfidenceGated:  true,
			Handle:           (*Agent).handlePlayerQuestion,
		},
		{
			Intent:           IntentMatchClaim,
			RouterIntent:     "match_fact_claim",
			RouterPromptLine: "match_fact_claim：用户主张一个比赛事实（进球/比分/破门）。用户坚持主张（明明进了、真的进了、确实进了）也算 match_fact_claim。",
			IsFact:           true,
			ConfidenceGated:  true,
			Handle:           (*Agent).handleMatchClaim,
		},
		{
			Intent:           IntentEmotionReaction,
			RouterIntent:     "emotion_reaction",
			RouterPromptLine: "emotion_reaction：看球情绪反应（漂亮、牛、紧张、离谱、真的假的）。",
			ReplyEligible:    true,
			Handle:           (*Agent).handleEmotionReaction,
		},
		{
			Intent:           IntentPersonalShare,
			RouterIntent:     "personal_share",
			RouterPromptLine: "personal_share：用户分享自己的生活（我赢了、我累了、我喜欢某队）。",
			ReplyEligible:    true,
			Handle:           (*Agent).handlePersonalShare,
		},
		{
			Intent:           IntentControlCommand,
			RouterIntent:     "control_command",
			RouterPromptLine: "control_command：让球球别说/少说/安静。",
			ConfidenceGated:  true,
			Handle:           (*Agent).handleControlCommand,
		},
		{
			Intent:           IntentReminderRequest,
			RouterIntent:     "reminder_request",
			RouterPromptLine: "reminder_request：让我在开球前提醒你（开球前叫我、赛前提醒我）。",
			ConfidenceGated:  true,
			Handle:           (*Agent).handleReminderRequest,
		},
		{
			Intent:           IntentUnknown,
			RouterIntent:     "unknown",
			RouterPromptLine: "unknown：以上都不适合。",
			ReplyEligible:    true,
			Handle:           (*Agent).handleUnknownTurn,
		},
	},
}

// IntentRegistry 聚合分类管道与意图规格，提供查表与漂移校验。
type IntentRegistry struct {
	pipeline []classifierStep
	specs    []IntentSpec
}

// classify 按声明顺序遍历分类管道；空文本与未命中照旧落 Unknown。
func (r *IntentRegistry) classify(text string) Intent {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return IntentUnknown
	}
	lower := strings.ToLower(trimmed)
	for _, step := range r.pipeline {
		if step.Match(lower) {
			return step.Intent
		}
	}
	return IntentUnknown
}

// specFor 返回意图规格；未知意图防御性落 Unknown 规格（启动校验保证
// Unknown spec 存在，这里不递归）。
func (r *IntentRegistry) specFor(intent Intent) IntentSpec {
	for _, spec := range r.specs {
		if spec.Intent == intent {
			return spec
		}
	}
	for _, spec := range r.specs {
		if spec.Intent == IntentUnknown {
			return spec
		}
	}
	return IntentSpec{Intent: IntentUnknown, Handle: (*Agent).handleUnknownTurn}
}

// routeIntent 把 router 的原始枚举映射到后端意图（locked decision 4 的
// 1:1 映射 + match_reaction 别名），未收录落 Unknown。
func (r *IntentRegistry) routeIntent(raw string) Intent {
	trimmed := strings.TrimSpace(raw)
	for _, spec := range r.specs {
		if spec.RouterIntent == trimmed {
			return spec.Intent
		}
	}
	if intent, ok := routerIntentAliases[trimmed]; ok {
		return intent
	}
	return IntentUnknown
}

// handle 按 IntentSpec 派发用户回合的确定性处理。
func (r *IntentRegistry) handle(a *Agent, intent Intent, t *userTurn) (intentHandling, error) {
	return r.specFor(intent).Handle(a, t)
}

// routableIntentsInOrder 返回注册表声明的可路由枚举（声明序即枚举序）。
func (r *IntentRegistry) routableIntentsInOrder() []string {
	declared := make([]string, 0, len(r.specs))
	for _, spec := range r.specs {
		if spec.RouterIntent != "" {
			declared = append(declared, spec.RouterIntent)
		}
	}
	return declared
}

// Validate 断言注册表与 router 包硬编码事实的镜像一致（漂移即红）：
// 枚举 1:1 同序、prompt 行按序出现、管道步骤引用已知意图、每个用户回合
// 规格都有 handler、RouterIntent 无重复且不含主动回合专属的 match_reaction。
func (r *IntentRegistry) Validate() error {
	seenIntent := make(map[Intent]bool, len(r.specs))
	seenRouterIntent := make(map[string]bool, len(r.specs))
	for _, spec := range r.specs {
		if spec.Intent == "" {
			return fmt.Errorf("intent registry has a spec with empty Intent")
		}
		if seenIntent[spec.Intent] {
			return fmt.Errorf("intent %q declared more than once", spec.Intent)
		}
		seenIntent[spec.Intent] = true
		if spec.Handle == nil {
			return fmt.Errorf("intent %q has no user-turn handler", spec.Intent)
		}
		if spec.RouterIntent == "" {
			continue
		}
		if spec.RouterIntent == "match_reaction" {
			return fmt.Errorf("match_reaction is proactive-only and must not be a routable user-turn intent")
		}
		if seenRouterIntent[spec.RouterIntent] {
			return fmt.Errorf("router intent %q declared more than once", spec.RouterIntent)
		}
		seenRouterIntent[spec.RouterIntent] = true
		if spec.RouterPromptLine == "" {
			return fmt.Errorf("routable intent %q is missing its router prompt line", spec.Intent)
		}
	}
	for _, step := range r.pipeline {
		if !seenIntent[step.Intent] {
			return fmt.Errorf("classify step %q references undeclared intent %q", step.Name, step.Intent)
		}
		if step.Match == nil {
			return fmt.Errorf("classify step %q has no matcher", step.Name)
		}
	}
	// ADR-0009 locked decisions 4/5 的不变式：事实类必须置信门控；且
	// Unknown spec 必须存在（specFor 的防御性兜底依赖它）。
	if unknownSeen := seenIntent[IntentUnknown]; !unknownSeen {
		return fmt.Errorf("intent registry is missing the %q spec", IntentUnknown)
	}
	for _, spec := range r.specs {
		if spec.IsFact && !spec.ConfidenceGated {
			return fmt.Errorf("intent %q is fact-class but not confidence-gated (ADR-0009)", spec.Intent)
		}
	}
	declared := r.routableIntentsInOrder()
	routable := router.RoutableIntents()
	if len(declared) != len(routable) {
		return fmt.Errorf("registry declares %d routable intents, router schema offers %d", len(declared), len(routable))
	}
	for i := range routable {
		if declared[i] != routable[i] {
			return fmt.Errorf("routable intent order drift at %d: registry %q vs router %q", i, declared[i], routable[i])
		}
	}
	prompt := router.SystemPrompt()
	last := -1
	for _, spec := range r.specs {
		if spec.RouterPromptLine == "" {
			continue
		}
		idx := strings.Index(prompt, spec.RouterPromptLine)
		if idx < 0 {
			return fmt.Errorf("router prompt is missing the intent line for %q (prompt/registry drift)", spec.Intent)
		}
		if idx <= last {
			return fmt.Errorf("router prompt line for %q appears out of declared order", spec.Intent)
		}
		last = idx
	}
	return nil
}

// ValidateIntentRegistry 是启动与 CI 的漂移断言入口。
func ValidateIntentRegistry() error {
	return intentRegistry.Validate()
}
