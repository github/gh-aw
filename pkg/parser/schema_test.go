//go:build !integration

package parser

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestValidateMainWorkflowFrontmatter_IssueFieldActivityTypes(t *testing.T) {
	frontmatter := map[string]any{
		"on": map[string]any{
			"issues": map[string]any{
				"types": []any{"typed", "untyped", "field_added", "field_removed"},
			},
		},
		"engine": "copilot",
	}

	if err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "workflow.md"); err != nil {
		t.Fatalf("expected issue field activity types to validate: %v", err)
	}
}

func TestValidateMainWorkflowFrontmatter_RejectsUnsupportedTopLevelFields(t *testing.T) {
	t.Parallel()

	for _, field := range []string{"version", "include", "bots"} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()

			err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{
				"on":  "workflow_dispatch",
				field: "unsupported",
			}, "workflow.md")
			if err == nil {
				t.Fatalf("expected unsupported top-level %q field to be rejected", field)
			}

			if !strings.Contains(err.Error(), field) {
				t.Fatalf("expected error to mention %q, got: %v", field, err)
			}
		})
	}
}

func TestValidateMainWorkflowFrontmatter_MetadataDocs(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name    string
		docs    any
		wantErr bool
	}{
		{name: "absolute HTTPS URL", docs: "https://docs.example.com/workflows/repo-health"},
		{name: "repository documentation URL", docs: "https://github.com/OWNER/REPO/blob/main/docs/workflows/repository-health.md"},
		{name: "relative path", docs: "docs/workflows/repo-health.md", wantErr: true},
		{name: "non-HTTPS URL", docs: "http://docs.example.com/workflows/repo-health", wantErr: true},
		{name: "JavaScript URL", docs: "javascript:alert(1)", wantErr: true},
		{name: "missing host", docs: "https://:", wantErr: true},
		{name: "invalid port", docs: "https://example.com:99999", wantErr: true},
		{name: "empty string", docs: "", wantErr: true},
		{name: "object", docs: map[string]any{"user": "https://docs.example.com/user-guide"}, wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{
				"on": "workflow_dispatch",
				"metadata": map[string]any{
					"docs": tt.docs,
				},
			}, "workflow.md")
			if (err != nil) != tt.wantErr {
				t.Fatalf("metadata.docs validation error = %v, wantErr %t", err, tt.wantErr)
			}
		})
	}
}

func TestValidateMainWorkflowFrontmatter_Plugins(t *testing.T) {
	valid := map[string]any{
		"on":      "workflow_dispatch",
		"engine":  "copilot",
		"plugins": []any{"github/awesome-copilot/plugins/example@v1"},
	}
	if err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(valid, "workflow.md"); err != nil {
		t.Fatalf("expected plugins to validate: %v", err)
	}

	for _, test := range []struct {
		name    string
		plugins any
	}{
		{name: "empty list", plugins: []any{}},
		{name: "missing ref", plugins: []any{"github/awesome-copilot"}},
		{name: "expression", plugins: []any{"${{ inputs.plugin }}"}},
		{name: "duplicate", plugins: []any{"github/awesome-copilot@v1", "github/awesome-copilot@v1"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			frontmatter := map[string]any{
				"on":      "workflow_dispatch",
				"engine":  "copilot",
				"plugins": test.plugins,
			}
			if err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "workflow.md"); err == nil {
				t.Fatalf("expected invalid plugins value to be rejected: %#v", test.plugins)
			}
		})
	}
}

func TestValidateMainWorkflowFrontmatter_UserRateLimitMaxField(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name        string
		rateLimit   map[string]any
		wantErr     bool
		errContains string
	}{
		{
			name: "canonical max-runs-per-window",
			rateLimit: map[string]any{
				"max-runs-per-window": 5,
			},
		},
		{
			name:        "legacy max-runs alias",
			rateLimit:   map[string]any{"max-runs": 5},
			wantErr:     true,
			errContains: "Unknown property: max-runs",
		},
		{
			name:        "legacy max alias",
			rateLimit:   map[string]any{"max": 5},
			wantErr:     true,
			errContains: "Unknown property: max",
		},
		{
			name:        "legacy max-runs expression alias",
			rateLimit:   map[string]any{"max-runs": "${{ inputs.max_runs }}"},
			wantErr:     true,
			errContains: "Unknown property: max-runs",
		},
		{
			name:      "missing max field",
			rateLimit: map[string]any{"window": 60},
			wantErr:   true,
		},
		{
			name:        "unknown nested field",
			rateLimit:   map[string]any{"max-runs-per-window": 5, "limit": 5},
			wantErr:     true,
			errContains: "Unknown property: limit",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{
				"on":              "workflow_dispatch",
				"user-rate-limit": tt.rateLimit,
			}, "workflow.md")
			if (err != nil) != tt.wantErr {
				t.Fatalf("validation error = %v, wantErr %t", err, tt.wantErr)
			}
			if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
				t.Fatalf("validation error = %v, want substring %q", err, tt.errContains)
			}
		})
	}
}

func TestValidateMainWorkflowFrontmatterEnclaves(t *testing.T) {
	valid := map[string]any{
		"on":     "workflow_dispatch",
		"engine": "copilot",
		"enclaves": []any{
			map[string]any{
				"script": nil,
				"repos": []any{
					map[string]any{"repo": "octo-org/private-service", "sensitivity": "confidential"},
				},
				"timeout": 45,
			},
			map[string]any{
				"agent": map[string]any{
					"model":  "gpt-5",
					"github": map[string]any{"cli": "issues-read-v1"},
				},
				"repos": []any{
					map[string]any{"repo": "octo-org/private-service", "sensitivity": "confidential"},
				},
				"timeout": 4740,
			},
		},
	}
	if err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(valid, "workflow.md"); err != nil {
		t.Fatalf("expected keyed top-level enclaves to validate: %v", err)
	}

	toolsShape := map[string]any{
		"on":     "workflow_dispatch",
		"engine": "copilot",
		"enclaves": []any{
			map[string]any{
				"agent": map[string]any{
					"model": "gpt-5",
					"tools": map[string]any{
						"github": map[string]any{
							"allowed":       []any{"list_issues", "issue_read"},
							"allowed-repos": []any{"octo-org/private-service"},
							"min-integrity": "none",
						},
					},
				},
				"repos": []any{
					map[string]any{"repo": "octo-org/private-service", "sensitivity": "confidential"},
				},
			},
		},
	}
	if err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(toolsShape, "workflow.md"); err != nil {
		t.Fatalf("expected enclave agent.tools.github shape to validate: %v", err)
	}

	dynamicShape := map[string]any{
		"on":     "workflow_dispatch",
		"engine": "copilot",
		"enclaves": []any{
			map[string]any{
				"agent": map[string]any{
					"model":              "gpt-5",
					"max-task-bytes":     4096,
					"max-model-requests": 8,
					"max-model-tokens":   1024,
				},
				"dynamic": map[string]any{
					"allowed-repositories": []any{"octo-org/private-service"},
					"sensitivity":          "confidential",
					"github-policy":        "github-repository-read-v1",
					"max-repositories":     4,
					"quotas": map[string]any{
						"max-invocations":       8,
						"max-output-bytes":      32768,
						"max-execution-seconds": 900,
					},
					"audit-labels": []any{"dynamic-enclave"},
					"expires-at":   "2999-01-01T00:00:00Z",
				},
				"timeout":          120,
				"memory-limit":     "512m",
				"cpu-limit":        "1",
				"pids-limit":       128,
				"tmpfs-limit":      "64m",
				"max-output-bytes": 8192,
				"max-invocations":  8,
			},
		},
	}
	if err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(dynamicShape, "workflow.md"); err != nil {
		t.Fatalf("expected dynamic enclave agent policy shape to validate: %v", err)
	}

	valid["enclaves"].([]any)[0].(map[string]any)["repos"].([]any)[0].(map[string]any)["sensitivity"] = "trusted"
	if err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(valid, "workflow.md"); err != nil {
		t.Fatalf("expected trusted enclave sensitivity to validate: %v", err)
	}
	valid["enclaves"].([]any)[0].(map[string]any)["repos"].([]any)[0].(map[string]any)["sensitivity"] = "unsupported"
	if err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(valid, "workflow.md"); err == nil {
		t.Fatal("expected unsupported enclave sensitivity to be rejected")
	}

	legacy := map[string]any{
		"on":     "workflow_dispatch",
		"engine": "copilot",
		"sandbox": map[string]any{
			"enclaves": []any{
				map[string]any{
					"type":         "script",
					"repositories": []any{},
				},
			},
		},
	}
	if err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(legacy, "workflow.md"); err == nil {
		t.Fatal("expected legacy sandbox.enclaves shape to be rejected")
	}

	tooLong := map[string]any{
		"on":     "workflow_dispatch",
		"engine": "copilot",
		"enclaves": []any{
			map[string]any{
				"agent": map[string]any{"model": "gpt-5"},
				"repos": []any{
					map[string]any{"repo": "octo-org/private-service", "sensitivity": "confidential"},
				},
				"timeout": 4741,
			},
		},
	}
	if err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(tooLong, "workflow.md"); err == nil {
		t.Fatal("expected enclave timeout above 4740 seconds to be rejected")
	}

	invalidMode := valid
	invalidMode["enclaves"].([]any)[1].(map[string]any)["agent"].(map[string]any)["github"] = map[string]any{"cli": "read-only"}
	if err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(invalidMode, "workflow.md"); err == nil {
		t.Fatal("expected generic enclave GitHub CLI mode to be rejected")
	}

	invalidTool := toolsShape
	invalidTool["enclaves"].([]any)[0].(map[string]any)["agent"].(map[string]any)["tools"].(map[string]any)["github"] = map[string]any{
		"allowed": []any{"search_issues"},
	}
	if err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(invalidTool, "workflow.md"); err == nil {
		t.Fatal("expected unsupported enclave GitHub tool to be rejected")
	}

	invalidRepoScope := toolsShape
	invalidRepoScope["enclaves"].([]any)[0].(map[string]any)["agent"].(map[string]any)["tools"].(map[string]any)["github"] = map[string]any{
		"allowed":       []any{"list_issues"},
		"allowed-repos": "all",
	}
	if err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(invalidRepoScope, "workflow.md"); err == nil {
		t.Fatal("expected scalar enclave GitHub repository scope to be rejected")
	}

	dynamicScript := map[string]any{
		"on":     "workflow_dispatch",
		"engine": "copilot",
		"enclaves": []any{
			map[string]any{
				"script":  nil,
				"dynamic": dynamicShape["enclaves"].([]any)[0].(map[string]any)["dynamic"],
			},
		},
	}
	if err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(dynamicScript, "workflow.md"); err == nil {
		t.Fatal("expected dynamic script enclave to be rejected")
	}

	staticAndDynamic := dynamicShape
	staticAndDynamic["enclaves"].([]any)[0].(map[string]any)["repos"] = []any{
		map[string]any{"repo": "octo-org/private-service", "sensitivity": "confidential"},
	}
	if err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(staticAndDynamic, "workflow.md"); err == nil {
		t.Fatal("expected static and dynamic enclave declaration to be rejected")
	}

	unknownDynamicPolicy := map[string]any{
		"on":       "workflow_dispatch",
		"engine":   "copilot",
		"enclaves": dynamicShape["enclaves"],
	}
	delete(staticAndDynamic["enclaves"].([]any)[0].(map[string]any), "repos")
	unknownDynamicPolicy["enclaves"].([]any)[0].(map[string]any)["dynamic"].(map[string]any)["github-policy"] = "github-repository-write-v1"
	if err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(unknownDynamicPolicy, "workflow.md"); err == nil {
		t.Fatal("expected unknown dynamic GitHub policy to be rejected")
	}

	scriptGitHub := map[string]any{
		"on":     "workflow_dispatch",
		"engine": "copilot",
		"enclaves": []any{
			map[string]any{
				"script": map[string]any{"github": map[string]any{"cli": "issues-read-v1"}},
				"repos": []any{
					map[string]any{"repo": "octo-org/private-service", "sensitivity": "confidential"},
				},
			},
		},
	}
	if err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(scriptGitHub, "workflow.md"); err == nil {
		t.Fatal("expected script enclave GitHub configuration to be rejected")
	}
}

func TestValidateWithSchema(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter map[string]any
		schema      string
		context     string
		wantErr     bool
		errContains string
	}{
		{
			name: "valid data with simple schema",
			frontmatter: map[string]any{
				"name": "test",
			},
			schema: `{
				"type": "object",
				"properties": {
					"name": {"type": "string"}
				},
				"additionalProperties": false
			}`,
			context: "test context",
			wantErr: false,
		},
		{
			name: "invalid data with additional property",
			frontmatter: map[string]any{
				"name":    "test",
				"invalid": "value",
			},
			schema: `{
				"type": "object",
				"properties": {
					"name": {"type": "string"}
				},
				"additionalProperties": false
			}`,
			context:     "test context",
			wantErr:     true,
			errContains: "additional properties 'invalid' not allowed",
		},
		{
			name: "invalid schema JSON",
			frontmatter: map[string]any{
				"name": "test",
			},
			schema:      `invalid json`,
			context:     "test context",
			wantErr:     true,
			errContains: "schema validation error for test context",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateWithSchema(tt.frontmatter, tt.schema, tt.context)

			if tt.wantErr && err == nil {
				t.Errorf("validateWithSchema() expected error, got nil")
				return
			}

			if !tt.wantErr && err != nil {
				t.Errorf("validateWithSchema() error = %v", err)
				return
			}

			if tt.wantErr && err != nil && tt.errContains != "" {
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("validateWithSchema() error = %v, expected to contain %v", err, tt.errContains)
				}
			}
		})
	}
}

