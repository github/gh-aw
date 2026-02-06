//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper functions for test setup

// testCompiler creates a test compiler with validation skipped
func testCompiler() *Compiler {
	c := NewCompiler()
	c.SetSkipValidation(true)
	return c
}

// workflowDataWithAgenticWorkflows creates test workflow data with agentic-workflows tool
func workflowDataWithAgenticWorkflows(options ...func(*WorkflowData)) *WorkflowData {
	wd := &WorkflowData{
		Tools: map[string]any{
			"agentic-workflows": nil,
		},
	}
	for _, opt := range options {
		opt(wd)
	}
	return wd
}

// withCustomToken is an option for workflowDataWithAgenticWorkflows
func withCustomToken(token string) func(*WorkflowData) {
	return func(wd *WorkflowData) {
		wd.GitHubToken = token
	}
}

// withImportedFiles is an option for workflowDataWithAgenticWorkflows
func withImportedFiles(files ...string) func(*WorkflowData) {
	return func(wd *WorkflowData) {
		wd.ImportedFiles = files
	}
}

// Test functions

func TestAgenticWorkflowsSyntaxVariations(t *testing.T) {
	tests := []struct {
		name        string
		toolValue   any
		shouldWork  bool
		description string
	}{
		{
			name:        "agentic-workflows with nil (no value)",
			toolValue:   nil,
			shouldWork:  true,
			description: "Should enable agentic-workflows when field is present without value",
		},
		{
			name:        "agentic-workflows with true",
			toolValue:   true,
			shouldWork:  true,
			description: "Should enable agentic-workflows with boolean true",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a minimal workflow with the agentic-workflows tool
			frontmatter := map[string]any{
				"on":    "workflow_dispatch",
				"tools": map[string]any{"agentic-workflows": tt.toolValue},
			}

			// Create compiler using helper
			c := testCompiler()

			// Extract tools from frontmatter
			tools := extractToolsFromFrontmatter(frontmatter)

			// Merge tools
			mergedTools, err := c.mergeToolsAndMCPServers(tools, make(map[string]any), "")

			if tt.shouldWork {
				require.NoError(t, err, "agentic-workflows tool should merge without errors for: %s", tt.description)
				assert.Contains(t, mergedTools, "agentic-workflows",
					"merged tools should contain agentic-workflows after successful merge")
			} else {
				require.Error(t, err, "agentic-workflows tool should fail for: %s", tt.description)
			}
		})
	}
}

func TestAgenticWorkflowsMCPConfigGeneration(t *testing.T) {
	engines := []struct {
		name   string
		engine CodingAgentEngine
	}{
		{"Claude", NewClaudeEngine()},
		{"Copilot", NewCopilotEngine()},
		{"Custom", NewCustomEngine()},
		{"Codex", NewCodexEngine()},
	}

	for _, e := range engines {
		t.Run(e.name, func(t *testing.T) {
			// Create workflow data using helper
			workflowData := workflowDataWithAgenticWorkflows()

			// Generate MCP config
			var yaml strings.Builder
			mcpTools := []string{"agentic-workflows"}

			e.engine.RenderMCPConfig(&yaml, workflowData.Tools, mcpTools, workflowData)
			result := yaml.String()

			// Verify the MCP config contains agentic-workflows
			assert.Contains(t, result, constants.AgenticWorkflowsMCPServerID,
				"%s engine should generate MCP config with agenticworkflows server name", e.name)
			assert.Contains(t, result, "gh",
				"%s engine MCP config should use gh CLI command for agentic-workflows", e.name)
			assert.Contains(t, result, "mcp-server",
				"%s engine MCP config should include mcp-server argument for gh-aw extension", e.name)
		})
	}
}

func TestAgenticWorkflowsHasMCPServers(t *testing.T) {
	// Create workflow data using helper
	workflowData := workflowDataWithAgenticWorkflows()

	assert.True(t, HasMCPServers(workflowData),
		"HasMCPServers should return true when agentic-workflows tool is configured")
}

func TestAgenticWorkflowsInstallStepIncludesGHToken(t *testing.T) {
	// Create workflow data using helper
	workflowData := workflowDataWithAgenticWorkflows()

	// Create compiler using helper
	c := testCompiler()

	// Generate MCP setup
	var yaml strings.Builder
	engine := NewCopilotEngine()

	c.generateMCPSetup(&yaml, workflowData.Tools, engine, workflowData)
	result := yaml.String()

	// Verify the install step is present
	assert.Contains(t, result, "Install gh-aw extension",
		"MCP setup should include gh-aw installation step when agentic-workflows tool is enabled and no import is present")

	// Verify GH_TOKEN environment variable is set with the default token expression
	assert.Contains(t, result, "GH_TOKEN: ${{ secrets.GH_AW_GITHUB_MCP_SERVER_TOKEN || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}",
		"install step should use default GH_TOKEN fallback chain when no custom token is specified")

	// Verify the install commands are present
	assert.Contains(t, result, "gh extension install github/gh-aw",
		"install step should include command to install gh-aw extension")
	assert.Contains(t, result, "gh aw --version",
		"install step should include command to verify gh-aw installation")

	// Verify the binary copy command is present for MCP server containerization
	assert.Contains(t, result, "cp \"$GH_AW_BIN\" /opt/gh-aw/gh-aw",
		"install step should copy gh-aw binary to /opt/gh-aw for MCP server containerization")
}

