//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCTR015AllowedLabelsGlobScope tests that the compiler rejects (CTR-015) when
// a bare "*" wildcard appears in any safe-outputs allowed-labels field.
func TestCTR015AllowedLabelsGlobScope(t *testing.T) {
	basePermissions := `
permissions:
  contents: read
  issues: read

on:
  issues:
    types: [opened]

engine: claude
strict: false
`

	tests := []struct {
		name        string
		safeOutputs string
		expectError bool
	}{
		{
			name: "create-issue with bare * in allowed-labels triggers error",
			safeOutputs: `safe-outputs:
  create-issue:
    allowed-labels: ["*"]
`,
			expectError: true,
		},
		{
			name: "create-discussion with bare * triggers error",
			safeOutputs: `safe-outputs:
  create-discussion:
    allowed-labels: ["*"]
`,
			expectError: true,
		},
		{
			name: "create-pull-request with bare * triggers error",
			safeOutputs: `safe-outputs:
  create-pull-request:
    allowed-labels: ["*"]
`,
			expectError: true,
		},
		{
			name: "merge-pull-request required-labels bare * is literal label name, not CTR-015",
			safeOutputs: `safe-outputs:
  merge-pull-request:
    allowed-base-branches: [agent-work/*]
    required-labels: ["*"]
`,
			expectError: false,
		},
		{
			name: "update-discussion with bare * triggers error",
			safeOutputs: `safe-outputs:
  update-discussion:
    allowed-labels: ["*"]
`,
			expectError: true,
		},
		{
			name: "specific label names do not trigger error",
			safeOutputs: `safe-outputs:
  create-issue:
    allowed-labels: ["bug", "enhancement"]
`,
			expectError: false,
		},
		{
			name: "prefix glob pattern does not trigger error",
			safeOutputs: `safe-outputs:
  create-issue:
    allowed-labels: ["team-*", "priority-*"]
`,
			expectError: false,
		},
		{
			name: "mixed specific and bare * triggers error",
			safeOutputs: `safe-outputs:
  create-issue:
    allowed-labels: ["bug", "*", "enhancement"]
`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "ctr015-test")

			content := "---\n" + basePermissions + tt.safeOutputs + "---\n\n# Test Workflow\n\nTest body.\n"
			wfPath := filepath.Join(tmpDir, "test.md")
			err := os.WriteFile(wfPath, []byte(content), 0o600)
			require.NoError(t, err, "Should write test workflow file")

			compiler := NewCompiler()
			compileErr := compiler.CompileWorkflow(wfPath)

			if tt.expectError {
				require.Error(t, compileErr,
					"CTR-015: expected error for bare \"*\" in allowed-labels")
				require.ErrorContains(t, compileErr, "CTR-015",
					"CTR-015: error message should reference the rule ID")
			} else {
				assert.NoError(t, compileErr,
					"CTR-015: did not expect error for valid allowed-labels")
			}
		})
	}
}

// TestRequiredLabelsConjunctive verifies that required-labels requires ALL labels to match
// (conjunctive semantics) for add-labels, remove-labels, and add-comment operations.
func TestRequiredLabelsConjunctive(t *testing.T) {
	basePermissions := `
permissions:
  contents: read
  issues: read

on:
  issues:
    types: [opened]

engine: copilot
strict: false
`
	tests := []struct {
		name        string
		safeOutputs string
		handler     string
		wantLabels  []any
	}{
		{
			name: "add-labels required-labels as array",
			safeOutputs: `safe-outputs:
  add-labels:
    allowed: [bug]
    required-labels: [approved, ready]
`,
			handler:    "add_labels",
			wantLabels: []any{"approved", "ready"},
		},
		{
			name: "add-comment required-labels as array",
			safeOutputs: `safe-outputs:
  add-comment:
    required-labels: [approved]
`,
			handler:    "add_comment",
			wantLabels: []any{"approved"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "conjunctive-rl-test")
			content := "---\n" + basePermissions + tt.safeOutputs + "---\n\n# Test\n\nBody.\n"
			wfPath := filepath.Join(tmpDir, "test.md")
			err := os.WriteFile(wfPath, []byte(content), 0o600)
			require.NoError(t, err)

			compiler := NewCompiler()
			compileErr := compiler.CompileWorkflow(wfPath)
			require.NoError(t, compileErr, "required-labels array should compile without error")

			lockPath := wfPath[:len(wfPath)-3] + ".lock.yml"
			lockBytes, err := os.ReadFile(lockPath)
			require.NoError(t, err, "lock file should exist")
			handlerConfig := extractHandlerConfig(t, string(lockBytes))
			assert.Equal(t, tt.wantLabels, handlerConfig[tt.handler]["required_labels"],
				"compiled config should contain required_labels as array")
		})
	}
}
