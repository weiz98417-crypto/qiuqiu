package companion

import (
	"regexp"
	"strings"
)

// Classify 走 intent_registry 的有序分类管道（步骤顺序即原判定顺序）；
// 谓词函数体留在本文件不动。
func Classify(text string) Intent {
	return intentRegistry.classify(text)
}

// matchesOperatorEditGuard：运营台改分手令不是用户回合，强制 Unknown。
func matchesOperatorEditGuard(lower string) bool {
	return containsAny(lower, "把比分改成", "比分改成", "修改比分", "记录进球", "记一条进球")
}

// isMatchFactClaimText：比分主张或事件主张（C2 检测入口）。
func isMatchFactClaimText(lower string) bool {
	return isScoreClaim(lower) || isEventClaim(lower)
}

// isQuestionShapedScoreText：比分数字 + 疑问语气 = 提问不是主张。
func isQuestionShapedScoreText(lower string) bool {
	return scoreClaimPattern.FindStringSubmatch(lower) != nil && containsAny(lower, "吗", "么", "是不是", "?", "？")
}

// matchesSilenceRequestCue：让球球闭嘴/少说的控制指令。
func matchesSilenceRequestCue(lower string) bool {
	return containsAny(lower, "别说", "少说", "闭嘴", "安静", "别播报")
}

// matchesRawEmotionCue：裸情绪词或比赛反应线索。
func matchesRawEmotionCue(lower string) bool {
	return containsAny(lower, "哈哈", "太激动", "紧张", "上头") || containsMatchReactionCue(lower)
}

// matchesPlayerQuestionCue：问某球员进球/表现。
func matchesPlayerQuestionCue(lower string) bool {
	return containsAny(lower, "进球了吗", "表现", "有没有进球", "有进球")
}

// matchesRecentEventCue：问刚发生的事（助攻/上一个/刚刚）。
func matchesRecentEventCue(lower string) bool {
	return containsAny(lower, "谁助攻", "谁主攻", "助攻", "刚才谁", "上一个", "刚刚")
}

// matchesRecentEventComboCue：「最新/刚才/上一球」+「进球/破门/比赛」组合。
func matchesRecentEventComboCue(lower string) bool {
	return containsAny(lower, "最新", "刚才", "上一球") && containsAny(lower, "进球", "破门", "比赛")
}

// matchesFollowUpCue：顺着上一条的追问。
func matchesFollowUpCue(lower string) bool {
	return containsAny(lower, "谁策动", "策动", "谁传的", "谁参与", "那球呢", "然后呢")
}

func isScheduleQuestion(text string) bool {
	normalized := normalizeConversationText(text)
	if containsAny(normalized, "比分", "进球", "分钟", "赛况", "球员", "比赛怎么样", "比赛什么情况", "比赛现在什么情况", "现在什么情况") {
		return false
	}
	footballTopic := containsAny(normalized, "比赛", "球赛", "赛程", "对阵", "足球", "有球", "什么球", "啥球", "哪些球")
	questionAct := containsAny(normalized, "有", "什么", "哪些", "哪场", "哪几场", "安排", "踢", "开赛")
	return footballTopic && questionAct
}

func ClassifyScheduleIntent(text string) ScheduleIntent {
	normalized := normalizeConversationText(text)
	scope := ScheduleScopeNearby
	switch {
	case containsAny(normalized, "明天", "明日"):
		scope = ScheduleScopeTomorrow
	case containsAny(normalized, "今天", "今日", "今晚"):
		scope = ScheduleScopeToday
	case containsAny(normalized, "现在", "当前", "正在"):
		scope = ScheduleScopeCurrent
	}
	return ScheduleIntent{
		Topic:      "football_schedule",
		Action:     "query",
		Scope:      scope,
		Confidence: 0.9,
	}
}

func isSimpleSocialTurn(text string) bool {
	normalized := normalizeConversationText(text)
	if normalized == "" || containsAny(strings.ToLower(strings.TrimSpace(text)),
		"吗", "么", "是不是", "为什么", "怎么", "什么", "哪", "多少", "?", "？",
	) {
		return false
	}
	for _, exact := range []string{
		"好", "好的", "好呀", "好啊", "好吧", "行", "行吧", "嗯", "嗯嗯", "收到", "谢谢", "谢了", "多谢",
		"继续", "接着", "你说", "看球", "累", "困", "难受", "烦", "不舒服", "开心", "高兴", "爽", "兴奋",
	} {
		if normalized == exact {
			return true
		}
	}
	return hasAnyPrefix(normalized,
		"谢谢", "谢了", "多谢", "继续", "接着", "先看", "看球", "收到", "辛苦", "你说",
		"陪我看", "陪我聊", "一起看", "接着看", "继续看", "累", "困", "难受", "烦", "不舒服",
		"开心", "高兴", "爽", "兴奋",
	)
}