func TestValidateWithSchemaAndLocation_CleanedErrorMessage(t *testing.T) {
	// Test that error messages are properly cleaned of unhelpful jsonschema prefixes
	frontmatter := map[string]any{
		"on":               "push",
		"timeout_minu tes": 10, // Invalid property name with space
	}

	// Create a temporary test file
	tempFile := "/tmp/gh-aw/test_schema_validation.md"
	// Ensure the directory exists
	if err := os.MkdirAll("/tmp/gh-aw", 0755); err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	err := os.WriteFile(tempFile, []byte(`---
on: push
timeout_minu tes: 10
---

# Test workflow`), 0644)
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tempFile)

	err = ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, tempFile)

	if err == nil {
		t.Fatal("Expected validation error, got nil")
	}

	errorMsg := err.Error()

	// The error message should NOT contain the unhelpful jsonschema prefixes
	if strings.Contains(errorMsg, "jsonschema validation failed") {
		t.Errorf("Error message should not contain 'jsonschema validation failed' prefix, got: %s", errorMsg)
	}

	if strings.Contains(errorMsg, "- at '': ") {
		t.Errorf("Error message should not contain '- at '':' prefix, got: %s", errorMsg)
	}

	// The error message should contain the friendly rewritten error description
	if !strings.Contains(errorMsg, "Unknown property: timeout_minu tes") {
		t.Errorf("Error message should contain the validation error, got: %s", errorMsg)
	}

	// The error message should be formatted with location information
	if !strings.Contains(errorMsg, tempFile) {
		t.Errorf("Error message should contain file path, got: %s", errorMsg)
	}
}

func TestMainWorkflowSchema_UserRateLimitAllowsRepositoryDispatch(t *testing.T) {
	frontmatter := map[string]any{
		"on": "repository_dispatch",
		"user-rate-limit": map[string]any{
			"max-runs-per-window": 1,
			"events":              []any{"repository_dispatch"},
		},
	}

	if err := validateWithSchema(frontmatter, mainWorkflowSchema, "main workflow file"); err != nil {
		t.Fatalf("repository_dispatch should be allowed for user-rate-limit events: %v", err)
	}
}

// TestValidateMCPConfigWithSchema tests the ValidateMCPConfigWithSchema function
// which validates a single MCP server configuration against the MCP config JSON schema.
func TestValidateMCPConfigWithSchema(t *testing.T) {
	tests := []struct {
		name        string
		mcpConfig   map[string]any
		wantErr     bool
		errContains string
	}{
		{
			name: "valid stdio config with command",
			mcpConfig: map[string]any{
				"type":    "stdio",
				"command": "npx",
				"args":    []any{"-y", "@modelcontextprotocol/server-filesystem"},
			},
			wantErr: false,
		},
		{
			name: "valid stdio config with container",
			mcpConfig: map[string]any{
				"type":      "stdio",
				"container": "docker.io/mcp/brave-search",
				"env": map[string]any{
					"BRAVE_API_KEY": "secret",
				},
			},
			wantErr: false,
		},
		{
			name: "valid stdio config with digest-pinned container",
			mcpConfig: map[string]any{
				"type":      "stdio",
				"container": "ghcr.io/oraios/serena:latest@sha256:0944b2ffe66dbcddeed531694b6819d7f9efd8125b442b282a1cc863f570a03e",
			},
			wantErr: false,
		},
		{
			name: "valid http config",
			mcpConfig: map[string]any{
				"type": "http",
				"url":  "https://api.example.com/mcp",
				"headers": map[string]any{
					"Authorization": "Bearer token",
				},
			},
			wantErr: false,
		},
		{
			name: "valid config inferred from url requires explicit type in schema",
			mcpConfig: map[string]any{
				"type": "http",
				"url":  "http://localhost:8765",
			},
			wantErr: false,
		},
		{
			name:        "empty config fails anyOf - missing type, url, command, and container",
			mcpConfig:   map[string]any{},
			wantErr:     true,
			errContains: "missing property",
		},
		{
			name: "invalid container pattern rejected by schema",
			mcpConfig: map[string]any{
				"container": "INVALID CONTAINER NAME WITH SPACES",
			},
			wantErr:     true,
			errContains: "jsonschema validation failed",
		},
		{
			name: "invalid env key pattern rejected by schema",
			mcpConfig: map[string]any{
				"type":    "stdio",
				"command": "node",
				"env": map[string]any{
					"lowercase-key": "value",
				},
			},
			wantErr:     true,
			errContains: "jsonschema validation failed",
		},
		{
			name: "invalid mounts item pattern rejected by schema",
			mcpConfig: map[string]any{
				"type":      "stdio",
				"container": "mcp/server",
				"mounts":    []any{"invalid-mount-format"},
			},
			wantErr:     true,
			errContains: "jsonschema validation failed",
		},
		{
			name: "additional property rejected by schema",
			mcpConfig: map[string]any{
				"type":          "stdio",
				"command":       "node",
				"unknown-field": "value",
			},
			wantErr:     true,
			errContains: "jsonschema validation failed",
		},
		{
			name: "valid local type (alias for stdio)",
			mcpConfig: map[string]any{
				"type":    "local",
				"command": "node",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMCPConfigWithSchema(tt.mcpConfig)

			if tt.wantErr && err == nil {
				t.Errorf("ValidateMCPConfigWithSchema() expected error, got nil")
				return
			}

			if !tt.wantErr && err != nil {
				t.Errorf("ValidateMCPConfigWithSchema() unexpected error = %v", err)
				return
			}

			if tt.wantErr && err != nil && tt.errContains != "" {
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("ValidateMCPConfigWithSchema() error = %v, expected to contain %v", err, tt.errContains)
				}
			}
		})
	}
}

func TestValidateMainWorkflowFrontmatterWithSchemaAndLocation_WorkflowDispatchNumberInputType(t *testing.T) {
	t.Parallel()

	frontmatter := map[string]any{
		"on": map[string]any{
			"workflow_dispatch": map[string]any{
				"inputs": map[string]any{
					"max_retries": map[string]any{
						"description": "Maximum retries",
						"type":        "number",
						"default":     3,
						"required":    false,
					},
				},
			},
		},
		"engine": "copilot",
	}

	err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/tmp/gh-aw/workflow_dispatch_number_test.md")
	if err != nil {
		t.Fatalf("expected workflow_dispatch number input type to validate, got: %v", err)
	}
}

func TestValidateMainWorkflowFrontmatterWithSchemaAndLocation_RejectsEngineTokenWeights(t *testing.T) {
	t.Parallel()

	frontmatter := map[string]any{
		"on": "push",
		"engine": map[string]any{
			"id": "claude",
			"token-weights": map[string]any{
				"multipliers": map[string]any{
					"gpt-4o": 2.5,
				},
			},
		},
	}

	err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/tmp/gh-aw/engine-token-weights-rejected-test.md")
	if err == nil {
		t.Fatal("expected engine.token-weights to fail schema validation")
	}
	if !strings.Contains(err.Error(), "Unknown property: token-weights") {
		t.Fatalf("expected token-weights rejection error, got: %v", err)
	}
}

func TestValidateMainWorkflowFrontmatterWithSchemaAndLocation_RejectsGitHubBoundedQueries(t *testing.T) {
	t.Parallel()

	frontmatter := map[string]any{
		"on": "push",
		"tools": map[string]any{
			"github": map[string]any{
				"bounded-queries": map[string]any{
					"runtime": "sbx",
					"private-repos": []any{
						map[string]any{
							"name":        "octo-org/private-service",
							"sensitivity": "confidential",
						},
					},
				},
			},
		},
	}

	err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/tmp/gh-aw/github-bounded-queries-rejected-test.md")
	if err == nil {
		t.Fatal("expected tools.github.bounded-queries to fail schema validation")
	}
	if !strings.Contains(err.Error(), "Unknown property: bounded-queries") {
		t.Fatalf("expected bounded-queries rejection error, got: %v", err)
	}
}

func TestValidateMainWorkflowFrontmatterWithSchemaAndLocation_EngineHarnessPattern(t *testing.T) {
	t.Parallel()

	validFrontmatter := map[string]any{
		"on": "push",
		"engine": map[string]any{
			"id":      "claude",
			"harness": "custom_harness.cjs",
		},
	}

	err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(validFrontmatter, "/tmp/gh-aw/engine-harness-valid-pattern-test.md")
	if err != nil {
		t.Fatalf("expected valid engine.harness pattern to pass schema validation, got: %v", err)
	}

	invalidFrontmatter := map[string]any{
		"on": "push",
		"engine": map[string]any{
			"id":      "claude",
			"harness": "../driver.cjs",
		},
	}

	err = ValidateMainWorkflowFrontmatterWithSchemaAndLocation(invalidFrontmatter, "/tmp/gh-aw/engine-harness-invalid-pattern-test.md")
	if err == nil {
		t.Fatal("expected invalid engine.harness pattern to fail schema validation")
	}

	invalidFlagLikeFrontmatter := map[string]any{
		"on": "push",
		"engine": map[string]any{
			"id":      "claude",
			"harness": "-driver.cjs",
		},
	}

	err = ValidateMainWorkflowFrontmatterWithSchemaAndLocation(invalidFlagLikeFrontmatter, "/tmp/gh-aw/engine-harness-invalid-flaglike-pattern-test.md")
	if err == nil {
		t.Fatal("expected flag-like engine.harness pattern to fail schema validation")
	}
}

func TestValidateMainWorkflowFrontmatterWithSchemaAndLocation_EngineHarnessWatchdogTimeout(t *testing.T) {
	t.Parallel()

	validFrontmatter := map[string]any{
		"on": "push",
		"engine": map[string]any{
			"id": "copilot",
			"harness": map[string]any{
				"watchdog-timeout": 120,
			},
		},
	}

	err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(validFrontmatter, "/tmp/gh-aw/engine-harness-watchdog-timeout-valid-test.md")
	if err != nil {
		t.Fatalf("expected valid engine.harness.watchdog-timeout to pass schema validation, got: %v", err)
	}

	invalidFrontmatter := map[string]any{
		"on": "push",
		"engine": map[string]any{
			"id": "copilot",
			"harness": map[string]any{
				"watchdog-timeout": 0,
			},
		},
	}

	err = ValidateMainWorkflowFrontmatterWithSchemaAndLocation(invalidFrontmatter, "/tmp/gh-aw/engine-harness-watchdog-timeout-invalid-test.md")
	if err == nil {
		t.Fatal("expected non-positive engine.harness.watchdog-timeout to fail schema validation")
	}
}

