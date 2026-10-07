//go:build !integration && !windows

package workflow

import (
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdjustToolsForNonMCPEnginePreservesProxyConfiguration(t *testing.T) {
	engine, err := NewBehaviorDefinedEngine(loadDeepSeekSample(t))
	require.NoError(t, err)
	tools := map[string]any{
		"github":       map[string]any{"mode": "gh-proxy", "toolsets": []string{"repos"}},
		"cache-memory": true,
		"bash":         []string{"*"},
		"cli-proxy":    true,
		"probe":        map[string]any{"command": "echo", "args": []string{"test"}},
	}
	compiler := NewCompiler()
	actual := compiler.adjustToolsForEngineCapabilities(map[string]any{"tools": tools}, engine, tools)
	assert.Equal(t, tools, actual)
	assert.Zero(t, compiler.warningCount, "CLI-backed tools do not require native MCP allow-listing")
}

func TestResolveNonMCPEngineToolsPreservesToolsetsAndServers(t *testing.T) {
	engine, err := NewBehaviorDefinedEngine(loadDeepSeekSample(t))
	require.NoError(t, err)
	frontmatter, err := parser.ExtractFrontmatterFromContent(`---
on: workflow_dispatch
model: copilot/gpt-5.4
tools:
  github:
    mode: gh-proxy
    toolsets: [repos]
  cache-memory: true
  bash: ["*"]
mcp-servers:
  probe:
    command: echo
    args: [test]
---
Run the configured tools.
`)
	require.NoError(t, err)
	compiler := NewCompiler()
	result, err := compiler.resolveToolsConfiguration(frontmatter, frontmatter.Markdown, t.TempDir(), &parser.ImportsResult{}, engine, engine.GetID())
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"mode": "gh-proxy", "toolsets": []any{"repos"}}, result.tools["github"])
	assert.Contains(t, result.tools, "cache-memory")
	assert.Contains(t, result.tools, "probe")
	assert.Equal(t, true, result.tools["cli-proxy"])
	assert.Zero(t, compiler.warningCount)
}

func TestResolveNonMCPEngineToolsRejectsRestrictedBash(t *testing.T) {
	engine, err := NewBehaviorDefinedEngine(loadDeepSeekSample(t))
	require.NoError(t, err)
	frontmatter, err := parser.ExtractFrontmatterFromContent(`---
on: workflow_dispatch
model: copilot/gpt-5.4
tools:
  bash: [echo]
---
Run echo.
`)
	require.NoError(t, err)
	_, err = NewCompiler().resolveToolsConfiguration(frontmatter, frontmatter.Markdown, t.TempDir(), &parser.ImportsResult{}, engine, engine.GetID())
	require.ErrorContains(t, err, "does not support bash command allow-listing")
}

func TestAdjustToolsForNativeMCPEngineWithoutAllowlistRetainsFallback(t *testing.T) {
	engine, err := NewBehaviorDefinedEngine(newHarnessEngineDefinition())
	require.NoError(t, err)
	tools := map[string]any{"bash": []string{"*"}}
	compiler := NewCompiler()
	actual := compiler.adjustToolsForEngineCapabilities(map[string]any{"tools": tools}, engine, tools)
	assert.Equal(t, map[string]any{"github": map[string]any{}}, actual)
	assert.Equal(t, 2, compiler.warningCount)
}
