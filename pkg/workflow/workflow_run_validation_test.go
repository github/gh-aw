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

func TestWorkflowRunBranchValidation(t *testing.T) {
	tests := []struct {
		name          string
		onYAML        string
		strictMode    bool
		errorContains string
		warningCount  int
	}{
		{
			name:         "workflow_run without branches - normal mode - should warn",
			onYAML:       "  workflow_run:\n    workflows: [build]\n    types: [completed]",
			warningCount: 1,
		},
		{
			name:          "workflow_run without branches - strict mode - should error",
			onYAML:        "  workflow_run:\n    workflows: [build]\n    types: [completed]",
			strictMode:    true,
			errorContains: "workflow_run trigger should include branch restrictions",
		},
		{
			name:         "workflow_run with branches - should pass",
			onYAML:       "  workflow_run:\n    workflows: [build]\n    types: [completed]\n    branches: [main, develop]",
			warningCount: 0,
		},
		{
			name:       "workflow_run with branches - strict mode - should pass",
			onYAML:     "  workflow_run:\n    workflows: [build]\n    types: [completed]\n    branches: [main]",
			strictMode: true,
		},
		{
			name:       "workflow_run with sibling bots - strict mode - should pass",
			onYAML:     "  bots: [dependabot]\n  workflow_run:\n    workflows: [build]\n    types: [completed]\n    branches: [main]",
			strictMode: true,
		},
		{
			name:       "workflow_run with sibling roles - strict mode - should pass",
			onYAML:     "  roles: all\n  workflow_run:\n    workflows: [build]\n    types: [completed]\n    branches: [main]",
			strictMode: true,
		},
		{
			name:          "workflow_run without workflows - should error",
			onYAML:        "  workflow_run:\n    types: [completed]",
			errorContains: "workflow_run trigger must include a non-empty workflows field",
		},
		{
			name:          "workflow_run without workflows - strict mode - should error",
			onYAML:        "  workflow_run:\n    types: [completed]",
			strictMode:    true,
			errorContains: "workflow_run trigger must include a non-empty workflows field",
		},
		{
			name:          "workflow_run with empty workflows - strict mode - should error",
			onYAML:        "  workflow_run:\n    workflows: []\n    types: [completed]",
			strictMode:    true,
			errorContains: "workflow_run trigger must include a non-empty workflows field",
		},
		{
			name:          "workflow_run with whitespace workflows - non-strict mode - should error",
			onYAML:        "  workflow_run:\n    workflows: [\" \", \"\"]\n    types: [completed]",
			errorContains: "workflow_run trigger must include a non-empty workflows field",
		},
		{
			name:   "no workflow_run - should pass",
			onYAML: "  push:\n    branches: [main]",
		},
		{
			name:         "mixed triggers with workflow_run without branches - should warn",
			onYAML:       "  push:\n    branches: [main]\n  workflow_run:\n    workflows: [build]\n    types: [completed]",
			warningCount: 1,
		},
		{
			name:   "workflow_run with empty branches array - should not warn",
			onYAML: "  workflow_run:\n    workflows: [build]\n    types: [completed]\n    branches: []",
		},
		{
			name:          "absent on field - should be detected as a shared workflow",
			errorContains: "Shared agentic workflow detected",
		},
		{
			name:         "workflow_run with multiple workflow names - should warn",
			onYAML:       "  workflow_run:\n    workflows: [build, test]\n    types: [completed]",
			warningCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filename := strings.ReplaceAll(tt.name, " ", "-") + ".md"
			mdFile := filepath.Join(t.TempDir(), filename)
			require.NoError(t, os.WriteFile(mdFile, []byte(frontmatterWithOn(tt.onYAML, tt.strictMode)), 0644))

			compiler := NewCompiler()
			compiler.SetStrictMode(tt.strictMode)
			compiler.SetNoEmit(true)

			err := compiler.CompileWorkflow(mdFile)
			if tt.errorContains != "" {
				require.ErrorContains(t, err, tt.errorContains)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.warningCount, compiler.GetWarningCount())
		})
	}
}