func TestAgenticWorkflowsInstallStepWithCustomToken(t *testing.T) {
	// Create workflow data using helper with custom token option
	workflowData := workflowDataWithAgenticWorkflows(
		withCustomToken("${{ secrets.CUSTOM_PAT }}"),
	)

	// Create compiler using helper
	c := testCompiler()

	// Generate MCP setup
	var yaml strings.Builder
	engine := NewCopilotEngine()

	c.generateMCPSetup(&yaml, workflowData.Tools, engine, workflowData)
	result := yaml.String()

	// Verify the install step is present
	assert.Contains(t, result, "Install gh-aw extension",
		"MCP setup should include gh-aw installation step even with custom token")

	// Verify GH_TOKEN environment variable is set with the custom token
	assert.Contains(t, result, "GH_TOKEN: ${{ secrets.CUSTOM_PAT }}",
		"install step should use custom GitHub token when specified in workflow config")

	// Verify it doesn't use the default token when custom is provided
	assert.NotContains(t, result, "GH_TOKEN: ${{ secrets.GH_AW_GITHUB_MCP_SERVER_TOKEN || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}",
		"install step should not use default token fallback when custom token is specified")
}

func TestAgenticWorkflowsInstallStepSkippedWithImport(t *testing.T) {
	// Create workflow data using helper with imported files option
	workflowData := workflowDataWithAgenticWorkflows(
		withImportedFiles("shared/mcp/gh-aw.md"),
	)

	// Create compiler using helper
	c := testCompiler()

	// Generate MCP setup
	var yaml strings.Builder
	engine := NewCopilotEngine()

	c.generateMCPSetup(&yaml, workflowData.Tools, engine, workflowData)
	result := yaml.String()

	// Verify the install step is NOT present when import exists
	assert.NotContains(t, result, "Install gh-aw extension",
		"install step should be skipped when shared/mcp/gh-aw.md is imported")

	// Verify the install command is also not present
	assert.NotContains(t, result, "gh extension install github/gh-aw",
		"gh extension install command should be absent when shared/mcp/gh-aw.md is imported")
}

func TestAgenticWorkflowsInstallStepPresentWithoutImport(t *testing.T) {
	// Create workflow data using helper with empty imports
	workflowData := workflowDataWithAgenticWorkflows(
		withImportedFiles(), // Empty imports
	)

	// Create compiler using helper
	c := testCompiler()

	// Generate MCP setup
	var yaml strings.Builder
	engine := NewCopilotEngine()

	c.generateMCPSetup(&yaml, workflowData.Tools, engine, workflowData)
	result := yaml.String()

	// Verify the install step IS present when no import exists
	assert.Contains(t, result, "Install gh-aw extension",
		"install step should be present when shared/mcp/gh-aw.md is NOT imported")

	// Verify the install command is present
	assert.Contains(t, result, "gh extension install github/gh-aw",
		"gh extension install command should be present when shared/mcp/gh-aw.md is NOT imported")
}

// TestAgenticWorkflowsErrorCases tests error handling for invalid configurations
func TestAgenticWorkflowsErrorCases(t *testing.T) {
	tests := []struct {
		name          string
		toolValue     any
		expectedError bool
		description   string
	}{
		{
			name:          "agentic-workflows with false",
			toolValue:     false,
			expectedError: false,
			description:   "Should allow explicitly disabling agentic-workflows with false",
		},
		{
			name:          "agentic-workflows with empty map",
			toolValue:     map[string]any{},
			expectedError: false,
			description:   "Should handle empty configuration map without error",
		},
		{
			name:          "agentic-workflows with string value",
			toolValue:     "enabled",
			expectedError: false,
			description:   "Should handle string value (non-standard but permitted)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a minimal workflow with the agentic-workflows tool
			frontmatter := map[string]any{
				"on":    "workflow_dispatch",
				"tools": map[string]any{"agentic-workflows": tt.toolValue},
			}

			// Create compiler using helper
			c := testCompiler()

			// Extract tools from frontmatter
			tools := extractToolsFromFrontmatter(frontmatter)

			// Merge tools
			mergedTools, err := c.mergeToolsAndMCPServers(tools, make(map[string]any), "")

			if tt.expectedError {
				require.Error(t, err, "should fail for: %s", tt.description)
			} else {
				require.NoError(t, err, "should succeed for: %s", tt.description)
				// When tool is false, it should not be in merged tools (or be explicitly false)
				if tt.toolValue == false {
					// The tool might be present but set to false, or absent entirely
					if val, exists := mergedTools["agentic-workflows"]; exists {
						assert.False(t, val.(bool), "agentic-workflows should be false when explicitly disabled")
					}
				} else {
					// For other values, the tool should be present
					assert.Contains(t, mergedTools, "agentic-workflows",
						"merged tools should contain agentic-workflows for non-false values")
				}
			}
		})
	}
}

