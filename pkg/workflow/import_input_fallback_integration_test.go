//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/testutil"
)

func TestImportInputFallbackExpressionCompilation(t *testing.T) {
	for _, tc := range []struct {
		name       string
		input      string
		expression string
		expected   string
	}{
		{name: "campaign", input: "campaign: eslint-rules", expression: "${{ github.aw.import-inputs.campaign || github.aw.import-inputs.package }}", expected: "eslint-rules"},
		{name: "package", input: "package: repo-assist", expression: "${{ github.aw.import-inputs.campaign || github.aw.import-inputs.package }}", expected: "repo-assist"},
		{name: "neither", input: "{}", expression: "${{ github.aw.import-inputs.campaign || github.aw.import-inputs.package }}", expected: ""},
		{name: "both provided campaign wins", input: "campaign: eslint-rules\n      package: repo-assist", expression: "${{ github.aw.import-inputs.campaign || github.aw.import-inputs.package }}", expected: "eslint-rules"},
		{name: "empty campaign falls back to package", input: "campaign: \"\"\n      package: repo-assist", expression: "${{ github.aw.import-inputs.campaign || github.aw.import-inputs.package }}", expected: "repo-assist"},
		{name: "missing campaign falls back to literal", input: "{}", expression: "${{ github.aw.import-inputs.campaign || 'fallback-literal' }}", expected: "fallback-literal"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "import-input-fallback-*")
			sharedDir := filepath.Join(tmpDir, "shared")
			if err := os.MkdirAll(sharedDir, 0o755); err != nil {
				t.Fatalf("failed to create shared directory: %v", err)
			}

			controlContent := `---
import-schema:
  campaign:
    type: string
  package:
    type: string
steps:
  - name: Check admission
    env:
      CAO_CAMPAIGN: ` + tc.expression + `
    run: echo "$CAO_CAMPAIGN"
---

Check admission for the selected campaign.
`
			if err := os.WriteFile(filepath.Join(sharedDir, "control.md"), []byte(controlContent), 0o644); err != nil {
				t.Fatalf("failed to write shared control workflow: %v", err)
			}

			workflowContent := `---
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
strict: false
imports:
  - uses: shared/control.md
    with:
      ` + tc.input + `
---

Run the campaign.
`
			workflowPath := filepath.Join(tmpDir, "campaign.md")
			if err := os.WriteFile(workflowPath, []byte(workflowContent), 0o644); err != nil {
				t.Fatalf("failed to write workflow: %v", err)
			}

			if err := NewCompiler().CompileWorkflow(workflowPath); err != nil {
				t.Fatalf("failed to compile workflow: %v", err)
			}

			lockContent, err := os.ReadFile(stringutil.MarkdownToLockFile(workflowPath))
			if err != nil {
				t.Fatalf("failed to read compiled workflow: %v", err)
			}
			compiled := string(lockContent)
			if tc.expected != "" && !strings.Contains(compiled, "CAO_CAMPAIGN: "+tc.expected) {
				t.Errorf("compiled workflow does not contain folded campaign value %q", tc.expected)
			}
			if strings.Contains(compiled, "github.aw.import-inputs") {
				t.Error("compiled workflow contains an unresolved import-inputs expression")
			}
		})
	}
}
