//go:build !integration

package workflow

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenerateMaintenanceCron(t *testing.T) {
	tests := []struct {
		name           string
		minExpiresDays int
		expectedCron   string
		expectedDesc   string
	}{
		{
			name:           "1 day or less - every 2 hours",
			minExpiresDays: 1,
			expectedCron:   "37 */2 * * *",
			expectedDesc:   "Every 2 hours",
		},
		{
			name:           "2 days - every 6 hours",
			minExpiresDays: 2,
			expectedCron:   "37 */6 * * *",
			expectedDesc:   "Every 6 hours",
		},
		{
			name:           "3 days - every 12 hours",
			minExpiresDays: 3,
			expectedCron:   "37 */12 * * *",
			expectedDesc:   "Every 12 hours",
		},
		{
			name:           "4 days - every 12 hours",
			minExpiresDays: 4,
			expectedCron:   "37 */12 * * *",
			expectedDesc:   "Every 12 hours",
		},
		{
			name:           "5 days - daily",
			minExpiresDays: 5,
			expectedCron:   "37 0 * * *",
			expectedDesc:   "Daily",
		},
		{
			name:           "7 days - daily",
			minExpiresDays: 7,
			expectedCron:   "37 0 * * *",
			expectedDesc:   "Daily",
		},
		{
			name:           "30 days - daily",
			minExpiresDays: 30,
			expectedCron:   "37 0 * * *",
			expectedDesc:   "Daily",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cron, desc := generateMaintenanceCron(tt.minExpiresDays)
			if cron != tt.expectedCron {
				t.Errorf("generateMaintenanceCron(%d) cron = %q, expected %q", tt.minExpiresDays, cron, tt.expectedCron)
			}
			if desc != tt.expectedDesc {
				t.Errorf("generateMaintenanceCron(%d) desc = %q, expected %q", tt.minExpiresDays, desc, tt.expectedDesc)
			}
		})
	}
}

func TestGenerateMaintenanceWorkflow_WithExpires(t *testing.T) {
	tests := []struct {
		name                    string
		workflowDataList        []*WorkflowData
		expectWorkflowGenerated bool
		expectError             bool
	}{
		{
			name: "with expires in discussions - should generate workflow",
			workflowDataList: []*WorkflowData{
				{
					Name: "test-workflow",
					SafeOutputs: &SafeOutputsConfig{
						CreateDiscussions: &CreateDiscussionsConfig{
							Expires: 168, // 7 days
						},
					},
				},
			},
			expectWorkflowGenerated: true,
			expectError:             false,
		},
		{
			name: "with expires in issues - should generate workflow",
			workflowDataList: []*WorkflowData{
				{
					Name: "test-workflow-issues",
					SafeOutputs: &SafeOutputsConfig{
						CreateIssues: &CreateIssuesConfig{
							Expires: 48, // 2 days
						},
					},
				},
			},
			expectWorkflowGenerated: true,
			expectError:             false,
		},
		{
			name: "without expires field - should NOT generate workflow",
			workflowDataList: []*WorkflowData{
				{
					Name: "test-workflow",
					SafeOutputs: &SafeOutputsConfig{
						CreateDiscussions: &CreateDiscussionsConfig{},
					},
				},
			},
			expectWorkflowGenerated: false,
			expectError:             false,
		},
		{
			name: "with both discussions and issues expires - should generate workflow",
			workflowDataList: []*WorkflowData{
				{
					Name: "multi-expires-workflow",
					SafeOutputs: &SafeOutputsConfig{
						CreateDiscussions: &CreateDiscussionsConfig{
							Expires: 168,
						},
						CreateIssues: &CreateIssuesConfig{
							Expires: 48,
						},
					},
				},
			},
			expectWorkflowGenerated: true,
			expectError:             false,
		},
		{
			name: "with noop report-as-issue default - should generate workflow",
			workflowDataList: []*WorkflowData{
				{
					Name: "noop-default-workflow",
					SafeOutputs: &SafeOutputsConfig{
						NoOp: &NoOpConfig{},
					},
				},
			},
			expectWorkflowGenerated: true,
			expectError:             false,
		},
		{
			name: "with noop report-as-issue false - should NOT generate workflow",
			workflowDataList: []*WorkflowData{
				{
					Name: "noop-disabled-report-workflow",
					SafeOutputs: &SafeOutputsConfig{
						NoOp: &NoOpConfig{
							ReportAsIssue: strPtr("false"),
						},
					},
				},
			},
			expectWorkflowGenerated: false,
			expectError:             false,
		},
		{
			name: "with noop report-as-issue true - should generate workflow",
			workflowDataList: []*WorkflowData{
				{
					Name: "noop-explicit-report-workflow",
					SafeOutputs: &SafeOutputsConfig{
						NoOp: &NoOpConfig{
							ReportAsIssue: strPtr("true"),
						},
					},
				},
			},
			expectWorkflowGenerated: true,
			expectError:             false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary directory for the workflow
			tmpDir := t.TempDir()

			// Call GenerateMaintenanceWorkflow
			err := GenerateMaintenanceWorkflow(context.Background(), GenerateMaintenanceWorkflowOptions{
				WorkflowDataList: tt.workflowDataList,
				WorkflowDir:      tmpDir,
				Version:          "v1.0.0",
				ActionMode:       ActionModeDev,
				ActionTag:        "",
				RepoConfig:       nil,
				RepoSlug:         "",
			})

			// Check error expectation
			if tt.expectError && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			// Check if workflow file was generated
			maintenanceFile := filepath.Join(tmpDir, "agentics-maintenance.yml")
			_, statErr := os.Stat(maintenanceFile)
			workflowExists := statErr == nil

			if tt.expectWorkflowGenerated && !workflowExists {
				t.Errorf("Expected maintenance workflow to be generated but it was not")
			}
			if !tt.expectWorkflowGenerated && workflowExists {
				t.Errorf("Expected maintenance workflow NOT to be generated but it was")
			}
		})
	}
}

