//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

func TestAgentJobNeedsAndIfAugmentationCompiles(t *testing.T) {
	tmpDir := testutil.TempDir(t, "agent-job-gating")

	workflowContent := `---
on: workflow_dispatch
engine: copilot
max-daily-ai-credits: -1
strict: false
jobs:
  build:
    runs-on: ubuntu-latest
    outputs:
      outcome: ${{ steps.result.outputs.outcome }}
    steps:
      - id: result
        run: echo "outcome=failure" >> "$GITHUB_OUTPUT"
  agent:
    needs: [build]
    if: needs.build.outputs.outcome == 'failure'
---

Run only when the build fails.
`

	workflowFile := filepath.Join(tmpDir, "agent-job-gating.md")
	require.NoError(t, os.WriteFile(workflowFile, []byte(workflowContent), 0o644))

	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(workflowFile))

	lockBytes, err := os.ReadFile(filepath.Join(tmpDir, "agent-job-gating.lock.yml"))
	require.NoError(t, err)

	var compiled struct {
		Jobs map[string]map[string]any `yaml:"jobs"`
	}
	require.NoError(t, yaml.Unmarshal(lockBytes, &compiled))

	agentJob := compiled.Jobs["agent"]
	require.NotNil(t, agentJob)
	assert.Contains(t, agentJob["needs"], "activation")
	assert.Contains(t, agentJob["needs"], "build")
	// Use Contains rather than Equal so the assertion holds even if the compiler
	// prepends additional generated conditions (e.g. label filters) in the future.
	assert.Contains(t, agentJob["if"], "needs.build.outputs.outcome == 'failure'")
}
