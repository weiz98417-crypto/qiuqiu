// Package knowledge 是知识域（第二事实域，ADR-0017）：repo 策展条目的
// 加载与检索。条目原文即答案锚——本包只做「检索命中」，绝不生成事实；
// embedding 缺席时自动退关键词单路（与语义记忆同一降级哲学）。
package knowledge

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// MinConfidence 低于它的条目不出答案——「不知道」好过「不太对」。
const MinConfidence = 0.6

// Embedder 是检索侧嵌入能力接缝（internal/embedding.Client 结构性满足）。
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

type Entry struct {
	ID          string    `yaml:"id" json:"id"`
	Topics      []string  `yaml:"topics" json:"topics"`
	Answer      string    `yaml:"answer" json:"answer"`
	Source      string    `yaml:"source" json:"source"`
	Confidence  float64   `yaml:"confidence" json:"confidence"`
	EffectiveAt time.Time `yaml:"effective_at" json:"effective_at,omitempty"`
}

type Library struct {
	entries  []Entry
	embedder Embedder

	mu        sync.Mutex
	topicVecs map[int][]float32 // 惰性嵌条目主题（topics 以空格相连）
}

// Load 读取目录下全部 *.yaml 条目；embedder 可为 nil（纯关键词检索）。
func Load(dir string, embedder Embedder) (*Library, error) {
	items, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("knowledge dir: %w", err)
	}
	library := &Library{embedder: embedder, topicVecs: map[int][]float32{}}
	for _, item := range items {
		if item.IsDir() || !strings.HasSuffix(item.Name(), ".yaml") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, item.Name()))
		if err != nil {
			return nil, fmt.Errorf("knowledge read %s: %w", item.Name(), err)
		}
		var entry Entry
		if err := yaml.Unmarshal(raw, &entry); err != nil {
			return nil, fmt.Errorf("knowledge parse %s: %w", item.Name(), err)
		}
		if entry.ID == "" || strings.TrimSpace(entry.Answer) == "" || len(entry.Topics) == 0 {
			return nil, fmt.Errorf("knowledge entry %s missing id/topics/answer", item.Name())
		}
		library.entries = append(library.entries, entry)
	}
	return library, nil
}

func (l *Library) Size() int {
	if l == nil {
		return 0
	}
	return len(l.entries)
}

// Search 返回最匹配的条目：关键词路（双向 contains，命中数计分）优先，
// 向量路补换说法（余弦 ≥ 0.55）；confidence 低于阈值视同无命中。
func (l *Library) Search(ctx context.Context, query string) (Entry, bool) {
	if l == nil || len(l.entries) == 0 {
		return Entry{}, false
	}
	normalized := strings.ToLower(strings.TrimSpace(query))
	if normalized == "" {
		return Entry{}, false
	}

	best := -1
	bestScore := 0
	for i, entry := range l.entries {
		score := 0
		for _, topic := range entry.Topics {
			topic = strings.ToLower(strings.TrimSpace(topic))
			if topic == "" {
				continue
			}
			if strings.Contains(normalized, topic) || strings.Contains(topic, normalized) {
				score++
			}
		}
		if score > bestScore {
			bestScore = score
			best = i
		}
	}
	if best >= 0 && bestScore > 0 {
		return l.guard(l.entries[best])
	}

	if l.embedder == nil {
		return Entry{}, false
	}
	queryCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	queryVector, err := l.embedder.Embed(queryCtx, normalized)
	if err != nil {
		return Entry{}, false
	}
	bestVec := -1
	bestCos := 0.0
	for i, entry := range l.entries {
		vector, ok := l.topicVector(i, entry)
		if !ok {
			continue
		}
		if cos := cosine(queryVector, vector); cos > bestCos {
			bestCos = cos
			bestVec = i
		}
	}
	if bestVec >= 0 && bestCos >= 0.55 {
		return l.guard(l.entries[bestVec])
	}
	return Entry{}, false
}

// guard 应用确信度阈值：低确信条目视同不知道。
func (l *Library) guard(entry Entry) (Entry, bool) {
	if entry.Confidence < MinConfidence {
		return Entry{}, false
	}
	return entry, true
}

func (l *Library) topicVector(index int, entry Entry) ([]float32, bool) {
	l.mu.Lock()
	if vector, ok := l.topicVecs[index]; ok {
		l.mu.Unlock()
		return vector, true
	}
	embedder := l.embedder
	l.mu.Unlock()
	if embedder == nil {
		return nil, false
	}
	// 嵌入在锁外执行：持锁做网络 IO 会串行化并发查询并放大延迟。
	embedCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	text := strings.Join(entry.Topics, " ")
	vector, err := embedder.Embed(embedCtx, text)
	if err != nil {
		return nil, false
	}
	l.mu.Lock()
	l.topicVecs[index] = vector
	l.mu.Unlock()
	return vector, true
}

func cosine(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}