func TestScanWorkflowsForExpires_TriggerReason(t *testing.T) {
	t.Run("no triggers", func(t *testing.T) {
		hasExpires, minExpires, triggerReason := scanWorkflowsForExpires([]*WorkflowData{
			{
				Name:        "no-safe-outputs",
				SafeOutputs: nil,
			},
		}, nil)
		require.False(t, hasExpires)
		require.Equal(t, 0, minExpires)
		require.Empty(t, triggerReason)
	})

	t.Run("captures first trigger reason", func(t *testing.T) {
		hasExpires, minExpires, triggerReason := scanWorkflowsForExpires([]*WorkflowData{
			{
				Name: "first-trigger",
				SafeOutputs: &SafeOutputsConfig{
					CreateIssues: &CreateIssuesConfig{
						Expires: 72,
					},
				},
			},
			{
				Name: "second-trigger",
				SafeOutputs: &SafeOutputsConfig{
					CreateDiscussions: &CreateDiscussionsConfig{
						Expires: 24,
					},
				},
			},
		}, nil)
		require.True(t, hasExpires)
		require.Equal(t, 24, minExpires)
		require.Contains(t, triggerReason, "first-trigger")
		require.Contains(t, triggerReason, "safe_outputs.create_issues.expires=72h")
		require.NotContains(t, triggerReason, "second-trigger")
	})

	t.Run("implicit noop does not trigger maintenance", func(t *testing.T) {
		compiler := NewCompiler()
		frontmatter := map[string]any{
			"safe-outputs": map[string]any{
				"create-issue": map[string]any{},
			},
		}
		safeOutputs := compiler.extractSafeOutputsConfig(frontmatter)
		require.NotNil(t, safeOutputs)
		require.NotNil(t, safeOutputs.NoOp)
		require.True(t, safeOutputs.NoOp.Implicit, "noop should be implicit when not authored")
		require.NotNil(t, safeOutputs.NoOp.Max)
		require.Equal(t, "2", *safeOutputs.NoOp.Max, "implicit noop should allow a probe and a final call")
		require.NotNil(t, safeOutputs.NoOp.ReportAsIssue)
		require.Equal(t, "false", *safeOutputs.NoOp.ReportAsIssue, "implicit noop must not create issues without a maintenance workflow to expire them")

		hasExpires, minExpires, triggerReason := scanWorkflowsForExpires([]*WorkflowData{
			{
				Name:        "implicit-noop",
				SafeOutputs: safeOutputs,
			},
		}, nil)
		require.False(t, hasExpires)
		require.Equal(t, 0, minExpires)
		require.Empty(t, triggerReason)
	})

	t.Run("explicit noop triggers maintenance", func(t *testing.T) {
		compiler := NewCompiler()
		frontmatter := map[string]any{
			"safe-outputs": map[string]any{
				"create-issue": map[string]any{},
				"noop":         map[string]any{},
			},
		}
		safeOutputs := compiler.extractSafeOutputsConfig(frontmatter)
		require.NotNil(t, safeOutputs)
		require.NotNil(t, safeOutputs.NoOp)
		require.False(t, safeOutputs.NoOp.Implicit, "noop should not be implicit when explicitly authored")

		hasExpires, minExpires, triggerReason := scanWorkflowsForExpires([]*WorkflowData{
			{
				Name:        "explicit-noop",
				SafeOutputs: safeOutputs,
			},
		}, nil)
		require.True(t, hasExpires)
		require.Equal(t, defaultNoOpIssueExpirationHours, minExpires)
		require.Contains(t, triggerReason, "explicit-noop")
		require.Contains(t, triggerReason, "no-op issue reporting")
	})

	t.Run("implicit action-failure default does not trigger maintenance", func(t *testing.T) {
		hasExpires, minExpires, triggerReason := scanWorkflowsForExpires([]*WorkflowData{
			{
				Name:        "default-action-failure",
				SafeOutputs: &SafeOutputsConfig{},
			},
		}, nil)
		require.False(t, hasExpires, "implicit 168h action-failure default must not force maintenance generation")
		require.Equal(t, 0, minExpires)
		require.Empty(t, triggerReason)
	})

	t.Run("explicit action-failure expiry triggers maintenance", func(t *testing.T) {
		repoConfig := &RepoConfig{
			Maintenance: &MaintenanceConfig{
				ActionFailureIssueExpires:         72,
				ActionFailureIssueExpiresExplicit: true,
			},
		}
		hasExpires, minExpires, triggerReason := scanWorkflowsForExpires([]*WorkflowData{
			{
				Name:        "explicit-action-failure",
				SafeOutputs: &SafeOutputsConfig{},
			},
		}, repoConfig)
		require.True(t, hasExpires)
		require.Equal(t, 72, minExpires)
		require.Contains(t, triggerReason, "action_failure_issue_expires=72h")
	})

	t.Run("explicit action-failure expiry coexists with shorter safe-output expiry", func(t *testing.T) {
		repoConfig := &RepoConfig{
			Maintenance: &MaintenanceConfig{
				ActionFailureIssueExpires:         72,
				ActionFailureIssueExpiresExplicit: true,
			},
		}
		hasExpires, minExpires, triggerReason := scanWorkflowsForExpires([]*WorkflowData{
			{
				Name: "shorter-issue-expiry",
				SafeOutputs: &SafeOutputsConfig{
					CreateIssues: &CreateIssuesConfig{
						Expires: 24,
					},
				},
			},
		}, repoConfig)
		require.True(t, hasExpires)
		require.Equal(t, 24, minExpires, "shorter safe-output expiry should win the minimum calculation")
		require.Contains(t, triggerReason, "shorter-issue-expiry")
	})

	t.Run("explicit action-failure expiry coexists with longer safe-output expiry", func(t *testing.T) {
		repoConfig := &RepoConfig{
			Maintenance: &MaintenanceConfig{
				ActionFailureIssueExpires:         12,
				ActionFailureIssueExpiresExplicit: true,
			},
		}
		hasExpires, minExpires, _ := scanWorkflowsForExpires([]*WorkflowData{
			{
				Name: "longer-issue-expiry",
				SafeOutputs: &SafeOutputsConfig{
					CreateIssues: &CreateIssuesConfig{
						Expires: 96,
					},
				},
			},
		}, repoConfig)
		require.True(t, hasExpires)
		require.Equal(t, 12, minExpires, "explicit action-failure expiry should win the minimum calculation when shorter")
	})

	t.Run("explicit action-failure expiry with no workflows enabling report-as-issue does not trigger", func(t *testing.T) {
		repoConfig := &RepoConfig{
			Maintenance: &MaintenanceConfig{
				ActionFailureIssueExpires:         72,
				ActionFailureIssueExpiresExplicit: true,
			},
		}
		disabled := TemplatableBool("false")
		hasExpires, minExpires, triggerReason := scanWorkflowsForExpires([]*WorkflowData{
			{
				Name: "no-failure-reporting",
				SafeOutputs: &SafeOutputsConfig{
					ReportFailureAsIssue: &disabled,
				},
			},
		}, repoConfig)
		require.False(t, hasExpires)
		require.Equal(t, 0, minExpires)
		require.Empty(t, triggerReason)
	})

	t.Run("explicit action-failure expiry with no workflows does not trigger", func(t *testing.T) {
		repoConfig := &RepoConfig{
			Maintenance: &MaintenanceConfig{
				ActionFailureIssueExpires:         72,
				ActionFailureIssueExpiresExplicit: true,
			},
		}
		hasExpires, minExpires, triggerReason := scanWorkflowsForExpires(nil, repoConfig)
		require.False(t, hasExpires)
		require.Equal(t, 0, minExpires)
		require.Empty(t, triggerReason)
	})
}