// TestAgenticWorkflowsNilSafety tests nil and empty input handling
func TestAgenticWorkflowsNilSafety(t *testing.T) {
	tests := []struct {
		name          string
		workflowData  *WorkflowData
		shouldHaveMCP bool
		description   string
	}{
		{
			name:          "nil workflow data",
			workflowData:  nil,
			shouldHaveMCP: false,
			description:   "Should handle nil workflow data gracefully",
		},
		{
			name: "nil tools map",
			workflowData: &WorkflowData{
				Tools: nil,
			},
			shouldHaveMCP: false,
			description:   "Should handle nil tools map gracefully",
		},
		{
			name: "empty tools map",
			workflowData: &WorkflowData{
				Tools: make(map[string]any),
			},
			shouldHaveMCP: false,
			description:   "Should handle empty tools map gracefully",
		},
		{
			name: "agentic-workflows with nil value",
			workflowData: &WorkflowData{
				Tools: map[string]any{
					"agentic-workflows": nil,
				},
			},
			shouldHaveMCP: true,
			description:   "Should detect agentic-workflows tool even with nil value",
		},
		{
			name: "agentic-workflows explicitly disabled",
			workflowData: &WorkflowData{
				Tools: map[string]any{
					"agentic-workflows": false,
				},
			},
			shouldHaveMCP: false,
			description:   "Should not detect MCP servers when agentic-workflows is explicitly false",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test that HasMCPServers doesn't panic
			var result bool
			assert.NotPanics(t, func() {
				result = HasMCPServers(tt.workflowData)
			}, "HasMCPServers should handle nil/empty data gracefully without panicking")

			// Verify the expected result
			assert.Equal(t, tt.shouldHaveMCP, result,
				"HasMCPServers result for: %s", tt.description)
		})
	}
}

// TestAgenticWorkflowsExtractToolsEdgeCases tests edge cases in extractToolsFromFrontmatter
func TestAgenticWorkflowsExtractToolsEdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter map[string]any
		expectTools bool
		description string
	}{
		{
			name:        "nil frontmatter",
			frontmatter: nil,
			expectTools: false,
			description: "Should handle nil frontmatter without panic",
		},
		{
			name:        "empty frontmatter",
			frontmatter: map[string]any{},
			expectTools: false,
			description: "Should handle empty frontmatter",
		},
		{
			name: "frontmatter without tools",
			frontmatter: map[string]any{
				"on": "workflow_dispatch",
			},
			expectTools: false,
			description: "Should handle frontmatter without tools field",
		},
		{
			name: "tools with invalid type (string)",
			frontmatter: map[string]any{
				"tools": "not-a-map",
			},
			expectTools: false,
			description: "Should handle tools field with invalid type",
		},
		{
			name: "tools with nil value",
			frontmatter: map[string]any{
				"tools": nil,
			},
			expectTools: false,
			description: "Should handle tools field with nil value",
		},
		{
			name: "valid tools with agentic-workflows",
			frontmatter: map[string]any{
				"tools": map[string]any{
					"agentic-workflows": nil,
				},
			},
			expectTools: true,
			description: "Should extract valid tools configuration",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test that extractToolsFromFrontmatter doesn't panic
			var result map[string]any
			assert.NotPanics(t, func() {
				result = extractToolsFromFrontmatter(tt.frontmatter)
			}, "extractToolsFromFrontmatter should handle edge cases without panicking")

			// Verify the expected result
			if tt.expectTools {
				assert.NotNil(t, result, "should extract tools for: %s", tt.description)
				assert.NotEmpty(t, result, "should extract non-empty tools for: %s", tt.description)
				assert.Contains(t, result, "agentic-workflows",
					"extracted tools should contain agentic-workflows for: %s", tt.description)
			} else {
				// ExtractMapField returns empty map (not nil) when field is missing or invalid
				assert.NotNil(t, result, "extractToolsFromFrontmatter should always return non-nil map")
				assert.Empty(t, result, "should return empty tools map for: %s", tt.description)
			}
		})
	}
}