func isNonLiteralMatchAside(text string) bool {
	return isNonLiteralMatchTalk(text) && (scoreClaimPattern.FindStringSubmatch(text) != nil || containsAny(text,
		"进球了", "破门了", "得分了", "进啦", "进咯", "进喽", "球进了",
	))
}

func isMatchStatusQuestion(text string) bool {
	return containsAny(text,
		"几比几", "比分", "现在多少", "现在几",
		"比赛什么情况", "比赛现在什么情况", "比赛怎么样", "赛况", "比赛时间",
		"踢到第几分钟", "进行到第几分钟", "第几分钟了", "多少分钟了",
	)
}

func isRecentGoalScorerQuestion(text string) bool {
	normalized := strings.ToLower(strings.TrimSpace(text))
	hasScorerQuestion := containsAny(normalized,
		"谁", "哪个球员", "哪位球员", "哪一个球员", "进球者",
	)
	hasGoalAction := containsAny(normalized,
		"进了", "进啦", "进咯", "进喽", "进的", "进球", "破门", "打进", "得分",
	)
	return hasScorerQuestion && hasGoalAction
}

func isPersonalShare(text string) bool {
	normalized := strings.ToLower(strings.TrimSpace(text))
	if containsAny(normalized,
		"吗", "么", "是不是", "为什么", "怎么", "谁", "什么", "哪", "多少", "几", "?", "？",
	) {
		return false
	}
	return isFirstPersonGoalAchievement(normalized) || containsAny(normalized,
		"我刚", "我今天", "我昨天", "我也", "我赢", "我输", "我累", "我困", "我开心", "我高兴",
		"我喜欢", "我支持", "我更看好", "我感觉", "我觉得", "我状态", "我们刚", "我们今天", "我们赢", "我们输",
	) || isAffectShare(normalized)
}

func isAffectShare(text string) bool {
	if containsAny(text, "你", "球球", "比赛", "球员", "球队", "场上") {
		return false
	}
	return containsAny(text, "我", "今天", "最近", "这会儿", "现在", "刚刚") && containsAny(text,
		"累", "困", "难受", "烦", "不舒服", "开心", "高兴", "爽", "兴奋",
	)
}

func isFirstPersonGoalAchievement(text string) bool {
	normalized := strings.ToLower(strings.TrimSpace(text))
	return containsAny(normalized,
		"我打进", "我踢进", "我进球", "我刚进", "我也进", "我们打进", "我们踢进", "我们进球",
	)
}

var scoreClaimPattern = regexp.MustCompile(`(\d{1,2})\s*(?:比|:|：|-)\s*(\d{1,2})`)

func isScoreClaim(text string) bool {
	if scoreClaimPattern.FindStringSubmatch(text) == nil {
		return false
	}
	if isNonLiteralMatchTalk(text) {
		return false
	}
	return !containsAny(text, "吗", "么", "是不是", "多少", "几比几", "?", "？")
}

// insistenceAdverbs generalizes the C2 claim detection: a 坚持副词
// (明明/真的/确实/千真万确/就是) plus a goal suffix counts as a claim —
// colloquial insistence variants ("明明进了") used to fall through to
// Unknown and the canned reply. First-person achievement and hypothetical
// exclusions are unchanged.
func isEventClaim(text string) bool {
	if isFirstPersonGoalAchievement(text) {
		return false
	}
	claimWords := containsAny(text, "进球了", "破门了", "得分了", "进啦", "进咯", "进喽", "球进了")
	if !claimWords && containsAny(text, insistenceAdverbs...) && containsAny(text, "进了", "破门", "得分") {
		claimWords = true
	}
	if !claimWords {
		return false
	}
	if isNonLiteralMatchTalk(text) {
		return false
	}
	return !containsAny(text, "谁", "吗", "么", "是不是", "有没有", "?", "？")
}

func isNonLiteralMatchTalk(text string) bool {
	return containsAny(text, "要是", "如果", "假如", "假设", "希望", "但愿", "梦里", "梦到", "做梦", "开玩笑", "逗你的", "说着玩", "比如说")
}

func containsMatchFactLanguage(text string) bool {
	return scoreClaimPattern.FindStringSubmatch(text) != nil || containsAny(text,
		"比分", "进球", "进啦", "进咯", "进喽", "球进了", "破门", "得分", "领先", "扳平", "反超", "红牌", "黄牌", "VAR", "var",
		"分钟", "换人", "换下", "换上", "上场", "下场", "首发", "替补", "点球", "判罚", "越位", "半场", "终场", "开场",
		"梅开二度", "帽子戏法", "伤退", "受伤", "停赛", "绝杀", "绝平", "助攻", "扑救", "扑出", "射门", "射正", "犯规",
	)
}
