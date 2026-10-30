package workflow

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConclusionFailureAnnotationAppPermissions(t *testing.T) {
	for _, steering := range []bool{false, true} {
		for _, enabled := range []string{"false", "true"} {
			t.Run("steering_"+strconv.FormatBool(steering)+"_reporting_"+enabled, func(t *testing.T) {
				reporting := TemplatableBool(enabled)
				data := &WorkflowData{
					SafeOutputs: &SafeOutputsConfig{
						Steer:                steering,
						ReportFailureAsIssue: &reporting,
						GitHubApp: &GitHubAppConfig{
							AppID:      "${{ vars.APP_ID }}",
							PrivateKey: "${{ secrets.APP_PRIVATE_KEY }}",
						},
					},
				}
				compiler := NewCompiler()
				steps := compiler.buildConclusionSetupSteps(data)
				if steering {
					tokenSteps, token := compiler.buildConclusionSteeringIssueTokenSteps(data)
					require.Empty(t, tokenSteps)
					require.Contains(t, token, "steps.safe-outputs-app-token.outputs.token")
				}
				require.Equal(t, enabled == "true", strings.Contains(strings.Join(steps, ""), "permission-checks: read"))
			})
		}
	}
}

func TestConclusionJobDurationPermissions(t *testing.T) {
	for _, failedJobsEnabled := range []string{"false", "true"} {
		for _, failureIssueEnabled := range []string{"false", "true"} {
			t.Run("failed_jobs_"+failedJobsEnabled+"_failure_issue_"+failureIssueEnabled, func(t *testing.T) {
				failedJobs := TemplatableBool(failedJobsEnabled)
				failureIssue := TemplatableBool(failureIssueEnabled)
				data := &WorkflowData{
					SafeOutputs: &SafeOutputsConfig{
						ReportFailedJobs:     &failedJobs,
						ReportFailureAsIssue: &failureIssue,
					},
				}

				perms := computeConclusionJobPermissions(data)
				actions, ok := perms.Get(PermissionActions)
				require.True(t, ok)
				require.Equal(t, PermissionRead, actions, "duration collection is independent of failure reporting")
				_, pullRequests := perms.Get(PermissionPullRequests)
				require.Equal(t, failureIssueEnabled == "true", pullRequests, "duration collection must not broaden PR access")
				checks, hasChecks := perms.Get(PermissionChecks)
				require.Equal(t, failureIssueEnabled == "true", hasChecks, "timeout annotation access is only needed for agent failure reporting")
				if hasChecks {
					require.Equal(t, PermissionRead, checks)
				}

				data.SafeOutputs.DispatchWorkflow = &DispatchWorkflowConfig{}
				actions, ok = computeConclusionJobPermissions(data).Get(PermissionActions)
				require.True(t, ok)
				require.Equal(t, PermissionWrite, actions, "duration collection must preserve existing Actions write access")
				data.SafeOutputs.CreateCheckRun = &CreateCheckRunConfig{}
				checks, ok = computeConclusionJobPermissions(data).Get(PermissionChecks)
				require.True(t, ok)
				require.Equal(t, PermissionWrite, checks, "timeout annotation access must preserve existing Checks write access")
			})
		}
	}
}

func TestConclusionJobOIDCIsLimitedToConfiguredOTLP(t *testing.T) {
	for _, worker := range []bool{false, true} {
		data := &WorkflowData{SafeOutputs: &SafeOutputsConfig{}}
		if worker {
			data.Tools = map[string]any{"work-queue": map[string]any{"worker": true}}
		}
		_, ok := computeConclusionJobPermissions(data).Get(PermissionIdToken)
		require.False(t, ok, "admission callback OIDC must not be granted to unrelated conclusion steps")

		data.RawFrontmatter = map[string]any{
			"observability": map[string]any{
				"otlp": map[string]any{
					"github-app": map[string]any{"audience": "https://otel.example.com"},
				},
			},
		}
		level, ok := computeConclusionJobPermissions(data).Get(PermissionIdToken)
		require.True(t, ok)
		require.Equal(t, PermissionWrite, level)
	}
}
