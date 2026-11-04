//go:build !integration

package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckoutRefWithStepsWarning(t *testing.T) {
	tests := []struct {
		name    string
		ref     string
		steps   string
		imports bool
		warn    bool
	}{
		{name: "planner branch with imported steps", ref: "${{ needs.plan.outputs.branch }}", imports: true, warn: true},
		{name: "planner branch with custom steps", ref: "${{ needs.plan.outputs.branch }}", steps: "steps:\n  - run: ./scripts/setup.sh", warn: true},
		{name: "event branch", ref: "${{ github.event.pull_request.head.ref }}", imports: true, warn: true},
		{name: "input branch", ref: "${{ inputs.branch }}", imports: true, warn: true},
		{name: "bracket notation", ref: "${{ needs['plan'].outputs.branch }}", imports: true, warn: true},
		{name: "case insensitive context", ref: "${{ INPUTS.branch }}", imports: true, warn: true},
		{name: "mixed default and input", ref: "${{ inputs.branch || github.event.repository.default_branch }}", imports: true, warn: true},
		{name: "prefixed default branch", ref: "untrusted/${{ github.event.repository.default_branch }}", imports: true, warn: true},
		{name: "computed default branch", ref: "${{ format('untrusted/{0}', github.event.repository.default_branch) }}", imports: true, warn: true},
		{name: "no steps", ref: "${{ needs.plan.outputs.branch }}"},
		{name: "empty steps", ref: "${{ needs.plan.outputs.branch }}", steps: "steps: []"},
		{name: "pre steps only", ref: "${{ needs.plan.outputs.branch }}", steps: "pre-steps:\n  - run: echo setup"},
		{name: "literal default branch", ref: "main", imports: true},
		{name: "default branch expression", ref: "${{ github.event.repository.default_branch }}", imports: true},
		{name: "bracket default branch", ref: "${{ github['event'].repository.default_branch }}", imports: true},
		{name: "literal expression", ref: "${{ 'inputs.branch' }}", imports: true},
		{name: "no ref", imports: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "checkout-ref-warning-test")
			shared := "---\nsteps:\n  - run: ./scripts/setup.sh\n---\n"
			require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "toolchain.md"), []byte(shared), 0644))
			checkout := ""
			if tt.ref != "" {
				checkout = "checkout:\n  ref: " + tt.ref + "\n"
			}
			imports := ""
			if tt.imports {
				imports = "imports:\n  - ./toolchain.md\n"
			}
			content := fmt.Sprintf(`---
on: workflow_dispatch
engine: claude
strict: false
permissions:
  contents: read
jobs:
  plan:
    runs-on: ubuntu-latest
    outputs:
      branch: ${{ steps.pick.outputs.branch }}
    steps:
      - id: pick
        run: echo "branch=feature/example" >> "$GITHUB_OUTPUT"
%s%s%s
---
# Checkout ref warning
Inspect the repository.
`, checkout, imports, tt.steps)
			testFile := filepath.Join(tmpDir, "workflow.md")
			require.NoError(t, os.WriteFile(testFile, []byte(content), 0644))
			compiler := NewCompiler()
			output := captureStderrOutput(t, func() {
				require.NoError(t, compiler.CompileWorkflow(testFile))
			})
			const warning = "checkout.ref is set to an expression"
			if tt.warn {
				assert.Contains(t, output, warning)
				assert.Contains(t, output, "on the host, outside the sandbox")
				assert.Contains(t, output, `git checkout -B "$BRANCH" --track "origin/$BRANCH"`)
				assert.Equal(t, 1, compiler.GetWarningCount())
			} else {
				assert.NotContains(t, output, warning)
				assert.Zero(t, compiler.GetWarningCount())
			}
		})
	}
}

func TestCheckoutRefWithStepsWarningMergedCheckouts(t *testing.T) {
	tests := []struct {
		name        string
		configs     []*CheckoutConfig
		customSteps string
		skipDefault bool
		warn        bool
	}{
		{
			name:    "first expression takes precedence",
			configs: []*CheckoutConfig{{Ref: "${{ inputs.branch }}"}, {Ref: "main"}},
			warn:    true,
		},
		{
			name:    "first literal takes precedence",
			configs: []*CheckoutConfig{{Ref: "main"}, {Ref: "${{ inputs.branch }}"}},
		},
		{
			name:        "custom checkout suppresses configured default",
			configs:     []*CheckoutConfig{{Ref: "${{ inputs.branch }}"}},
			customSteps: "steps:\n  - uses: actions/checkout@v7\n  - run: ./scripts/setup.sh",
		},
		{
			name:        "contents none suppresses configured default",
			configs:     []*CheckoutConfig{{Ref: "${{ inputs.branch }}"}},
			skipDefault: true,
		},
		{
			name:        "additional checkout still runs with contents none",
			configs:     []*CheckoutConfig{{Repository: "org/repo", Path: "target", Ref: "${{ inputs.branch }}"}},
			skipDefault: true,
			warn:        true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			steps := tt.customSteps
			if steps == "" {
				steps = "steps:\n  - run: ./scripts/setup.sh"
			}
			compiler := NewCompiler()
			output := captureStderrOutput(t, func() {
				compiler.warnCheckoutRefWithSteps(&WorkflowData{
					CheckoutConfigs: tt.configs, CustomSteps: steps, CheckoutSkipDefault: tt.skipDefault,
				}, "workflow.md")
			})
			assert.Equal(t, tt.warn, compiler.GetWarningCount() > 0)
			assert.Equal(t, tt.warn, strings.Contains(output, "checkout.ref is set to an expression"))
		})
	}
}
