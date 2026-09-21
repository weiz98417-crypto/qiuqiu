package relationship

import "testing"

func TestValidateCharacterSetting(t *testing.T) {
	if !ValidateCharacterSetting("initiative", "quiet") || !ValidateCharacterSetting("banter_level", "off") {
		t.Fatal("legal values rejected")
	}
	if ValidateCharacterSetting("initiative", "yelling") {
		t.Fatal("unknown value accepted")
	}
	if ValidateCharacterSetting("stance", "anything") {
		t.Fatal("stance must not be a settable field (CONTEXT.md 角色立场边界)")
	}
}

func TestCharacterSettingsRoundTrip(t *testing.T) {
	settings, err := NewCharacterSettings(NewMemoryCharacterSettingStore())
	if err != nil {
		t.Fatalf("NewCharacterSettings: %v", err)
	}
	if _, err := settings.Set(t.Context(), "user-1", "analysis_appetite", "deep"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	values, err := settings.Get(t.Context(), "user-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if values[SettingAnalysisAppetite] != "deep" || values[SettingInitiative] != "" {
		t.Fatalf("values = %+v", values)
	}
	if _, err := settings.Set(t.Context(), "user-1", "initiative", "max"); err == nil {
		t.Fatal("invalid value must be rejected")
	}
}
