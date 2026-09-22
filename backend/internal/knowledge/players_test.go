package knowledge

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 球员/球队档案（openspec/changes/knowledge-players）：递归加载子目录、
// 队名/人名 topic 命中、换说法向量命中。

type clubStubEmbedder struct{}

func (clubStubEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	if strings.Contains(text, "巴黎") || strings.Contains(text, "欧冠") {
		return []float32{1, 0, 0}, nil
	}
	return []float32{0, 1, 0}, nil
}

func writePlayersLibrary(t *testing.T, embedder Embedder) *Library {
	t.Helper()
	dir := t.TempDir()
	rules := filepath.Join(dir, "rules")
	players := filepath.Join(dir, "players")
	if err := os.MkdirAll(rules, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(players, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(path, content string) {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(rules, "rule-offside.yaml"), `id: rule-offside
topics: ["越位"]
answer: "越位的答案。"
source: "IFAB"
confidence: 0.95
`)
	write(filepath.Join(players, "team-psg.yaml"), `id: team-psg
topics: ["巴黎圣日耳曼", "大巴黎", "PSG"]
answer: "巴黎圣日耳曼 1970 年成立，法甲 14 冠，2025 年首夺欧冠、2026 年卫冕。"
source: "wikipedia"
confidence: 0.9
`)
	write(filepath.Join(players, "player-mbappe.yaml"), `id: player-mbappe
topics: ["姆巴佩", "Mbappé"]
answer: "姆巴佩是法国前锋，现在效力皇家马德里。"
source: "zh.wikipedia.org"
confidence: 0.95
`)
	library, err := Load(dir, embedder)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return library
}

func TestPlayersLibraryLoadsRecursively(t *testing.T) {
	library := writePlayersLibrary(t, nil)
	if library.Size() != 3 {
		t.Fatalf("size = %d, want 3 (rules + players recursive)", library.Size())
	}
}

func TestPlayerTopicKeywordHit(t *testing.T) {
	library := writePlayersLibrary(t, nil)
	entry, ok := library.Search(context.Background(), "姆巴佩现在在哪个队")
	if !ok || entry.ID != "player-mbappe" {
		t.Fatalf("entry = %+v, ok = %v", entry, ok)
	}
}

func TestClubAliasVectorHit(t *testing.T) {
	// 关键词全落空（"大巴黎"不在查询里，查询说"巴黎的那支队"）时由
	// 向量路兜底——文本感知 stub 让 PSG 主题与查询同簇、姆巴佩异簇。
	library := writePlayersLibrary(t, clubStubEmbedder{})
	entry, ok := library.Search(context.Background(), "巴黎的那支队夺冠了吗")
	if !ok || entry.ID != "team-psg" {
		t.Fatalf("entry = %+v, ok = %v, want vector hit on team-psg", entry, ok)
	}
}
