package knowledge

// 消费验证(verification & consumption 轮,ADR-0023):策展台真实录入的
// 全链路验证——用生产 Library/Store 缝模拟「运营台 Put → 保存即生效 →
// 检索双路命中 → TriggerLookup 附句 → 待复查过滤」,录第一批 2025-26
// 赛季公共事实(联赛/杯赛赛制与核心规则,公共稳定知识,confidence 高)。
//
// 运行:QIUQIU_CONSUMPTION_VERIFY=1 go test ./internal/knowledge -run TestConsumptionVerifyCuratedBatch -v
// 默认跳过(不污染常规门禁)。

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func consumptionVerifyEnabled() bool {
	return os.Getenv("QIUQIU_CONSUMPTION_VERIFY") == "1"
}

// 第一批真实条目:2025-26 赛季公共事实(逐字即锚点,ADR-0017 口径)。
func consumptionBatch() []Entry {
	effective := time.Date(2024, 8, 1, 0, 0, 0, 0, time.UTC)
	return []Entry{
		{
			ID: "rules-ucl-2024-swiss", Topics: []string{"欧冠", "欧冠赛制", "新赛制", "联赛阶段"},
		Quote:    "欧冠冠军要连打淘汰赛，联赛阶段前 8 才能少踢一轮附加赛。",
			Answer:       "2024-25 起欧冠改为 36 队联赛阶段（瑞士轮），每队踢 8 场不同对手；排名前 8 直接进 16 强，第 9 到 24 名打两回合附加赛，后 8 名出局。",
			Source:       "UEFA 官方赛制（2024-06 公告）", Confidence: 0.95, EffectiveAt: effective,
			Triggers: []string{"欧冠冠军"},
		},
		{
			ID: "rules-laliga-2025-format", Topics: []string{"西甲", "西甲赛制", "联赛轮次"},
		Quote:    "西甲冠军按 38 轮积分定，同分先比相互战绩——国家德比因此格外值钱。",
			Answer:   "西甲 20 队打 38 轮主客场双循环，排名末三位直接降入西乙；同分先看相互战绩再看净胜球。",
			Source:   "LaLiga 官方竞赛规则", Confidence: 0.95, EffectiveAt: effective,
			Triggers: []string{"西甲冠军"},
		},
		{
			ID: "rules-substitutions-5", Topics: []string{"换人", "换人规则", "五个换人名额"},
			Answer:   "现行规则每队每场最多 5 个换人名额，须在最多 3 个比赛暂停窗口完成（中场休息不算窗口）；加时赛额外多 1 个名额。",
			Source:   "IFAB 足球竞赛规则（2022-23 起永久化）", Confidence: 0.9, EffectiveAt: effective,
			Triggers: nil,
		},
		{
			ID: "rules-offside-basics", Topics: []string{"越位", "越位规则"},
		Quote:    "越位看出脚瞬间，半打身位都不保险——VAR 现在画线画得比头发丝还细。",
			Answer:   "越位看传球出脚瞬间：进攻队员比对方倒数第二名防守队员（通常含门将）更接近球门线且参与进攻即越位；平行不算越位。",
			Source:   "IFAB 足球竞赛规则 Law 11", Confidence: 0.9, EffectiveAt: effective,
			Triggers: []string{"越位"},
		},
		{
			ID: "rules-yellow-suspension", Topics: []string{"黄牌", "累积停赛", "红牌"},
			Answer:   "西甲累积 5 张黄牌自动停赛一场，红牌直接停赛至少一场（严重犯规件数由纪律委员会裁定）；杯赛与联赛的累积分开计算。",
			Source:   "LaLiga 竞赛纪律规则", Confidence: 0.85, EffectiveAt: effective,
			Triggers: nil,
		},
		{
			ID: "rules-ucl-tiebreak", Topics: []string{"欧冠", "客场进球", "淘汰赛规则"},
			Answer:   "欧冠淘汰赛两回合总比分相同直接进加时与点球——客场进球规则 2021-22 起已废除，主客场待遇一致。",
			Source:   "UEFA 官方（2021-06 宣布废除客场进球）", Confidence: 0.95, EffectiveAt: effective,
			Triggers: nil,
		},
		{
			ID: "rules-ucl-2025-qualification", Topics: []string{"欧冠资格", "欧战名额", "西甲前四"},
			Answer:   "西甲欧冠名额通常为前四名直接进联赛阶段；另有欧联（第 5）与欧协联资格位，国王杯冠军也可拿到欧战门票。",
			Source:   "UEFA 名额分配（access list）", Confidence: 0.85, EffectiveAt: effective,
			Triggers: nil,
		},
	}
}