func TestGenerateMaintenanceWorkflow_DisablesImplicitActionFailureExpiryMarker(t *testing.T) {
	tmpDir := t.TempDir()

	// Simulate a workflow that was already compiled by CompileWorkflow with the
	// implicit 168-hour action-failure expiry default (buildAgentFailureCoreVars
	// always writes some value before the maintenance decision is known).
	lockFile := filepath.Join(tmpDir, "sample.lock.yml")
	lockContent := "name: Sample\njobs:\n  conclusion:\n    steps:\n      - env:\n          GH_AW_ACTION_FAILURE_ISSUE_EXPIRES_HOURS: \"168\"\n"
	require.NoError(t, os.WriteFile(lockFile, []byte(lockContent), 0o644))

	err := GenerateMaintenanceWorkflow(context.Background(), GenerateMaintenanceWorkflowOptions{
		WorkflowDataList: []*WorkflowData{
			{
				Name:        "Sample",
				WorkflowID:  "sample",
				SafeOutputs: &SafeOutputsConfig{},
			},
		},
		WorkflowDir: tmpDir,
		Version:     "dev",
		ActionMode:  ActionModeDev,
	})
	require.NoError(t, err)

	// No maintenance workflow should be generated for the implicit default alone.
	_, statErr := os.Stat(filepath.Join(tmpDir, "agentics-maintenance.yml"))
	require.True(t, os.IsNotExist(statErr), "agentics-maintenance.yml should not be generated for the implicit default")

	patched, err := os.ReadFile(lockFile)
	require.NoError(t, err)
	require.Contains(t, string(patched), `GH_AW_ACTION_FAILURE_ISSUE_EXPIRES_HOURS: "0"`, "implicit default marker should be disabled when no maintenance workflow will enforce it")
	require.NotContains(t, string(patched), `GH_AW_ACTION_FAILURE_ISSUE_EXPIRES_HOURS: "168"`)
}

