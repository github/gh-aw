//go:build integration

package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateTokenWiringIntegration(t *testing.T) {
	tests := []struct {
		name        string
		action      string
		expectWired bool
	}{
		{
			name:        "official action",
			action:      "actions/create-github-app-token@v3",
			expectWired: true,
		},
		{
			name:   "third-party action",
			action: "tibdex/github-app-token@v3",
		},
		{
			name:   "lookalike official action",
			action: "actions/create-github-app-token-fork@v3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "create-token-wiring-integration")
			workflowPath := filepath.Join(tmpDir, "create-token.md")
			workflow := fmt.Sprintf(`---
name: Create Token Wiring Integration
on: workflow_dispatch
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
strict: false
tools:
  github:
    mode: remote
    allowed: [list_issues]
pre-steps:
  - id: create-token
    uses: %s
    with:
      app-id: ${{ vars.APP_ID }}
      private-key: ${{ vars.APP_PRIVATE_KEY }}
      owner: ${{ github.repository_owner }}
      repositories: ${{ github.event.repository.name }}
---

Compile the workflow and verify create-token wiring.
`, tt.action)
			require.NoError(t, os.WriteFile(workflowPath, []byte(workflow), 0600))

			compiler := NewCompiler()
			require.NoError(t, compiler.CompileWorkflow(workflowPath))

			compiled, err := os.ReadFile(filepath.Join(tmpDir, "create-token.lock.yml"))
			require.NoError(t, err)
			agentJob := extractJobSection(string(compiled), "agent")
			require.NotEmpty(t, agentJob)

			if tt.expectWired {
				assert.Contains(t, agentJob, "id-token: write")
				assert.Contains(t, agentJob, "GITHUB_MCP_SERVER_TOKEN: ${{ steps.create-token.outputs.token || secrets.GH_AW_GITHUB_MCP_SERVER_TOKEN || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}")
			} else {
				assert.NotContains(t, agentJob, "id-token: write")
				assert.NotContains(t, agentJob, "steps.create-token.outputs.token")
				assert.Contains(t, agentJob, "GITHUB_MCP_SERVER_TOKEN: ${{ secrets.GH_AW_GITHUB_MCP_SERVER_TOKEN || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}")
			}
		})
	}
}