func TestConsumptionVerifyCuratedBatch(t *testing.T) {
	if !consumptionVerifyEnabled() {
		t.Skip("QIUQIU_CONSUMPTION_VERIFY 未设置:消费验证按需运行")
	}
	store := NewMemoryStore()
	library, err := NewStoreLibrary(context.Background(), store, nil) // nil embedder:关键词路照常,向量路禁用
	if err != nil {
		t.Fatalf("NewStoreLibrary: %v", err)
	}

	// 1) 策展台保存即生效:逐条 Put(运营口径),Reload 后立刻可检索。
	operator := "consumption-drill"
	ctx := context.Background()
	for i, entry := range consumptionBatch() {
		if _, err := library.Put(ctx, entry, operator); err != nil {
			t.Fatalf("Put %s: %v", entry.ID, err)
		}
		if _, ok := library.Search(ctx, entry.Topics[0]); !ok {
			t.Fatalf("保存即生效失败:%s 保存后检索 %q 无命中", entry.ID, entry.Topics[0])
		}
		if got := library.Size(); got != i+1 {
			t.Fatalf("Library size = %d, want %d", got, i+1)
		}
	}

	// 2) 检索双路:关键词命中语义对齐(答案可被问题语言找到)。
	cases := []struct{ query, wantFragment string }{
		{"欧冠新赛制怎么打", "36 队联赛阶段"},
		{"客场进球还算吗", "客场进球规则"},
		{"换人能换几个", "5 个换人名额"},
		{"西甲多少轮", "38 轮"},
	}
	for _, tc := range cases {
		answer, ok := library.Search(ctx, tc.query)
		if !ok {
			t.Errorf("检索 %q 无命中(双路检索对真实问题语言失效)", tc.query)
			continue
		}
		if !strings.Contains(answer.Answer, tc.wantFragment) {
			t.Errorf("检索 %q 命中了错误条目:含 %q 期望含 %q", tc.query, answer.Answer, tc.wantFragment)
		}
	}

	// 3) TriggerLookup:比赛事件触发知识附句(确定性拼装管线)。
	entry, ok := library.TriggerLookup("越位")
	if !ok || entry.ID != "rules-offside-basics" {
		t.Fatalf("TriggerLookup(越位) = (%+v, %v), want rules-offside-basics", entry, ok)
	}

	// 4) 待复查过滤:effective_at=2024-08-01 早于最近转会窗闭(2025-07-01),
	//    全部条目应标记待复查——转会窗复查制度的机器面。
	due := 0
	records, err := store.List(ctx)
	if err != nil {
		t.Fatalf("store.List: %v", err)
	}
	for _, record := range records {
		if DueReview(time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC), record.EffectiveAt) {
			due++
		}
	}
	if due != len(consumptionBatch()) {
		t.Errorf("待复查标记 = %d/%d, want 全部(生效窗口早于最近窗闭)", due, len(consumptionBatch()))
	}

	// 5) 审计归属:operator 落 record。
	for _, record := range records {
		if record.CreatedBy != operator {
			t.Errorf("审计归属丢失:%s createdBy=%q", record.Entry.ID, record.CreatedBy)
		}
	}

	fmt.Printf("[consumption-verify] OK: %d 条录入/检索/触发/待复查/审计全链通过\n", len(consumptionBatch()))
}