func TestValidateMainWorkflowFrontmatterWithSchemaAndLocation_EngineDriverPattern(t *testing.T) {
	t.Parallel()

	validFrontmatter := map[string]any{
		"on": "push",
		"engine": map[string]any{
			"id":     "copilot",
			"driver": ".github/drivers/custom_copilot_sdk_driver.cjs",
		},
	}

	err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(validFrontmatter, "/tmp/gh-aw/engine-driver-valid-pattern-test.md")
	if err != nil {
		t.Fatalf("expected valid engine.driver pattern to pass schema validation, got: %v", err)
	}

	// Bare basename (no path) should still be valid.
	basenameDriverFrontmatter := map[string]any{
		"on": "push",
		"engine": map[string]any{
			"id":     "copilot",
			"driver": "custom_copilot_sdk_driver.cjs",
		},
	}

	err = ValidateMainWorkflowFrontmatterWithSchemaAndLocation(basenameDriverFrontmatter, "/tmp/gh-aw/engine-driver-basename-test.md")
	if err != nil {
		t.Fatalf("expected bare-basename engine.driver to pass schema validation, got: %v", err)
	}

	// Python driver should be valid.
	pythonDriverFrontmatter := map[string]any{
		"on": "push",
		"engine": map[string]any{
			"id":     "copilot",
			"driver": ".github/drivers/my_driver.py",
		},
	}

	err = ValidateMainWorkflowFrontmatterWithSchemaAndLocation(pythonDriverFrontmatter, "/tmp/gh-aw/engine-driver-python-test.md")
	if err != nil {
		t.Fatalf("expected Python engine.driver to pass schema validation, got: %v", err)
	}

	// TypeScript driver should be valid.
	tsDriverFrontmatter := map[string]any{
		"on": "push",
		"engine": map[string]any{
			"id":     "copilot",
			"driver": ".github/drivers/my_driver.ts",
		},
	}

	err = ValidateMainWorkflowFrontmatterWithSchemaAndLocation(tsDriverFrontmatter, "/tmp/gh-aw/engine-driver-ts-test.md")
	if err != nil {
		t.Fatalf("expected TypeScript engine.driver to pass schema validation, got: %v", err)
	}

	// Ruby driver should be valid.
	rubyDriverFrontmatter := map[string]any{
		"on": "push",
		"engine": map[string]any{
			"id":     "copilot",
			"driver": ".github/drivers/my_driver.rb",
		},
	}

	err = ValidateMainWorkflowFrontmatterWithSchemaAndLocation(rubyDriverFrontmatter, "/tmp/gh-aw/engine-driver-ruby-test.md")
	if err != nil {
		t.Fatalf("expected Ruby engine.driver to pass schema validation, got: %v", err)
	}

	// Arbitrary command (no extension) should be valid.
	arbitraryDriverFrontmatter := map[string]any{
		"on": "push",
		"engine": map[string]any{
			"id":     "copilot",
			"driver": "my-copilot-driver",
		},
	}

	err = ValidateMainWorkflowFrontmatterWithSchemaAndLocation(arbitraryDriverFrontmatter, "/tmp/gh-aw/engine-driver-arbitrary-test.md")
	if err != nil {
		t.Fatalf("expected arbitrary command engine.driver to pass schema validation, got: %v", err)
	}

	inlineNodeDriverFrontmatter := map[string]any{
		"on": "push",
		"engine": map[string]any{
			"id": "copilot",
			"driver": map[string]any{
				"node": "console.log('hi')",
			},
		},
	}

	err = ValidateMainWorkflowFrontmatterWithSchemaAndLocation(inlineNodeDriverFrontmatter, "/tmp/gh-aw/engine-driver-inline-node-test.md")
	if err != nil {
		t.Fatalf("expected inline node engine.driver to pass schema validation, got: %v", err)
	}

	inlineJavaDriverFrontmatter := map[string]any{
		"on": "push",
		"engine": map[string]any{
			"id": "copilot",
			"driver": map[string]any{
				"java": "class Main {}",
			},
		},
	}

	err = ValidateMainWorkflowFrontmatterWithSchemaAndLocation(inlineJavaDriverFrontmatter, "/tmp/gh-aw/engine-driver-inline-java-test.md")
	if err != nil {
		t.Fatalf("expected inline java engine.driver to pass schema validation, got: %v", err)
	}

	invalidFrontmatter := map[string]any{
		"on": "push",
		"engine": map[string]any{
			"id":     "copilot",
			"driver": "../driver.cjs",
		},
	}

	err = ValidateMainWorkflowFrontmatterWithSchemaAndLocation(invalidFrontmatter, "/tmp/gh-aw/engine-driver-invalid-pattern-test.md")
	if err == nil {
		t.Fatal("expected invalid engine.driver pattern to fail schema validation")
	}

	invalidFlagLikeFrontmatter := map[string]any{
		"on": "push",
		"engine": map[string]any{
			"id":     "copilot",
			"driver": "-driver.cjs",
		},
	}

	err = ValidateMainWorkflowFrontmatterWithSchemaAndLocation(invalidFlagLikeFrontmatter, "/tmp/gh-aw/engine-driver-invalid-flaglike-pattern-test.md")
	if err == nil {
		t.Fatal("expected flag-like engine.driver pattern to fail schema validation")
	}

	invalidInlineFrontmatter := map[string]any{
		"on": "push",
		"engine": map[string]any{
			"id": "copilot",
			"driver": map[string]any{
				"node":   "console.log('hi')",
				"python": "print('hi')",
			},
		},
	}

	err = ValidateMainWorkflowFrontmatterWithSchemaAndLocation(invalidInlineFrontmatter, "/tmp/gh-aw/engine-driver-invalid-inline-runtime-count-test.md")
	if err == nil {
		t.Fatal("expected multi-runtime inline engine.driver to fail schema validation")
	}
}

func TestValidateMainWorkflowFrontmatterWithSchemaAndLocation_EnginePermissionMode(t *testing.T) {
	t.Parallel()

	validFrontmatter := map[string]any{
		"on": "push",
		"engine": map[string]any{
			"id":              "claude",
			"permission-mode": "auto",
		},
	}

	err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(validFrontmatter, "/tmp/gh-aw/engine-permission-mode-valid-test.md")
	if err != nil {
		t.Fatalf("expected valid engine.permission-mode to pass schema validation, got: %v", err)
	}

	invalidFrontmatter := map[string]any{
		"on": "push",
		"engine": map[string]any{
			"id":              "claude",
			"permission-mode": "invalid-mode",
		},
	}

	err = ValidateMainWorkflowFrontmatterWithSchemaAndLocation(invalidFrontmatter, "/tmp/gh-aw/engine-permission-mode-invalid-test.md")
	if err == nil {
		t.Fatal("expected invalid engine.permission-mode to fail schema validation")
	}
}

func TestValidateMainWorkflowFrontmatterWithSchemaAndLocation_PermissionsNoneShorthand(t *testing.T) {
	t.Parallel()

	validFrontmatter := map[string]any{
		"on":          "push",
		"permissions": "none",
	}

	err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(validFrontmatter, "/workflow.md")
	if err != nil {
		t.Fatalf("expected 'permissions: none' to pass schema validation, got: %v", err)
	}
}

func TestValidateMainWorkflowFrontmatterWithSchemaAndLocation_ToolsEditBoolean(t *testing.T) {
	t.Parallel()

	frontmatter := map[string]any{
		"on": "push",
		"tools": map[string]any{
			"edit": false,
		},
	}

	err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/tmp/gh-aw/tools-edit-boolean-test.md")
	if err != nil {
		t.Fatalf("expected tools.edit=false to pass schema validation, got: %v", err)
	}
}

func TestValidateMainWorkflowFrontmatterWithSchemaAndLocation_LSPConfig(t *testing.T) {
	t.Parallel()

	frontmatter := map[string]any{
		"on":     "push",
		"engine": "copilot",
		"lsp": map[string]any{
			"typescript": map[string]any{
				"command": "typescript-language-server",
				"args":    []any{"--stdio"},
				"fileExtensions": map[string]any{
					".ts": "typescript",
				},
			},
		},
	}

	err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/tmp/gh-aw/lsp-config-test.md")
	if err != nil {
		t.Fatalf("expected valid lsp configuration to pass schema validation, got: %v", err)
	}
}

func TestValidateMainWorkflowFrontmatterWithSchemaAndLocation_MaxLimitsAllowExpressions(t *testing.T) {
	t.Parallel()

	validFrontmatter := map[string]any{
		"on":                   "push",
		"max-runs":             "${{ inputs.max-runs }}",
		"max-ai-credits":       "${{ inputs.max-ai-credits }}",
		"max-daily-ai-credits": "${{ inputs.max-daily-ai-credits }}",
	}

	err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(validFrontmatter, "/tmp/gh-aw/max-limits-expression-test.md")
	if err != nil {
		t.Fatalf("expected max-runs/max-ai-credits/max-daily-ai-credits expressions to pass schema validation, got: %v", err)
	}
}

func TestValidateMainWorkflowFrontmatterWithSchemaAndLocation_MaxLimitsAllowSuffixStrings(t *testing.T) {
	t.Parallel()

	validFrontmatter := map[string]any{
		"on":                   "push",
		"max-ai-credits":       "1k",
		"max-daily-ai-credits": "100k",
	}

	err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(validFrontmatter, "/tmp/gh-aw/max-limits-suffix-test.md")
	if err != nil {
		t.Fatalf("expected max-ai-credits/max-daily-ai-credits suffix strings to pass schema validation, got: %v", err)
	}
}

func TestValidateMainWorkflowFrontmatterWithSchemaAndLocation_MaxLimitsAllowSuffixStringsCaseVariants(t *testing.T) {
	t.Parallel()

	validFrontmatter := map[string]any{
		"on":                   "push",
		"max-ai-credits":       "2M",
		"max-daily-ai-credits": "100M",
	}

	err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(validFrontmatter, "/tmp/gh-aw/max-limits-suffix-case-variants-test.md")
	if err != nil {
		t.Fatalf("expected max-ai-credits/max-daily-ai-credits suffix case variants to pass schema validation, got: %v", err)
	}
}

func TestValidateMainWorkflowFrontmatterWithSchemaAndLocation_ThreatDetectionMaxAICreditsExpressionRejected(t *testing.T) {
	t.Parallel()

	// Expressions are not supported for safe-outputs.threat-detection.max-ai-credits
	// because the field is stored as int64 and expressions would be silently ignored
	// by the compiler. The schema enforces numeric-only values to prevent confusion.
	invalidFrontmatter := map[string]any{
		"on": "push",
		"safe-outputs": map[string]any{
			"threat-detection": map[string]any{
				"max-ai-credits": "${{ inputs.detection-max-ai-credits }}",
			},
		},
	}

	err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(invalidFrontmatter, "/tmp/gh-aw/threat-detection-max-ai-credits-expression-test.md")
	if err == nil {
		t.Fatal("expected safe-outputs.threat-detection.max-ai-credits expression to fail schema validation (expressions not supported)")
	}
}

func TestValidateMainWorkflowFrontmatterWithSchemaAndLocation_ThreatDetectionMaxAICreditsNumericAccepted(t *testing.T) {
	t.Parallel()

	for name, raw := range map[string]any{
		"integer":   750,
		"km-string": "2k",
		"m-string":  "1M",
		"neg-one":   -1,
		"neg-str":   "-1",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			validFrontmatter := map[string]any{
				"on": "push",
				"safe-outputs": map[string]any{
					"threat-detection": map[string]any{
						"max-ai-credits": raw,
					},
				},
			}

			err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(validFrontmatter, "/tmp/gh-aw/threat-detection-max-ai-credits-"+name+"-test.md")
			if err != nil {
				t.Fatalf("expected safe-outputs.threat-detection.max-ai-credits=%v (%s) to pass schema validation, got: %v", raw, name, err)
			}
		})
	}
}

func TestValidateMainWorkflowFrontmatterWithSchemaAndLocation_SafeOutputsStagedExpressionAccepted(t *testing.T) {
	t.Parallel()

	validFrontmatter := map[string]any{
		"on": "push",
		"safe-outputs": map[string]any{
			"staged": "${{ inputs.staged }}",
			"create-issue": map[string]any{
				"staged": "${{ inputs['issue-staged'] }}",
			},
		},
	}

	err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(validFrontmatter, "/tmp/gh-aw/safe-outputs-staged-expression-test.md")
	if err != nil {
		t.Fatalf("expected safe-outputs staged expressions to pass schema validation, got: %v", err)
	}
}

func TestValidateMainWorkflowFrontmatterWithSchemaAndLocation_MaxAICreditsZeroInvalid(t *testing.T) {
	t.Parallel()

	invalidFrontmatter := map[string]any{
		"on":             "push",
		"max-ai-credits": 0,
	}

	err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(invalidFrontmatter, "/tmp/gh-aw/max-ai-credits-zero-integer-test.md")
	if err == nil {
		t.Fatal("expected max-ai-credits=0 to fail schema validation")
	}
}

func TestValidateMainWorkflowFrontmatterWithSchemaAndLocation_MaxAICreditsNegativeAllowed(t *testing.T) {
	t.Parallel()

	for name, raw := range map[string]any{
		"integer": -1,
		"string":  "-1",
	} {
		t.Run(name, func(t *testing.T) {
			validFrontmatter := map[string]any{
				"on":             "push",
				"max-ai-credits": raw,
			}

			err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(validFrontmatter, "/tmp/gh-aw/max-ai-credits-negative-"+name+"-test.md")
			if err != nil {
				t.Fatalf("expected max-ai-credits=%v (%s) to pass schema validation, got: %v", raw, name, err)
			}
		})
	}
}

func TestValidateMainWorkflowFrontmatterWithSchemaAndLocation_MaxAICreditsOtherNegativeRejected(t *testing.T) {
	t.Parallel()

	invalidFrontmatter := map[string]any{
		"on":             "push",
		"max-ai-credits": -5,
	}

	err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(invalidFrontmatter, "/tmp/gh-aw/max-ai-credits-negative-other-test.md")
	if err == nil {
		t.Fatal("expected max-ai-credits=-5 to fail schema validation (only -1 is the disable sentinel)")
	}
}

func TestValidateMainWorkflowFrontmatterWithSchemaAndLocation_MaxTurnCacheMissesPositiveAccepted(t *testing.T) {
	t.Parallel()

	validFrontmatter := map[string]any{
		"on":                    "push",
		"max-turn-cache-misses": 5,
	}

	err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(validFrontmatter, "/tmp/gh-aw/max-turn-cache-misses-positive-test.md")
	if err != nil {
		t.Fatalf("expected max-turn-cache-misses=5 to pass schema validation, got: %v", err)
	}
}

func TestValidateMainWorkflowFrontmatterWithSchemaAndLocation_MaxTurnCacheMissesZeroRejected(t *testing.T) {
	t.Parallel()

	invalidFrontmatter := map[string]any{
		"on":                    "push",
		"max-turn-cache-misses": 0,
	}

	err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(invalidFrontmatter, "/tmp/gh-aw/max-turn-cache-misses-zero-test.md")
	if err == nil {
		t.Fatal("expected max-turn-cache-misses=0 to fail schema validation")
	}
}

