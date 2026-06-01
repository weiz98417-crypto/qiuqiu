package pipeline

import (
	"strings"
	"time"
	"unicode"
)

// SentenceSplitter detects sentence boundaries in streaming LLM output.
// Best-effort: splits on Chinese/English punctuation, with 2s timeout fallback.
type SentenceSplitter struct {
	buf       strings.Builder
	timeout   time.Duration
	lastFlush time.Time
}

func NewSentenceSplitter() *SentenceSplitter {
	return &SentenceSplitter{timeout: 2 * time.Second, lastFlush: time.Now()}
}

// Feed adds a token chunk. Returns a complete sentence if a boundary is found.
func (s *SentenceSplitter) Feed(chunk string) string {
	s.buf.WriteString(chunk)
	s.lastFlush = time.Now()

	text := s.buf.String()
	for i, r := range text {
		if isSentenceBoundary(r) {
			sentence := strings.TrimSpace(text[:i+len(string(r))-1])
			if len([]rune(sentence)) > 0 {
				s.buf.Reset()
				s.buf.WriteString(strings.TrimSpace(text[i+len(string(r)):]))
				s.lastFlush = time.Now()
				return sentence
			}
		}
	}
	return ""
}

// ForceFlush returns accumulated text if timeout exceeded, or empty.
func (s *SentenceSplitter) ForceFlush() string {
	if time.Since(s.lastFlush) > s.timeout {
		text := strings.TrimSpace(s.buf.String())
		if len([]rune(text)) > 0 {
			s.buf.Reset()
			s.lastFlush = time.Now()
			return text
		}
	}
	return ""
}

// FlushAll returns all remaining text.
func (s *SentenceSplitter) FlushAll() string {
	text := strings.TrimSpace(s.buf.String())
	s.buf.Reset()
	return text
}

func isSentenceBoundary(r rune) bool {
	switch r {
	case '。', '！', '？', '!', '?', '\n':
		return true
	case '…', '～', '~', '.', ',', '，':
		return false
	default:
		return unicode.Is(unicode.Terminal_Punctuation, r)
	}
}
