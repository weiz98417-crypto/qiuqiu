package pipeline

import (
	"fmt"
	"qiuqiu/internal/llm"
	"strings"
	"sync"
	"text/template"
)

// PromptManager loads and caches prompt templates.
type PromptManager struct {
	mu       sync.RWMutex
	system   string
	templates map[string]*template.Template
}

func NewPromptManager() *PromptManager {
	return &PromptManager{
		templates: make(map[string]*template.Template),
	}
}

// LoadSystem sets the system persona prompt.
func (pm *PromptManager) LoadSystem(text string) {
	pm.mu.Lock()
	pm.system = text
	pm.mu.Unlock()
}

// LoadTemplate registers a named prompt template.
func (pm *PromptManager) LoadTemplate(name, text string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	tmpl, err := template.New(name).Parse(text)
	if err != nil {
		return err
	}
	pm.templates[name] = tmpl
	return nil
}

// BuildPrompt assembles the full prompt for an event.
func (pm *PromptManager) BuildPrompt(inst *AIGenerationInstruction, userCtx UserContext) ([]Message, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	// Select event template
	tmplName := templateName(inst.Event.Type)
	tmpl, ok := pm.templates[tmplName]
	if !ok {
		tmpl = pm.templates["shot"] // fallback
	}

	var eventPrompt strings.Builder
	data := buildTemplateData(inst, userCtx)
	if err := tmpl.Execute(&eventPrompt, data); err != nil {
		return nil, fmt.Errorf("template: %w", err)
	}

	messages := []Message{
		{Role: "system", Content: pm.system},
	}

	// User context (sanitized, wrapped in separator)
	if userCtx.Nickname != "" {
		userInfo := fmt.Sprintf("[用户信息]\n昵称：%s\n主队：%s\n[/用户信息]", userCtx.Nickname, userCtx.FavoriteTeam)
		messages = append(messages, Message{Role: "system", Content: userInfo})
	}

	messages = append(messages, Message{Role: "user", Content: eventPrompt.String()})
	return messages, nil
}

func templateName(eventType string) string {
	switch eventType {
	case "goal", "penalty":
		return "goal"
	case "shot":
		return "shot"
	case "yellow_card", "red_card":
		return "card"
	case "match_start", "match_end":
		return "match_status"
	default:
		return "shot"
	}
}

func buildTemplateData(inst *AIGenerationInstruction, userCtx UserContext) map[string]string {
	d := map[string]string{
		"team_name":    inst.Event.Team,
		"player_name":  inst.Event.Player.Name,
		"minute":       itoa(inst.Event.Minute),
		"home_team":    inst.Event.Team,
		"away_team":    inst.Event.Team,
		"score":        inst.MatchContext.ScoreAfter,
		"significance": inst.MatchContext.Significance,
		"score_line":   "",
		"card_type":    cardDisplayName(inst.Event.Type),
		"status":       inst.Event.Type,
		"result":       inst.Event.Type,
	}
	if inst.MatchContext.ScoreAfter != "0-0" {
		d["score_line"] = "，比分 " + inst.MatchContext.ScoreAfter
	}
	if userCtx.FavoriteTeam == inst.Event.Team {
		d["is_home"] = "true"
	}
	return d
}

// UserContext is sanitized user info for prompt injection.
func cardDisplayName(t string) string {
	switch t {
	case "yellow_card":
		return "黄牌"
	case "red_card":
		return "红牌"
	default:
		return t
	}
}

type UserContext struct {
	Nickname     string
	FavoriteTeam string
}

// System returns the system persona prompt.
func (pm *PromptManager) System() string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.system
}

// BuildReplyMessages creates chat messages for user speech → LLM reply.
func BuildReplyMessages(systemPrompt, text, intent string) []llm.Message {
	return []llm.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: fmt.Sprintf("用户%s说：%s\n请根据语境简短回复", intentLabel(intent), text)},
	}
}

func intentLabel(intent string) string {
	switch intent {
	case "question":
		return "在问"
	case "command":
		return "命令"
	case "praise":
		return "在夸"
	case "complain":
		return "在吐槽"
	default:
		return ""
	}
}

// Message is a chat message for LLM API.
type Message struct {
	Role    string
	Content string
}
