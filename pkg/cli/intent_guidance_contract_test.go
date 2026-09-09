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

func TestIntentGuidanceContract(t *testing.T) {
	t.Parallel()
	repoRoot, err := gitutil.FindGitRoot()
	if err != nil {
		t.Skipf("Skipping test: not in a git repository: %v", err)
	}

	intentPath := filepath.Join(repoRoot, ".github", "aw", "intent.md")
	intentContent, err := os.ReadFile(intentPath)
	require.NoError(t, err, "Should read intent guidance")
	intentText := string(intentContent)
	for _, token := range []string{
		"IntentSpec",
		"activation conditions",
		"noop conditions",
		"candidate intents",
		"scenario fixtures",
		"UNKNOWN",
	} {
		assert.Containsf(t, intentText, token, "Intent guidance must include %q", token)
	}

	creatorContent, err := os.ReadFile(filepath.Join(repoRoot, ".github", "aw", "create-agentic-workflow.md"))
	require.NoError(t, err, "Should read workflow creator")
	assert.Contains(t, string(creatorContent), "When the request already states a goal", "Workflow creator must keep explicit requests lightweight")

	for _, name := range []string{
		"designer.md",
		"create-agentic-workflow.md",
		"update-agentic-workflow.md",
		"evals.md",
		"maintainer.md",
	} {
		content, readErr := os.ReadFile(filepath.Join(repoRoot, ".github", "aw", name))
		require.NoErrorf(t, readErr, "Should read %s", name)
		assert.Containsf(t, string(content), "intent.md", "%s must use shared intent guidance", name)
	}

	updaterContent, err := os.ReadFile(filepath.Join(repoRoot, ".github", "aw", "update-agentic-workflow.md"))
	require.NoError(t, err, "Should read workflow updater")
	assert.Contains(t, string(updaterContent), "when an implementation-only change selects a different architecture", "Workflow updater must revalidate architecture-dependent conditions")

	generatorContent, err := os.ReadFile(filepath.Join(repoRoot, ".github", "workflows", "workflow-generator.md"))
	require.NoError(t, err, "Should read workflow generator")
	assert.Contains(t, string(generatorContent), "canonical intent", "Workflow generator must require intent-driven issue-form design")
}
