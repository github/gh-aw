//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentPersonaExplorerWorkflowSubAgentContract(t *testing.T) {
	t.Parallel()
	repoRoot, err := gitutil.FindGitRoot()
	if err != nil {
		t.Skipf("Skipping test: not in a git repository: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(repoRoot, ".github", "workflows", "agent-persona-explorer.md"))
	require.NoError(t, err)

	main, agents, err := parser.ExtractInlineSubAgents(string(content))
	require.NoError(t, err)
	require.Len(t, agents, 1)
	require.Equal(t, "persona-evaluator", agents[0].Name)
	assert.Contains(t, main, "## Success Criteria", "Extraction must preserve the parent's publishing instructions")

	agent, err := parser.ExtractFrontmatterFromContent(agents[0].Content)
	require.NoError(t, err)
	assert.Equal(t, agents[0].Name, agent.Frontmatter["name"], "Claude requires a name in the generated agent's frontmatter")
	assert.NotEmpty(t, agent.Frontmatter["description"], "Claude requires a description to register the agent")
	assert.Equal(t, "inherit", agent.Frontmatter["model"], "Claude uses inherit, not inherited, for the parent model")
	assert.Contains(t, agent.Markdown, ".github/agents/agentic-workflows.md")
	assert.Contains(t, agent.Markdown, ".github/aw/create-agentic-workflow.md")
	assert.NotContains(t, agent.Markdown, "## Success Criteria", "Parent instructions must stay outside the evaluator")
}
