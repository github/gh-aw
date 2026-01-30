//go:build !integration

package parser

import (
	"strings"
	"testing"
)

// TestAdditionalPropertiesFalse_CommonTypos tests that common typos in frontmatter
// are properly rejected by the schema validation due to additionalProperties: false
func TestAdditionalPropertiesFalse_CommonTypos(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter map[string]any
		typoField   string // The typo field name that should be rejected
	}{
		{
			name: "typo: permisions instead of permissions", //nolint:misspell
			frontmatter: map[string]any{
				"on":         "push",
				"permisions": "write-all", //nolint:misspell // typo: should be "permissions"
			},
			typoField: "permisions", //nolint:misspell
		},
		{
			name: "typo: engnie instead of engine",
			frontmatter: map[string]any{
				"on":     "push",
				"engnie": "claude", // typo: should be "engine"
			},
			typoField: "engnie",
		},
		{
			name: "typo: toolz instead of tools",
			frontmatter: map[string]any{
				"on": "push",
				"toolz": map[string]any{ // typo: should be "tools"
					"github": nil,
				},
			},
			typoField: "toolz",
		},
		{
			name: "typo: timeout_minute instead of timeout_minutes",
			frontmatter: map[string]any{
				"on":             "push",
				"timeout_minute": 10, // typo: should be "timeout_minutes"
			},
			typoField: "timeout_minute",
		},
		{
			name: "typo: runs_on instead of runs-on",
			frontmatter: map[string]any{
				"on":      "push",
				"runs_on": "ubuntu-latest", // typo: should be "runs-on" with dash
			},
			typoField: "runs_on",
		},
		{
			name: "typo: safe_outputs instead of safe-outputs",
			frontmatter: map[string]any{
				"on": "push",
				"safe_outputs": map[string]any{ // typo: should be "safe-outputs" with dash
					"create-issue": nil,
				},
			},
			typoField: "safe_outputs",
		},
		{
			name: "typo: mcp_servers instead of mcp-servers",
			frontmatter: map[string]any{
				"on": "push",
				"mcp_servers": map[string]any{ // typo: should be "mcp-servers" with dash
					"test": map[string]any{
						"command": "test",
					},
				},
			},
			typoField: "mcp_servers",
		},
		{
			name: "multiple typos: permisions, engnie, toolz", //nolint:misspell
			frontmatter: map[string]any{
				"on":         "push",
				"permisions": "write-all", //nolint:misspell // typo
				"engnie":     "claude",    // typo
				"toolz": map[string]any{ // typo
					"github": nil,
				},
			},
			typoField: "permisions", //nolint:misspell // error should mention at least one typo
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMainWorkflowFrontmatterWithSchema(tt.frontmatter)

			if err == nil {
				t.Fatalf("Expected validation error for typo field '%s', but validation passed", tt.typoField)
			}

			errorMsg := err.Error()

			// The error should mention unknown/additional properties
			if !strings.Contains(strings.ToLower(errorMsg), "unknown") &&
				!strings.Contains(strings.ToLower(errorMsg), "additional") &&
				!strings.Contains(strings.ToLower(errorMsg), "not allowed") {
				t.Errorf("Error message should mention unknown/additional properties, got: %s", errorMsg)
			}

			// The error should mention the typo field
			if !strings.Contains(errorMsg, tt.typoField) {
				t.Errorf("Error message should mention the typo field '%s', got: %s", tt.typoField, errorMsg)
			}
		})
	}
}

// TestAdditionalPropertiesFalse_IncludedFileSchema tests that the included file schema
// also rejects unknown properties
func TestAdditionalPropertiesFalse_IncludedFileSchema(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter map[string]any
		typoField   string
	}{
		{
			name: "typo in included file: toolz instead of tools",
			frontmatter: map[string]any{
				"toolz": map[string]any{ // typo: should be "tools"
					"github": nil,
				},
			},
			typoField: "toolz",
		},
		{
			name: "typo in included file: mcp_servers instead of mcp-servers",
			frontmatter: map[string]any{
				"mcp_servers": map[string]any{ // typo: should be "mcp-servers"
					"test": map[string]any{
						"command": "test",
					},
				},
			},
			typoField: "mcp_servers",
		},
		{
			name: "typo in included file: safe_outputs instead of safe-outputs",
			frontmatter: map[string]any{
				"safe_outputs": map[string]any{ // typo: should be "safe-outputs"
					"jobs": map[string]any{
						"test": map[string]any{
							"inputs": map[string]any{
								"test": map[string]any{
									"type": "string",
								},
							},
						},
					},
				},
			},
			typoField: "safe_outputs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateIncludedFileFrontmatterWithSchema(tt.frontmatter)

			if err == nil {
				t.Fatalf("Expected validation error for typo field '%s', but validation passed", tt.typoField)
			}

			errorMsg := err.Error()

			// The error should mention the typo field
			if !strings.Contains(errorMsg, tt.typoField) {
				t.Errorf("Error message should mention the typo field '%s', got: %s", tt.typoField, errorMsg)
			}
		})
	}
}

// TestAdditionalPropertiesFalse_MCPConfigSchema tests that the MCP config schema
// also rejects unknown properties
func TestAdditionalPropertiesFalse_MCPConfigSchema(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter map[string]any
		typoField   string
	}{
		{
			name: "typo in MCP config: comand instead of command",
			frontmatter: map[string]any{
				"comand": "npx", // typo: should be "command"
			},
			typoField: "comand",
		},
		{
			name: "typo in MCP config: typ instead of type",
			frontmatter: map[string]any{
				"typ":     "stdio", // typo: should be "type"
				"command": "test",
			},
			typoField: "typ",
		},
		{
			name: "typo in MCP config: environement instead of env",
			frontmatter: map[string]any{
				"command": "test",
				"environement": map[string]any{ // typo: should be "env"
					"TEST": "value",
				},
			},
			typoField: "environement",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMCPConfigWithSchema(tt.frontmatter, "test-tool")

			if err == nil {
				t.Fatalf("Expected validation error for typo field '%s', but validation passed", tt.typoField)
			}

			errorMsg := err.Error()

			// The error should mention the typo field
			if !strings.Contains(errorMsg, tt.typoField) {
				t.Errorf("Error message should mention the typo field '%s', got: %s", tt.typoField, errorMsg)
			}
		})
	}
}

// TestValidProperties_NotRejected ensures that valid properties are still accepted
func TestValidProperties_NotRejected(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter map[string]any
	}{
		{
			name: "valid main workflow with all common fields",
			frontmatter: map[string]any{
				"on":          "push",
				"permissions": "read-all",
				"engine":      "claude",
				"tools": map[string]any{
					"github": nil,
				},
				"timeout-minutes": 10,
				"runs-on":         "ubuntu-latest",
				"safe-outputs": map[string]any{
					"create-issue": nil,
				},
				"mcp-servers": map[string]any{
					"test": map[string]any{
						"command": "test",
					},
				},
			},
		},
		{
			name: "valid minimal workflow",
			frontmatter: map[string]any{
				"on": "push",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMainWorkflowFrontmatterWithSchema(tt.frontmatter)

			if err != nil {
				t.Fatalf("Expected no validation error for valid frontmatter, got: %v", err)
			}
		})
	}
}
