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
		name             string
		ref              string
		steps            string
		preAgentSteps    string
		imports          bool
		importedPreAgent bool
		warn             bool
	}{
		{name: "planner branch with imported steps", ref: "${{ needs.plan.outputs.branch }}", imports: true, warn: true},
		{name: "planner branch with custom steps", ref: "${{ needs.plan.outputs.branch }}", steps: "steps:\n  - run: ./scripts/setup.sh", warn: true},
		{name: "event branch", ref: "${{ github.event.pull_request.head.ref }}", imports: true, warn: true},
		{name: "head ref", ref: "${{ github.head_ref }}", imports: true, warn: true},
		{name: "ref name", ref: "${{ github.ref_name }}", imports: true, warn: true},
		{name: "ref", ref: "${{ github.ref }}", imports: true, warn: true},
		{name: "input branch", ref: "${{ inputs.branch }}", imports: true, warn: true},
		{name: "bracket notation", ref: "${{ needs['plan'].outputs.branch }}", imports: true, warn: true},
		{name: "case insensitive context", ref: "${{ INPUTS.branch }}", imports: true, warn: true},
		{name: "mixed default and input", ref: "${{ inputs.branch || github.event.repository.default_branch }}", imports: true, warn: true},
		{name: "prefixed default branch", ref: "untrusted/${{ github.event.repository.default_branch }}", imports: true, warn: true},
		{name: "computed default branch", ref: "${{ format('untrusted/{0}', github.event.repository.default_branch) }}", imports: true, warn: true},
		{name: "workflow-owned pre-agent steps", ref: "${{ inputs.branch }}", preAgentSteps: "pre-agent-steps:\n  - run: ./scripts/setup.sh", warn: true},
		{name: "imported pre-agent steps", ref: "${{ inputs.branch }}", imports: true, importedPreAgent: true, warn: true},
		{name: "no steps", ref: "${{ needs.plan.outputs.branch }}"},
		{name: "empty steps", ref: "${{ needs.plan.outputs.branch }}", steps: "steps: []"},
		{name: "empty pre-agent steps", ref: "${{ needs.plan.outputs.branch }}", preAgentSteps: "pre-agent-steps: []"},
		{name: "pre steps only", ref: "${{ needs.plan.outputs.branch }}", steps: "pre-steps:\n  - run: echo setup"},
		{name: "literal default branch", ref: "main", imports: true},
		{name: "default branch expression", ref: "${{ github.event.repository.default_branch }}", imports: true},
		{name: "bracket default branch", ref: "${{ github['event'].repository.default_branch }}", imports: true},
		{name: "trusted base SHA", ref: "${{ github.event.pull_request.base.sha }}", imports: true},
		{name: "trusted base ref", ref: "${{ github.event.pull_request.base.ref }}", imports: true},
		{name: "literal expression", ref: "${{ 'inputs.branch' }}", imports: true},
		{name: "no ref", imports: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "checkout-ref-warning-test")
			shared := "---\n"
			if tt.importedPreAgent {
				shared += "pre-agent-steps:\n  - run: ./scripts/setup.sh\n"
			} else {
				shared += "steps:\n  - run: ./scripts/setup.sh\n"
			}
			shared += "---\n"
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
%s%s%s%s
---
# Checkout ref warning
Inspect the repository.
`, checkout, imports, tt.steps, tt.preAgentSteps)
			testFile := filepath.Join(tmpDir, "workflow.md")
			require.NoError(t, os.WriteFile(testFile, []byte(content), 0644))
			compiler := NewCompiler()
			output := captureStderrOutput(t, func() {
				require.NoError(t, compiler.CompileWorkflow(testFile))
			})
			const warning = "uses checkout.ref"
			if tt.warn {
				assert.Contains(t, output, warning)
				assert.Contains(t, output, "repository")
				assert.Contains(t, output, tt.ref)
				assert.Contains(t, output, "execute outside the sandbox")
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
		name          string
		configs       []*CheckoutConfig
		customSteps   string
		preAgentSteps string
		skipDefault   bool
		warn          bool
		wantName      string
		wantRef       string
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
			wantName:    "org/repo into target",
			wantRef:     "${{ inputs.branch }}",
		},
		{
			name:    "unparseable expression fails closed",
			configs: []*CheckoutConfig{{Ref: "${{ inputs.branch"}},
			warn:    true,
		},
		{
			name:          "workflow-owned pre-agent steps",
			configs:       []*CheckoutConfig{{Ref: "${{ inputs.branch }}"}},
			preAgentSteps: "pre-agent-steps:\n  - run: ./scripts/setup.sh",
			warn:          true,
		},
		{
			name: "suppressed wiki default is ignored",
			configs: []*CheckoutConfig{
				{Wiki: true, Ref: "${{ inputs.branch }}"},
				{Ref: "main"},
			},
		},
		{
			name:     "default branch expression on another repository",
			configs:  []*CheckoutConfig{{Repository: "org/repo", Path: "target", Ref: "${{ github.event.repository.default_branch }}"}},
			warn:     true,
			wantName: "org/repo into target",
			wantRef:  "${{ github.event.repository.default_branch }}",
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
					CheckoutConfigs: tt.configs, CustomSteps: steps, PreAgentSteps: tt.preAgentSteps, CheckoutSkipDefault: tt.skipDefault,
				}, "workflow.md")
			})
			assert.Equal(t, tt.warn, compiler.GetWarningCount() > 0)
			assert.Equal(t, tt.warn, strings.Contains(output, "uses checkout.ref"))
			if tt.wantName != "" {
				assert.Contains(t, output, tt.wantName)
				assert.Contains(t, output, tt.wantRef)
			}
		})
	}
}
