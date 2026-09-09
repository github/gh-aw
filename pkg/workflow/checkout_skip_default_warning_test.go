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

// TestCheckoutSkipDefaultWithNoConfigsWarns verifies that a workflow using
// permissions.contents: none without any checkout: entries emits a warning,
// since it would otherwise silently leave the agent job with no repository
// checked out at all.
func TestCheckoutSkipDefaultWithNoConfigsWarns(t *testing.T) {
	tests := []struct {
		name            string
		frontmatter     string
		expectedWarning bool
	}{
		{
			name: "permissions.contents: none with no checkout entries warns",
			frontmatter: `---
on: push
engine: claude
permissions:
  contents: none
  issues: read
strict: false
---`,
			expectedWarning: true,
		},
		{
			name: "permissions.contents: none with a target checkout entry does not warn",
			frontmatter: `---
on: push
engine: claude
permissions:
  contents: none
  issues: read
checkout:
  - repository: octo-org/target-repository
    path: target
strict: false
---`,
			expectedWarning: false,
		},
		{
			name: "permissions.contents: none with checkout: false does not warn",
			frontmatter: `---
on: push
engine: claude
permissions:
  contents: none
  issues: read
checkout: false
strict: false
---`,
			expectedWarning: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "checkout-skip-default-warning-test")
			testFile := filepath.Join(tmpDir, "test-workflow.md")
			content := tt.frontmatter + "\n\n# Test Workflow\n\nThis is a test workflow.\n"
			require.NoError(t, os.WriteFile(testFile, []byte(content), 0644))

			compiler := NewCompiler()
			require.NoError(t, compiler.CompileWorkflow(testFile))

			if tt.expectedWarning {
				assert.Positive(t, compiler.GetWarningCount(), "expected a warning about the missing checkout")
			} else {
				assert.Equal(t, 0, compiler.GetWarningCount(), "unexpected warning emitted")
			}
		})
	}
}
