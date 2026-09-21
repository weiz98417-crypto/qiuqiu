package router

// schema 形状锁（openspec/changes/semantic-memory 任务 1.5）：router 迁入
// structured.Extract 后，工具参数 schema 由 Result 的 jsonschema tag 反射
// 生成——本测试把反射 schema 与迁移前手写 schema 的关键形状逐项锁定
// （枚举同序同值、必填、槽位描述），漂移即红。

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/invopop/jsonschema"
)

func TestReflectedRouteSchemaMatchesMigrationShape(t *testing.T) {
	reflector := jsonschema.Reflector{ExpandedStruct: true, DoNotReference: true}
	schema := reflector.ReflectFromType(reflect.TypeOf(&Result{}))
	raw, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("marshal schema: %v", err)
	}
	var parsed struct {
		Properties map[string]struct {
			Enum        []string `json:"enum"`
			Description string   `json:"description"`
		} `json:"properties"`
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}

	// 枚举同序同值（match_reaction 主动回合专属，不在用户回合枚举）。
	wantEnum := []string{
		"smalltalk", "schedule_question", "match_status_question",
		"recent_event_question", "follow_up_question", "player_question",
		"match_fact_claim", "emotion_reaction", "personal_share",
		"control_command", "reminder_request", "knowledge_question", "unknown",
	}
	intent := parsed.Properties["intent"]
	if len(intent.Enum) != len(wantEnum) {
		t.Fatalf("intent enum = %v, want %v", intent.Enum, wantEnum)
	}
	for i := range wantEnum {
		if intent.Enum[i] != wantEnum[i] {
			t.Fatalf("intent enum[%d] = %q, want %q", i, intent.Enum[i], wantEnum[i])
		}
	}
	if intent.Description == "" || intent.Description != "用户这回合的意图，必须从这个枚举里选" {
		t.Fatalf("intent description = %q", intent.Description)
	}

	// 必填：intent + confidence。
	if len(parsed.Required) != 2 {
		t.Fatalf("required = %v, want exactly intent+confidence", parsed.Required)
	}
	seen := map[string]bool{}
	for _, name := range parsed.Required {
		seen[name] = true
	}
	if !seen["intent"] || !seen["confidence"] {
		t.Fatalf("required = %v, want intent and confidence", parsed.Required)
	}

	// 槽位描述原样保留。
	for name, want := range map[string]string{
		"player": "提到的球员名，没有则空串",
		"team":   "提到的球队名，没有则空串",
	} {
		if got := parsed.Properties[name].Description; got != want {
			t.Fatalf("%s description = %q, want %q", name, got, want)
		}
	}

	// RoutableIntents 与 schema 枚举同源（tag）同序。
	if routable := RoutableIntents(); len(routable) != len(wantEnum) {
		t.Fatalf("RoutableIntents = %v, want %d entries", routable, len(wantEnum))
	}
}
