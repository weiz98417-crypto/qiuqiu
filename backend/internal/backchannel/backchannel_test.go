package backchannel

import (
	"strings"
	"testing"
	"time"

	"qiuqiu/internal/relationship"
	"qiuqiu/internal/tts"
)

func TestDecideWhitelistAndLimits(t *testing.T) {
	now := time.Date(2026, 9, 22, 20, 0, 0, 0, time.UTC)
	state := &State{}

	// 白名单外：永不发。
	if _, ok := Decide(state, "goal", "first_half", "normal", false, now); ok {
		t.Fatal("goal must go through the full turn pipeline, not backchannel")
	}

	// 半场 ≤3 + 风暴去重（同刻连发被 10s 去重挡下）。
	emitted := 0
	quarter := now
	for i := 0; i < 6; i++ {
		if _, ok := Decide(state, "big_chance", "first_half", "normal", false, quarter); ok {
			emitted++
		}
		quarter = quarter.Add(15 * time.Second) // 越过 10s 风暴去重
	}
	if emitted != 3 {
		t.Fatalf("emitted %d in one half, want capped at %d", emitted, MaxPerHalf)
	}

	// 下半场计数重置，但全场上限 6 仍生效。
	secondHalf := now.Add(2 * time.Hour)
	next := secondHalf
	total := 0
	for i := 0; i < 6; i++ {
		if _, ok := Decide(state, "miss", "second_half", "normal", false, next); ok {
			total++
		}
		next = next.Add(15 * time.Second)
	}
	if total != 3 {
		t.Fatalf("emitted %d in second half, want capped at %d", total, MaxPerHalf)
	}
	if state.fullCount != MaxPerMatch {
		t.Fatalf("full count = %d, want %d", state.fullCount, MaxPerMatch)
	}

	// 全场打满后：不再发。
	if _, ok := Decide(state, "save", "second_half", "normal", false, next); ok {
		t.Fatal("match cap must hold")
	}
}

func TestDecideQuietAndSpeaking(t *testing.T) {
	state := &State{}
	now := time.Now()
	if _, ok := Decide(state, "save", "first_half", QuietTier, false, now); ok {
		t.Fatal("quiet tier must disable backchannel")
	}
	if _, ok := Decide(state, "save", "first_half", "normal", true, now); ok {
		t.Fatal("user speaking must yield")
	}
}

func TestDecidePhrasesStayShortAndRotate(t *testing.T) {
	state := &State{}
	base := time.Date(2026, 9, 22, 20, 0, 0, 0, time.UTC)
	seen := map[string]bool{}
	for i := 0; i < 2; i++ {
		verdict, ok := Decide(state, "miss", "first_half", "normal", false, base.Add(time.Duration(i)*15*time.Second))
		if !ok {
			t.Fatalf("turn %d blocked unexpectedly", i)
		}
		if len([]rune(verdict.Phrase)) > 10 {
			t.Fatalf("phrase %q exceeds 10 runes", verdict.Phrase)
		}
		if seen[verdict.Phrase] {
			t.Fatalf("phrase %q repeated on consecutive emits", verdict.Phrase)
		}
		seen[verdict.Phrase] = true
		if verdict.Expression == "" || verdict.Motion == "" {
			t.Fatalf("verdict missing presentation: %+v", verdict)
		}
		if !strings.Contains(verdict.EventType, "miss") {
			t.Fatalf("event type = %q", verdict.EventType)
		}
	}
}

// v1.1 短 TTS（ADR-0016 留尾）：Verdict 携带事件折算的 Affect State——
// 险球/神扑落激动档、丢机落遗憾档、VAR 落紧张档（与短语内容同源，配音
// 不与文字打架）；且短语 ≤10 字必然咬合 InstructionFor 的紧凑尾巴，
// 音频一闪而过。
func TestVerdictAffectNamesEmotionAndEngagesCompactTail(t *testing.T) {
	wantAffect := map[string]relationship.AffectState{
		"big_chance": {Arousal: 0.70, Valence: 0.35},
		"save":       {Arousal: 0.72, Valence: 0.45},
		"miss":       {Arousal: 0.20, Valence: -0.40},
		"var_check":  {Arousal: 0.45, Valence: 0.10, Tension: 0.60},
	}
	wantBandCell := map[string]string{
		"big_chance": "即时的情绪爆发",
		"save":       "即时的情绪爆发",
		"miss":       "失落感",
		"var_check":  "紧张而专注",
	}
	base := time.Date(2026, 9, 22, 20, 0, 0, 0, time.UTC)
	for eventType, want := range wantAffect {
		verdict, ok := Decide(&State{}, eventType, "first_half", "normal", false, base)
		if !ok {
			t.Fatalf("%s blocked unexpectedly", eventType)
		}
		if verdict.Affect != want {
			t.Fatalf("%s affect = %+v, want %+v", eventType, verdict.Affect, want)
		}
		instruction := tts.InstructionFor(verdict.Affect, relationship.ActReact, len([]rune(verdict.Phrase)))
		if !strings.Contains(instruction, wantBandCell[eventType]) {
			t.Fatalf("%s affect lands in wrong band: %q lacks %q", eventType, instruction, wantBandCell[eventType])
		}
		if !strings.Contains(instruction, "反应一闪而过") {
			t.Fatalf("%s instruction missing compact tail: %q", eventType, instruction)
		}
	}
}
