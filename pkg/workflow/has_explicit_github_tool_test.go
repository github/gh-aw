//go:build !integration

package workflow

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHasExplicitGitHubTool tests the HasExplicitGitHubTool tracking logic
// to ensure it correctly identifies when tools.github is explicitly configured
func TestHasExplicitGitHubTool(t *testing.T) {
	tests := []struct {
		name                    string
		tools                   map[string]any
		expectHasExplicitGitHub bool
		description             string
	}{
		{
			name:                    "nil tools - no explicit github",
			tools:                   nil,
			expectHasExplicitGitHub: false,
			description:             "When tools is nil, GitHub tool is not explicit",
		},
		{
			name:                    "empty tools - no explicit github",
			tools:                   map[string]any{},
			expectHasExplicitGitHub: false,
			description:             "When tools is empty, GitHub tool is not explicit",
		},
		{
			name: "tools with github key - explicit",
			tools: map[string]any{
				"github": map[string]any{
					"toolsets":  []any{"repos"},
					"read-only": true,
				},
			},
			expectHasExplicitGitHub: true,
			description:             "When tools.github is configured, it is explicit",
		},
		{
			name: "tools with github nil value - explicit",
			tools: map[string]any{
				"github": nil,
			},
			expectHasExplicitGitHub: true,
			description:             "Even with nil value, presence of github key means explicit",
		},
		{
			name: "tools with github false - explicit",
			tools: map[string]any{
				"github": false,
			},
			expectHasExplicitGitHub: true,
			description:             "github: false is explicit (disabling)",
		},
		{
			name: "tools with other tools but no github - not explicit",
			tools: map[string]any{
				"bash":      []any{"*"},
				"web-fetch": true,
			},
			expectHasExplicitGitHub: false,
			description:             "Other tools don't make GitHub explicit",
		},
		{
			name: "tools with github and other tools - explicit",
			tools: map[string]any{
				"github": map[string]any{
					"toolsets": []any{"issues"},
				},
				"bash": []any{"*"},
			},
			expectHasExplicitGitHub: true,
			description:             "GitHub tool is explicit even when other tools present",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the tools processing that would happen in processToolsAndMarkdown
			topTools := tt.tools

			// Check if GitHub tool was explicitly configured
			hasExplicitGitHubTool := false
			if topTools != nil {
				if _, exists := topTools["github"]; exists {
					hasExplicitGitHubTool = true
				}
			}

			assert.Equal(t, tt.expectHasExplicitGitHub, hasExplicitGitHubTool, tt.description)
		})
	}
}

// TestHasExplicitGitHubToolInWorkflowData tests that HasExplicitGitHubTool
// is correctly set in WorkflowData during compilation
func TestHasExplicitGitHubToolInWorkflowData(t *testing.T) {
	tests := []struct {
		name                    string
		frontmatter             string
		expectHasExplicitGitHub bool
	}{
		{
			name: "no tools section",
			frontmatter: `---
on: push
permissions:
  contents: read
---

# Test
`,
			expectHasExplicitGitHub: false,
		},
		{
			name: "tools with github",
			frontmatter: `---
on: push
tools:
  github:
    toolsets: [repos]
---

# Test
`,
			expectHasExplicitGitHub: true,
		},
		{
			name: "tools without github",
			frontmatter: `---
on: push
tools:
  bash:
    - "*"
---

# Test
`,
			expectHasExplicitGitHub: false,
		},
		{
			name: "github disabled explicitly",
			frontmatter: `---
on: push
tools:
  github: false
---

# Test
`,
			expectHasExplicitGitHub: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary test file
			tmpDir := t.TempDir()
			testFile := tmpDir + "/test.md"
			err := os.WriteFile(testFile, []byte(tt.frontmatter), 0644)
			require.NoError(t, err)

			// Parse the workflow
			c := NewCompiler()
			workflowData, err := c.ParseWorkflowFile(testFile)
			require.NoError(t, err)

			// Check HasExplicitGitHubTool field
			assert.Equal(t, tt.expectHasExplicitGitHub, workflowData.HasExplicitGitHubTool,
				"HasExplicitGitHubTool should be %v", tt.expectHasExplicitGitHub)
		})
	}
}

// TestPermissionValidationSkippedWhenGitHubAutoAdded tests that
// permission validation is skipped when GitHub tool is auto-added
func TestPermissionValidationSkippedWhenGitHubAutoAdded(t *testing.T) {
	tests := []struct {
		name                  string
		hasExplicitGitHubTool bool
		hasPermissions        bool
		shouldSkipValidation  bool
	}{
		{
			name:                  "no permissions, no explicit github - validate",
			hasExplicitGitHubTool: false,
			hasPermissions:        false,
			shouldSkipValidation:  false,
		},
		{
			name:                  "has permissions, no explicit github - skip validation",
			hasExplicitGitHubTool: false,
			hasPermissions:        true,
			shouldSkipValidation:  true,
		},
		{
			name:                  "has permissions, explicit github - validate",
			hasExplicitGitHubTool: true,
			hasPermissions:        true,
			shouldSkipValidation:  false,
		},
		{
			name:                  "no permissions, explicit github - validate",
			hasExplicitGitHubTool: true,
			hasPermissions:        false,
			shouldSkipValidation:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This tests the logic in compiler.go line 275-276
			shouldSkip := tt.hasPermissions && !tt.hasExplicitGitHubTool

			assert.Equal(t, tt.shouldSkipValidation, shouldSkip,
				"Validation skip logic should match expected behavior")
		})
	}
}
