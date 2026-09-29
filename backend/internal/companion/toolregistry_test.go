package companion

import "testing"

func TestToolRegistryMustRegisterRejectsBadDescriptors(t *testing.T) {
	registry := NewToolRegistry()
	registry.MustRegister(ToolSchema{Name: "a.tool"})

	defer func() {
		if recover() == nil {
			t.Error("duplicate MustRegister must panic")
		}
	}()
	registry.MustRegister(ToolSchema{Name: "a.tool"})
}

func TestToolRegistryAllowedSemantics(t *testing.T) {
	registry := NewToolRegistry()
	registry.MustRegister(ToolSchema{Name: "read.tool", MutatesMatchFacts: false})
	registry.MustRegister(ToolSchema{Name: "write.tool", MutatesMatchFacts: true})

	if !registry.Allowed("read.tool") {
		t.Error("non-mutating registered tool must be allowed")
	}
	if registry.Allowed("write.tool") {
		t.Error("mutating tool must be denied for user agents")
	}
	if registry.Allowed("unregistered.tool") {
		t.Error("unregistered tool must be denied")
	}
}

func TestUserAgentToolsRegisteredOnce(t *testing.T) {
	schemas := CompanionToolSchemas()
	if len(schemas) == 0 {
		t.Fatal("user agent tool schemas must not be empty")
	}
	seen := make(map[string]bool)
	for _, schema := range schemas {
		if seen[schema.Name] {
			t.Errorf("duplicate schema %s", schema.Name)
		}
		seen[schema.Name] = true
		if !IsUserAgentToolAllowed(schema.Name) && !schema.MutatesMatchFacts {
			t.Errorf("non-mutating registered tool %s must be allowed", schema.Name)
		}
	}
}
