package pipeline

import "strings"

// ClassifyIntent returns the intent category of user speech.
func ClassifyIntent(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return "ignore"
	}

	lower := strings.ToLower(text)

	// Commands
	if matchAny(lower, "安静", "闭嘴", "别说了", "少说", "小声", "大声", "换一场", "换台", "切换") {
		return "command"
	}

	// Questions (fact-seeking)
	if matchAny(lower, "谁进", "谁进的", "比分", "多少比", "几比", "还有多久", "什么时候", "哪个队", "什么情况",
		"会进吗", "能进吗", "会赢吗", "能赢吗", "厉害吗", "强吗", "是谁") {
		return "question"
	}

	// Praise
	if matchAny(lower, "好球", "漂亮", "进了", "太棒", "精彩", "牛", "厉害", "神了", "nice", "goal") || strings.HasPrefix(text, "哇") {
		return "praise"
	}

	// Complaint
	if matchAny(lower, "这都不进", "裁判", "越位", "假摔", "什么鬼", "无语", "服了", "离谱", "垃圾", "菜") {
		return "complain"
	}

	// Nonsense / filler
	if len([]rune(text)) <= 2 || matchAny(lower, "额", "嗯", "啊", "哦", "呃", "唔", "诶") {
		return "ignore"
	}

	return "chat"
}

func matchAny(text string, patterns ...string) bool {
	for _, p := range patterns {
		if strings.Contains(text, p) {
			return true
		}
	}
	return false
}