func TestValidateMainWorkflowFrontmatterWithSchemaAndLocation_MaxDailyAICreditsZeroInvalid(t *testing.T) {
	t.Parallel()

	invalidFrontmatter := map[string]any{
		"on":                   "push",
		"max-daily-ai-credits": 0,
	}

	err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(invalidFrontmatter, "/tmp/gh-aw/max-daily-ai-credits-zero-integer-test.md")
	if err == nil {
		t.Fatal("expected max-daily-ai-credits=0 to fail schema validation")
	}
}

func TestValidateMainWorkflowFrontmatterWithSchemaAndLocation_MaxDailyAICreditsStringZeroInvalid(t *testing.T) {
	t.Parallel()

	invalidFrontmatter := map[string]any{
		"on":                   "push",
		"max-daily-ai-credits": "0",
	}

	err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(invalidFrontmatter, "/tmp/gh-aw/max-daily-ai-credits-zero-string-test.md")
	if err == nil {
		t.Fatal("expected max-daily-ai-credits='0' to fail schema validation")
	}
}

func TestValidateMainWorkflowFrontmatterWithSchemaAndLocation_MaxDailyAICreditsNegativeAllowed(t *testing.T) {
	t.Parallel()

	validFrontmatter := map[string]any{
		"on":                   "push",
		"max-daily-ai-credits": -1,
	}

	err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(validFrontmatter, "/tmp/gh-aw/max-daily-ai-credits-negative-test.md")
	if err != nil {
		t.Fatalf("expected negative max-daily-ai-credits to pass schema validation, got: %v", err)
	}
}

func TestValidateMainWorkflowFrontmatterWithSchemaAndLocation_SandboxAgentPlatform(t *testing.T) {
	t.Parallel()

	t.Run("valid platform is accepted", func(t *testing.T) {
		t.Parallel()

		frontmatter := map[string]any{
			"on": "push",
			"sandbox": map[string]any{
				"agent": map[string]any{
					"id":       "awf",
					"platform": "ghes",
				},
			},
		}

		err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/tmp/gh-aw/awf-platform-ghes-test.md")
		if err != nil {
			t.Fatalf("expected sandbox.agent.platform=ghes to pass schema validation, got: %v", err)
		}
	})

	t.Run("unknown platform is rejected", func(t *testing.T) {
		t.Parallel()

		frontmatter := map[string]any{
			"on": "push",
			"sandbox": map[string]any{
				"agent": map[string]any{
					"id":       "awf",
					"platform": "github-enterprise",
				},
			},
		}

		err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/tmp/gh-aw/awf-platform-invalid-test.md")
		if err == nil {
			t.Fatal("expected sandbox.agent.platform=github-enterprise to fail schema validation")
		}
	})
}

func TestValidateMainWorkflowFrontmatterWithSchemaAndLocation_WorkflowRunConclusion(t *testing.T) {
	t.Parallel()

	workflowRunFrontmatter := func(conclusion any) map[string]any {
		return map[string]any{
			"on": map[string]any{
				"workflow_run": map[string]any{
					"workflows":  []any{"CI"},
					"types":      []any{"completed"},
					"conclusion": conclusion,
				},
			},
		}
	}

	t.Run("conclusion as string is accepted", func(t *testing.T) {
		t.Parallel()

		frontmatter := workflowRunFrontmatter("startup_failure")

		if err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/tmp/gh-aw/workflow-run-conclusion-string-test.md"); err != nil {
			t.Fatalf("expected on.workflow_run.conclusion=startup_failure to pass schema validation, got: %v", err)
		}
	})

	t.Run("conclusion as array is accepted", func(t *testing.T) {
		t.Parallel()

		frontmatter := workflowRunFrontmatter([]any{"failure", "cancelled"})

		if err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/tmp/gh-aw/workflow-run-conclusion-array-test.md"); err != nil {
			t.Fatalf("expected on.workflow_run.conclusion (array) to pass schema validation, got: %v", err)
		}
	})

	t.Run("unknown conclusion value is rejected", func(t *testing.T) {
		t.Parallel()

		frontmatter := workflowRunFrontmatter([]any{"bogus"})

		if err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/tmp/gh-aw/workflow-run-conclusion-invalid-test.md"); err == nil {
			t.Fatal("expected on.workflow_run.conclusion with unknown value to fail schema validation")
		}
	})

	t.Run("empty conclusion array is rejected", func(t *testing.T) {
		t.Parallel()

		frontmatter := workflowRunFrontmatter([]any{})

		if err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/tmp/gh-aw/workflow-run-conclusion-empty-test.md"); err == nil {
			t.Fatal("expected an empty on.workflow_run.conclusion array to fail schema validation")
		}
	})
}

func TestValidateMainWorkflowFrontmatterWithSchemaAndLocation_OperationalValueGrader(t *testing.T) {
	t.Parallel()

	for _, runPath := range []string{
		".github/workflows/graders/example-operational-value.sh",
		".github/workflows/graders/..secret.sh",
		"./graders/example-operational-value.sh",
	} {
		t.Run(runPath, func(t *testing.T) {
			frontmatter := map[string]any{
				"on": "workflow_dispatch",
				"graders": map[string]any{
					"operational-value": map[string]any{
						"run": runPath,
					},
				},
			}

			if err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/tmp/gh-aw/operational-value-grader-test.md"); err != nil {
				t.Fatalf("expected operational-value evaluator to pass schema validation, got: %v", err)
			}
		})
	}
}

func TestMainWorkflowSchema_WorkflowDispatchNumberTypeDocumentation(t *testing.T) {
	t.Parallel()

	schemaPath := "schemas/main_workflow_schema.json"
	schemaContent, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("failed to read schema: %v", err)
	}

	var schema map[string]any
	if err := json.Unmarshal(schemaContent, &schema); err != nil {
		t.Fatalf("failed to parse schema json: %v", err)
	}

	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("schema properties section not found")
	}
	onField, ok := properties["on"].(map[string]any)
	if !ok {
		t.Fatal("'on' field not found in schema")
	}

	onOneOf, ok := onField["oneOf"].([]any)
	if !ok {
		t.Fatal("'on.oneOf' not found in schema")
	}

	var workflowDispatchInputType map[string]any
	for _, onEntry := range onOneOf {
		onEntryMap, ok := onEntry.(map[string]any)
		if !ok {
			continue
		}
		onProps, ok := onEntryMap["properties"].(map[string]any)
		if !ok {
			continue
		}
		eventsConfig, ok := onProps["workflow_dispatch"].(map[string]any)
		if !ok {
			continue
		}
		eventsOneOf, ok := eventsConfig["oneOf"].([]any)
		if !ok {
			continue
		}

		for _, eventEntry := range eventsOneOf {
			eventEntryMap, ok := eventEntry.(map[string]any)
			if !ok {
				continue
			}
			eventProps, ok := eventEntryMap["properties"].(map[string]any)
			if !ok {
				continue
			}
			inputsField, ok := eventProps["inputs"].(map[string]any)
			if !ok {
				continue
			}
			inputDefs, ok := inputsField["additionalProperties"].(map[string]any)
			if !ok {
				continue
			}
			inputDefProps, ok := inputDefs["properties"].(map[string]any)
			if !ok {
				continue
			}
			typeField, ok := inputDefProps["type"].(map[string]any)
			if !ok {
				t.Fatal("'on.workflow_dispatch.inputs.<id>.type' field missing")
			}
			workflowDispatchInputType = typeField
			break
		}
	}

	if workflowDispatchInputType == nil {
		t.Fatal("workflow_dispatch input type schema not found")
	}

	enumVals, ok := workflowDispatchInputType["enum"].([]any)
	if !ok {
		t.Fatal("workflow_dispatch input type enum not found")
	}
	hasNumber := false
	for _, val := range enumVals {
		if val == "number" {
			hasNumber = true
			break
		}
	}
	if !hasNumber {
		t.Fatalf("workflow_dispatch input type enum should include 'number', got: %v", enumVals)
	}

	typeDescription, ok := workflowDispatchInputType["description"].(string)
	if !ok {
		t.Fatal("workflow_dispatch input type description not found")
	}
	if !strings.Contains(typeDescription, "number") {
		t.Fatalf("workflow_dispatch input type description should mention 'number', got: %q", typeDescription)
	}
}

func TestMainWorkflowSchema_WorkflowCallAndDispatchInputDefsDisallowUnknownKeys(t *testing.T) {
	t.Parallel()

	schemaContent, err := os.ReadFile("schemas/main_workflow_schema.json")
	if err != nil {
		t.Fatalf("failed to read schema: %v", err)
	}

	var schema map[string]any
	if err := json.Unmarshal(schemaContent, &schema); err != nil {
		t.Fatalf("failed to parse schema json: %v", err)
	}

	getMapAtPath := func(t *testing.T, root map[string]any, path ...any) map[string]any {
		t.Helper()
		var cur any = root
		for _, p := range path {
			switch step := p.(type) {
			case string:
				m, ok := cur.(map[string]any)
				if !ok {
					t.Fatalf("expected map before key %q", step)
				}
				next, ok := m[step]
				if !ok {
					t.Fatalf("missing key %q in path", step)
				}
				cur = next
			case int:
				arr, ok := cur.([]any)
				if !ok {
					t.Fatalf("expected array before index %d", step)
				}
				if step < 0 || step >= len(arr) {
					t.Fatalf("index %d out of range in path", step)
				}
				cur = arr[step]
			default:
				t.Fatalf("unsupported path step type %T", p)
			}
		}

		out, ok := cur.(map[string]any)
		if !ok {
			t.Fatalf("expected map at target path, got %T", cur)
		}
		return out
	}

	paths := []struct {
		name string
		path []any
	}{
		{
			name: "on.workflow_call.inputs.<id>",
			path: []any{"properties", "on", "oneOf", 1, "properties", "workflow_call", "oneOf", 1, "properties", "inputs", "additionalProperties"},
		},
		{
			name: "on.workflow_call.secrets.<id>",
			path: []any{"properties", "on", "oneOf", 1, "properties", "workflow_call", "oneOf", 1, "properties", "secrets", "additionalProperties"},
		},
		{
			name: "safe-outputs.dispatch-repository.<tool>.inputs.<id>",
			path: []any{"properties", "safe-outputs", "properties", "dispatch-repository", "additionalProperties", "properties", "inputs", "additionalProperties"},
		},
	}

	for _, tc := range paths {
		t.Run(tc.name, func(t *testing.T) {
			subschema := getMapAtPath(t, schema, tc.path...)
			if subschema["additionalProperties"] != false {
				t.Fatalf("%s should set additionalProperties to false", tc.name)
			}
		})
	}
}

func TestMainWorkflowSchema_CreatePullRequestAllowedBaseBranches(t *testing.T) {
	t.Parallel()

	schemaPath := "schemas/main_workflow_schema.json"
	schemaContent, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("failed to read schema: %v", err)
	}

	var schema map[string]any
	if err := json.Unmarshal(schemaContent, &schema); err != nil {
		t.Fatalf("failed to parse schema json: %v", err)
	}

	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("schema properties section not found")
	}

	safeOutputs, ok := properties["safe-outputs"].(map[string]any)
	if !ok {
		t.Fatal("'safe-outputs' field not found in schema")
	}

	safeOutputsProperties, ok := safeOutputs["properties"].(map[string]any)
	if !ok {
		t.Fatal("'safe-outputs.properties' not found in schema")
	}

	createPullRequest, ok := safeOutputsProperties["create-pull-request"].(map[string]any)
	if !ok {
		t.Fatal("'safe-outputs.create-pull-request' not found in schema")
	}

	createPullRequestOneOf, ok := createPullRequest["oneOf"].([]any)
	if !ok {
		t.Fatal("'safe-outputs.create-pull-request.oneOf' not found in schema")
	}

	var createPullRequestProperties map[string]any
	for _, candidate := range createPullRequestOneOf {
		candidateMap, ok := candidate.(map[string]any)
		if !ok {
			continue
		}

		properties, ok := candidateMap["properties"].(map[string]any)
		if !ok {
			continue
		}

		createPullRequestProperties = properties
		break
	}
	if createPullRequestProperties == nil {
		t.Fatal("'safe-outputs.create-pull-request' object schema with properties not found")
	}

	allowedBaseBranches, ok := createPullRequestProperties["allowed-base-branches"].(map[string]any)
	if !ok {
		t.Fatal("'allowed-base-branches' not found under safe-outputs.create-pull-request")
	}

	// The field accepts either a literal array or an expression string (oneOf).
	// Validate that the array variant is present and has the right structure.
	var arraySchema map[string]any
	if oneOf, hasOneOf := allowedBaseBranches["oneOf"].([]any); hasOneOf {
		// New schema: oneOf[array, string-expression]
		for _, candidate := range oneOf {
			candidateMap, ok := candidate.(map[string]any)
			if !ok {
				continue
			}
			if t2, _ := candidateMap["type"].(string); t2 == "array" {
				arraySchema = candidateMap
				break
			}
		}
		if arraySchema == nil {
			t.Fatal("'allowed-base-branches' oneOf does not include an array variant")
		}
	} else {
		// Legacy schema: direct type:array
		if gotType, _ := allowedBaseBranches["type"].(string); gotType != "array" {
			t.Fatalf("'allowed-base-branches' should be type array (or oneOf with array), got: %v", allowedBaseBranches["type"])
		}
		arraySchema = allowedBaseBranches
	}

	items, ok := arraySchema["items"].(map[string]any)
	if !ok {
		t.Fatal("'allowed-base-branches.items' not found in schema")
	}

	if gotItemType, _ := items["type"].(string); gotItemType != "string" {
		t.Fatalf("'allowed-base-branches.items' should be type string, got: %v", items["type"])
	}

	if _, ok := createPullRequestProperties["max-patch-size"].(map[string]any); !ok {
		t.Fatal("'max-patch-size' not found under safe-outputs.create-pull-request")
	}
	if _, ok := createPullRequestProperties["max-patch-files"].(map[string]any); !ok {
		t.Fatal("'max-patch-files' not found under safe-outputs.create-pull-request")
	}

	autoMerge, ok := createPullRequestProperties["auto-merge"].(map[string]any)
	if !ok {
		t.Fatal("'auto-merge' not found under safe-outputs.create-pull-request")
	}

	autoMergeOneOf, ok := autoMerge["oneOf"].([]any)
	if !ok || len(autoMergeOneOf) < 2 {
		t.Fatal("'auto-merge.oneOf' not found under safe-outputs.create-pull-request")
	}

	var foundBoolean bool
	var foundMergeMethodEnum bool
	for _, candidate := range autoMergeOneOf {
		candidateMap, ok := candidate.(map[string]any)
		if !ok {
			continue
		}
		if candidateMap["type"] == "boolean" {
			foundBoolean = true
		}
		if enumVals, ok := candidateMap["enum"].([]any); ok && len(enumVals) == 3 {
			foundMergeMethodEnum = enumVals[0] == "squash" && enumVals[1] == "merge" && enumVals[2] == "rebase"
		}
	}
	if !foundBoolean {
		t.Fatal("'auto-merge.oneOf' should include a boolean variant")
	}
	if !foundMergeMethodEnum {
		t.Fatal("'auto-merge.oneOf' should include a squash|merge|rebase enum variant")
	}
}

