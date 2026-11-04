//go:build !integration

package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type subagentModelDeclaration struct {
	Name     string   `json:"name"`
	Model    string   `json:"model"`
	Patterns []string `json:"patterns"`
}

// compileSubagentModelDeclarations compiles a workflow and returns the generate_aw_info
// GH_AW_INFO_SUB_AGENT_MODELS declarations, or nil when the variable is absent.
func compileSubagentModelDeclarations(t *testing.T, content string) []subagentModelDeclaration {
	t.Helper()
	source := filepath.Join(t.TempDir(), "workflow.md")
	require.NoError(t, os.WriteFile(source, []byte(content), 0600))
	compiler := NewCompiler()
	data, err := compiler.ParseWorkflowFile(source)
	require.NoError(t, err)
	generated, err := compiler.CompileToYAML(data, source)
	require.NoError(t, err)
	var workflow struct {
		Jobs map[string]struct {
			Steps []struct {
				ID  string            `yaml:"id"`
				Env map[string]string `yaml:"env"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(generated), &workflow))
	for _, step := range workflow.Jobs["activation"].Steps {
		if step.ID != "generate_aw_info" {
			continue
		}
		encoded, ok := step.Env["GH_AW_INFO_SUB_AGENT_MODELS"]
		if !ok {
			return nil
		}
		var declarations []subagentModelDeclaration
		require.NoError(t, json.Unmarshal([]byte(encoded), &declarations))
		return declarations
	}
	t.Fatal("generate_aw_info step must exist")
	return nil
}

// TestClaudeCodexAwInfoContainsDeclaredSubagentModels covers
// claude_codex_aw_info_contains_declared_subagent_models with the inline declarations of
// typist (Claude, `small`), agent-persona-explorer (Claude, `inherit`) and
// daily-arxiv-researcher (Codex, provider-prefixed models; its run 39394965127 never delegated).
func TestClaudeCodexAwInfoContainsDeclaredSubagentModels(t *testing.T) {
	const frontmatter = "---\non: workflow_dispatch\nstrict: false\npermissions:\n  contents: read\n"
	tests := []struct {
		name string
		body string
		want []subagentModelDeclaration
	}{
		{
			name: "claude small uses anthropic patterns",
			body: frontmatter + "engine: claude\n---\n# Work\nUse duplicate-type-finder.\n\n" +
				"## agent: `duplicate-type-finder`\n---\ndescription: Collects all Go type definitions under pkg/ and groups them into exact, near, and semantic duplicate clusters\nmodel: small\n---\nYou are a Go type-definition analyst.\n",
			want: []subagentModelDeclaration{{Name: "duplicate-type-finder", Model: "small", Patterns: []string{"anthropic/*haiku*"}}},
		},
		{
			name: "claude on the copilot provider uses github-copilot patterns",
			body: frontmatter + "engine: claude\nmodel: copilot/claude-sonnet-4.6\n---\n# Work\nUse duplicate-type-finder.\n\n" +
				"## agent: `duplicate-type-finder`\n---\ndescription: Collects all Go type definitions\nmodel: small\n---\nYou are a Go type-definition analyst.\n",
			want: []subagentModelDeclaration{{Name: "duplicate-type-finder", Model: "small", Patterns: []string{"github-copilot/*haiku*", "github-copilot/gpt-5*mini*", "github-copilot/gpt-5*nano*", "github-copilot/gemini-*flash*lite*"}}},
		},
		{
			name: "claude inherit declares no patterns",
			body: frontmatter + "engine: claude\n---\n# Work\nUse persona-evaluator.\n\n" +
				"## agent: `persona-evaluator`\n---\nname: persona-evaluator\ndescription: Evaluates workflow scenarios using the repository's agentic-workflows authoring guidance\nmodel: inherit\n---\nYou are acting as the agentic-workflows custom agent.\n",
			want: []subagentModelDeclaration{{Name: "persona-evaluator", Model: "inherit", Patterns: []string{}}},
		},
		{
			name: "codex provider-prefixed models",
			body: frontmatter + "engine: codex\nmodel: openai/gpt-5.6-sol\n---\n# Work\nScreen papers.\n\n" +
				"## agent: `paper-screener`\n---\ndescription: Fast relevance screening of arXiv paper abstracts for GitHub Agentic Workflows\nmodel: openai/gpt-5.4-mini\n---\nScreen an arXiv paper abstract.\n\n" +
				"## agent: `opportunity-extractor`\n---\ndescription: Extracts a specific actionable improvement for gh-aw from a relevant arXiv paper\nmodel: openai/gpt-5.6-sol\n---\nExtract one improvement.\n",
			want: []subagentModelDeclaration{
				{Name: "paper-screener", Model: "openai/gpt-5.4-mini", Patterns: []string{"openai/gpt-5.4-mini"}},
				{Name: "opportunity-extractor", Model: "openai/gpt-5.6-sol", Patterns: []string{"openai/gpt-5.6-sol"}},
			},
		},
		{
			name: "codex alias keeps only openai patterns",
			body: frontmatter + "engine: codex\n---\n# Work\nScreen papers.\n\n" +
				"## agent: `paper-screener`\n---\ndescription: Screens papers\nmodel: small\n---\nScreen.\n",
			want: []subagentModelDeclaration{{Name: "paper-screener", Model: "small", Patterns: []string{"openai/gpt-5*mini*", "openai/gpt-5*nano*"}}},
		},
		{
			name: "claude provider-qualified alias expands the alias",
			body: frontmatter + "engine: claude\nmodels:\n  fast: [anthropic/*haiku*]\n---\n# Work\nUse fast-worker.\n\n" +
				"## agent: `fast-worker`\n---\ndescription: Works fast\nmodel: anthropic/fast\n---\nWork.\n\n" +
				"## agent: `small-worker`\n---\ndescription: Works small\nmodel: anthropic/small\n---\nWork.\n",
			want: []subagentModelDeclaration{
				{Name: "fast-worker", Model: "anthropic/fast", Patterns: []string{"anthropic/*haiku*"}},
				{Name: "small-worker", Model: "anthropic/small", Patterns: []string{"anthropic/*haiku*"}},
			},
		},
		{
			name: "claude on the copilot provider expands a copilot-qualified alias",
			body: frontmatter + "engine: claude\nmodel: copilot/claude-sonnet-4.6\n---\n# Work\nUse duplicate-type-finder.\n\n" +
				"## agent: `duplicate-type-finder`\n---\ndescription: Collects all Go type definitions\nmodel: copilot/small\n---\nYou are a Go type-definition analyst.\n",
			want: []subagentModelDeclaration{{Name: "duplicate-type-finder", Model: "copilot/small", Patterns: []string{"github-copilot/*haiku*", "github-copilot/gpt-5*mini*", "github-copilot/gpt-5*nano*", "github-copilot/gemini-*flash*lite*"}}},
		},
		{
			name: "codex provider-qualified alias expands the alias",
			body: frontmatter + "engine: codex\n---\n# Work\nScreen papers.\n\n" +
				"## agent: `paper-screener`\n---\ndescription: Screens papers\nmodel: openai/small\n---\nScreen.\n",
			want: []subagentModelDeclaration{{Name: "paper-screener", Model: "openai/small", Patterns: []string{"openai/gpt-5*mini*", "openai/gpt-5*nano*"}}},
		},
		{
			name: "another provider's prefix is not stripped",
			body: frontmatter + "engine: codex\n---\n# Work\nScreen papers.\n\n" +
				"## agent: `paper-screener`\n---\ndescription: Screens papers\nmodel: anthropic/small\n---\nScreen.\n",
			want: []subagentModelDeclaration{{Name: "paper-screener", Model: "anthropic/small", Patterns: []string{}}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := compileSubagentModelDeclarations(t, test.body)
			assert.Equal(t, test.want, got)
		})
	}
}
