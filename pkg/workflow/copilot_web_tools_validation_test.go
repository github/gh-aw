package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWebFetchMCPRequiresAWF(t *testing.T) {
	_, err := NewCompiler().ParseWorkflowString(`---
on: workflow_dispatch
strict: false
engine: copilot
sandbox:
  agent: false
tools:
  web-fetch:
---
Fetch a page.
`, "web-fetch-no-awf.md")
	require.ErrorContains(t, err, "requires the AWF agent sandbox")
}

func TestWebFetchMCPCompilation(t *testing.T) {
	for _, engine := range []string{"copilot", "pi", "codex", "gemini", "claude"} {
		t.Run(engine, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "web-fetch.md")
			require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf(`---
on: workflow_dispatch
engine: %s
tools:
  github: false
  web-fetch:
---
Fetch a page.
`, engine)), 0600))
			require.NoError(t, NewCompiler(WithSkipValidation(true)).CompileWorkflow(path))
			lock, err := os.ReadFile(filepath.Join(filepath.Dir(path), "web-fetch.lock.yml"))
			require.NoError(t, err)
			expected := engine == "copilot" || engine == "pi"
			assert.Equal(t, expected, strings.Contains(string(lock), "export GH_AW_WEB_FETCH_MCP=1"))
			assert.Equal(t, expected, strings.Contains(string(lock), `"name":"web-fetch","tools":["web_fetch"]`))
			assert.NotContains(t, string(lock), "web_fetch_mcp_server.cjs", "the host gateway must not start the server")
		})
	}
}

func TestWebFetchMCPDeclarativeEngine(t *testing.T) {
	sample, err := os.ReadFile("../../.github/workflows/shared/opencode.md")
	require.NoError(t, err)
	sampleDefinition := strings.ReplaceAll(string(sample), "id: opencode", "id: custom-fetch")
	require.Contains(t, sampleDefinition, "web-fetch-fallback: true")
	for _, tc := range []struct {
		name       string
		capability string
		tool       string
		want       bool
	}{
		{"opted in", "web-fetch-fallback: true", "  web-fetch: {}", true},
		{"not opted in", "web-fetch-fallback: false", "  web-fetch: {}", false},
		{"tool omitted", "web-fetch-fallback: true", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			definition := strings.Replace(sampleDefinition, "web-fetch-fallback: true", tc.capability, 1)
			require.NoError(t, os.WriteFile(filepath.Join(dir, "engine.md"), []byte(definition), 0600))
			path := filepath.Join(dir, "workflow.md")
			require.NoError(t, os.WriteFile(path, fmt.Appendf(nil, `---
on: workflow_dispatch
imports:
  - engine.md
engine:
  id: custom-fetch
  model: copilot/auto
tools:
  github: false
%s
---
Fetch a page.
`, tc.tool), 0600))
			require.NoError(t, NewCompiler(WithSkipValidation(true)).CompileWorkflow(path))
			lock, err := os.ReadFile(filepath.Join(dir, "workflow.lock.yml"))
			require.NoError(t, err)
			assert.Equal(t, tc.want, strings.Contains(string(lock), "export GH_AW_WEB_FETCH_MCP=1"))
			assert.Equal(t, tc.want, strings.Contains(string(lock), `"name":"web-fetch","tools":["web_fetch"]`))
		})
	}
}

func TestWebFetchMCPDeclarativeEngineRequiresAdapter(t *testing.T) {
	_, err := NewBehaviorDefinedEngine(&EngineDefinition{
		ID: "custom-fetch",
		Behaviors: &EngineBehaviorDefinition{
			Capabilities: EngineCapabilitiesDefinition{WebFetchFallback: true},
		},
	})
	require.ErrorContains(t, err, "enables web-fetch-fallback without MCP support and a config-adapter")
}

func TestCopilotWebToolsCompilation(t *testing.T) {
	for _, strict := range []bool{false, true} {
		for _, sdk := range []bool{false, true} {
			for _, tool := range []string{"web-fetch", "web-search"} {
				for _, value := range []string{"", "{}"} {
					t.Run(fmt.Sprintf("strict=%t/sdk=%t/%s=%s", strict, sdk, tool, value), func(t *testing.T) {
						compiler := NewCompiler()
						data, err := compiler.ParseWorkflowString(fmt.Sprintf(`---
on: workflow_dispatch
strict: %t
engine:
  id: copilot
  copilot-sdk: %t
tools:
  %s: %s
---
Test Copilot web tool availability.
`, strict, sdk, tool, value), "copilot-web-tools.md")
						if tool == "web-fetch" {
							require.NoError(t, err)
							require.True(t, usesWebFetchMCP(data))
							return
						}

						require.ErrorContains(t, err, "tools."+tool)
						require.ErrorContains(t, err, "COPILOT_OFFLINE=true")
						require.Nil(t, data)
					})
				}
			}
		}
	}
	require.False(t, NewCopilotEngine().GetCapabilities().WebSearch)
}

