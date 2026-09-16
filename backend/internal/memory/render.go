package memory

import (
	"fmt"
	"strings"
	"time"
)

var portraitTopicLabels = map[string]string{
	"basic_info":            "基本信息",
	"preferences":           "偏好",
	"interaction_patterns":  "互动习惯",
	"companion_preferences": "陪伴偏好",
}

// portraitSubTopicLabels drives the 球球懂我 page titles; unknown slots fall
// through unchanged so new config slots still render without a deploy of the
// client.
var portraitSubTopicLabels = map[string]string{
	"favorite_team":      "支持球队",
	"favorite_player":    "最喜欢的球员",
	"league_interests":   "关注的联赛",
	"watching_habits":    "看球习惯",
	"timezone":           "所在时区",
	"analysis_appetite":  "分析口味",
	"banter_preference":  "调侃接受度",
	"banter_domains":     "可调侃范围",
	"reply_style":        "语气偏好",
	"topics_to_avoid":    "避开的话题",
	"emotional_triggers": "情绪爆点",
	"emotional_style":    "情绪表达",
	"prediction_habits":  "预测习惯",
	"recurring_topics":   "常聊话题",
	"feedback_history":   "反馈记录",
}

// PortraitTopicLabel maps a Memobase profile topic to its Chinese label.
func PortraitTopicLabel(topic string) string {
	if label, ok := portraitTopicLabels[topic]; ok {
		return label
	}
	return topic
}

// PortraitSubTopicLabel maps a profile slot to its Chinese label.
func PortraitSubTopicLabel(subTopic string) string {
	if label, ok := portraitSubTopicLabels[subTopic]; ok {
		return label
	}
	return subTopic
}

// RenderPortraitBlock renders the merged portrait entries as the bounded
// Chinese block injected into the realization context. The header restates
// the fact discipline: portrait content must never become a Match Fact claim.
// updatedAt is cited so prompts can tell fresh user edits from stale
// synthesis; zero omits the line. No entries renders as "" and the caller
// keeps 无.
func RenderPortraitBlock(entries []PortraitEntry, updatedAt time.Time) string {
	lines := make([]string, 0, len(entries)+1)
	total := len([]rune(portraitHeader))
	for _, entry := range entries {
		if len(lines) >= maxPortraitEntries || total >= maxPortraitRunes {
			break
		}
		content := strings.TrimSpace(entry.Content)
		if content == "" {
			continue
		}
		line := fmt.Sprintf("- %s/%s：%s", PortraitTopicLabel(entry.Topic), entry.SubTopic, content)
		if overflow := total + len([]rune(line)); overflow > maxPortraitRunes {
			break
		}
		total += len([]rune(line))
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return ""
	}
	if !updatedAt.IsZero() {
		stamp := "画像更新：" + updatedAt.UTC().Format("2006-01-02")
		if overflow := total + 1 + len([]rune(stamp)); overflow <= maxPortraitRunes {
			lines = append(lines, stamp)
		}
	}
	return portraitHeader + strings.Join(lines, "\n")
}

const portraitHeader = "【用户画像】长期综合记忆，仅供自然带过，不得据此新增赛况事实：\n"

// RenderRecallBlock renders top-k memories with per-line provenance citations,
// bounded so context assembly cost stays predictable. Degraded or empty recall
// yields "" and the caller keeps conversation.read_recent.
func RenderRecallBlock(recalls []Recall) string {
	if len(recalls) == 0 {
		return ""
	}
	var builder strings.Builder
	builder.WriteString(recallHeader)
	total := len([]rune(recallHeader))
	for _, recall := range recalls {
		content := strings.TrimSpace(recall.Content)
		if content == "" {
			continue
		}
		line := fmt.Sprintf("- %s（%s）", content, recall.Source)
		if overflow := total + len([]rune(line)); overflow > maxRecallBlockRunes {
			break
		}
		total += len([]rune(line))
		builder.WriteString("\n")
		builder.WriteString(line)
	}
	return builder.String()
}

const recallHeader = "【长期记忆】可能相关的过往，供自然引用，禁止当作赛况事实："
