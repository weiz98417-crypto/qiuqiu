package companion

// 注册表注入的字节级等价锁(agent-internals 3.6;ADR-0009 字节锁):注入产物
// 渲染出的 router prompt 必须与 2026-10-04 前的硬编码 prompt 逐字节一致——
// 「字节不动,来源单一」是本次注入的验收门。此测试即字节快照。

import (
	"strings"
	"testing"

	"qiuqiu/internal/router"
)

// wantPromptLines 是 2026-10-04 前 routeSystemPrompt 的意图定义行(枚举序)。
var wantPromptLines = []string{
	"smalltalk：陪你聊天、打招呼、问你在干嘛、让你陪着看球。",
	"schedule_question：问赛程、今天/明天有什么比赛。",
	"match_status_question：问当前比分或比赛进行情况。",
	"recent_event_question：问刚才发生了什么（谁进的、上一球）。",
	"follow_up_question：顺着上一条追问（那球呢、然后呢、谁策动）。",
	"player_question：问某个球员的表现或进球。",
	"match_fact_claim：用户主张一个比赛事实（进球/比分/破门）。用户坚持主张（明明进了、真的进了、确实进了）也算 match_fact_claim。",
	"emotion_reaction：看球情绪反应（漂亮、牛、紧张、离谱、真的假的）。",
	"personal_share：用户分享自己的生活（我赢了、我累了、我喜欢某队）。",
	"control_command：让球球别说/少说/安静。",
	"reminder_request：让我在开球前提醒你（开球前叫我、赛前提醒我）。",
	"knowledge_question：问足球规则或赛制知识（越位是什么、积分怎么算）。",
	"subscription_manage：管理球队提醒订阅（以后都叫我、列出我的订阅、别叫我XX的了）。",
}

const wantPromptFixedTail = `- unknown：以上都不适合。
置信度低于 0.7 时直接给 unknown。闲聊类（smalltalk/emotion_reaction/personal_share）额外给一句自然回复建议 reply，口语、最多两句60字、绝不提具体比分球员等比赛事实；其余意图 reply 留空。
示例：
用户说"球进了" → match_fact_claim（confidence 0.9，reply 空）
用户说"你在干嘛" → smalltalk（confidence 0.9，reply "我在盯着比分呢，陪你一起看。"）
用户说"明明进了，裁判瞎了吗" → match_fact_claim（confidence 0.85，reply 空）`

func TestRouterPromptInjectionIsByteIdentical(t *testing.T) {
	prompt := router.SystemPrompt()
	if !strings.HasPrefix(prompt, "你是陪看足球助手\"球球\"的意图路由器。") {
		t.Fatalf("prompt header drifted: %q", prompt[:min(60, len(prompt))])
	}
	// 每一行注入定义都必须原样出现在 prompt 中(顺序=枚举序)。
	offset := 0
	for _, line := range wantPromptLines {
		want := "- " + line
		index := strings.Index(prompt[offset:], want)
		if index < 0 {
			t.Fatalf("prompt line %q missing or out of order after offset %d", want, offset)
		}
		offset += index + len(want)
	}
	// 固定尾逐字节一致。
	if !strings.HasSuffix(prompt, wantPromptFixedTail) {
		t.Fatalf("prompt tail drifted:\n--- got tail ---\n%s\n--- want tail ---\n%s",
			prompt[max(0, len(prompt)-len(wantPromptFixedTail)):], wantPromptFixedTail)
	}
	// 总行数守恒:注入 13 行 + unknown 行 = 14 个定义行(每个 "- " 前都是换行)。
	if got := strings.Count(prompt, "\n- "); got != 14 {
		t.Fatalf("definition lines = %d, want 14", got)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
