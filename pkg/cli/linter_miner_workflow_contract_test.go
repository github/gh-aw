//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLinterMinerWorkflowSubAgentModelContract(t *testing.T) {
	t.Parallel()
	repoRoot, err := gitutil.FindGitRoot()
	if err != nil {
		t.Skipf("Skipping test: not in a git repository: %v", err)
	}

	workflowPath := filepath.Join(repoRoot, ".github", "workflows", "linter-miner.md")
	content, err := os.ReadFile(workflowPath)
	require.NoError(t, err, "Should read linter-miner workflow")

	text := string(content)
	assert.Contains(t, text, "## agent: `code-pattern-scanner`", "Workflow should define the code-pattern-scanner sub-agent")
	assert.Contains(t, text, "## agent: `linter-writer`", "Workflow should define the linter-writer sub-agent")
	assert.Contains(t, text, "model: copilot/mai-code-1-flash-picker", "Workflow should use the supported MAI Flash model")
	assert.NotContains(t, text, "model: copilot/gpt-5.4", "Workflow should not use the unavailable GPT-5.4 model")
	assert.NotContains(t, text, "model: inherited", "Sub-agents should not use the unsupported model: inherited value")
	assert.NotContains(t, text, "model: kiwi", "Sub-agents should not use the unavailable kiwi model")
}
