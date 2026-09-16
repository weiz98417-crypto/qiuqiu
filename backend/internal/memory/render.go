package memory

import (
	"fmt"
	"strings"
)

var portraitTopicLabels = map[string]string{
	"basic_info":          "基本信息",
	"preferences":         "偏好",
	"interaction_patterns": "互动习惯",
	"companion_preferences": "陪伴偏好",
}

// RenderPortraitBlock renders profile entries as the bounded Chinese block
// injected into the realization context. The header restates the fact
// discipline: portrait content must never become a Match Fact claim.
func RenderPortraitBlock(entries []profileEntry) string {
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
		line := fmt.Sprintf("- %s/%s：%s", topicLabel(entry.Attributes.Topic), entry.Attributes.SubTopic, content)
		if overflow := total + len([]rune(line)); overflow > maxPortraitRunes {
			break
		}
		total += len([]rune(line))
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return ""
	}
	return portraitHeader + strings.Join(lines, "\n")
}

const portraitHeader = "【用户画像】长期综合记忆，仅供自然带过，不得据此新增赛况事实：\n"

func topicLabel(topic string) string {
	if label, ok := portraitTopicLabels[topic]; ok {
		return label
	}
	return topic
}

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
