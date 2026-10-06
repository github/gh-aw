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
