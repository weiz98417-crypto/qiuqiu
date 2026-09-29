package speech

import (
	"reflect"
	"strings"
	"testing"
)

func assertSentences(t *testing.T, name string, got []string, want ...string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s: got %#v, want %#v", name, got, want)
	}
}

// 整段一次 Feed：句尾标点逐句切出，缓冲末尾的标点也是边界（整段文本
// 模式下分片终点即已知终点），Flush 收零。
func TestFeedWholeTextSplitsBySentence(t *testing.T) {
	agg := NewAggregator()
	got := agg.Feed("进球了！VAR 在看回放。你说呢?")
	assertSentences(t, "feed", got, "进球了！", "VAR 在看回放。", "你说呢?")
	if tail := agg.Flush(); tail != "" {
		t.Fatalf("flush = %q, want empty", tail)
	}
}

// 中文引号内的句尾标点归属句内，引号闭合才出句。
func TestQuoteScopedPunctuation(t *testing.T) {
	agg := NewAggregator()
	got := agg.Feed("他说「进球了！」然后欢呼。")
	assertSentences(t, "feed", got, "他说「进球了！」", "然后欢呼。")
	if tail := agg.Flush(); tail != "" {
		t.Fatalf("flush = %q, want empty", tail)
	}

	// 引号内多个标点、全角引号与括号嵌套同样归属句内。
	agg = NewAggregator()
	got = agg.Feed("解说喊道：“这球进了！太不可思议？”全场(都站起来)沸腾了。")
	assertSentences(t, "nested", got, "解说喊道：“这球进了！太不可思议？”", "全场(都站起来)沸腾了。")
}

// 中英混排与小数点：数字后的 ASCII '.' 不切（比分/小数/版本号）。
func TestDigitDotNotSentenceBoundary(t *testing.T) {
	agg := NewAggregator()
	got := agg.Feed("比分 2.0 变 3.5 了！Still v2.5 model! Nice.")
	assertSentences(t, "feed", got, "比分 2.0 变 3.5 了！", "Still v2.5 model!", "Nice.")
	if tail := agg.Flush(); tail != "" {
		t.Fatalf("flush = %q, want empty", tail)
	}

	// 宽松代价的自觉锁定：英文句点紧跟数字（如 2:2.）不切，与后文粘连。
	// 这是任务口径「数字后的 . 不切」的刻意取舍，不是 bug。
	agg = NewAggregator()
	got = agg.Feed("现在是 2:2. Unbelievable!")
	assertSentences(t, "loose", got, "现在是 2:2. Unbelievable!")
}

// 连续标点成游程：只在游程最后一个标点出句（？!、!! 是一句）。
func TestConsecutiveTerminatorsStayOneSentence(t *testing.T) {
	agg := NewAggregator()
	got := agg.Feed("真的吗？！进了！！")
	assertSentences(t, "feed", got, "真的吗？！", "进了！！")

	// 省略号游程同理。
	agg = NewAggregator()
	got = agg.Feed("等一下……好吧。")
	assertSentences(t, "ellipsis", got, "等一下……", "好吧。")
}

// 分号按句尾标点切分（全半角一致）。
func TestSemicolonSplits(t *testing.T) {
	agg := NewAggregator()
	got := agg.Feed("传球；射门！")
	assertSentences(t, "fullwidth", got, "传球；", "射门！")
	agg = NewAggregator()
	got = agg.Feed("传中; 头球!")
	assertSentences(t, "halfwidth", got, "传中;", "头球!")
}

// 无标点尾句与单句回复退化为整段：Feed 不产出，Flush 才出。
func TestNoPunctuationTailFlushes(t *testing.T) {
	agg := NewAggregator()
	if got := agg.Feed("好 一分钟补时"); got != nil {
		t.Fatalf("feed = %#v, want nil", got)
	}
	if tail := agg.Flush(); tail != "好 一分钟补时" {
		t.Fatalf("flush = %q, want the tail sentence", tail)
	}

	agg = NewAggregator()
	if got := agg.Feed("你好呀"); got != nil {
		t.Fatalf("single reply feed = %#v, want nil", got)
	}
	if tail := agg.Flush(); tail != "你好呀" {
		t.Fatalf("single reply flush = %q, want the whole utterance", tail)
	}
}

// 空串与纯空白不产出，也不进残余。
func TestEmptyAndWhitespaceProduceNothing(t *testing.T) {
	agg := NewAggregator()
	if got := agg.Feed(""); got != nil {
		t.Fatalf("empty feed = %#v, want nil", got)
	}
	if got := agg.Feed("   \n\t "); got != nil {
		t.Fatalf("whitespace feed = %#v, want nil", got)
	}
	if tail := agg.Flush(); tail != "" {
		t.Fatalf("flush = %q, want empty", tail)
	}
}

// 安全阀：无标点长文到上限强制切，残余照常 Flush。上限可配置。
func TestSafetyValveForceCut(t *testing.T) {
	agg := NewAggregatorWithLimit(10)
	got := agg.Feed(strings.Repeat("好", 25))
	assertSentences(t, "cut", got, strings.Repeat("好", 10), strings.Repeat("好", 10))
	if tail := agg.Flush(); tail != strings.Repeat("好", 5) {
		t.Fatalf("flush = %d runes, want 5", len([]rune(tail)))
	}

	// 默认上限 120：121 个无标点字符在 120 处切。
	agg = NewAggregator()
	got = agg.Feed(strings.Repeat("啊", DefaultMaxSentenceRunes+1))
	assertSentences(t, "default cut", got, strings.Repeat("啊", DefaultMaxSentenceRunes))
	if tail := agg.Flush(); tail != "啊" {
		t.Fatalf("flush = %q, want single rune", tail)
	}

	// 非正上限回落默认，零值滥用不至于把安全阀拆了。
	if agg := NewAggregatorWithLimit(0); agg.maxRunes != DefaultMaxSentenceRunes {
		t.Fatalf("limit 0 = %d, want default", agg.maxRunes)
	}
}

// 分片多次 Feed 与整段一次 Feed 得到同样的句序列（分片边界不影响切分）。
func TestIncrementalFeedsMatchWholeFeed(t *testing.T) {
	whole := "他说「进球了！」然后欢呼。比分 2:0。"
	aggWhole := NewAggregator()
	want := append([]string{}, aggWhole.Feed(whole)...)
	if tail := aggWhole.Flush(); tail != "" {
		want = append(want, tail)
	}

	agg := NewAggregator()
	var got []string
	for _, piece := range []string{"他说「进球", "了！」然后", "欢呼。比分 ", "2:0。"} {
		got = append(got, agg.Feed(piece)...)
	}
	if tail := agg.Flush(); tail != "" {
		got = append(got, tail)
	}
	assertSentences(t, "incremental", got, want...)
}

// Flush 后聚合器归零，可复用于下一段文本。
func TestFlushResetsForReuse(t *testing.T) {
	agg := NewAggregator()
	if got := agg.Feed("第一句。"); !reflect.DeepEqual(got, []string{"第一句。"}) {
		t.Fatalf("first round = %#v", got)
	}
	if tail := agg.Flush(); tail != "" {
		t.Fatalf("first flush = %q", tail)
	}
	got := agg.Feed("第二回合")
	if got != nil {
		t.Fatalf("second round feed = %#v, want nil", got)
	}
	if tail := agg.Flush(); tail != "第二回合" {
		t.Fatalf("second round flush = %q", tail)
	}
}
