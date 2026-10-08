//go:build !integration

package workflow

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReportedWorkflowToolPermissions(t *testing.T) {
	tests := []struct {
		workflow string
		grants   []string
	}{
		{"daily-graft-intelligence", []string{"shell(graft:*)", "shell(cat /tmp/gh-aw/agent/graft-changed-files.txt)", "shell(cat /tmp/gh-aw/agent/graft-recent-activity.txt)"}},
		{"daily-schema-audit-cursor", []string{"shell(find)", "shell(jq)", "shell(sort)", "shell(echo)"}},
		{"daily-regression-audit-kiro", []string{"shell(cd)", "shell(cut)", "shell(jq)"}},
		{"daily-compiler-quality", []string{"shell(cd)", "shell(test)", "shell([)", "shell(awk)"}},
		{"code-scanning-fixer", []string{"shell(cut)", "shell(github:*)", "shell(safeoutputs:*)"}},
		{"smoke-copilot-aoai-apikey", []string{"shell", "safeoutputs"}},
		{"smoke-workflow-call", []string{"Bash(git status:*)", "Bash(git branch:*)", "Bash(git remote:*)", "Bash(git log:*)"}},
		{"outcome-collector", nil},
		{"sighthound-security-scan", nil},
	}
	for _, tt := range tests {
		t.Run(tt.workflow, func(t *testing.T) {
			path := filepath.Join("..", "..", ".github", "workflows", tt.workflow+".md")
			compiler := NewCompiler()
			compiler.SetWorkflowIdentifier(tt.workflow)
			data, err := compiler.ParseWorkflowFile(path)
			require.NoError(t, err)
			switch ResolveEngineID(data) {
			case "claude":
				assert.False(t, data.BashDisabled)
				allowed := NewClaudeEngine().computeAllowedClaudeToolsString(
					data.Tools, data.SafeOutputs, data.CacheMemoryConfig, data.DriveMemoryConfig, data.MCPScripts, data.SandboxConfig)
				for _, grant := range tt.grants {
					assert.Contains(t, strings.Split(allowed, ","), grant)
				}
			case "copilot":
				assert.False(t, data.BashDisabled)
				args := NewCopilotEngine().computeCopilotToolArguments(data.Tools, data.SafeOutputs, data.MCPScripts, data)
				for _, grant := range tt.grants {
					assert.Contains(t, args, grant)
				}
				if tt.workflow == "daily-graft-intelligence" {
					assert.Contains(t, getMCPCLIExcludeFromAgentConfig(data), "graft")
					graft, ok := data.Tools["graft"].(map[string]any)
					require.True(t, ok)
					assert.Contains(t, graft["allowed"], "graft_check")
					assert.Contains(t, graft["allowed"], "graft_map")
				}
				if tt.workflow == "smoke-copilot-aoai-apikey" {
					assert.Contains(t, getMCPCLIExcludeFromAgentConfig(data), "safeoutputs")
					require.NotNil(t, data.SafeOutputs.UploadArtifact)
					require.NotNil(t, data.SafeOutputs.DispatchWorkflow)
				}
				if tt.workflow == "daily-compiler-quality" {
					assert.NotContains(t, getMCPCLIExcludeFromAgentConfig(data), "serena")
					assert.NotContains(t, getMCPCLIExcludeFromAgentConfig(data), "safeoutputs")
				}
			case "codex":
				assert.True(t, data.BashDisabled)
				require.True(t, IsMCPScriptsEnabled(data.MCPScripts))
				assert.NotContains(t, getMCPCLIExcludeFromAgentConfig(data), "mcpscripts")
				if tt.workflow == "outcome-collector" {
					require.Contains(t, data.MCPScripts.Tools, "read-outcome-summary")
					require.Contains(t, data.MCPScripts.Tools, "read-outcome-evaluations")
					assert.Equal(t, "cat /tmp/gh-aw/outcome-summary.json", data.MCPScripts.Tools["read-outcome-summary"].Run)
					assert.Equal(t, "cat /tmp/gh-aw/outcome-evaluations.jsonl", data.MCPScripts.Tools["read-outcome-evaluations"].Run)
					assert.Contains(t, data.MarkdownContent, "and stop; the evaluations file may not exist.")
					assert.Contains(t, data.MarkdownContent, "Call `mcp__mcpscripts__read-outcome-evaluations`")
					assert.Less(t, strings.Index(data.MarkdownContent, "If `total_outcomes` is 0"), strings.Index(data.MarkdownContent, "Call `mcp__mcpscripts__read-outcome-evaluations`"))
				} else {
					require.Contains(t, data.MCPScripts.Tools, "read-sighthound-findings")
					require.Contains(t, data.MCPScripts.Tools, "read-sighthound-summary")
					assert.Equal(t, "cat /tmp/gh-aw/agent/sighthound/actionable.json", data.MCPScripts.Tools["read-sighthound-findings"].Run)
					assert.Equal(t, "cat /tmp/gh-aw/agent/sighthound/summary.md", data.MCPScripts.Tools["read-sighthound-summary"].Run)
				}
			default:
				t.Fatalf("unexpected engine: %s", ResolveEngineID(data))
			}
		})
	}
}
