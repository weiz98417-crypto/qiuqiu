package config

import "testing"

func TestMiMoVoiceDefaultsToBingTang(t *testing.T) {
	t.Setenv("MIMO_VOICE", "")
	if got := Load().MiMoVoice; got != "冰糖" {
		t.Fatalf("default MiMoVoice = %q, want 冰糖", got)
	}
}
