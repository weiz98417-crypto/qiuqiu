package relationship

// 事件场景×话痨档内容预算矩阵（openspec/changes/policy-bits B5）。
//
// 「话痨档是场景设置不是性格设置」：进球瞬间全档短句快报、中场休息
// normal/active 畅聊、quiet 保持现状。矩阵是内容预算（句数/字数）的 policy
// 单源——presentation_table 同款纪律：数据表 + 查表函数 + 钉住单元格的表
// 测试（scenario_matrix_test.go）；数值可调，行为口径单源，消费方只有
// contentPolicyFor。
//
// 纪律基线：矩阵只改预算，不动 act/沉默判定——silence/冷却/边界门在
// selectTurnActs 里原样先行。

// scenarioContentBudget 是矩阵单元格：话轮内容预算。
type scenarioContentBudget struct {
	MaxSentences  int
	MaxCharacters int
}

var (
	// 默认单元格 = 矩阵入表前的现状（2 句/80 字），未命中场景原样回落。
	scenarioDefaultBudget = scenarioContentBudget{MaxSentences: 2, MaxCharacters: 80}
	// 进球瞬间短句快报：全档 ≤2 句（含 active——热闹档是话多不是抢先播报）。
	// 数值与默认相同系有意钉住：后续调高默认单元格时进球快报上限不被带走。
	scenarioGoalFlashBudget = scenarioContentBudget{MaxSentences: 2, MaxCharacters: 80}
	// 中场休息畅聊：normal/active 放宽到 4 句/200 字——半场总结配得上展开聊。
	scenarioHalftimeChatBudget = scenarioContentBudget{MaxSentences: 4, MaxCharacters: 200}
	// 中场 quiet 保持现状：安静档的畅聊只来自用户主动，不是场景派发。
	scenarioHalftimeQuietBudget = scenarioContentBudget{MaxSentences: 2, MaxCharacters: 80}
)

// ScenarioContentFor 查事件场景×话痨档的内容预算单元格。未知事件类型与
// 未知档位一律落默认单元格（= 现状），矩阵只能放宽或钉住、不能收窄现状。
func ScenarioContentFor(eventType, talkativeness string) (maxSentences, maxCharacters int) {
	cell := scenarioDefaultBudget
	switch eventType {
	case "goal":
		cell = scenarioGoalFlashBudget
	case "halftime":
		if IsQuiet(talkativeness) {
			cell = scenarioHalftimeQuietBudget
		} else {
			cell = scenarioHalftimeChatBudget
		}
	}
	return cell.MaxSentences, cell.MaxCharacters
}
