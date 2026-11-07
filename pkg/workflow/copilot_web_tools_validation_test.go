package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

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
						if sdk && tool == "web-fetch" {
							require.NoError(t, err)
							require.NotNil(t, data)
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

				if sdk {
					require.NoError(t, err)
					require.True(t, data.EngineConfig.CopilotSDK)
					require.True(t, buildCopilotSDKToolConfig(data, nil).Capabilities.WebFetch)
				} else {
					require.ErrorContains(t, err, "tools.web-fetch")
					require.ErrorContains(t, err, "COPILOT_OFFLINE=true")
				}
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
						if config.wantErr {
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
					if provider == "github" {
						require.ErrorContains(t, err, "tools."+tool)
						require.ErrorContains(t, err, "COPILOT_OFFLINE=true")
						require.Nil(t, data)
					} else {
						require.NoError(t, err)
						require.NotNil(t, data)
					}
				}
			})
		}
	}
}