func frontmatterWithOn(onYAML string, strictMode bool) string {
	if onYAML == "" {
		return `---
features:
  samples: true
---

# Workflow Run Validation
Test workflow content.`
	}

	onField := "on:\n" + onYAML + "\n"

	var settings string
	if strictMode {
		settings = `tools:
  github:
    toolsets: [repos]`
	} else {
		settings = `strict: false
tools:
  github: false
features:
  dangerously-disable-sandbox-agent: true
sandbox:
  agent: false`
	}

	return `---
` + onField + settings + `
---

# Workflow Run Validation
Test workflow content.`
}

func TestParseWorkflowRunTrigger(t *testing.T) {
	tests := []struct {
		name      string
		onYAML    string
		wantMap   map[string]any
		wantFound bool
	}{
		{
			name:   "valid workflow_run mapping",
			onYAML: "on:\n  workflow_run:\n    workflows: [build]",
			wantMap: map[string]any{
				"workflows": []any{"build"},
			},
			wantFound: true,
		},
		{name: "invalid YAML", onYAML: "on: [", wantFound: false},
		{name: "missing on key", onYAML: "push: {}", wantFound: false},
		{name: "on is scalar", onYAML: "on: push", wantFound: false},
		{name: "on is null", onYAML: "on: null", wantFound: false},
		{name: "workflow_run is scalar", onYAML: "on:\n  workflow_run: build", wantFound: false},
		{name: "workflow_run is null", onYAML: "on:\n  workflow_run: null", wantFound: false},
		{name: "workflow_run key missing", onYAML: "on:\n  push: {}", wantFound: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workflowRunMap, ok := parseWorkflowRunTrigger(tt.onYAML)
			assert.Equal(t, tt.wantFound, ok)
			if tt.wantFound {
				assert.Equal(t, tt.wantMap, workflowRunMap)
			} else {
				assert.Nil(t, workflowRunMap)
			}
		})
	}
}

func TestValidateWorkflowRunHasWorkflows(t *testing.T) {
	tests := []struct {
		name        string
		workflowRun map[string]any
		wantError   bool
	}{
		{name: "missing workflows", workflowRun: map[string]any{}, wantError: true},
		{name: "empty workflows array", workflowRun: map[string]any{"workflows": []any{}}, wantError: true},
		{name: "empty workflows string", workflowRun: map[string]any{"workflows": ""}, wantError: true},
		{name: "whitespace workflows string", workflowRun: map[string]any{"workflows": " \t"}, wantError: true},
		{name: "non-string workflows value", workflowRun: map[string]any{"workflows": 42}, wantError: true},
		{name: "non-string array items", workflowRun: map[string]any{"workflows": []any{42, nil}}, wantError: true},
		{name: "valid workflow name", workflowRun: map[string]any{"workflows": []any{"build"}}, wantError: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateWorkflowRunHasWorkflows(tt.workflowRun, "workflow.md")
			if tt.wantError {
				require.ErrorContains(t, err, "workflow_run trigger must include a non-empty workflows field")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestHasNonEmptyWorkflowRunWorkflows(t *testing.T) {
	tests := []struct {
		name      string
		workflows any
		want      bool
	}{
		{name: "non-empty string", workflows: "build", want: true},
		{name: "empty string", workflows: "", want: false},
		{name: "whitespace string", workflows: " \t", want: false},
		{name: "string slice with non-empty name", workflows: []string{" ", "build"}, want: true},
		{name: "empty string slice", workflows: []string{}, want: false},
		{name: "any slice with non-empty name", workflows: []any{42, "build"}, want: true},
		{name: "any slice without non-empty string", workflows: []any{42, ""}, want: false},
		{name: "non-string value", workflows: 42, want: false},
		{name: "nil value", workflows: nil, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, hasNonEmptyWorkflowRunWorkflows(tt.workflows))
		})
	}
}
