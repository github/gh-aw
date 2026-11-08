//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToolsetPermissionsLoadedFromJSON(t *testing.T) {
	toolsetPermissionsMap := getToolsetPermissionsMap()

	// Test that toolsetPermissionsMap is populated from JSON
	if len(toolsetPermissionsMap) == 0 {
		t.Fatal("toolsetPermissionsMap is empty - JSON loading failed")
	}

	// Test a few known toolsets
	expectedToolsets := []string{"context", "repos", "issues", "pull_requests", "actions"}
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
}

func TestGitHubMCPGovernanceAndAutomationToolsets(t *testing.T) {
	toolsets := getToolsetPermissionsMap()

	governance, ok := toolsets["governance"]
	require.True(t, ok, "governance toolset must be mapped")
	assert.ElementsMatch(t, []string{
		"custom_properties_read",
		"custom_properties_write",
		"repository_ruleset_read",
	}, governance.Tools)

	copilot, ok := toolsets["copilot"]
	require.True(t, ok, "copilot toolset must be mapped")
	for _, tool := range []string{
		"get_automation",
		"get_automation_group",
		"get_automation_revision",
		"get_automations_configuration_for_repository",
		"get_copilot_session_events",
		"get_copilot_task_status",
		"list_automation_groups",
		"list_automation_revisions",
		"list_automations",
		"list_available_automation_tools",
		"list_available_automation_triggers",
	} {
		assert.Contains(t, copilot.Tools, tool)
	}

	reference, err := os.ReadFile(filepath.Join("..", "..", ".github", "aw", "github-mcp-server-tools.md"))
	require.NoError(t, err)
	referenceText := string(reference)
	const sectionMarker = "### copilot\n"
	sectionStart := strings.Index(referenceText, sectionMarker)
	require.NotEqual(t, -1, sectionStart, "Copilot toolset section must be documented")
	section := referenceText[sectionStart+len(sectionMarker):]
	sectionEnd := strings.Index(section, "\n---")
	require.NotEqual(t, -1, sectionEnd, "Copilot toolset section must be delimited")
	section = section[:sectionEnd]

	var documentedTools []string
	for line := range strings.SplitSeq(section, "\n") {
		if !strings.HasPrefix(line, "| `") {
			continue
		}
		toolEnd := strings.Index(line[3:], "`")
		require.NotEqual(t, -1, toolEnd, "malformed tool row: %s", line)
		documentedTools = append(documentedTools, line[3:3+toolEnd])
	}
	assert.ElementsMatch(t, copilot.Tools, documentedTools)
}