func TestGetSafeOutputTypeKeys(t *testing.T) {
	keys, err := GetSafeOutputTypeKeys()
	if err != nil {
		t.Fatalf("GetSafeOutputTypeKeys() returned error: %v", err)
	}

	// Should return multiple keys
	if len(keys) == 0 {
		t.Error("GetSafeOutputTypeKeys() returned empty list")
	}

	// Should include known safe output types
	expectedKeys := []string{
		"create-issue",
		"add-comment",
		"create-discussion",
		"create-pull-request",
		"update-issue",
	}

	keySet := make(map[string]bool)
	for _, key := range keys {
		keySet[key] = true
	}

	for _, expected := range expectedKeys {
		if !keySet[expected] {
			t.Errorf("GetSafeOutputTypeKeys() missing expected key: %s", expected)
		}
	}

	// Should NOT include meta-configuration fields
	metaFields := []string{
		"allowed-domains",
		"staged",
		"env",
		"github-token",
		"github-app",
		"max-patch-size",
		"jobs",
		"runs-on",
		"messages",
		"needs",
		"timeout-minutes",
	}

	for _, meta := range metaFields {
		if keySet[meta] {
			t.Errorf("GetSafeOutputTypeKeys() should not include meta field: %s", meta)
		}
	}

	// Keys should be sorted
	for i := 1; i < len(keys); i++ {
		if keys[i-1] > keys[i] {
			t.Errorf("GetSafeOutputTypeKeys() keys are not sorted: %s > %s", keys[i-1], keys[i])
		}
	}
}

func TestMainWorkflowSchema_CreateDiscussionRequiredCategoryAllowed(t *testing.T) {
	t.Parallel()

	frontmatter := map[string]any{
		"on": "daily",
		"safe-outputs": map[string]any{
			"create-discussion": map[string]any{
				"category":                "Ideas",
				"close-older-discussions": true,
				"required-category":       "Ideas",
			},
		},
	}

	if err := validateWithSchema(frontmatter, mainWorkflowSchema, "main workflow file"); err != nil {
		t.Fatalf("expected create-discussion.required-category to pass schema validation, got: %v", err)
	}
}

func TestMainWorkflowSchema_CreateBodyFootersAllowed(t *testing.T) {
	t.Parallel()

	frontmatter := map[string]any{
		"on": "daily",
		"safe-outputs": map[string]any{
			"create-issue": map[string]any{
				"body-footer": "Issue footer from {workflow_name}",
			},
			"create-pull-request": map[string]any{
				"body-footer": "Pull request footer from {workflow_name}",
			},
		},
	}

	if err := validateWithSchema(frontmatter, mainWorkflowSchema, "main workflow file"); err != nil {
		t.Fatalf("expected create body footers to pass schema validation, got: %v", err)
	}
}

func TestMainWorkflowSchema_GitHubTokenAllowsStepOutputs(t *testing.T) {
	t.Parallel()

	frontmatter := map[string]any{
		"on": "daily",
		"safe-outputs": map[string]any{
			"github-token": "${{ steps.fetch-token.outputs.my-token }}",
			"create-issue": map[string]any{
				"github-token": "${{ steps.fetch-token.outputs.my-token }}",
			},
		},
	}

	if err := validateWithSchema(frontmatter, mainWorkflowSchema, "main workflow file"); err != nil {
		t.Fatalf("expected steps.*.outputs.* github-token expression to pass schema validation, got: %v", err)
	}
}

func TestMainWorkflowSchema_SkillsGitHubTokenRejectsStepOutputs(t *testing.T) {
	t.Parallel()

	frontmatter := map[string]any{
		"on": "daily",
		"skills": []any{
			map[string]any{
				"skill":        "githubnext/skills@1f181b37d3fe5862ab590648f25a292e345b5de6",
				"github-token": "${{ steps.fetch-token.outputs.my-token }}",
			},
		},
	}

	err := validateWithSchema(frontmatter, mainWorkflowSchema, "main workflow file")
	if err == nil {
		t.Fatal("expected skills[].github-token steps.*.outputs.* expression to fail schema validation")
	}
	if !strings.Contains(err.Error(), "github-token") {
		t.Fatalf("expected schema error to mention github-token, got: %v", err)
	}
}

func TestMainWorkflowSchemaPushToPullRequestBranchHasMaxPatchSize(t *testing.T) {
	schemaPath := "schemas/main_workflow_schema.json"
	schemaContent, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("failed to read schema: %v", err)
	}

	var schemaMap map[string]any
	if err := json.Unmarshal(schemaContent, &schemaMap); err != nil {
		t.Fatalf("failed to parse schema json: %v", err)
	}

	properties, ok := schemaMap["properties"].(map[string]any)
	if !ok {
		t.Fatal("'properties' not found in main workflow schema")
	}

	safeOutputs, ok := properties["safe-outputs"].(map[string]any)
	if !ok {
		t.Fatal("'safe-outputs' not found in main workflow schema")
	}

	safeOutputsProps, ok := safeOutputs["properties"].(map[string]any)
	if !ok {
		t.Fatal("'safe-outputs.properties' not found in main workflow schema")
	}

	pushCfg, ok := safeOutputsProps["push-to-pull-request-branch"].(map[string]any)
	if !ok {
		t.Fatal("'safe-outputs.push-to-pull-request-branch' not found")
	}

	pushOneOf, ok := pushCfg["oneOf"].([]any)
	if !ok || len(pushOneOf) == 0 {
		t.Fatal("'safe-outputs.push-to-pull-request-branch.oneOf' not found")
	}

	var pushProperties map[string]any
	for _, candidate := range pushOneOf {
		candidateMap, ok := candidate.(map[string]any)
		if !ok {
			continue
		}
		p, ok := candidateMap["properties"].(map[string]any)
		if ok {
			pushProperties = p
			break
		}
	}
	if pushProperties == nil {
		t.Fatal("'safe-outputs.push-to-pull-request-branch' object schema with properties not found")
	}

	if _, ok := pushProperties["max-patch-size"].(map[string]any); !ok {
		t.Fatal("'max-patch-size' not found under safe-outputs.push-to-pull-request-branch")
	}
}

// TestNormalizeForJSONSchema verifies that normalizeForJSONSchema correctly converts
// YAML-native integer types to float64 while leaving other types unchanged.
func TestNormalizeForJSONSchema(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected any
	}{
		// Integer type conversions
		{name: "int", input: int(42), expected: float64(42)},
		{name: "int8", input: int8(8), expected: float64(8)},
		{name: "int16", input: int16(16), expected: float64(16)},
		{name: "int32", input: int32(32), expected: float64(32)},
		{name: "int64", input: int64(64), expected: float64(64)},
		{name: "int64 negative", input: int64(-5), expected: float64(-5)},

		// Unsigned integer type conversions
		{name: "uint", input: uint(42), expected: float64(42)},
		{name: "uint8", input: uint8(8), expected: float64(8)},
		{name: "uint16", input: uint16(16), expected: float64(16)},
		{name: "uint32", input: uint32(32), expected: float64(32)},
		{name: "uint64", input: uint64(64), expected: float64(64)},
		{name: "uint64 large", input: uint64(9999999999999), expected: float64(9999999999999)},

		// Float type conversions
		{name: "float32", input: float32(3.14), expected: float64(float32(3.14))},

		// Pass-through types
		{name: "float64 passthrough", input: float64(2.718), expected: float64(2.718)},
		{name: "string passthrough", input: "hello", expected: "hello"},
		{name: "bool true passthrough", input: true, expected: true},
		{name: "bool false passthrough", input: false, expected: false},
		{name: "nil passthrough", input: nil, expected: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeForJSONSchema(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeForJSONSchema(%T(%v)) = %T(%v), want %T(%v)",
					tt.input, tt.input, result, result, tt.expected, tt.expected)
			}
		})
	}
}

func TestValidateMainWorkflowFrontmatterWithSchemaAndLocation_GitHubAppClientID(t *testing.T) {
	frontmatter := map[string]any{
		"name": "Client ID validation",
		"on": map[string]any{
			"issues": map[string]any{
				"types": []any{"opened"},
			},
		},
		"github-app": map[string]any{
			"client-id":   "${{ vars.APP_ID }}",
			"private-key": "${{ secrets.APP_PRIVATE_KEY }}",
		},
	}

	err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/tmp/gh-aw/client-id-schema-test.md")
	if err != nil {
		t.Fatalf("expected client-id in github-app to pass schema validation, got: %v", err)
	}
}

func TestValidateMainWorkflowFrontmatterWithSchemaAndLocation_OTLPGitHubAppImplicitOIDC(t *testing.T) {
	frontmatter := map[string]any{
		"name": "OTLP implicit OIDC github-app config",
		"on": map[string]any{
			"issues": map[string]any{
				"types": []any{"opened"},
			},
		},
		"observability": map[string]any{
			"otlp": map[string]any{
				"github-app": map[string]any{},
			},
		},
	}

	err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/tmp/gh-aw/otlp-github-app-implicit-oidc-schema-test.md")
	if err != nil {
		t.Fatalf("expected empty observability.otlp.github-app to pass schema validation for implicit OIDC, got: %v", err)
	}
}

func TestValidateMainWorkflowFrontmatterWithSchemaAndLocation_OTLPCustomAttributes(t *testing.T) {
	frontmatter := map[string]any{
		"name": "OTLP custom attributes config",
		"on": map[string]any{
			"issues": map[string]any{
				"types": []any{"opened"},
			},
		},
		"observability": map[string]any{
			"otlp": map[string]any{
				"attributes": map[string]any{
					"langfuse.session.id": "{{ gh-aw.episode.id }}",
					"langfuse.user.id":    "{{ github.actor }}",
					"deployment.stage":    "production",
				},
			},
		},
	}

	err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/tmp/gh-aw/otlp-custom-attributes-schema-test.md")
	if err != nil {
		t.Fatalf("expected observability.otlp.attributes to pass schema validation, got: %v", err)
	}
}

func TestValidateMainWorkflowFrontmatterWithSchemaAndLocation_OTLPResourceAttributes(t *testing.T) {
	frontmatter := map[string]any{
		"name": "OTLP resource attributes config",
		"on": map[string]any{
			"issues": map[string]any{
				"types": []any{"opened"},
			},
		},
		"observability": map[string]any{
			"otlp": map[string]any{
				"resource-attributes": map[string]any{
					"my.target-repo": "${{ github.repository }}",
					"my.event":       "${{ github.event_name }}",
				},
			},
		},
	}

	err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/tmp/gh-aw/otlp-resource-attributes-schema-test.md")
	if err != nil {
		t.Fatalf("expected observability.otlp.resource-attributes to pass schema validation, got: %v", err)
	}
}

func TestValidateMainWorkflowFrontmatterWithSchemaAndLocation_OTLPGitHubAppAudienceAccepted(t *testing.T) {
	frontmatter := map[string]any{
		"name": "OTLP github-app audience acceptance",
		"on": map[string]any{
			"issues": map[string]any{
				"types": []any{"opened"},
			},
		},
		"observability": map[string]any{
			"otlp": map[string]any{
				"github-app": map[string]any{
					"audience": "https://collector.example.com",
				},
			},
		},
	}

	err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/tmp/gh-aw/otlp-github-app-audience-accept-schema-test.md")
	if err != nil {
		t.Fatalf("expected observability.otlp.github-app.audience to pass schema validation (it is the credential-less OIDC audience field), got: %v", err)
	}
}

