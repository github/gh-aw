//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatWorkflowContentAppliesCodemodsAndPreservesComments(t *testing.T) {
	t.Parallel()
	input := `---
timeout_minutes: 30 # migrate this
engine:
    model: test
    id: copilot
# Trigger comment
on:
    workflow_dispatch:
permissions:
    issues: read
    contents: read
---

# Workflow

Keep this Markdown exactly.
`

	formatted, err := formatWorkflowContent(input, "workflow.md", GetAllCodemods())
	require.NoError(t, err)

	assert.NotContains(t, formatted, "timeout_minutes:")
	assert.Contains(t, formatted, "timeout-minutes: 30 # migrate this")
	assert.Contains(t, formatted, "# Trigger comment")
	assert.Contains(t, formatted, "  workflow_dispatch:")
	assert.Less(t, strings.Index(formatted, "on:"), strings.Index(formatted, "permissions:"))
	assert.Less(t, strings.Index(formatted, "permissions:"), strings.Index(formatted, "engine:"))
	assert.Less(t, strings.Index(formatted, "  contents:"), strings.Index(formatted, "  issues:"))
	assert.Contains(t, formatted, "# Workflow\n\nKeep this Markdown exactly.")
}

func TestFormatWorkflowContentRoundTrips(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"nested lists and inline comment": `---
tools:
   web-fetch:
      allowed-domains:
       - example.com
on:
 workflow_dispatch:
engine: copilot # engine comment
---
Body with no trailing newline`,
		"standalone and nested comments": `---
# tools comment
tools:
  web-fetch:
    # domain comment
    allowed-domains: [example.com]
engine: copilot
---

# Body
`,
	}

	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			once, err := formatWorkflowContent(input, "workflow.md", nil)
			require.NoError(t, err)
			twice, err := formatWorkflowContent(once, "workflow.md", nil)
			require.NoError(t, err)

			assert.Equal(t, once, twice)
			assert.Contains(t, once, "comment")
		})
	}
}

func TestNormalizeFrontmatterDoesNotTreatBlockScalarAsDelimiter(t *testing.T) {
	t.Parallel()
	input := `---
description: |
  before
  ---
  after
engine: copilot
---
# Body`

	formatted, err := normalizeFrontmatter(input)
	require.NoError(t, err)

	assert.Contains(t, formatted, "  ---")
	assert.Contains(t, formatted, "  after")
	assert.Contains(t, formatted, "engine: copilot")
	assert.True(t, strings.HasSuffix(formatted, "\n# Body"))
}

func TestNormalizeFrontmatterUsesLogicalThenAlphabeticalFieldOrder(t *testing.T) {
	t.Parallel()
	input := `---
zzz: last
steps: []
bbb: middle
on: workflow_dispatch
aaa: first
permissions: read-all
network: defaults
---
`

	formatted, err := normalizeFrontmatter(input)
	require.NoError(t, err)

	expectedOrder := []string{
		"on:",
		"permissions:",
		"network:",
		"steps:",
		"aaa:",
		"bbb:",
		"zzz:",
	}
	previous := -1
	for _, field := range expectedOrder {
		index := strings.Index(formatted, field)
		require.Greater(t, index, previous, "%s was not in logical/alphabetical order:\n%s", field, formatted)
		previous = index
	}
}

func TestRunFormatFormatsSelectedWorkflow(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	workflowPath := filepath.Join(tempDir, "example.md")
	input := "---\nengine: copilot\non:\n workflow_dispatch:\n---\n# Body"
	require.NoError(t, os.WriteFile(workflowPath, []byte(input), 0o644))

	err := RunFormat(FormatConfig{
		WorkflowIDs: []string{workflowPath},
		WorkflowDir: tempDir,
	})
	require.NoError(t, err)

	formatted, err := os.ReadFile(workflowPath)
	require.NoError(t, err)
	assert.Equal(t, "---\non:\n  workflow_dispatch:\nengine: copilot\n---\n# Body", string(formatted))
}

func TestRunFormatOnlyFormatsAgenticWorkflowsInDirectory(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	workflowPath := filepath.Join(tempDir, "workflow.md")
	documentationPath := filepath.Join(tempDir, "documentation.md")
	require.NoError(t, os.WriteFile(workflowPath, []byte("---\nengine: copilot\non: push\n---\n# Body"), 0o644))
	require.NoError(t, os.WriteFile(documentationPath, []byte("# Documentation\n"), 0o644))

	err := RunFormat(FormatConfig{WorkflowDir: tempDir})
	require.NoError(t, err)

	formatted, err := os.ReadFile(workflowPath)
	require.NoError(t, err)
	assert.Equal(t, "---\non: push\nengine: copilot\n---\n# Body", string(formatted))
	documentation, err := os.ReadFile(documentationPath)
	require.NoError(t, err)
	assert.Equal(t, "# Documentation\n", string(documentation))
}

func TestNewFormatCommand(t *testing.T) {
	t.Parallel()
	cmd := NewFormatCommand()

	assert.Equal(t, "format", cmd.Name())
	assert.NotNil(t, cmd.Flags().Lookup("dir"))
	assert.NotNil(t, cmd.ValidArgsFunction)
}
