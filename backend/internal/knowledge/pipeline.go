package knowledge

// 检索后处理管道（openspec/changes/knowledge-worldinfo，只抄 SillyTavern
// World Info 的算法思想，Go 自写、全确定性）。四段依序：
//
//	1. 预算裁剪：priority 降序稳定排序后依序累计答案字节，预算耗尽即停
//	   （BudgetBytes ≤0 = 不限）；sticky 活跃条目豁免——无条件存活且不计
//	   入预算（连续话题保位，不受停机点位置影响）。
//	2. 互斥消歧：非空 InclusionGroup 内按当前序只留第一个（取最高——当前
//	   序已是 priority→得分→声明序）；互斥是策展显式意图，sticky 不豁免。
//	3. 生命周期状态机：cooldown（turn-last ≤ CooldownTurns → 剔除，冷却中
//	   不重复作答）；sticky（0 < turn-last ≤ StickyTurns → 活跃：存活者提到
//	   队首、本轮免概率掷骰）。
//	4. 概率掷骰：Probability < 1 且非 sticky 活跃 → 确定性骰（fnv64a 种子
//	   ×轮次×条目 id），未中即弃。
//
// 硬纪律：默认值（priority=0/组空/sticky=cooldown=0/p=1）下输出=输入序，
// 首元素与 Search 冠军逐字节一致——管道是可选层。红线：只作用知识条目
// 注入，永不触碰比赛事实通道（本包现状零 matchstate 依赖，保持）。
// 确信度门不在此管道——MinConfidence 门仍由消费口对最终入选者执行，与
// Search 现状一致。
//
// 状态归属裁决：Library 全局跨用户、永不感知用户；Lifecycle 由消费方
// （companion 层每用户有界 map）持有，管道纯函数——状态进、状态出，
// 跨用户不串味。

import (
	"encoding/binary"
	"hash/fnv"
	"sort"
)

// Scored 是检索候选：条目加双路融合分（SearchTopN 的元素）。
type Scored struct {
	Entry Entry
	Score float64
}

// Lifecycle 是一个用户的 sticky/cooldown 轮次记忆。Turn 由消费方每个知识
// 问答轮 Advance 一次；Fired 记条目最近一次入选轮次（Select 最终存活者
// 登记）。零值可用（等价全新状态）；并发纪律：一个用户同一时刻一个轮，
// 消费方按用户取用，不加锁。
type Lifecycle struct {
	Turn  int
	Fired map[string]int
}

// NewLifecycle 返回全新状态。
func NewLifecycle() *Lifecycle {
	return &Lifecycle{Fired: map[string]int{}}
}

// Advance 推进一轮并返回新轮次。
func (lc *Lifecycle) Advance() int {
	if lc == nil {
		return 0
	}
	lc.Turn++
	return lc.Turn
}

// SelectOptions 是管道参数：预算字节总预算（0=不限）与概率掷骰种子。
// Seed 相同 + 轮次相同 + 条目相同 ⇒ 骰果相同（evals 传固定值即种子可复现；
// 运行时传按用户派生的盐，跨用户独立、重放一致）。
type SelectOptions struct {
	BudgetBytes int
	Seed        uint64
}

// Select 对候选（得分降序、并列声明序——SearchTopN 的输出形状）执行四段
// 后处理，返回存活者（序即注入序）并登记生命周期。candidates 为空返回空；
// lc 为 nil 按全新状态处理（无 sticky/cooldown、无登记）。全默认条目下
// 输出=输入序。
func Select(candidates []Scored, lc *Lifecycle, opts SelectOptions) []Scored {
	if len(candidates) == 0 {
		return nil
	}
	turn := 0
	var fired map[string]int
	if lc != nil {
		turn = lc.Turn
		if lc.Fired == nil {
			lc.Fired = map[string]int{}
		}
		fired = lc.Fired
	} else {
		fired = map[string]int{}
	}
	stickyActive := func(entry Entry) bool {
		last, ok := fired[entry.ID]
		return ok && entry.StickyTurns > 0 && turn > last && turn-last <= entry.StickyTurns
	}

	// 1) 预算裁剪：priority 降序稳定排序（同优先级保持候选序），预算耗尽
	// 即停；sticky 活跃者无条件存活且不计入预算（豁免与停机点位置无关）。
	ordered := make([]Scored, len(candidates))
	copy(ordered, candidates)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].Entry.Priority > ordered[j].Entry.Priority
	})
	clipped := make([]Scored, 0, len(ordered))
	used := 0
	stopped := false
	for _, candidate := range ordered {
		if stickyActive(candidate.Entry) {
			clipped = append(clipped, candidate)
			continue
		}
		if stopped {
			continue
		}
		if opts.BudgetBytes > 0 && used+len(candidate.Entry.Answer) > opts.BudgetBytes {
			stopped = true
			continue
		}
		used += len(candidate.Entry.Answer)
		clipped = append(clipped, candidate)
	}

	// 2) 互斥消歧：非空组内按当前序只留第一个（取最高）。
	seenGroups := map[string]bool{}
	grouped := make([]Scored, 0, len(clipped))
	for _, candidate := range clipped {
		if group := candidate.Entry.InclusionGroup; group != "" {
			if seenGroups[group] {
				continue
			}
			seenGroups[group] = true
		}
		grouped = append(grouped, candidate)
	}

	// 3) 生命周期状态机：冷却中剔除；sticky 活跃者提到队首（多个 sticky
	// 活跃按原有相对序——即最近连续话题整体前置）。
	kept := make([]Scored, 0, len(grouped))
	var stickyFirst []Scored
	for _, candidate := range grouped {
		last, firedBefore := fired[candidate.Entry.ID]
		if firedBefore && candidate.Entry.CooldownTurns > 0 && turn-last <= candidate.Entry.CooldownTurns {
			continue
		}
		if stickyActive(candidate.Entry) {
			stickyFirst = append(stickyFirst, candidate)
			continue
		}
		kept = append(kept, candidate)
	}

	// 4) 概率掷骰：p<1 且非 sticky 活跃 → 确定性骰，未中即弃。
	survivors := make([]Scored, 0, len(kept))
	for _, candidate := range kept {
		if candidate.Entry.Probability < 1 && !probabilityRoll(opts.Seed, turn, candidate.Entry.ID, candidate.Entry.Probability) {
			continue
		}
		survivors = append(survivors, candidate)
	}
	survivors = append(stickyFirst, survivors...)

	// 登记生命周期：非 sticky 活跃的存活者记为本轮命中（窗口起点）——
	// sticky 活跃期的存活不刷新命中轮（ST 式固定窗口：从真触发轮起保位
	// N 轮，不因持续命中而续期；窗口过期后重新命中才重新起算）。
	for _, survivor := range survivors {
		if !stickyActive(survivor.Entry) {
			fired[survivor.Entry.ID] = turn
		}
	}
	return survivors
}

// probabilityRoll 确定性概率骰：fnv64a(seed × turn × entryID) 万分位对照。
// p ≥ 1 恒真；同种子同轮同条目恒同果。
func probabilityRoll(seed uint64, turn int, entryID string, probability float64) bool {
	if probability >= 1 {
		return true
	}
	if probability <= 0 {
		return false
	}
	h := fnv.New64a()
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], seed)
	h.Write(buf[:])
	binary.LittleEndian.PutUint64(buf[:], uint64(turn))
	h.Write(buf[:])
	h.Write([]byte(entryID))
	return h.Sum64()%10000 < uint64(probability*10000)
}
