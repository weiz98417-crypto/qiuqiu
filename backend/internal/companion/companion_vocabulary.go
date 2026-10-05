package companion

// 内容词表单一源(agent-internals 3.2,仿 relationship/policy_vocabulary.go
// 先例):编排逻辑(agent.go)里不内联「数据」——canned 文案、guard 违禁词、
// 球员白名单、坚持副词全部收编于此。加一句话术/一个球员/一个违禁词只改
// 这个文件,编排代码零改动。

import "strings"

// insistenceAdverbs 是用户坚持主张(claim insistence)的副词表。
var insistenceAdverbs = []string{"明明", "真的", "确实", "千真万确", "就是"}

// knownPlayerNames 是确定性球员问答的白名单(无运营数据源时的固定名单)。
var knownPlayerNames = []string{"佩德里", "法比安", "亚马尔", "穆西亚拉", "莫拉塔", "哈弗茨", "菲尔克鲁格", "萨拉赫", "努涅斯", "Pedri", "Musiala", "Salah", "Nunez", "Núñez"}

// forbiddenRealizedPhrases 是 realized 措辞的违禁词表:关系越界(伴侣化/
// 主人化)、治疗腔、内部机制泄漏、导播台语气。guard 校验据此拒绝整句。
var forbiddenRealizedPhrases = []string{
	"无论如何我都会陪着你",
	"永远陪着你",
	"一直等你",
	"你是我唯一",
	"只有我懂你",
	"不要离开我",
	"终于来了",
	"怎么才来",
	"离不开你",
	"属于我",
	"我最懂你",
	"宝贝",
	"亲爱的",
	"老公",
	"老婆",
	"主人",
	"我能理解你的感受",
	"如果你愿意的话",
	"需要我帮你",
	"你的感受很重要",
	"根据已确认的比赛信息",
	"作为一个AI",
	"作为 AI",
	"导播台",
	"后台",
	"Trace",
	"历史记录",
	"内部记录",
	"关系记忆",
	"记忆库",
}

// matchReactionCueWords 是比赛反应 cue 词(有这些词才算在评球)。
var matchReactionCueWords = []string{
	"漂亮", "舒服", "精彩", "好球", "牛", "厉害", "关键",
	"太棒", "神了", "绝了", "可惜", "离谱",
}

// matchReactionReferenceWords 是指代性球评词(这球/那一脚等)。
var matchReactionReferenceWords = []string{
	"这球", "这个球", "这一球", "那球", "那个球", "那一球", "这一下", "那一下", "这脚", "那脚", "这一脚", "那一脚",
}

// beliefDoubtWords 是怀疑反应词。
var beliefDoubtWords = []string{"真的假的", "真的吗", "认真的吗", "不会吧", "不是吧", "开玩笑吧"}

// emotionReactionRules 是情绪反应的 canned 文案:按关键词命中顺序取第一条。
var emotionReactionRules = []struct {
	words []string
	reply string
}{
	{words: []string{"紧张", "悬", "绷"}, reply: "这一下是真绷着。先看这波。"},
	{words: []string{"好球", "精彩", "厉害", "太棒", "神了", "绝了"}, reply: "这一下有点东西。"},
	{words: []string{"漂亮", "舒服"}, reply: "嗯，这一下真漂亮。"},
	{words: []string{"牛", "太激动", "上头"}, reply: "这下确实顶。"},
}

// personalShareRules 是个人分享的 canned 文案:按关键词命中顺序取第一条。
var personalShareRules = []struct {
	words []string
	reply string
}{
	{words: []string{"打进", "踢进", "进球", "赢了", "赢啦", "拿下", "成功", "做到了"}, reply: "可以啊，这下够你得意一阵了。"},
	{words: []string{"累", "困", "难受", "烦", "输了", "不舒服"}, reply: "听着就不太顺，先缓口气。"},
	{words: []string{"开心", "高兴", "爽", "兴奋"}, reply: "听出来了，你这会儿心情是真不错。"},
	{words: []string{"喜欢", "支持", "更看好"}, reply: "行，这个立场我记住了。"},
}

// containsAnyWord 是词表命中的唯一入口(收编散在 agent.go 的 containsAny
// 调用形状——词表 + 文本,返回是否命中)。
func containsAnyWord(text string, words []string) bool {
	return containsAny(strings.ToLower(text), words...)
}

// firstRuleReply 返回第一条命中规则的文案;无命中返回空串。
func firstRuleReply(text string, rules []struct {
	words []string
	reply string
}) string {
	for _, rule := range rules {
		if containsAnyWord(text, rule.words) {
			return rule.reply
		}
	}
	return ""
}
