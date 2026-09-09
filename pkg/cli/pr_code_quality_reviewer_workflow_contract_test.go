//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// explicitMainAgentModelPattern matches a top-level, provider-qualified model
// declaration (for example "model: openai/gpt-5.4") in the workflow frontmatter.
var explicitMainAgentModelPattern = regexp.MustCompile(`(?m)^model: \S+/\S+$`)

func TestPRCodeQualityReviewerWorkflowSubAgentModelContract(t *testing.T) {
	t.Parallel()
	repoRoot, err := gitutil.FindGitRoot()
	if err != nil {
		t.Skipf("Skipping test: not in a git repository: %v", err)
	}

	workflowPath := filepath.Join(repoRoot, ".github", "workflows", "pr-code-quality-reviewer.md")
	content, err := os.ReadFile(workflowPath)
	require.NoError(t, err, "Should read pr-code-quality-reviewer workflow")

	text := string(content)
	assert.Regexp(t, explicitMainAgentModelPattern, text, "Main agent should use an explicit provider-qualified model")
	assert.Contains(t, text, "## agent: `grumpy-coder`", "Workflow should define the grumpy-coder sub-agent")
	assert.Contains(t, text, "model: small", "Sub-agent should use the portable small alias")
	assert.NotContains(t, text, "model: inherited", "Sub-agent should not inherit an unsupported tier-specific model")
}
