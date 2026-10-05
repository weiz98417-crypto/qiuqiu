package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"qiuqiu/internal/companion"
	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/relationship"
)

// voiceSessionOptions 流转回归（policy-bits C2 接线顺手修复的存量丢失）：
// Settings（ADR-0018 粘性覆盖）与 UserAffect（policy-bits C2）必须从
// options 完整到达 agent 的关系信号——此前 Settings 在
// completeVoiceSessionWithOptions 处被静默丢弃，WS 语音路径的用户显式
// 设置从未生效。转发类改动必须有断言流转的测试（CRLF 吞注册的教训）。
func TestVoiceSessionOptionsForwardSettingsAndUserAffect(t *testing.T) {
	agent := companion.NewAgent(companion.NewStoreMemoryTools(matchstate.NewStore())).WithDirector(
		relationship.NewDirector(relationship.NewMemoryRepository()),
	)
	result, err := completeVoiceSessionWithOptions(
		context.Background(),
		agent,
		nil,
		"forward-options-match",
		"user-1",
		time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC),
		"signal-forward-options",
		voiceSessionResult{Text: "防线为什么回收得这么深"},
		nil,
		voiceSessionOptions{
			Settings: &relationship.PreferenceOverrides{InitiativeMode: "active"},
			UserAffect: &relationship.UserAffectBias{
				Label: "sad", Confidence: 0.8, TeamBehind: true,
			},
		},
	)
	if err != nil {
		t.Fatalf("completeVoiceSessionWithOptions: %v", err)
	}
	decision := result.Trace.RelationshipDecision
	if decision == nil {
		t.Fatal("relationship decision missing — options did not reach policy")
	}
	codes := strings.Join(decision.ReasonCodes, ",")
	// 偏置命中：战术问答改道安慰型，理由码随行。
	if !strings.Contains(codes, "user_affect:comfort_over_analysis") {
		t.Fatalf("reason codes = %q, want user_affect bias code", codes)
	}
	// 设置粘性覆盖随行：initiative 设置生效的理由码必须在场。
	if !strings.Contains(codes, "policy_user_setting:initiative") {
		t.Fatalf("reason codes = %q, want policy_user_setting:initiative (Settings forwarding)", codes)
	}
}
