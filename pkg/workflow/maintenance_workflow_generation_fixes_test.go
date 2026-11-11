//go:build !integration

package workflow

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	yamlv3 "gopkg.in/yaml.v3"
)

func TestMaintenanceWorkflowJobMetadata(t *testing.T) {
	labelTriggers := true
	generated, err := buildMaintenanceWorkflowYAML(context.Background(), buildMaintenanceWorkflowYAMLOptions{
		cronSchedule: "37 0 * * *", scheduleDesc: "Daily", runsOnValue: "ubuntu-slim",
		actionMode: ActionModeDev, version: "dev", defaultBranch: "main",
		hasCacheMemory: true, hasSteering: true, compactionLedgers: []LedgerConfig{{Name: "events"}},
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
	lines := strings.Split(generated, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == "permissions:" || strings.TrimSpace(line) == "permissions: {}" {
			require.Positive(t, i)
			require.True(t, strings.HasPrefix(strings.TrimSpace(lines[i-1]), "#"), "permissions on line %d needs an adjacent explanation", i+1)
		}
	}
	for _, job := range []string{
		"close-expired-discussions", "close-expired-issues", "close-expired-pull-requests",
		"cleanup-stale-steering-issues", "cleanup-cache-memory", "run_operation", "update_pull_request_branches",
		"apply_safe_outputs", "create_labels", "activity_report", "forecast_report",
		"close_agentic_workflows_issues", "validate_workflows", "compile-workflows",
		"secret-validation", "ledger_compaction_plan", "ledger_compaction_apply",
		"label_disable_agentic_workflow", "label_apply_safe_outputs",
	} {
		require.NotEmpty(t, doc.Jobs[job].Name, "job %s needs a readable name", job)
	}
	steeringCleanup := generated[strings.Index(generated, "\n  cleanup-stale-steering-issues:"):]
	require.Contains(t, steeringCleanup, "github.event_name == 'schedule'")
	require.Contains(t, steeringCleanup, "actions: read")
	require.Contains(t, steeringCleanup, "issues: write")
	require.Contains(t, steeringCleanup, "cleanup_stale_steering_issues.cjs")
}

func TestMaintenanceWorkflowQuotesCompileGitHubToken(t *testing.T) {
	token := "${{ secrets.MAINTENANCE_TOKEN }}: 'quoted' # comment \\\n  injected: true"
	generated, err := buildMaintenanceWorkflowYAML(context.Background(), buildMaintenanceWorkflowYAMLOptions{
		cronSchedule: "37 0 * * *", scheduleDesc: "Daily", runsOnValue: "ubuntu-slim",
		actionMode: ActionModeDev, version: "dev", defaultBranch: "main",
		compileGitHubToken: token,
	})
	require.NoError(t, err)

	var doc struct {
		Jobs map[string]struct {
			Steps []struct {
				Env map[string]string `yaml:"env"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	require.NoError(t, yamlv3.Unmarshal([]byte(generated), &doc))
	var got string
	for _, step := range doc.Jobs["compile-workflows"].Steps {
		if value, ok := step.Env["GH_AW_MAINTENANCE_GITHUB_TOKEN"]; ok {
			got = value
			break
		}
	}
	require.Equal(t, token, got)
}

func TestMaintenanceWorkflowDisabledManualJobsAcceptedByConfig(t *testing.T) {
	for _, job := range []string{
		"run_operation", "cleanup-cache-memory", "update_pull_request_branches",
		"apply_safe_outputs", "create_labels", "activity_report",
		"forecast_report", "close_agentic_workflows_issues", "validate_workflows",
	} {
		t.Run(job, func(t *testing.T) {
			config := &MaintenanceConfig{DisabledJobs: []string{job}}
			require.NoError(t, validateRepoConfigValues(&RepoConfig{Maintenance: config}))
			require.True(t, config.IsJobDisabled(job))
		})
	}
	require.ErrorContains(t, validateRepoConfigValues(&RepoConfig{
		Maintenance: &MaintenanceConfig{DisabledJobs: []string{"validate"}},
	}), `unrecognized maintenance.disabled_jobs entry "validate"`)
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

func TestMaintenanceOperationNames(t *testing.T) {
	tests := []struct {
		name           string
		hasCacheMemory bool
		hasLedgers     bool
	}{
		{name: "default"},
		{name: "cache memory", hasCacheMemory: true},
		{name: "ledgers", hasLedgers: true},
		{name: "cache memory and ledgers", hasCacheMemory: true, hasLedgers: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := []string{
				"disable",
				"enable",
				"update",
				"upgrade",
				"safe_outputs",
				"create_labels",
				"activity_report",
				"close_agentic_workflows_issues",
			}
			if tt.hasCacheMemory {
				want = append(want, "clean_cache_memories")
			}
			want = append(want, "update_pull_request_branches", "validate", "forecast")
			if tt.hasLedgers {
				want = append(want, maintenanceCompactLedgerOperation)
			}

			operations := maintenanceOperationNames(tt.hasCacheMemory, tt.hasLedgers)
			require.Equal(t, want, operations)

			dispatchOperations := append([]string{maintenanceNoOperationValue}, operations...)
			dispatchYAML := buildMaintenanceDispatchInputsYAML(tt.hasLedgers, tt.hasCacheMemory)
			require.Contains(t, dispatchYAML, "options:\n          - '"+strings.Join(dispatchOperations, "'\n          - '")+"'\n")

			workflowCallYAML := buildMaintenanceWorkflowCallYAML("", "", tt.hasLedgers, tt.hasCacheMemory, &MaintenanceConfig{})
			require.Contains(t, workflowCallYAML, "description: 'Optional maintenance operation to run ("+strings.Join(operations, ", ")+")'")
		})
	}
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
				require.Contains(t, generated, "inputs.operation == 'safe_outputs'")
				require.NotContains(t, generated, "value: ${{ inputs.operation }}")
				require.NotContains(t, generated, "jobs.run_operation.outputs.operation")
			}
		})
	}
}

func TestMaintenanceDisabledOperationIsNotReportedComplete(t *testing.T) {
	opts := buildMaintenanceWorkflowYAMLOptions{
		cronSchedule: "37 0 * * *", scheduleDesc: "Daily", runsOnValue: "ubuntu-slim",
		actionMode: ActionModeRelease, version: "v1.0.0",
		maintenanceConfig: &MaintenanceConfig{DisabledJobs: []string{
			"run_operation", "apply_safe_outputs", "validate_workflows",
		}},
	}
	generated, err := buildMaintenanceWorkflowYAML(context.Background(), opts)
	require.NoError(t, err)
	require.NotContains(t, generated, "jobs.run_operation.outputs.operation")
	require.NotContains(t, generated, "inputs.operation == 'safe_outputs'")
	require.NotContains(t, generated, "inputs.operation == 'validate'")
	require.Contains(t, generated, "inputs.operation == 'create_labels'")
}

func TestMaintenanceSecretValidationUsesLatestNodeAction(t *testing.T) {
	generated := buildMaintenanceSecretValidationJob(buildMaintenanceWorkflowYAMLOptions{
		runsOnValue: "ubuntu-latest",
	}, "./actions/setup")

	require.Contains(t, generated, "uses: "+getActionPin("actions/setup-node"))
	require.NotContains(t, generated, "39370e3970a6d050c480ffad4ff0ed4d3fdee5af")
}
