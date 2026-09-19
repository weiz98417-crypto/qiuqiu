package relationship

import "strings"

// 策略触发词总表（openspec/changes/deep-water-polish 1.1）：relationship
// policy 全部中文触发词收敛在此一张表，词集逐字保留自原先散落的 5 个分类
// 器函数（inferUserCues / interactionFeedback / isProfanityBoundary /
// isTacticalQuestion / containsPersonalInsult）。分类器只查表——调语气不再
// 找五个函数，与 presentation_vocabulary.go 的词汇表惯例对齐。
//
// 行为等价约定：改这张表即改策略触发；改分类器逻辑必须保持查表语义。

// 稳定偏好（CueStablePreference）。
var stablePreferenceTriggers = []string{"我喜欢", "我支持", "我不喜欢", "我更吃"}

// 开放线程就绪（CueOpenThreadReady），优先于延续线程判定。
var openThreadReadyTriggers = []string{"刚才说的", "接着上次", "上次说"}

// 延续线程（CueContinuedThread，openThreadReady 未命中时的宽松回退）。
var continuedThreadTriggers = []string{"接着", "上次"}

// 拒绝调侃（CueBanterDenied）要求两个短语同时出现："别拿" + "开我玩笑"。
var banterDeniedRequiredTriggers = [2]string{"别拿", "开我玩笑"}

// 允许调侃（CueBanterAllowed），作用域固定为预测（scope "prediction"）。
var banterAllowedTriggers = []string{"毒奶"}

// 需要安静（CueNeedsSilence）。
var needsSilenceTriggers = []string{"不想分析", "缓会儿", "先别说话"}

// 接受判断（CueAcceptedJudgment）。
var acceptedJudgmentTriggers = []string{"判断挺准", "判断很准", "你说准了", "被你说中了", "你上次说得对"}

// 接受主动（CueAcceptedInitiative）。
var acceptedInitiativeTriggers = []string{"接着来", "继续提醒我", "继续这样", "就这么来", "有变化提醒我"}

// 共同回忆（CueSharedMomentRecalled）需要成对命中：回忆动词 + 回忆对象；
// 或直接命中完整短语兜底。
var sharedMomentVerbTriggers = []string{"想起", "记得"}
var sharedMomentObjectTriggers = []string{"上次那场", "那场", "上次那个球"}
var sharedMomentDirectTriggers = []string{"那个被吹掉的球", "上次那个绝杀"}

// interactionFeedback 修复类别：调侃边界（单命中"开我玩笑"即触发，与上面
// CueBanterDenied 的成对判定是两条不同规则，词面相同）。
var repairBanterBoundaryTriggers = []string{"开我玩笑"}

// interactionFeedback 修复类别：重复。
var repairRepetitionTriggers = []string{"你怎么老说", "又重复"}

// interactionFeedback 修复类别：过度分析。
var repairOverAnalysisTriggers = []string{"大道理", "分析太多", "太啰嗦"}

// 脏话边界（isProfanityBoundary）。
var profanityBoundaryTriggers = []string{"别说脏话", "别爆粗", "不要爆粗", "别说卧槽", "不喜欢你说脏话"}

// 战术提问：先命中疑问引导词，再命中战术主题词（isTacticalQuestion）。
var tacticalQuestionTriggers = []string{"为什么", "怎么", "换人"}
var tacticalSubjectTriggers = []string{"右路", "左路", "站位", "防线", "中场", "边后卫", "回收", "压迫"}

// 人身侮辱（containsPersonalInsult）。
var personalInsultTriggers = []string{"废物", "垃圾", "蠢货", "裁判瞎", "人没了"}

// containsAnyTriggers reports whether text contains any trigger verbatim.
func containsAnyTriggers(text string, triggers []string) bool {
	for _, trigger := range triggers {
		if strings.Contains(text, trigger) {
			return true
		}
	}
	return false
}

// containsAllTriggers reports whether text contains every trigger verbatim.
func containsAllTriggers(text string, triggers []string) bool {
	for _, trigger := range triggers {
		if !strings.Contains(text, trigger) {
			return false
		}
	}
	return true
}