func TestCopilotWebToolsImportedEngineConfig(t *testing.T) {
	for _, sdk := range []bool{false, true} {
		t.Run(fmt.Sprintf("sdk=%t", sdk), func(t *testing.T) {
			dir := t.TempDir()
			shared := fmt.Sprintf(`---
engine:
  id: copilot
  copilot-sdk: %t
tools:
  web-fetch:
---
Shared fetch configuration.
`, sdk)
			require.NoError(t, os.WriteFile(filepath.Join(dir, "shared.md"), []byte(shared), 0600))
			const markdown = `---
on: workflow_dispatch
imports:
  - shared.md
---
Use imported fetch configuration.
`
			path := filepath.Join(dir, "workflow.md")
			require.NoError(t, os.WriteFile(path, []byte(markdown), 0600))
			for _, parseFile := range []bool{false, true} {
				compiler := NewCompiler()
				var data *WorkflowData
				var err error
				if parseFile {
					data, err = compiler.ParseWorkflowFile(path)
				} else {
					data, err = compiler.ParseWorkflowString(markdown, path)
				}

				require.NoError(t, err)
				require.True(t, usesWebFetchMCP(data))
				require.Equal(t, sdk, data.EngineConfig.CopilotSDK)
				require.False(t, buildCopilotSDKToolConfig(data, nil).Capabilities.WebFetch)
			}
		})
	}
}

func TestClaudeCopilotWebToolsCompilation(t *testing.T) {
	for _, config := range []struct {
		name    string
		engine  string
		model   string
		wantErr bool
	}{
		{"github provider", "  model-provider: github\n", "", true},
		{"copilot model", "", "model: copilot/claude-haiku-4.5\n", true},
		{"anthropic provider", "  model-provider: anthropic\n", "", false},
		{"anthropic override", "  model-provider: anthropic\n", "model: copilot/claude-haiku-4.5\n", false},
		{"default provider", "", "", false},
	} {
		for _, strict := range []bool{false, true} {
			for _, tool := range []string{"web-fetch", "web-search"} {
				for _, value := range []string{"", "{}"} {
					t.Run(fmt.Sprintf("%s/strict=%t/%s=%s", config.name, strict, tool, value), func(t *testing.T) {
						compiler := NewCompiler()
						data, err := compiler.ParseWorkflowString(fmt.Sprintf(`---
on: workflow_dispatch
strict: %t
engine:
  id: claude
%s%stools:
  %s: %s
---
Test Claude web tool availability.
`, strict, config.engine, config.model, tool, value), "claude-web-tools.md")
						if config.wantErr && tool == "web-search" {
							require.ErrorContains(t, err, "tools."+tool)
							require.ErrorContains(t, err, "GitHub Copilot inference")
							require.ErrorContains(t, err, "COPILOT_OFFLINE=true")
							require.ErrorContains(t, err, "engine.model-provider: anthropic")
							server := "fetch"
							if tool == "web-search" {
								server = "search"
							}
							require.ErrorContains(t, err, "MCP "+server+" server")
							require.Nil(t, data)
						} else {
							require.NoError(t, err)
							require.NotNil(t, data)
							require.Equal(t, config.wantErr && tool == "web-fetch", usesWebFetchMCP(data))
						}
					})
				}
			}
		}
	}
}

func TestClaudeCopilotWebToolsImportedEngineConfig(t *testing.T) {
	for _, provider := range []string{"github", "anthropic"} {
		for _, tool := range []string{"web-fetch", "web-search"} {
			t.Run(provider+"/"+tool, func(t *testing.T) {
				dir := t.TempDir()
				shared := fmt.Sprintf("---\nengine:\n  id: claude\n  model-provider: %s\ntools:\n  %s:\n---\nShared web tool configuration.\n", provider, tool)
				require.NoError(t, os.WriteFile(filepath.Join(dir, "shared.md"), []byte(shared), 0600))
				const markdown = "---\non: workflow_dispatch\nimports:\n  - shared.md\n---\nUse imported web tool configuration.\n"
				path := filepath.Join(dir, "workflow.md")
				require.NoError(t, os.WriteFile(path, []byte(markdown), 0600))
				for _, parseFile := range []bool{false, true} {
					compiler := NewCompiler()
					var data *WorkflowData
					var err error
					if parseFile {
						data, err = compiler.ParseWorkflowFile(path)
					} else {
						data, err = compiler.ParseWorkflowString(markdown, path)
					}
					if provider == "github" && tool == "web-search" {
						require.ErrorContains(t, err, "tools."+tool)
						require.ErrorContains(t, err, "COPILOT_OFFLINE=true")
						require.Nil(t, data)
					} else {
						require.NoError(t, err)
						require.NotNil(t, data)
						require.Equal(t, provider == "github" && tool == "web-fetch", usesWebFetchMCP(data))
					}
				}
			})
		}
	}
}
