//go:build !integration

package workflow

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	yamlv3 "gopkg.in/yaml.v3"
)

func TestMaintenanceWorkflowJobMetadata(t *testing.T) {
	labelTriggers := true
	generated, err := buildMaintenanceWorkflowYAML(context.Background(), buildMaintenanceWorkflowYAMLOptions{
		cronSchedule: "37 0 * * *", scheduleDesc: "Daily", runsOnValue: "ubuntu-slim",
		actionMode: ActionModeDev, version: "dev", defaultBranch: "main",
		hasCacheMemory: true, compactionLedgers: []LedgerConfig{{Name: "events"}},
		maintenanceConfig: &MaintenanceConfig{LabelTriggers: &labelTriggers},
	})
	require.NoError(t, err)

	var doc struct {
		Permissions map[string]any `yaml:"permissions"`
		Concurrency struct {
			Group            string `yaml:"group"`
			CancelInProgress bool   `yaml:"cancel-in-progress"`
		} `yaml:"concurrency"`
		Jobs map[string]struct {
			Name string `yaml:"name"`
		} `yaml:"jobs"`
	}
	require.NoError(t, yamlv3.Unmarshal([]byte(generated), &doc))
	require.Empty(t, doc.Permissions)
	require.Equal(t, "${{ github.workflow }}-${{ github.repository }}", doc.Concurrency.Group)
	require.False(t, doc.Concurrency.CancelInProgress)
	require.Contains(t, generated, "# Jobs request only the permissions needed")
	require.Contains(t, generated, "# Replay can create or update any supported safe-output resource.")
	for _, job := range []string{
		"close-expired-discussions", "close-expired-issues", "close-expired-pull-requests",
		"cleanup-cache-memory", "run_operation", "update_pull_request_branches",
		"apply_safe_outputs", "create_labels", "activity_report", "forecast_report",
		"close_agentic_workflows_issues", "validate_workflows", "compile-workflows",
		"secret-validation", "ledger_compaction_plan", "ledger_compaction_apply",
		"label_disable_agentic_workflow", "label_apply_safe_outputs",
	} {
		require.NotEmpty(t, doc.Jobs[job].Name, "job %s needs a readable name", job)
	}
}

func TestMaintenanceWorkflowOptionalCacheCleanup(t *testing.T) {
	opts := buildMaintenanceWorkflowYAMLOptions{
		cronSchedule: "37 0 * * *", scheduleDesc: "Daily", runsOnValue: "ubuntu-slim",
		actionMode: ActionModeRelease, version: "v1.0.0",
	}
	generated, err := buildMaintenanceWorkflowYAML(context.Background(), opts)
	require.NoError(t, err)
	require.NotContains(t, generated, "\n  cleanup-cache-memory:")
	require.NotContains(t, generated, "- 'clean_cache_memories'")
	require.NotContains(t, generated, "issues, clean_cache_memories, update_pull_request_branches")

	opts.hasCacheMemory = true
	generated, err = buildMaintenanceWorkflowYAML(context.Background(), opts)
	require.NoError(t, err)
	require.Contains(t, generated, "\n  cleanup-cache-memory:")
	require.Contains(t, generated, "- 'clean_cache_memories'")
}

func TestMaintenanceWorkflowDisabledManualJobs(t *testing.T) {
	manualJobs := []string{
		"run_operation", "cleanup-cache-memory", "update_pull_request_branches",
		"apply_safe_outputs", "create_labels", "activity_report",
		"forecast_report", "close_agentic_workflows_issues", "validate_workflows",
	}
	for _, disabled := range manualJobs {
		t.Run(disabled, func(t *testing.T) {
			opts := buildMaintenanceWorkflowYAMLOptions{
				cronSchedule: "37 0 * * *", scheduleDesc: "Daily", runsOnValue: "ubuntu-slim",
				actionMode: ActionModeRelease, version: "v1.0.0", hasCacheMemory: true,
				maintenanceConfig: &MaintenanceConfig{DisabledJobs: []string{disabled}},
			}
			generated, err := buildMaintenanceWorkflowYAML(context.Background(), opts)
			require.NoError(t, err)
			require.NotContains(t, generated, "\n  "+disabled+":")
			for _, other := range manualJobs {
				if other != disabled {
					require.Contains(t, generated, "\n  "+other+":", "%s must remain enabled", other)
				}
			}
			if disabled == "run_operation" {
				require.Contains(t, generated, "value: ${{ inputs.operation }}")
				require.NotContains(t, generated, "jobs.run_operation.outputs.operation")
			}
		})
	}
}
