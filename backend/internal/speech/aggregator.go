// Package speech 承载语音链路的文本切分原语（句聚合）。
//
// 语义同源：Aggregator 是 pipecat SentenceAggregator（pipecat/
// aggregators/sentence.py，BSD-2）与 stream2sentence（MIT）的算法思想在
// Go 上的重写——只抄语义不抄代码（BSD-2/MIT 均无传染），并按 qiuqiu 的
// 现实做了三处宽松化：
//
//   - 数字后的 ASCII '.' 不切（小数/比分/版本号，如 2.0、v2.5）；
//   - 中文引号内的句尾标点归属句内，引号闭合才出句（「进球了！」是
//     一句，不以引号内的 ！ 切分）；
//   - 缓冲超过上限（默认 120 字符）强制切句，防 realizer 抽风产出无
//     标点长文堵死流水线。
//
// qiuqiu 用法（voice-streaming-delivery Phase A）：realizer 产出的是整段
// 文本（80 token 预算，非流式），对整段一次性 Feed 即可——聚合器按句切
// 分、把凑齐的完整句逐个返回，供逐句流式 TTS；文本结束时必须 Flush() 收
// 残余（无句尾标点的尾句、单句回复都靠它出）。Feed 的分片边界不影响切
// 分结果：整段一次 Feed 与多次增量 Feed 得到同样的句序列（终态一致）。
//
// 典型调用（投递层 task 3.4/3.5 接线参考）：
//
//	agg := speech.NewAggregator()
//	for _, sentence := range agg.Feed(replyText) { synthesize(sentence) }
//	if tail := agg.Flush(); tail != "" { synthesize(tail) }
package speech

import "strings"

// DefaultMaxSentenceRunes 是安全阀默认值（rune 计）：缓冲里的待出句超过
// 这个长度还没有句尾标点，就整体强制切出。realizer 80 token 预算下正常
// 句远短于此；它只该在文本异常（无标点长文）时兜底触发。
const DefaultMaxSentenceRunes = 120

// quotePairs 是引号/括号配对：key 是开引号，value 是对应闭引号。句尾
// 标点落在配对内（深度 > 0）时归属句内；ASCII 直引号 '"' 配对歧义
// （吋标/撇号），不纳入。全角字符用码点转义书写，防编辑链路归一化。
var quotePairs = map[rune]rune{
	'「':      '」',
	'『':      '』',
	'“':      '”',
	'‘':      '’',
	'(':      ')',
	'\uFF08': '\uFF09', // （）
	'《':      '》',
}

// closeQuotes 是闭引号集合（由 quotePairs 派生，保持单一事实源）。
var closeQuotes = func() map[rune]bool {
	closers := make(map[rune]bool, len(quotePairs))
	for _, closer := range quotePairs {
		closers[closer] = true
	}
	return closers
}()

// Aggregator 按句聚合文本流：句尾标点（。!?;…及对应全半角）凑齐即出句，
// 引号内标点归属句内，无标点残余由 Flush 收出。零值不可直接用，经
// NewAggregator / NewAggregatorWithLimit 构造。非并发安全：同一文本流的
// Feed/Flush 须在同一线程（qiuqiu 内是逐回合的单一投递 goroutine）。
type Aggregator struct {
	buf      string
	maxRunes int
}

// NewAggregator 以默认安全阀上限（DefaultMaxSentenceRunes）构造。
func NewAggregator() *Aggregator {
	return &Aggregator{maxRunes: DefaultMaxSentenceRunes}
}

// NewAggregatorWithLimit 以自定义安全阀上限（rune 计）构造；非正值回落
// 默认。qiuqiu 现役用 NewAggregator 即可，此构造器留给真机调参。
func NewAggregatorWithLimit(maxRunes int) *Aggregator {
	if maxRunes <= 0 {
		maxRunes = DefaultMaxSentenceRunes
	}
	return &Aggregator{maxRunes: maxRunes}
}

// Feed 追加一段文本，返回已凑齐的完整句切片（可能为 nil）。空串/纯空白
// 不产出任何句。返回的句已去首尾空白。
func (a *Aggregator) Feed(chunk string) []string {
	a.buf += chunk
	return a.drain()
}

// Flush 收残余：缓冲里没等到句尾标点的文本（无标点尾句、单句回复）整体
// 出，返回值已去首尾空白、可能为空串。调用方在文本流结束时必须调用；
// Flush 后聚合器归零，可复用于下一段文本。
func (a *Aggregator) Flush() string {
	rest := strings.TrimSpace(a.buf)
	a.buf = ""
	return rest
}

// drain 扫描缓冲切出完整句。每次全量重扫缓冲（缓冲受安全阀约束，长度
// 有界，代价可忽略）：引号深度、标点游程等状态全部从缓冲自身重推导，
// 分片边界不影响结果。
func (a *Aggregator) drain() []string {
	runes := []rune(a.buf)
	var sentences []string
	start := 0
	var quoteDepth []rune // 开引号栈（宽松配对：闭引号就近弹栈）
	pendingClose := false // 引号内已见句尾标点，等闭引号出句
	prev := rune(0)
	emit := func(end int) {
		if sentence := strings.TrimSpace(string(runes[start:end])); sentence != "" {
			sentences = append(sentences, sentence)
		}
		start = end
	}
	for i, r := range runes {
		switch {
		case quotePairs[r] != 0:
			quoteDepth = append(quoteDepth, quotePairs[r])
		case closeQuotes[r]:
			for j := len(quoteDepth) - 1; j >= 0; j-- {
				if quoteDepth[j] == r {
					quoteDepth = append(quoteDepth[:j], quoteDepth[j+1:]...)
					break
				}
			}
			// 引号闭合且句尾标点落在引号内：出句到闭引号为止
			// （「进球了！」是一句）。
			if len(quoteDepth) == 0 && pendingClose {
				emit(i + 1)
				pendingClose = false
			}
		case isSentenceTerminator(r, prev):
			if len(quoteDepth) == 0 {
				// 标点游程（？!、……）只在最后一个标点出句；
				// 缓冲末尾的标点即边界（整段文本模式：分片终点
				// 就是已知终点，不必像流式那样保守等下一片）。
				if i+1 >= len(runes) || !isSentenceTerminator(runes[i+1], r) {
					emit(i + 1)
					pendingClose = false
				}
			} else {
				pendingClose = true
			}
		}
		prev = r
		// 安全阀：待出句达到上限强制切，防无标点长文堵死流水线。强制
		// 切点之后引号深度已不可信，宽松重置。
		if i+1-start >= a.maxRunes {
			emit(i + 1)
			pendingClose = false
			quoteDepth = quoteDepth[:0]
		}
	}
	a.buf = string(runes[start:])
	return sentences
}

// isSentenceTerminator 判定 r 是否句尾标点（。!?;…及对应全半角）。宽松
// 处理：数字后的 ASCII '.' 是小数点/比分/版本号，不算句号；全角 。 无此
// 歧义，数字后照切。全角 ！?; 用码点转义书写。
func isSentenceTerminator(r, prev rune) bool {
	if r == '.' && prev >= '0' && prev <= '9' {
		return false
	}
	switch r {
	case '。', '.', '!', '?', ';', '…', '\uFF01', '\uFF1F', '\uFF1B':
		return true
	}
	return false
}
