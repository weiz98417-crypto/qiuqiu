package pipeline

import (
	"regexp"
	"strings"
)

var (
	mdFenceRe   = regexp.MustCompile("```|~~~")
	jsonFragRe  = regexp.MustCompile(`[{}\[\]"]`)
	blacklistRe = regexp.MustCompile(`(?i)(脏话|赌博|政治|宗教|种族|fuck|shit)`)
	absRe       = regexp.MustCompile(`(?i)(必[赢输]|肯定[赢输]|绝对)`)
)

// ValidateLLMOutput checks LLM output for safety and quality.
func ValidateLLMOutput(text string) (bool, string) {
	text = strings.TrimSpace(text)

	// Length check
	if len([]rune(text)) == 0 || len([]rune(text)) > 200 {
		return false, "length"
	}

	// Format check
	if mdFenceRe.MatchString(text) || jsonFragRe.MatchString(text) {
		return false, "format"
	}

	// Safety blacklist
	if blacklistRe.MatchString(text) {
		return false, "safety"
	}

	return true, ""
}

// Sanitize for safety: replace absolutes with softer expressions.
func Sanitize(text string) string {
	text = absRe.ReplaceAllStringFunc(text, func(m string) string {
		switch {
		case strings.Contains(m, "必赢") || strings.Contains(m, "肯定赢"):
			return "有很大优势"
		case strings.Contains(m, "必输") || strings.Contains(m, "肯定输"):
			return "不太乐观"
		default:
			return m
		}
	})
	return text
}

// SoftMatch checks if two short Chinese texts are semantically similar using character n-gram simhash.
func SoftMatch(a, b string) bool {
	return simhash(a) == simhash(b)
}
