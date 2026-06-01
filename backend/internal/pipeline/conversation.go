package pipeline

// ConversationContext tracks recent dialog turns for multi-turn coherence.
type ConversationContext struct {
	turns   []ConversationTurn
	maxTurns int
}

type ConversationTurn struct {
	Role   string // "user" | "qiuqiu" | "event"
	Text   string
	Minute int
}

func NewConversationContext(maxTurns int) *ConversationContext {
	return &ConversationContext{maxTurns: maxTurns}
}

func (c *ConversationContext) Add(role, text string, minute int) {
	c.turns = append(c.turns, ConversationTurn{Role: role, Text: text, Minute: minute})
	if len(c.turns) > c.maxTurns {
		c.turns = c.turns[1:]
	}
}

// Recent returns the last N turns for LLM prompt injection.
func (c *ConversationContext) Recent(n int) []ConversationTurn {
	start := len(c.turns) - n
	if start < 0 {
		start = 0
	}
	return c.turns[start:]
}

// UserTurns returns only user turns from the context.
func (c *ConversationContext) UserTurns() int {
	count := 0
	for _, t := range c.turns {
		if t.Role == "user" {
			count++
		}
	}
	return count
}
