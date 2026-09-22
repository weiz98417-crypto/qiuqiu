package companion

// 决策偏好 → character-settings 镜像（openspec/changes/polish-round-3，
// T2-cue）：cue 词改写 relationship_state 后，把非空偏好同步落
// user_character_settings，使 cue 词与 WS/HTTP 共用一份状态。写前比对，
// 只落变化；账本事件与 WS/HTTP 入口同形状（source=cue）。审计尽力而为，
// 永不影响回合。

import (
	"context"
	"strings"

	"qiuqiu/internal/interaction"
	"qiuqiu/internal/relationship"
)

// cueMirrorFields 是决策视图 → settings 槽位的映射（banter 许可是
// state.Banter 映射，非单值槽位，不在此列）。
var cueMirrorFields = map[relationship.CharacterSettingField]func(relationship.RelationshipView) string{
	relationship.SettingInitiative:       func(v relationship.RelationshipView) string { return v.InitiativeMode },
	relationship.SettingAnalysisAppetite: func(v relationship.RelationshipView) string { return v.AnalysisAppetite },
}

// mirrorCharacterSettings 在 cue 驱动的决策后同步 settings。读改比对：
// 与现值相同不写不记账；失败静默（含 Ledger），绝不影响回合。
func (a *Agent) mirrorCharacterSettings(ctx context.Context, userID, matchID string, decision relationship.Decision, trace *Trace) {
	if a == nil || a.characterSettings == nil || trace == nil {
		return
	}
	current, err := a.characterSettings.Get(ctx, userID)
	if err != nil {
		return
	}
	for field, extract := range cueMirrorFields {
		value := strings.TrimSpace(extract(decision.Relationship))
		if value == "" || value == current[field] {
			continue
		}
		if _, err := a.characterSettings.Set(ctx, userID, string(field), value); err != nil {
			return
		}
		trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "character.setting_mirror", Args: map[string]string{"field": string(field), "value": value}})
		if a.interactions != nil {
			_, _ = a.interactions.Append(ctx, interaction.Event{
				Kind: interaction.KindCharacterSetting, UserID: userID, MatchID: matchID,
				TraceID: trace.ID, InputText: "field=" + string(field) + " from=" + current[field] + " to=" + value,
				Source: "cue", CreatedAt: trace.CreatedAt,
			})
		}
	}
}
