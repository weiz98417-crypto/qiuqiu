package companion

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestToolCallNamesSingleSourced 守卫 trace-genai-alignment:工具名常量
// (tracefields.go)是唯一允许出现工具名字面量的生产文件——散布字面量曾让
// 「加一个工具」要同步 trace 构造点与 boundary schema 两处。测试文件豁免
// (断言字面值是测试的本职)。
func TestToolCallNamesSingleSourced(t *testing.T) {
	names := []string{
		ToolCallCharacterSettingMirror, ToolCallConversationAppendTurn,
		ToolCallConversationReadRecent, ToolCallIntentRoute,
		ToolCallKnowledgeAnswer, ToolCallKnowledgeTrigger,
		ToolCallKnowledgeTriggerFallback, ToolCallMatchGetPlayerTimeline,
		ToolCallMatchReadSnapshot, ToolCallMatchSearchEvents,
		ToolCallMatchVerifyUserClaim, ToolCallMemoryAppendThread,
		ToolCallMemoryPortrait, ToolCallMemoryRecall,
		ToolCallMemoryRecoverThread, ToolCallObservationRecord,
		ToolCallRelationshipApply, ToolCallRelationshipGoalComfort,
		ToolCallReminderCreate, ToolCallReminderDuplicate,
		ToolCallResponseEmitCompanionReply, ToolCallScheduleReadToday,
		ToolCallScheduleSearch, ToolCallSubscriptionCancel,
		ToolCallSubscriptionCreate, ToolCallTraceWriteDecision,
	}
	roots := []string{"."}
	for len(roots) > 0 {
		root := roots[len(roots)-1]
		roots = roots[:len(roots)-1]
		entries, err := os.ReadDir(root)
		if err != nil {
			t.Fatalf("read dir %s: %v", root, err)
		}
		for _, entry := range entries {
			name := entry.Name()
			path := filepath.Join(root, name)
			if entry.IsDir() {
				roots = append(roots, path)
				continue
			}
			if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == "tracefields.go" {
				continue
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			for _, toolName := range names {
				if strings.Contains(string(data), `"`+toolName+`"`) {
					t.Errorf("%s 含工具名字面量 %q——改用 tracefields.go 的常量", path, toolName)
				}
			}
		}
	}
}
