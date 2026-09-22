package companion

// T2-cue 镜像锁（openspec/changes/polish-round-3）：cue 驱动的偏好变化
// 镜像进 user_character_settings（三入口一状态），变化才落、审计同形状。

import (
	"context"
	"testing"
	"qiuqiu/internal/matchstate"

	"qiuqiu/internal/interaction"
	"qiuqiu/internal/relationship"
)

func mustSettings(t *testing.T, store relationship.CharacterSettingStore) *relationship.CharacterSettings {
	t.Helper()
	s, err := relationship.NewCharacterSettings(store)
	if err != nil {
		t.Fatalf("NewCharacterSettings: %v", err)
	}
	return s
}

func TestMirrorCharacterSettingsWritesChangedFields(t *testing.T) {
	store := relationship.NewMemoryCharacterSettingStore()
	agent := NewAgent(NewStoreMemoryTools(matchstate.NewStore())).WithCharacterSettings(mustSettings(t, store))
	trace := &Trace{ID: "t-mirror"}
	decision := relationship.Decision{
		Relationship: relationship.RelationshipView{InitiativeMode: "active", AnalysisAppetite: "detailed"},
	}
	agent.mirrorCharacterSettings(context.Background(), "user-1", "m1", decision, trace)

	values, err := store.Get(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if values[relationship.SettingInitiative] != "active" || values[relationship.SettingAnalysisAppetite] != "detailed" {
		t.Fatalf("values = %+v, want both mirrored", values)
	}
	found := false
	for _, call := range trace.ToolCalls {
		if call.Name == "character.setting_mirror" {
			found = true
		}
	}
	if !found {
		t.Fatalf("trace toolcalls = %+v, want the mirror marker", trace.ToolCalls)
	}
}

func TestMirrorCharacterSettingsSkipsUnchanged(t *testing.T) {
	store := relationship.NewMemoryCharacterSettingStore()
	agent := NewAgent(NewStoreMemoryTools(matchstate.NewStore())).WithCharacterSettings(mustSettings(t, store))
	trace := &Trace{ID: "t-mirror-2"}
	decision := relationship.Decision{Relationship: relationship.RelationshipView{InitiativeMode: "quiet"}}
	agent.mirrorCharacterSettings(context.Background(), "user-1", "m1", decision, trace)
	calls := len(trace.ToolCalls)
	agent.mirrorCharacterSettings(context.Background(), "user-1", "m1", decision, trace)
	if len(trace.ToolCalls) != calls {
		t.Fatalf("unchanged mirror must not append toolcalls")
	}
}

func TestMirrorCharacterSettingsNilStoreNoop(t *testing.T) {
	agent := NewAgent(NewStoreMemoryTools(matchstate.NewStore()))
	agent.mirrorCharacterSettings(context.Background(), "user-1", "m1", relationship.Decision{}, &Trace{ID: "t"})
}

func TestLedgerRecordsCharacterSetting(t *testing.T) {
	ledger := interaction.NewMemoryLedger()
	_, err := ledger.Append(context.Background(), interaction.Event{
		Kind: interaction.KindCharacterSetting, ID: "e1", UserID: "user-1", MatchID: "account",
		InputText: "field=initiative from= to=active", Source: "ws",
	})
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	events, err := ledger.ListUser(context.Background(), "user-1", 10)
	if err != nil || len(events) != 1 || events[0].Kind != interaction.KindCharacterSetting {
		t.Fatalf("events = %+v, %v", events, err)
	}
}
