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

// A3 双路融合:关键词路与向量路并行计分融合——原「关键词一票短路」被替换,
// 以下锁定新检索排序的三个方向性断言。

func TestSearchVectorOnlyHitWithoutKeywordMatch(t *testing.T) {
	// 查询不含「越位」字面,但语义向量聚在同一簇——原实现关键词路零分即
	// 弃权,新融合下向量路独立入选。
	library := writeLibrary(t, topicEmbedder{})
	entry, ok := library.Search(context.Background(), "传球在对方防线身后那种球算不算犯规")
	if !ok {
		t.Fatalf("vector-only hit lost after fusion")
	}
	if entry.ID != "rule-offside" {
		t.Fatalf("entry = %q, want rule-offside", entry.ID)
	}
}

func TestSearchAnswerBodyParticipatesInRetrieval(t *testing.T) {
	// 检索面扩展:查询词不出现在 topics,但出现在答案原文——正文参与打分。
	library := writeLibrary(t, nil)
	entry, ok := library.Search(context.Background(), "防守球员更靠近球门线")
	if !ok {
		t.Fatalf("answer-body hit lost")
	}
	if entry.ID != "rule-offside" {
		t.Fatalf("entry = %q, want rule-offside (answer body retrieval)", entry.ID)
	}
}

func TestSearchKeywordBeatsVectorOnTie(t *testing.T) {
	// 双路同分时关键词路优先(声明序),且融合分零 = 无命中不变。
	library := writeLibrary(t, topicEmbedder{})
	if _, ok := library.Search(context.Background(), "完全无关的句子"); ok {
		t.Fatalf("zero-total query must not hit")
	}
	entry, ok := library.Search(context.Background(), "越位")
	if !ok || entry.ID != "rule-offside" {
		t.Fatalf("keyword path = %v/%q, want rule-offside", ok, entry.ID)
	}
}

func TestSetCosThresholdAdjustsVectorGate(t *testing.T) {
	library := writeLibrary(t, topicEmbedder{})
	// 阈值抬到不可能的高度:向量路弃权,关键词路照常。
	library.SetCosThreshold(1.5)
	entry, ok := library.Search(context.Background(), "越位")
	if !ok || entry.ID != "rule-offside" {
		t.Fatalf("keyword path with raised threshold = %v/%q", ok, entry.ID)
	}
}
