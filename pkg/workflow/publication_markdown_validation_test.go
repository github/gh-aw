package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNestedPublicationAlertLine(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		line         int
	}{
		{"details", "<details open>\n<summary>Context</summary>\n\n> [!WARNING]\n> Text\n\n</details>", 4},
		{"quote", "> Outer\n>\n> > [!NOTE]\n> > Text", 3},
		{"list", "- Context\n  > [!TIP]\n  > Text", 2},
		{"top-level", "<details>\n<summary>Context</summary>\n</details>\n\n> [!IMPORTANT]\n> Text", 0},
		{"fence", "<details>\n<summary>Context</summary>\n\n````markdown\n```\n> [!NOTE]\n````\n</details>", 0},
		{"quoted fence", "> ```markdown\n> > [!WARNING]\n> ```", 0},
		{"labeled quote", "<details>\n<summary>Context</summary>\n\n> **Warning:** Text\n</details>", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.line, nestedPublicationAlertLine(tc.source, 0))
		})
	}
}

func TestCompileWarnsAboutNestedPublicationAlerts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested-alerts.md")
	require.NoError(t, os.WriteFile(path, []byte(`---
on: workflow_dispatch
permissions:
  contents: read
safe-outputs:
  create-issue:
    body-footer: |
      > [!WARNING]
      > Review the result.

      </details>
  messages:
    body-header: |
      <details>
      <summary>Result</summary>
---
Report the result.
`), 0600))
	compiler := NewCompiler()
	stderr := testutil.CaptureStderr(t, func() {
		require.NoError(t, compiler.CompileWorkflow(path))
	})
	assert.Contains(t, stderr, "safe-outputs.create-issue.body-footer (template line 1): GitHub alerts must be top-level blocks")
}