func TestValidateMainWorkflowFrontmatterWithSchemaAndLocation_OTLPGitHubAppPermissionsRejected(t *testing.T) {
	frontmatter := map[string]any{
		"name": "OTLP github-app permissions rejection",
		"on": map[string]any{
			"issues": map[string]any{
				"types": []any{"opened"},
			},
		},
		"observability": map[string]any{
			"otlp": map[string]any{
				"github-app": map[string]any{
					"permissions": map[string]any{
						"contents": "read",
					},
				},
			},
		},
	}

	err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/tmp/gh-aw/otlp-github-app-permissions-reject-schema-test.md")
	if err == nil {
		t.Fatal("expected observability.otlp.github-app.permissions to fail schema validation")
	}
	errText := err.Error()
	if !strings.Contains(errText, "permissions") ||
		(!strings.Contains(errText, "github-app") && !strings.Contains(errText, "Unknown property")) {
		t.Fatalf("expected schema validation error to reference unsupported github-app.permissions syntax, got: %v", err)
	}
}

func TestValidateMainWorkflowFrontmatterWithSchemaAndLocation_OTLPGitHubAppLegacyTypeRejected(t *testing.T) {
	frontmatter := map[string]any{
		"name": "OTLP legacy github-oidc type rejection",
		"on": map[string]any{
			"issues": map[string]any{
				"types": []any{"opened"},
			},
		},
		"observability": map[string]any{
			"otlp": map[string]any{
				"github-app": map[string]any{
					"type":     "github-oidc",
					"audience": "https://collector.example.com",
				},
			},
		},
	}

	err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/tmp/gh-aw/otlp-github-app-legacy-type-schema-test.md")
	if err == nil {
		t.Fatal("expected legacy observability.otlp.github-app.type: github-oidc to fail schema validation")
	}
	errText := err.Error()
	if !strings.Contains(errText, "type") ||
		(!strings.Contains(errText, "github-app") && !strings.Contains(errText, "Unknown properties")) {
		t.Fatalf("expected schema validation error to reference unsupported legacy github-app.type syntax, got: %v", err)
	}
}

// TestNormalizeForJSONSchema_NestedMap verifies recursive normalization of maps.
func TestNormalizeForJSONSchema_NestedMap(t *testing.T) {
	input := map[string]any{
		"name":    "test",
		"count":   uint64(42),
		"offset":  int64(-3),
		"enabled": true,
		"nested": map[string]any{
			"port":  uint64(8080),
			"label": "inner",
		},
	}

	result := normalizeForJSONSchema(input)
	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", result)
	}

	if resultMap["name"] != "test" {
		t.Errorf("name: got %v, want test", resultMap["name"])
	}
	if resultMap["count"] != float64(42) {
		t.Errorf("count: got %T(%v), want float64(42)", resultMap["count"], resultMap["count"])
	}
	if resultMap["offset"] != float64(-3) {
		t.Errorf("offset: got %T(%v), want float64(-3)", resultMap["offset"], resultMap["offset"])
	}
	if resultMap["enabled"] != true {
		t.Errorf("enabled: got %v, want true", resultMap["enabled"])
	}

	nestedMap, ok := resultMap["nested"].(map[string]any)
	if !ok {
		t.Fatalf("nested: expected map[string]any, got %T", resultMap["nested"])
	}
	if nestedMap["port"] != float64(8080) {
		t.Errorf("nested.port: got %T(%v), want float64(8080)", nestedMap["port"], nestedMap["port"])
	}
	if nestedMap["label"] != "inner" {
		t.Errorf("nested.label: got %v, want inner", nestedMap["label"])
	}

	// Verify the original input is NOT mutated
	if input["count"] != uint64(42) {
		t.Errorf("original input mutated: count is %T(%v), expected uint64(42)", input["count"], input["count"])
	}
}

// TestNormalizeForJSONSchema_Slice verifies recursive normalization of slices.
func TestNormalizeForJSONSchema_Slice(t *testing.T) {
	input := []any{uint64(1), "two", int64(-3), true, nil, float64(4.5)}

	result := normalizeForJSONSchema(input)
	resultSlice, ok := result.([]any)
	if !ok {
		t.Fatalf("expected []any, got %T", result)
	}

	expected := []any{float64(1), "two", float64(-3), true, nil, float64(4.5)}
	if len(resultSlice) != len(expected) {
		t.Fatalf("length mismatch: got %d, want %d", len(resultSlice), len(expected))
	}
	for i, want := range expected {
		if resultSlice[i] != want {
			t.Errorf("[%d]: got %T(%v), want %T(%v)", i, resultSlice[i], resultSlice[i], want, want)
		}
	}
}

// TestNormalizeForJSONSchema_TypedSlice verifies that typed slices (e.g. []string)
// are converted to []any, since goccy/go-yaml may produce typed slices that the
// JSON schema validator does not recognize.
func TestNormalizeForJSONSchema_TypedSlice(t *testing.T) {
	input := []string{"a", "b", "c"}

	result := normalizeForJSONSchema(input)
	resultSlice, ok := result.([]any)
	if !ok {
		t.Fatalf("expected []any, got %T", result)
	}

	if len(resultSlice) != 3 {
		t.Fatalf("length mismatch: got %d, want 3", len(resultSlice))
	}
	for i, want := range []string{"a", "b", "c"} {
		if resultSlice[i] != want {
			t.Errorf("[%d]: got %T(%v), want %T(%v)", i, resultSlice[i], resultSlice[i], want, want)
		}
	}
}

// TestValidateWithSchema_YAMLTypedSlice verifies that validateWithSchema accepts
// typed slices (e.g. []string) that goccy/go-yaml produces for array fields.
func TestValidateWithSchema_YAMLTypedSlice(t *testing.T) {
	schema := `{
		"type": "object",
		"properties": {
			"tags": {"type": "array", "items": {"type": "string"}},
			"name": {"type": "string"}
		},
		"additionalProperties": false
	}`

	frontmatter := map[string]any{
		"tags": []string{"v1", "v2"},
		"name": "test",
	}

	err := validateWithSchema(frontmatter, schema, "yaml typed slice")
	if err != nil {
		t.Errorf("validateWithSchema should accept typed slices, got: %v", err)
	}
}

