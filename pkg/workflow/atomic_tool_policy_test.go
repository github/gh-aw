//go:build !integration

package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAtomicToolPoliciesSurviveCapabilityNormalization(t *testing.T) {
	engine, err := NewBehaviorDefinedEngine(loadAtomicSample(t))
	require.NoError(t, err)
	for _, imported := range []bool{false, true} {
		t.Run(fmt.Sprintf("imported=%t", imported), func(t *testing.T) {
			toolYAML := `  github:
    min-integrity: approved
    allowed-repos: [example/private-repo]
    toolsets: [repos]
  bash: ["*"]
`
			content := "---\non: workflow_dispatch\nmodel: copilot/gpt-5.4\ntools:\n" + toolYAML
			imports := &parser.ImportsResult{}
			content += `mcp-servers:
  probe:
    command: echo
    args: [test]
---
Run the configured tools.
`
			frontmatter, err := parser.ExtractFrontmatterFromContent(content)
			require.NoError(t, err)
			if imported {
				encoded, err := json.Marshal(frontmatter.Frontmatter["tools"])
				require.NoError(t, err)
				imports.MergedTools = string(encoded)
				delete(frontmatter.Frontmatter, "tools")
			}
			compiler := NewCompiler()
			result, err := compiler.resolveToolsConfiguration(frontmatter, frontmatter.Markdown, t.TempDir(), imports, engine, engine.GetID())
			require.NoError(t, err)
			tools := NewTools(result.tools)
			require.NoError(t, tools.ParseError())
			require.NotNil(t, tools.GitHub)
			assert.Equal(t, GitHubIntegrityApproved, tools.GitHub.MinIntegrity)
			assert.Equal(t, GitHubReposScope{"example/private-repo"}, tools.GitHub.AllowedRepos)
			assert.Equal(t, GitHubToolsets{"repos"}, tools.GitHub.Toolset)
			assert.Contains(t, result.tools, "probe", "native MCP servers must reach the gateway")
			assert.Equal(t, compiler.GetExperimentalWarningCount(), compiler.GetWarningCount(), "gateway settings are not ignored")
		})
	}
}

func TestAtomicToolRestrictionsRejectedBeforeNormalization(t *testing.T) {
	engine, err := NewBehaviorDefinedEngine(loadAtomicSample(t))
	require.NoError(t, err)
	for _, tt := range []struct {
		tools, failure string
	}{
		{"bash: false", "cannot disable bash"},
		{"bash: []", "cannot disable bash"},
		{"bash: [echo]", "does not support bash command allow-listing"},
		{"edit: false", "cannot enforce tools.edit: false"},
	} {
		for _, imported := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/imported=%t", tt.tools, imported), func(t *testing.T) {
				content := "---\non: workflow_dispatch\nmodel: copilot/gpt-5.4\ntools:\n  " + tt.tools + "\n"
				imports := &parser.ImportsResult{}
				content += "---\nRun the configured tools.\n"
				frontmatter, err := parser.ExtractFrontmatterFromContent(content)
				require.NoError(t, err)
				if imported {
					encoded, err := json.Marshal(frontmatter.Frontmatter["tools"])
					require.NoError(t, err)
					imports.MergedTools = string(encoded)
					delete(frontmatter.Frontmatter, "tools")
				}
				_, err = NewCompiler().resolveToolsConfiguration(frontmatter, frontmatter.Markdown, t.TempDir(), imports, engine, engine.GetID())
				require.ErrorContains(t, err, tt.failure)
				if tt.tools == "bash: [echo]" {
					assert.NotContains(t, err.Error(), "'bash: false' to fully disable bash")
				}
			})
		}
	}
}

func TestAtomicCompiledGatewayPreservesGuardPolicy(t *testing.T) {
	dir := t.TempDir()
	engineSource, err := os.ReadFile("../../.github/workflows/shared/atomic.md")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "atomic-engine.md"), engineSource, 0o600))
	source := filepath.Join(dir, "policy.md")
	require.NoError(t, os.WriteFile(source, []byte(`---
on: workflow_dispatch
imports: [atomic-engine.md]
engine:
  id: atomic
model: copilot/gpt-5.4
permissions:
  contents: read
tools:
  bash: ["*"]
  github:
    min-integrity: approved
    allowed-repos: [example/private-repo]
---
Use the configured repository.
`), 0o600))
	require.NoError(t, NewCompiler().CompileWorkflow(source))
	lock, err := os.ReadFile(filepath.Join(dir, "policy.lock.yml"))
	require.NoError(t, err)
	assert.Contains(t, string(lock), `"guard-policies"`)
	assert.Contains(t, string(lock), `"private:example/private-repo"`)
	assert.Contains(t, string(lock), `"min-integrity": "approved"`)
}