func TestGenerateMaintenanceWorkflow_PreservesActionFailureExpiryMarkerWhenAnotherSourceTriggersMaintenance(t *testing.T) {
	tmpDir := t.TempDir()

	lockFile := filepath.Join(tmpDir, "sample.lock.yml")
	lockContent := "name: Sample\njobs:\n  conclusion:\n    steps:\n      - env:\n          GH_AW_ACTION_FAILURE_ISSUE_EXPIRES_HOURS: \"168\"\n"
	require.NoError(t, os.WriteFile(lockFile, []byte(lockContent), 0o644))

	err := GenerateMaintenanceWorkflow(context.Background(), GenerateMaintenanceWorkflowOptions{
		WorkflowDataList: []*WorkflowData{
			{
				Name:       "Sample",
				WorkflowID: "sample",
				SafeOutputs: &SafeOutputsConfig{
					CreateIssues: &CreateIssuesConfig{
						Expires: 48,
					},
				},
			},
		},
		WorkflowDir: tmpDir,
		Version:     "dev",
		ActionMode:  ActionModeDev,
	})
	require.NoError(t, err)

	// Maintenance workflow should be generated because of the create_issues expiry.
	_, statErr := os.Stat(filepath.Join(tmpDir, "agentics-maintenance.yml"))
	require.NoError(t, statErr, "agentics-maintenance.yml should be generated when another expiry source exists")

	// The implicit action-failure marker should be preserved (not disabled),
	// since the generic close-expired-issues sweeper can now enforce it.
	preserved, err := os.ReadFile(lockFile)
	require.NoError(t, err)
	require.Contains(t, string(preserved), `GH_AW_ACTION_FAILURE_ISSUE_EXPIRES_HOURS: "168"`)
}

func TestGenerateMaintenanceWorkflow_DisablesImplicitActionFailureExpiryMarkerWhenMaintenanceDisabled(t *testing.T) {
	tmpDir := t.TempDir()

	lockFile := filepath.Join(tmpDir, "sample.lock.yml")
	lockContent := "name: Sample\njobs:\n  conclusion:\n    steps:\n      - env:\n          GH_AW_ACTION_FAILURE_ISSUE_EXPIRES_HOURS: \"168\"\n"
	require.NoError(t, os.WriteFile(lockFile, []byte(lockContent), 0o644))

	err := GenerateMaintenanceWorkflow(context.Background(), GenerateMaintenanceWorkflowOptions{
		WorkflowDataList: []*WorkflowData{
			{
				Name:        "Sample",
				WorkflowID:  "sample",
				SafeOutputs: &SafeOutputsConfig{},
			},
		},
		WorkflowDir: tmpDir,
		RepoConfig: &RepoConfig{
			MaintenanceDisabled: true,
		},
		Version:    "dev",
		ActionMode: ActionModeDev,
	})
	require.NoError(t, err)

	_, statErr := os.Stat(filepath.Join(tmpDir, "agentics-maintenance.yml"))
	require.True(t, os.IsNotExist(statErr), "agentics-maintenance.yml should not be generated when maintenance is disabled")

	patched, err := os.ReadFile(lockFile)
	require.NoError(t, err)
	require.Contains(t, string(patched), `GH_AW_ACTION_FAILURE_ISSUE_EXPIRES_HOURS: "0"`, "implicit default marker should be disabled when maintenance is disabled")
	require.NotContains(t, string(patched), `GH_AW_ACTION_FAILURE_ISSUE_EXPIRES_HOURS: "168"`)
}