// TestValidateMainWorkflowFrontmatterWithSchemaAndLocation_ProtectedFilesObjectForm
// verifies that the protected-files field on create-pull-request and
// push-to-pull-request-branch accepts the documented object form
// {policy, exclude} in addition to the plain string enum.
//
// This is a regression test for the bug where the schema only accepted
// "string or null" for protected-files, rejecting object-form configurations
// with "expected string or null, got object".
func TestValidateMainWorkflowFrontmatterWithSchemaAndLocation_ProtectedFilesObjectForm(t *testing.T) {
	t.Parallel()

	baseFrontmatter := func(safeOutputs map[string]any) map[string]any {
		return map[string]any{
			"on":           map[string]any{"issues": map[string]any{"types": []any{"opened"}}},
			"engine":       "copilot",
			"safe-outputs": safeOutputs,
		}
	}

	tests := []struct {
		name        string
		safeOutputs map[string]any
		wantErr     bool
		errContains string
	}{
		{
			name: "create-pull-request: string form passes",
			safeOutputs: map[string]any{
				"create-pull-request": map[string]any{
					"protected-files": "fallback-to-issue",
				},
			},
			wantErr: false,
		},
		{
			name: "create-pull-request: object form with policy and exclude passes",
			safeOutputs: map[string]any{
				"create-pull-request": map[string]any{
					"protected-files": map[string]any{
						"policy":  "fallback-to-issue",
						"exclude": []any{".claude/", ".github/instructions/"},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "create-pull-request: object form with only exclude passes",
			safeOutputs: map[string]any{
				"create-pull-request": map[string]any{
					"protected-files": map[string]any{
						"exclude": []any{"AGENTS.md"},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "create-pull-request: object form with only policy passes",
			safeOutputs: map[string]any{
				"create-pull-request": map[string]any{
					"protected-files": map[string]any{
						"policy": "allowed",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "create-pull-request: object form with invalid extra field is rejected",
			safeOutputs: map[string]any{
				"create-pull-request": map[string]any{
					"protected-files": map[string]any{
						"policy":       "blocked",
						"unknown-prop": "value",
					},
				},
			},
			wantErr:     true,
			errContains: "unknown-prop",
		},
		{
			name: "push-to-pull-request-branch: object form with policy and exclude passes",
			safeOutputs: map[string]any{
				"push-to-pull-request-branch": map[string]any{
					"protected-files": map[string]any{
						"policy":  "fallback-to-issue",
						"exclude": []any{"AGENTS.md", ".agents/"},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "push-to-pull-request-branch: string form passes",
			safeOutputs: map[string]any{
				"push-to-pull-request-branch": map[string]any{
					"protected-files": "blocked",
				},
			},
			wantErr: false,
		},
		{
			name: "create-pull-request: expression string for protected-files passes",
			safeOutputs: map[string]any{
				"create-pull-request": map[string]any{
					"protected-files": "${{ inputs.protected-files-policy }}",
				},
			},
			wantErr: false,
		},
		{
			name: "create-pull-request: expression string for patch-format passes",
			safeOutputs: map[string]any{
				"create-pull-request": map[string]any{
					"patch-format": "${{ inputs.patch-format }}",
				},
			},
			wantErr: false,
		},
		{
			name: "push-to-pull-request-branch: expression string for protected-files passes",
			safeOutputs: map[string]any{
				"push-to-pull-request-branch": map[string]any{
					"protected-files": "${{ inputs.protected-files-policy }}",
				},
			},
			wantErr: false,
		},
		{
			name: "push-to-pull-request-branch: expression string for patch-format passes",
			safeOutputs: map[string]any{
				"push-to-pull-request-branch": map[string]any{
					"patch-format": "${{ inputs.patch-format }}",
				},
			},
			wantErr: false,
		},
		{
			name: "create-pull-request: object form with expression policy passes",
			safeOutputs: map[string]any{
				"create-pull-request": map[string]any{
					"protected-files": map[string]any{
						"policy":  "${{ inputs.policy }}",
						"exclude": []any{"AGENTS.md"},
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			frontmatter := baseFrontmatter(tt.safeOutputs)
			err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/tmp/gh-aw/protected-files-schema-test.md")
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected validation error for %q, got nil", tt.name)
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("expected error to contain %q, got: %v", tt.errContains, err)
				}
			} else {
				if err != nil {
					t.Fatalf("expected %q to pass schema validation, got: %v", tt.name, err)
				}
			}
		})
	}
}

// TestMainWorkflowSchema_ProtectedFilesObjectFormStructure verifies that the
// main workflow JSON schema defines protected-files as a oneOf [string, object],
// not as a oneOf [string, null] (the old broken form that caused
// "expected string or null, got object" errors).
func TestMainWorkflowSchema_ProtectedFilesObjectFormStructure(t *testing.T) {
	t.Parallel()

	schemaContent, err := os.ReadFile("schemas/main_workflow_schema.json")
	if err != nil {
		t.Fatalf("failed to read schema: %v", err)
	}

	var schema map[string]any
	if err := json.Unmarshal(schemaContent, &schema); err != nil {
		t.Fatalf("failed to parse schema JSON: %v", err)
	}

	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("schema missing 'properties'")
	}
	safeOutputsSchema, ok := properties["safe-outputs"].(map[string]any)
	if !ok {
		t.Fatal("schema missing 'properties.safe-outputs'")
	}
	safeOutputsProps, ok := safeOutputsSchema["properties"].(map[string]any)
	if !ok {
		t.Fatal("schema missing 'properties.safe-outputs.properties'")
	}

	for _, handlerName := range []string{"create-pull-request", "push-to-pull-request-branch"} {
		t.Run(handlerName, func(t *testing.T) {
			handlerSchema, ok := safeOutputsProps[handlerName].(map[string]any)
			if !ok {
				t.Fatalf("schema missing 'safe-outputs.%s'", handlerName)
			}
			handlerOneOf, ok := handlerSchema["oneOf"].([]any)
			if !ok {
				t.Fatalf("'safe-outputs.%s' missing oneOf", handlerName)
			}

			// Find the object branch (the one with properties)
			var objectBranchProps map[string]any
			for _, candidate := range handlerOneOf {
				c, ok := candidate.(map[string]any)
				if !ok {
					continue
				}
				if c["type"] == "object" {
					if props, ok := c["properties"].(map[string]any); ok {
						objectBranchProps = props
						break
					}
				}
			}
			if objectBranchProps == nil {
				t.Fatalf("'safe-outputs.%s' has no object branch in oneOf", handlerName)
			}

			pfSchema, ok := objectBranchProps["protected-files"].(map[string]any)
			if !ok {
				t.Fatalf("'safe-outputs.%s.properties.protected-files' not found", handlerName)
			}

			pfOneOf, ok := pfSchema["oneOf"].([]any)
			if !ok {
				t.Fatalf("'safe-outputs.%s.properties.protected-files' missing oneOf", handlerName)
			}

			var hasStringBranch, hasObjectBranch bool
			for _, branch := range pfOneOf {
				b, ok := branch.(map[string]any)
				if !ok {
					continue
				}
				switch b["type"] {
				case "string":
					hasStringBranch = true
				case "object":
					hasObjectBranch = true
				case "null":
					t.Errorf("'safe-outputs.%s.protected-files' has a null branch in its oneOf; "+
						"the object form would produce 'expected string or null, got object' errors", handlerName)
				}
			}
			if !hasStringBranch {
				t.Errorf("'safe-outputs.%s.protected-files' missing string branch in oneOf", handlerName)
			}
			if !hasObjectBranch {
				t.Errorf("'safe-outputs.%s.protected-files' missing object branch in oneOf; "+
					"the object form {policy, exclude} would fail compilation", handlerName)
			}

			// Verify the object branch has the expected sub-fields
			for _, branch := range pfOneOf {
				b, ok := branch.(map[string]any)
				if !ok || b["type"] != "object" {
					continue
				}
				objProps, ok := b["properties"].(map[string]any)
				if !ok {
					t.Fatalf("'safe-outputs.%s.protected-files' object branch missing properties", handlerName)
				}
				if _, hasPolicyField := objProps["policy"]; !hasPolicyField {
					t.Errorf("'safe-outputs.%s.protected-files' object branch missing 'policy' field", handlerName)
				}
				if _, hasExcludeField := objProps["exclude"]; !hasExcludeField {
					t.Errorf("'safe-outputs.%s.protected-files' object branch missing 'exclude' field", handlerName)
				}
				if b["additionalProperties"] != false {
					t.Errorf("'safe-outputs.%s.protected-files' object branch should have additionalProperties: false", handlerName)
				}
			}
		})
	}
}

func TestMainWorkflowSchema_SandboxAgentModelFallback(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		modelFallback any
	}{
		{name: "boolean", modelFallback: false},
		{name: "expression", modelFallback: "${{ inputs.model-fallback }}"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			validFrontmatter := map[string]any{
				"name": "sandbox-agent-model-fallback",
				"on": map[string]any{
					"workflow_dispatch": map[string]any{},
				},
				"permissions": map[string]any{
					"contents": "read",
				},
				"engine": "copilot",
				"sandbox": map[string]any{
					"agent": map[string]any{
						"id":             "awf",
						"model-fallback": tc.modelFallback,
					},
				},
			}

			err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(validFrontmatter, "/tmp/gh-aw/sandbox-agent-model-fallback-test.md")
			if err != nil {
				t.Fatalf("expected sandbox.agent.model-fallback to pass schema validation, got: %v", err)
			}
		})
	}
}

// TestMainWorkflowSchema_ModelsDefaultAiCreditsPricing verifies that
// models.default-ai-credits-pricing is accepted by the frontmatter schema.
func TestMainWorkflowSchema_ModelsDefaultAiCreditsPricing(t *testing.T) {
	t.Parallel()

	t.Run("zero pricing for self-hosted BYOK model is accepted", func(t *testing.T) {
		t.Parallel()

		frontmatter := map[string]any{
			"on":     "push",
			"engine": "copilot",
			"models": map[string]any{
				"default-ai-credits-pricing": map[string]any{
					"input":  0,
					"output": 0,
				},
			},
		}

		err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/tmp/gh-aw/byok-pricing-zero-test.md")
		if err != nil {
			t.Fatalf("expected zero default-ai-credits-pricing to pass schema validation, got: %v", err)
		}
	})

	t.Run("non-zero pricing is accepted", func(t *testing.T) {
		t.Parallel()

		frontmatter := map[string]any{
			"on":     "push",
			"engine": "copilot",
			"models": map[string]any{
				"default-ai-credits-pricing": map[string]any{
					"input":  3.0,
					"output": 15.0,
				},
			},
		}

		err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/tmp/gh-aw/byok-pricing-nonzero-test.md")
		if err != nil {
			t.Fatalf("expected non-zero default-ai-credits-pricing to pass schema validation, got: %v", err)
		}
	})

	t.Run("pricing with cached token classes is accepted", func(t *testing.T) {
		t.Parallel()

		frontmatter := map[string]any{
			"on":     "push",
			"engine": "copilot",
			"models": map[string]any{
				"default-ai-credits-pricing": map[string]any{
					"input":       3.0,
					"output":      15.0,
					"cache_read":  0.3,
					"cache_write": 3.0,
				},
			},
		}

		err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/tmp/gh-aw/byok-pricing-cached-test.md")
		if err != nil {
			t.Fatalf("expected default-ai-credits-pricing with cached token classes to pass schema validation, got: %v", err)
		}
	})

	t.Run("pricing without output is accepted", func(t *testing.T) {
		t.Parallel()

		frontmatter := map[string]any{
			"on":     "push",
			"engine": "copilot",
			"models": map[string]any{
				"default-ai-credits-pricing": map[string]any{
					"input": 3.0,
				},
			},
		}

		err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/tmp/gh-aw/byok-pricing-missing-output-test.md")
		if err != nil {
			t.Fatalf("expected default-ai-credits-pricing without output to pass schema validation, got: %v", err)
		}
	})

	t.Run("pricing with non-numeric cached value is rejected", func(t *testing.T) {
		t.Parallel()

		frontmatter := map[string]any{
			"on":     "push",
			"engine": "copilot",
			"models": map[string]any{
				"default-ai-credits-pricing": map[string]any{
					"input":       3.0,
					"output":      15.0,
					"cache_write": "free",
				},
			},
		}

		err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/tmp/gh-aw/byok-pricing-invalid-cache-write-test.md")
		if err == nil {
			t.Fatal("expected default-ai-credits-pricing with non-numeric cache_write to fail schema validation")
		}
	})
}

// TestMainWorkflowSchema_ModelsProvidersAiCreditsPricing verifies that
// models.providers.<provider>.models.<model>.cost uses the shared ai_credits_pricing schema.
func TestMainWorkflowSchema_ModelsProvidersAiCreditsPricing(t *testing.T) {
	t.Parallel()

	t.Run("numeric and string token-class costs are accepted", func(t *testing.T) {
		t.Parallel()

		frontmatter := map[string]any{
			"on":     "push",
			"engine": "copilot",
			"models": map[string]any{
				"providers": map[string]any{
					"anthropic": map[string]any{
						"models": map[string]any{
							"claude-custom": map[string]any{
								"cost": map[string]any{
									"input":       "3e-07",
									"output":      1.5e-06,
									"cache_read":  "3e-08",
									"cache_write": 3.75e-07,
									"reasoning":   "0",
									"custom":      "1e-09",
								},
							},
						},
					},
				},
			},
		}

		err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/tmp/gh-aw/models-providers-cost-test.md")
		if err != nil {
			t.Fatalf("expected models.providers pricing to pass schema validation, got: %v", err)
		}
	})

	t.Run("non-price value types are rejected", func(t *testing.T) {
		t.Parallel()

		frontmatter := map[string]any{
			"on":     "push",
			"engine": "copilot",
			"models": map[string]any{
				"providers": map[string]any{
					"anthropic": map[string]any{
						"models": map[string]any{
							"claude-custom": map[string]any{
								"cost": map[string]any{
									"input": true,
								},
							},
						},
					},
				},
			},
		}

		err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/tmp/gh-aw/models-providers-cost-invalid-test.md")
		if err == nil {
			t.Fatal("expected models.providers pricing with invalid value type to fail schema validation")
		}
	})

	t.Run("trailing-text numeric strings are rejected", func(t *testing.T) {
		t.Parallel()

		frontmatter := map[string]any{
			"on":     "push",
			"engine": "copilot",
			"models": map[string]any{
				"providers": map[string]any{
					"anthropic": map[string]any{
						"models": map[string]any{
							"claude-custom": map[string]any{
								"cost": map[string]any{
									"input": "3oops",
								},
							},
						},
					},
				},
			},
		}

		err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/tmp/gh-aw/models-providers-cost-invalid-trailing-text-test.md")
		if err == nil {
			t.Fatal("expected models.providers pricing with trailing-text numeric strings to fail schema validation")
		}
	})

	t.Run("non-numeric strings are rejected", func(t *testing.T) {
		t.Parallel()

		frontmatter := map[string]any{
			"on":     "push",
			"engine": "copilot",
			"models": map[string]any{
				"providers": map[string]any{
					"anthropic": map[string]any{
						"models": map[string]any{
							"claude-custom": map[string]any{
								"cost": map[string]any{
									"cache_write": "free",
								},
							},
						},
					},
				},
			},
		}

		err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/tmp/gh-aw/models-providers-cost-invalid-nonnumeric-test.md")
		if err == nil {
			t.Fatal("expected models.providers pricing with non-numeric strings to fail schema validation")
		}
	})
}

// TestMainWorkflowSchema_SandboxAgentRuntime guards the sandbox runtime profile
// selector: sandbox.agent.runtime accepts only the supported profiles, and the removed
// sudo / legacy-security / network-isolation fields stay rejected so that the Go struct
// YAML tags and the schema cannot drift apart.
func TestMainWorkflowSchema_SandboxAgentRuntime(t *testing.T) {
	t.Parallel()

	agentFrontmatter := func(agent map[string]any) map[string]any {
		return map[string]any{
			"on":      "push",
			"engine":  "copilot",
			"sandbox": map[string]any{"agent": agent},
		}
	}

	for _, runtime := range []string{"docker", "docker-sudo-iptables", "gvisor", "docker-sbx", "cloud-hypervisor"} {
		t.Run("runtime: "+runtime+" is accepted", func(t *testing.T) {
			t.Parallel()

			frontmatter := agentFrontmatter(map[string]any{"id": "awf", "runtime": runtime})
			err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/tmp/gh-aw/sandbox-agent-runtime-test.md")
			if err != nil {
				t.Fatalf("expected sandbox.agent.runtime: %s to pass schema validation, got: %v", runtime, err)
			}
		})
	}

	t.Run("unknown runtime is rejected", func(t *testing.T) {
		t.Parallel()

		frontmatter := agentFrontmatter(map[string]any{"id": "awf", "runtime": "podman"})
		err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/tmp/gh-aw/sandbox-agent-runtime-unknown-test.md")
		if err == nil {
			t.Error("expected an unsupported sandbox.agent.runtime to be rejected by schema validation")
		}
	})

	for _, removed := range []struct {
		name  string
		agent map[string]any
	}{
		{name: "sudo", agent: map[string]any{"id": "awf", "sudo": false}},
		{name: "legacy-security", agent: map[string]any{"id": "awf", "legacy-security": "enable"}},
		{name: "network-isolation", agent: map[string]any{"id": "awf", "network-isolation": true}},
	} {
		t.Run(removed.name+" is rejected", func(t *testing.T) {
			t.Parallel()

			frontmatter := agentFrontmatter(removed.agent)
			err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/tmp/gh-aw/sandbox-agent-removed-field-test.md")
			if err == nil {
				t.Errorf("expected removed field sandbox.agent.%s to be rejected (use sandbox.agent.runtime instead)", removed.name)
			}
		})
	}
}

func TestMainWorkflowSchema_SandboxAgentImagesCanonicalReferences(t *testing.T) {
	t.Parallel()

	frontmatterWithImage := func(image string) map[string]any {
		return map[string]any{
			"on":     "push",
			"engine": "copilot",
			"sandbox": map[string]any{
				"agent": map[string]any{
					"id": "awf",
					"images": map[string]any{
						"squid": image,
					},
				},
			},
		}
	}

	valid := "registry.example.com/approved/squid:v0.28.4@sha256:" + strings.Repeat("a", 64)
	if err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatterWithImage(valid), "/tmp/gh-aw/sandbox-agent-images-valid-test.md"); err != nil {
		t.Fatalf("expected canonical digest-pinned image to pass schema validation: %v", err)
	}

	for _, invalid := range []string{
		"approved/squid:v1@sha256:" + strings.Repeat("a", 64),
		"registry.example.com/Approved/squid:v1@sha256:" + strings.Repeat("a", 64),
		"registry.example.com/approved/squid:.v1@sha256:" + strings.Repeat("a", 64),
		"registry.example.com/approved/squid:" + strings.Repeat("a", 129) + "@sha256:" + strings.Repeat("a", 64),
	} {
		if err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatterWithImage(invalid), "/tmp/gh-aw/sandbox-agent-images-invalid-test.md"); err == nil {
			t.Errorf("expected non-canonical image reference %q to fail schema validation", invalid)
		}
	}
}

// TestValidateWithSchema_YAMLIntegerTypes verifies that validateWithSchema accepts
// YAML-native integer types (uint64/int64) when the schema expects number/integer.
func TestValidateWithSchema_YAMLIntegerTypes(t *testing.T) {
	schema := `{
		"type": "object",
		"properties": {
			"timeout-minutes": {"type": "integer"},
			"max-retries": {"type": "number"},
			"name": {"type": "string"}
		},
		"additionalProperties": false
	}`

	// Simulate what goccy/go-yaml produces: uint64 for positive, int64 for negative
	frontmatter := map[string]any{
		"timeout-minutes": uint64(20),
		"max-retries":     int64(3),
		"name":            "test",
	}

	err := validateWithSchema(frontmatter, schema, "yaml integer types")
	if err != nil {
		t.Errorf("validateWithSchema should accept YAML integer types, got: %v", err)
	}
}

func TestValidateMainWorkflowSchema_TimeoutMinutesTemplatableInteger(t *testing.T) {
	t.Parallel()

	frontmatter := map[string]any{
		"name": "templated-timeout-test",
		"on": map[string]any{
			"workflow_dispatch": map[string]any{},
		},
		"timeout-minutes": "${{ inputs.workflow_timeout }}",
		"jobs": map[string]any{
			"build": map[string]any{
				"runs-on":         "ubuntu-latest",
				"timeout-minutes": "${{ inputs.job_timeout }}",
				"steps": []any{
					map[string]any{"run": "echo hello"},
				},
			},
		},
	}

	err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/tmp/timeout-templatable.md")
	if err != nil {
		t.Fatalf("expected templated timeout-minutes values to pass schema validation, got: %v", err)
	}
}

func TestMainWorkflowSchema_GitHubAllowedSupportsToolCallLimits(t *testing.T) {
	t.Parallel()

	schemaContent, err := os.ReadFile("schemas/main_workflow_schema.json")
	if err != nil {
		t.Fatalf("failed to read schema: %v", err)
	}

	var schema map[string]any
	if err := json.Unmarshal(schemaContent, &schema); err != nil {
		t.Fatalf("failed to parse schema json: %v", err)
	}

	properties := schema["properties"].(map[string]any)
	tools := properties["tools"].(map[string]any)
	toolsProps := tools["properties"].(map[string]any)
	github := toolsProps["github"].(map[string]any)
	githubOneOf := github["oneOf"].([]any)

	var githubObjectSchema map[string]any
	for _, item := range githubOneOf {
		candidate, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if candidate["type"] == "object" {
			githubObjectSchema = candidate
			break
		}
	}
	if githubObjectSchema == nil {
		t.Fatal("tools.github object schema not found")
	}

	allowed := githubObjectSchema["properties"].(map[string]any)["allowed"].(map[string]any)
	items := allowed["items"].(map[string]any)
	itemOneOf := items["oneOf"].([]any)

	var objectBranch map[string]any
	for _, branch := range itemOneOf {
		candidate, ok := branch.(map[string]any)
		if !ok {
			continue
		}
		if candidate["type"] == "object" {
			objectBranch = candidate
			break
		}
	}
	if objectBranch == nil {
		t.Fatal("tools.github.allowed object entry schema not found")
	}

	entryProps := objectBranch["properties"].(map[string]any)
	maxCalls, hasMaxCalls := entryProps["max-calls"].(map[string]any)
	if !hasMaxCalls {
		t.Fatal("tools.github.allowed[].max-calls schema not found")
	}
	if maxCalls["type"] != "integer" {
		t.Fatalf("expected max-calls type integer, got: %v", maxCalls["type"])
	}
	if maxCalls["minimum"] != float64(1) {
		t.Fatalf("expected max-calls minimum 1, got: %v", maxCalls["minimum"])
	}
	if _, hasMaxAlias := entryProps["max"]; hasMaxAlias {
		t.Fatal("tools.github.allowed[].max alias should not be present")
	}
}

// TestValidateMainWorkflowFrontmatterWithSchemaAndLocation_AwfApiProxyTargets verifies that
// the sandbox.agent.targets frontmatter section is validated by the schema, accepting
// valid authHeader strings and rejecting non-string values.
func TestValidateMainWorkflowFrontmatterWithSchemaAndLocation_AwfApiProxyTargets(t *testing.T) {
	t.Run("valid string authHeader for openai is accepted", func(t *testing.T) {
		frontmatter := map[string]any{
			"on":     "push",
			"engine": "codex",
			"sandbox": map[string]any{
				"agent": map[string]any{
					"targets": map[string]any{
						"openai": map[string]any{
							"authHeader": "api-key",
						},
					},
				},
			},
		}
		err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/tmp/gh-aw/awf-auth-header-openai-test.md")
		if err != nil {
			t.Errorf("valid openai authHeader should be accepted, got error: %v", err)
		}
	})

	t.Run("valid string authHeader for anthropic is accepted", func(t *testing.T) {
		frontmatter := map[string]any{
			"on":     "push",
			"engine": "claude",
			"sandbox": map[string]any{
				"agent": map[string]any{
					"targets": map[string]any{
						"anthropic": map[string]any{
							"authHeader": "api-key",
						},
					},
				},
			},
		}
		err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/tmp/gh-aw/awf-auth-header-anthropic-test.md")
		if err != nil {
			t.Errorf("valid anthropic authHeader should be accepted, got error: %v", err)
		}
	})

	t.Run("non-string authHeader is rejected", func(t *testing.T) {
		frontmatter := map[string]any{
			"on":     "push",
			"engine": "codex",
			"sandbox": map[string]any{
				"agent": map[string]any{
					"targets": map[string]any{
						"openai": map[string]any{
							"authHeader": 42,
						},
					},
				},
			},
		}
		err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/tmp/gh-aw/awf-auth-header-invalid-test.md")
		if err == nil {
			t.Error("non-string authHeader should be rejected by schema validation")
		}
	})

	t.Run("unknown provider in targets is rejected", func(t *testing.T) {
		frontmatter := map[string]any{
			"on":     "push",
			"engine": "codex",
			"sandbox": map[string]any{
				"agent": map[string]any{
					"targets": map[string]any{
						"unknown-provider": map[string]any{
							"authHeader": "api-key",
						},
					},
				},
			},
		}
		err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/tmp/gh-aw/awf-unknown-provider-test.md")
		if err == nil {
			t.Error("unknown provider in sandbox.agent.targets should be rejected")
		}
	})

	t.Run("copilot extraHeaders map is accepted", func(t *testing.T) {
		frontmatter := map[string]any{
			"on":     "push",
			"engine": "copilot",
			"sandbox": map[string]any{
				"agent": map[string]any{
					"targets": map[string]any{
						"copilot": map[string]any{
							"extraHeaders": map[string]any{
								"x-openrouter-title": "my-workflow",
								"http-referer":       "https://github.com/org/repo",
							},
						},
					},
				},
			},
		}
		err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/tmp/gh-aw/awf-copilot-extra-headers-test.md")
		if err != nil {
			t.Errorf("valid copilot extraHeaders should be accepted, got error: %v", err)
		}
	})

	t.Run("copilot extraBodyFields map is accepted", func(t *testing.T) {
		frontmatter := map[string]any{
			"on":     "push",
			"engine": "copilot",
			"sandbox": map[string]any{
				"agent": map[string]any{
					"targets": map[string]any{
						"copilot": map[string]any{
							"extraBodyFields": map[string]any{
								"custom-field": "custom-value",
							},
						},
					},
				},
			},
		}
		err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/tmp/gh-aw/awf-copilot-extra-body-fields-test.md")
		if err != nil {
			t.Errorf("valid copilot extraBodyFields should be accepted, got error: %v", err)
		}
	})

	t.Run("copilot sessionId string is accepted", func(t *testing.T) {
		frontmatter := map[string]any{
			"on":     "push",
			"engine": "copilot",
			"sandbox": map[string]any{
				"agent": map[string]any{
					"targets": map[string]any{
						"copilot": map[string]any{
							"sessionId": "${{ github.run_id }}",
						},
					},
				},
			},
		}
		err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/tmp/gh-aw/awf-copilot-session-id-test.md")
		if err != nil {
			t.Errorf("valid copilot sessionId should be accepted, got error: %v", err)
		}
	})

	t.Run("copilot all three BYOK fields together are accepted", func(t *testing.T) {
		frontmatter := map[string]any{
			"on":     "push",
			"engine": "copilot",
			"sandbox": map[string]any{
				"agent": map[string]any{
					"targets": map[string]any{
						"copilot": map[string]any{
							"extraHeaders": map[string]any{
								"x-openrouter-title": "my-workflow",
							},
							"extraBodyFields": map[string]any{
								"custom-field": "custom-value",
							},
							"sessionId": "${{ github.run_id }}",
						},
					},
				},
			},
		}
		err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/tmp/gh-aw/awf-copilot-byok-all-fields-test.md")
		if err != nil {
			t.Errorf("all three copilot BYOK fields together should be accepted, got error: %v", err)
		}
	})

	t.Run("copilot non-string extraHeaders value is rejected", func(t *testing.T) {
		frontmatter := map[string]any{
			"on":     "push",
			"engine": "copilot",
			"sandbox": map[string]any{
				"agent": map[string]any{
					"targets": map[string]any{
						"copilot": map[string]any{
							"extraHeaders": map[string]any{
								"x-count": 42,
							},
						},
					},
				},
			},
		}
		err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/tmp/gh-aw/awf-copilot-extra-headers-invalid-test.md")
		if err == nil {
			t.Error("non-string extraHeaders value should be rejected by schema validation")
		}
	})

	t.Run("copilot unknown field in target is rejected", func(t *testing.T) {
		frontmatter := map[string]any{
			"on":     "push",
			"engine": "copilot",
			"sandbox": map[string]any{
				"agent": map[string]any{
					"targets": map[string]any{
						"copilot": map[string]any{
							"unknownField": "value",
						},
					},
				},
			},
		}
		err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/tmp/gh-aw/awf-copilot-unknown-field-test.md")
		if err == nil {
			t.Error("unknown field in copilot target should be rejected by schema validation")
		}
	})
}

// TestValidateMainWorkflowFrontmatter_OnPermissionsVulnerabilityAlerts validates that
// vulnerability-alerts is accepted as a scope in on.permissions (regression for #40063).
func TestValidateMainWorkflowFrontmatter_OnPermissionsVulnerabilityAlerts(t *testing.T) {
	frontmatter := map[string]any{
		"on": map[string]any{
			"schedule":          []any{map[string]any{"cron": "0 0 * * *"}},
			"workflow_dispatch": nil,
			"permissions": map[string]any{
				"vulnerability-alerts": "read",
			},
			"steps": []any{
				map[string]any{
					"id":  "check",
					"run": "echo checking",
				},
			},
		},
		"engine": "copilot",
	}
	err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/tmp/gh-aw/on-permissions-vulnerability-alerts-test.md")
	if err != nil {
		t.Errorf("vulnerability-alerts: read should be accepted in on.permissions, got error: %v", err)
	}
}

// TestValidateMainWorkflowFrontmatter_OnPermissionsUnknownScopeRejected validates that
// unknown scopes in on.permissions are still rejected.
func TestValidateMainWorkflowFrontmatter_OnPermissionsUnknownScopeRejected(t *testing.T) {
	frontmatter := map[string]any{
		"on": map[string]any{
			"workflow_dispatch": nil,
			"permissions": map[string]any{
				"unknown-scope": "read",
			},
		},
		"engine": "copilot",
	}
	err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/tmp/gh-aw/on-permissions-unknown-scope-test.md")
	if err == nil {
		t.Error("unknown scope in on.permissions should be rejected by schema validation")
	}
}

func TestValidateMainWorkflowFrontmatter_JobsInputsRejectedBeforeSchema(t *testing.T) {
	frontmatter := map[string]any{
		"on":     "push",
		"engine": "copilot",
		"jobs": map[string]any{
			"my-job": map[string]any{
				"runs-on": "ubuntu-latest",
				"inputs": map[string]any{
					"name": map[string]any{"description": "test"},
				},
				"steps": []any{map[string]any{"run": "echo hi"}},
			},
		},
	}

	err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/tmp/gh-aw/jobs-inputs-pre-schema-test.md")
	if err == nil {
		t.Fatal("expected jobs.<name>.inputs validation error")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "jobs.my-job.inputs: inputs are not supported on jobs") {
		t.Fatalf("expected actionable jobs.inputs error, got: %v", err)
	}
	if strings.Contains(errMsg, "Unknown property: inputs") {
		t.Fatalf("expected pre-schema validation error instead of schema unknown-property error, got: %v", err)
	}
}

func TestGetParsedSchemaDocReturnsObject(t *testing.T) {
	// The cached well-known schemas and arbitrary schemas alike are returned as
	// map[string]any, so callers do not need a type assertion.
	for name, schemaJSON := range map[string]string{
		"main workflow": mainWorkflowSchema,
		"mcp config":    mcpConfigSchema,
		"custom":        `{"type": "object"}`,
	} {
		doc, err := getParsedSchemaDoc(schemaJSON)
		if err != nil {
			t.Fatalf("getParsedSchemaDoc(%s) returned error: %v", name, err)
		}
		if len(doc) == 0 {
			t.Errorf("getParsedSchemaDoc(%s) returned empty document", name)
		}
	}

	if _, err := getParsedSchemaDoc(`["not", "an", "object"]`); err == nil {
		t.Error("getParsedSchemaDoc() should return an error for a non-object schema")
	}

	if _, err := getParsedSchemaDoc(`null`); err == nil {
		t.Error("getParsedSchemaDoc() should return an error for a null schema")
	}
}
