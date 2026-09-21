package knowledge

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type stubEmbedder struct{ vector []float32 }

func (s stubEmbedder) Embed(_ context.Context, text string) ([]float32, error) { return s.vector, nil }

// topicEmbedder 按文本定向量：含「越位/犯规」的主题聚在一簇，其余在另一簇
//——模拟真实 embedding 的语义分簇。
type topicEmbedder struct{}

func (topicEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	if strings.Contains(text, "越位") || strings.Contains(text, "犯规") {
		return []float32{1, 0, 0}, nil
	}
	return []float32{0, 1, 0}, nil
}

func writeLibrary(t *testing.T, embedder Embedder) *Library {
	t.Helper()
	dir := t.TempDir()
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	write("rule-offside.yaml", `id: rule-offside
topics: ["越位"]
answer: "传球一瞬间比对方最后一名防守球员更靠近球门线就算越位。"
source: "IFAB Law 11"
confidence: 0.95
`)
	write("low-conf.yaml", `id: low-conf
topics: ["玄学"]
answer: "这个说不清。"
source: "道听途说"
confidence: 0.3
`)
	library, err := Load(dir, embedder)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return library
}

func TestSearchKeywordHit(t *testing.T) {
	library := writeLibrary(t, nil)
	entry, ok := library.Search(context.Background(), "越位到底是什么意思啊")
	if !ok || entry.ID != "rule-offside" {
		t.Fatalf("entry = %+v, ok = %v", entry, ok)
	}
	if !strings.Contains(entry.Answer, "越位") {
		t.Fatalf("answer = %q", entry.Answer)
	}
}

func TestSearchNoHitReturnsFalse(t *testing.T) {
	library := writeLibrary(t, nil)
	if _, ok := library.Search(context.Background(), "今天天气怎么样"); ok {
		t.Fatal("irrelevant query must not hit")
	}
}

func TestSearchLowConfidenceGuarded(t *testing.T) {
	library := writeLibrary(t, nil)
	if _, ok := library.Search(context.Background(), "玄学到底是什么"); ok {
		t.Fatal("low-confidence entry must be guarded")
	}
}

func TestSearchVectorPathWithEmbedder(t *testing.T) {
	// 向量路：关键词全落空时，固定向量的 stub 让条目以余弦 1.0 命中，
	// 证明换说法仍能到达条目（contains 做不到的部分）。
	library := writeLibrary(t, topicEmbedder{})
	entry, ok := library.Search(context.Background(), "传球的时候人在后面算不算犯规")
	if !ok || entry.ID != "rule-offside" {
		t.Fatalf("entry = %+v, ok = %v, want vector hit on rule-offside", entry, ok)
	}
}
