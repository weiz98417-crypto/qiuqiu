package mcpserve

// 一致性断言（mcp-registry-serve 5.2）：MCP server 的四个工具与
// companion.ToolRegistry 单一源同源——名字经 IsUserAgentToolAllowed 为
// true、描述与注册表逐字一致、注册表里恰好这四条且全部只读。

import (
	"testing"

	"qiuqiu/internal/companion"
)

func TestServedToolNamesAreAllowedUserAgentTools(t *testing.T) {
	for _, name := range servedToolNames() {
		if !companion.IsUserAgentToolAllowed(name) {
			t.Errorf("served tool %q is not allowed by companion.IsUserAgentToolAllowed", name)
		}
	}
}

func TestServedToolSchemasMatchRegistryVerbatim(t *testing.T) {
	registry := make(map[string]companion.ToolSchema)
	for _, schema := range companion.CompanionToolSchemas() {
		registry[schema.Name] = schema
	}
	served := servedToolSchemas()
	if len(served) != len(servedToolNames()) {
		t.Fatalf("served schemas = %d, want %d", len(served), len(servedToolNames()))
	}
	for _, schema := range served {
		registered, ok := registry[schema.Name]
		if !ok {
			t.Errorf("served tool %q missing from companion.CompanionToolSchemas", schema.Name)
			continue
		}
		if schema.Description != registered.Description {
			t.Errorf("tool %q description drifted from the registry:\n served:  %q\n registry: %q", schema.Name, schema.Description, registered.Description)
		}
		if schema.MutatesMatchFacts != registered.MutatesMatchFacts {
			t.Errorf("tool %q MutatesMatchFacts drifted from the registry", schema.Name)
		}
	}
}

func TestServedToolDescriptionsFeedMCPServer(t *testing.T) {
	session := connectTestSession(t, testDeps())
	listing, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	descriptions := make(map[string]string, len(listing.Tools))
	for _, tool := range listing.Tools {
		descriptions[tool.Name] = tool.Description
	}
	for _, schema := range servedToolSchemas() {
		got, ok := descriptions[schema.Name]
		if !ok {
			t.Errorf("MCP server is missing tool %q", schema.Name)
			continue
		}
		if got != schema.Description {
			t.Errorf("MCP tool %q description = %q, want registry description %q", schema.Name, got, schema.Description)
		}
	}
}
