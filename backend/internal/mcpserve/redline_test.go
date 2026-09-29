package mcpserve

// 宪法负例测试（mcp-registry-serve 5.3）：MCP 路径对比赛事实账本零写入。
// 三道闸：
//  1. 四工具的 companion 注册表描述全部 MutatesMatchFacts==false（与
//     IsUserAgentToolAllowed 的只读门同源）；
//  2. 包源文件（非测试）不含任何账本写路径引用——务实做法：扫自身源码
//     的写方法标识符；
//  3. server 层对未登记工具名（拿写语义名字探测）的 tools/call 一律报错
//     ——未登记 = 不可达。

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"qiuqiu/internal/companion"
)

// TestAllServedToolsAreNonMutating pins the read-only contract on the single
// source: every served tool's registry schema must declare MutatesMatchFacts
// false, and the registry read gate must agree.
func TestAllServedToolsAreNonMutating(t *testing.T) {
	for _, schema := range servedToolSchemas() {
		if schema.MutatesMatchFacts {
			t.Errorf("served tool %q declares MutatesMatchFacts=true — MCP tools are read-only forever", schema.Name)
		}
		if !companion.IsUserAgentToolAllowed(schema.Name) {
			t.Errorf("served tool %q fails the companion read gate", schema.Name)
		}
	}
}

// TestPackageSourceHasNoFactWritePath scans this package's own non-test
// source files for the match fact ledger's write-path identifiers. The MCP
// server may only call the public read half; a write reference here means
// the red line was crossed.
func TestPackageSourceHasNoFactWritePath(t *testing.T) {
	forbidden := []string{
		"operatorwrite",  // the operator write service seam
		"OperatorWrite",  // ditto, exported form
		"SetConfig(",     // matchstate repository write
		"SetAutomation(", // matchstate repository write
		"Create(",        // matchstate event append
		"Correct(",       // matchstate event correction
		"ConfirmFact(",   // fact confirm transition
		"RevokeFact(",    // fact revoke transition
		"ReconcileFact(", // fact reconcile transition
		"ResolveFact",    // conflict resolution writes
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	scanned := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		scanned++
		data, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			t.Fatalf("read source %s: %v", name, err)
		}
		source := string(data)
		for _, token := range forbidden {
			if strings.Contains(source, token) {
				t.Errorf("%s references write-path identifier %q — the MCP surface must stay read-only", name, token)
			}
		}
	}
	if scanned < 3 {
		t.Fatalf("scanned %d source files, want at least 3 (doc/server/auth)", scanned)
	}
}

// TestUnregisteredToolCallFails pins the server-side half of the red line:
// a tools/call for a name that is not in the read-only registry — here a
// write-semantic probe name — must fail. Unregistered = unreachable, so no
// write capability can be smuggled in through the MCP surface.
func TestUnregisteredToolCallFails(t *testing.T) {
	session := connectTestSession(t, testDeps())
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "match.confirm_fact",
		Arguments: map[string]any{"matchId": "m-1", "factId": "f-1", "operatorId": "probe"},
	})
	if err == nil {
		t.Fatalf("tools/call to unregistered write-semantic tool returned result %+v, want an error", result)
	}
}
