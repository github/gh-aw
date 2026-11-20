//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"os/exec"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildLogsCommandArgsPreservesWorkflowPositionals(t *testing.T) {
	t.Setenv("GITHUB_REPOSITORY", "github/gh-aw")
	for _, workflowName := range []string{"--repo=owner/private", "-nightly", "--", "Nightly Workflow", ""} {
		t.Run(workflowName, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			maxStorage := 2048
			args := logsArgs{
				WorkflowName:          workflowName,
				Count:                 41,
				StartDate:             "2026-01-01",
				EndDate:               "2026-02-01",
				Engine:                "copilot",
				Runtime:               "cloud-hypervisor",
				Firewall:              workflowName != "-nightly",
				NoFirewall:            workflowName == "-nightly",
				FilteredIntegrity:     true,
				Graders:               true,
				Delivery:              true,
				Adoption:              true,
				Branch:                "main",
				AfterRunID:            123,
				BeforeRunID:           456,
				IgnoreWorkflowRuns:    []int64{234, 345},
				Timeout:               10,
				MaxGitHubAPIRateLimit: -2000,
				MaxStorageMB:          &maxStorage,
				PruneOlderRuns:        true,
				Artifacts:             []string{"agent", "firewall"},
			}
			cmdArgs, count, timeout := buildLogsCommandArgs(ctx, args)
			require.Equal(t, "logs", cmdArgs[0])
			assert.Equal(t, args.Count, count)
			assert.Equal(t, args.Timeout, timeout)

			cmd := NewLogsCommand()
			require.NoError(t, cmd.ParseFlags(cmdArgs[1:]))
			if workflowName == "" {
				assert.Empty(t, cmd.Flags().Args())
				assert.NotContains(t, cmdArgs, "--")
			} else {
				assert.Equal(t, []string{workflowName}, cmd.Flags().Args())
				assert.Equal(t, []string{"--", workflowName}, cmdArgs[len(cmdArgs)-2:],
					"no options may follow the positional boundary")
			}
			assert.Equal(t, constants.TmpAwMcpLogsDir, getStringFlag(cmd, "output"))
			assert.True(t, getBoolFlag(cmd, "json"))
			assert.Equal(t, "github/gh-aw", getStringFlag(cmd, "repo"))
			assert.Equal(t, args.Count, getIntFlag(cmd, "count"))
			assert.Equal(t, args.StartDate, getStringFlag(cmd, "start-date"))
			assert.Equal(t, args.EndDate, getStringFlag(cmd, "end-date"))
			assert.Equal(t, args.Engine, getStringFlag(cmd, "engine"))
			assert.Equal(t, args.Runtime, getStringFlag(cmd, "runtime"))
			assert.Equal(t, args.Firewall, getBoolFlag(cmd, "firewall"))
			assert.Equal(t, args.NoFirewall, getBoolFlag(cmd, "no-firewall"))
			assert.True(t, getBoolFlag(cmd, "filtered-integrity"))
			assert.True(t, getBoolFlag(cmd, "graders"))
			assert.True(t, getBoolFlag(cmd, "delivery"))
			assert.True(t, getBoolFlag(cmd, "adoption"))
			assert.Equal(t, args.Branch, getStringFlag(cmd, "ref"))
			assert.Equal(t, args.AfterRunID, getInt64Flag(cmd, "after-run-id"))
			assert.Equal(t, args.BeforeRunID, getInt64Flag(cmd, "before-run-id"))
			assert.Equal(t, []string{"234", "345"}, getStringSliceFlag(cmd, "ignore-workflow-runs"))
			assert.Equal(t, args.Timeout, getIntFlag(cmd, "timeout"))
			assert.InDelta(t, 240, getIntFlag(cmd, "timeout-seconds"), 2)
			assert.Equal(t, args.MaxGitHubAPIRateLimit, getIntFlag(cmd, "max-github-api-rate-limit"))
			assert.Equal(t, maxStorage, getIntFlag(cmd, "max-storage"))
			assert.True(t, getBoolFlag(cmd, "prune-older-runs"))
			assert.Equal(t, []string{"agent", "firewall", "usage"}, getStringSliceFlag(cmd, "artifacts"))
		})
	}
}

func TestLogsToolFlagShapedMissingWorkflowReturnsEmptyResult(t *testing.T) {
	for _, workflowName := range []string{"--repo=owner/private", "-nonexistent-mcp-workflow", "--"} {
		t.Run(workflowName, func(t *testing.T) {
			execCalled := false
			handler := newLogsToolHandler(func(context.Context, ...string) *exec.Cmd {
				execCalled = true
				return nil
			}, "", false)
			result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, logsArgs{WorkflowName: workflowName})
			require.NoError(t, err, "workflow existence validation must retain its structured empty response")
			assert.False(t, execCalled, "missing workflows must not execute a subprocess")
			require.NotNil(t, result)
			require.Len(t, result.Content, 1)
			text, ok := result.Content[0].(*mcp.TextContent)
			require.True(t, ok)
			var data LogsData
			require.NoError(t, json.Unmarshal([]byte(text.Text), &data))
			assert.Empty(t, data.Runs)
			assert.Equal(t, "unavailable", data.Summary.AICCoverage)
			assert.Contains(t, data.Message, "workflow '"+workflowName+"' not found")
		})
	}
}
