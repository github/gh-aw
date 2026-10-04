//go:build !integration

package workflow

import (
	"testing"
)

func TestToolsetPermissionsLoadedFromJSON(t *testing.T) {
	toolsetPermissionsMap := getToolsetPermissionsMap()

	// Test that toolsetPermissionsMap is populated from JSON
	if len(toolsetPermissionsMap) == 0 {
		t.Fatal("toolsetPermissionsMap is empty - JSON loading failed")
	}

	// Test a few known toolsets
	expectedToolsets := []string{"context", "repos", "issues", "pull_requests", "actions", "governance", "automations"}
	for _, toolset := range expectedToolsets {
		if _, exists := toolsetPermissionsMap[toolset]; !exists {
			t.Errorf("Expected toolset %s not found in toolsetPermissionsMap", toolset)
		}
	}

	// Test specific permission mappings
	repoPerms, exists := toolsetPermissionsMap["repos"]
	if !exists {
		t.Fatal("repos toolset not found")
	}
	if len(repoPerms.ReadPermissions) == 0 {
		t.Error("repos toolset should have read permissions")
	}
	if repoPerms.ReadPermissions[0] != PermissionContents {
		t.Errorf("Expected repos read permission to be 'contents', got %v", repoPerms.ReadPermissions[0])
	}

	// Test tools list is populated
	if len(repoPerms.Tools) == 0 {
		t.Error("repos toolset should have tools listed")
	}

	// Test context toolset has no permissions but has tools
	contextPerms, exists := toolsetPermissionsMap["context"]
	if !exists {
		t.Fatal("context toolset not found")
	}
	if len(contextPerms.ReadPermissions) != 0 || len(contextPerms.WritePermissions) != 0 {
		t.Error("context toolset should have no required permissions")
	}
	if len(contextPerms.Tools) == 0 {
		t.Error("context toolset should have tools listed")
	}

	governancePerms := toolsetPermissionsMap["governance"]
	if len(governancePerms.ReadPermissions) != 1 || governancePerms.ReadPermissions[0] != PermissionAdministration {
		t.Errorf("governance toolset should require administration read permission, got %v", governancePerms.ReadPermissions)
	}
	if len(governancePerms.WritePermissions) != 1 || governancePerms.WritePermissions[0] != PermissionAdministration {
		t.Errorf("governance toolset should require administration write permission, got %v", governancePerms.WritePermissions)
	}
	if len(governancePerms.Tools) != 4 {
		t.Errorf("governance toolset should list four tools, got %v", governancePerms.Tools)
	}

	automationsPerms := toolsetPermissionsMap["automations"]
	if len(automationsPerms.ReadPermissions) != 0 || len(automationsPerms.WritePermissions) != 0 {
		t.Errorf("provisional automations toolset should have no permission requirements, got read=%v write=%v", automationsPerms.ReadPermissions, automationsPerms.WritePermissions)
	}
	if len(automationsPerms.Tools) != 6 {
		t.Errorf("automations toolset should list six tools, got %v", automationsPerms.Tools)
	}
}
