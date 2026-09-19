package companion

import (
	"strings"

	"qiuqiu/internal/relationship"
)

// guardValidateReply 是两条回复路径（realizer 输出与意图路由器的建议回复）
// 共用的护栏：句数/字数上限、ForbiddenClaims、锚定纪律（锚源统一含
// requiredAnchors）、反重复。一个 module 定义「放行」与「拒绝」，两条路径
// 的判定一致、拒绝可见（openspec/changes/router-trace-durability）。
//
// 返回 (通过后的候选, "") 或 ("", 拒绝原因)：ReasonGuardEmpty（候选为空或
// 决策禁言）或 ReasonGuardRejected（违反护栏）。
func guardValidateReply(input string, intent Intent, candidate string, anchors []string, reliable string, decision relationship.Decision) (string, string) {
	text := strings.TrimSpace(candidate)
	if text == "" || decision.Speech == nil {
		return "", ReasonGuardEmpty
	}
	allowedSource := strings.Join(compactAnchors(input, reliable, strings.Join(anchors, " ")), " ")
	if err := validateRealizedText(text, allowedSource, decision); err != nil {
		return "", ReasonGuardRejected
	}
	if err := validateRealizedConversationTurn(input, intent, text); err != nil {
		return "", ReasonGuardRejected
	}
	return text, ""
}
