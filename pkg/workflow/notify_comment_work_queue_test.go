//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConclusionWorkQueueSummary(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(map[bool]string{true: "enabled", false: "disabled"}[enabled], func(t *testing.T) {
			data := &WorkflowData{
				Name:        "Work queue worker",
				Tools:       map[string]any{"work-queue": enabled},
				SafeOutputs: &SafeOutputsConfig{},
			}
			compiler := NewCompiler()
			job, err := compiler.buildConclusionJob(data, "agent", []string{"safe_outputs"})
			require.NoError(t, err)
			require.NotNil(t, job)
			steps := strings.Join(job.Steps, "")
			if !enabled {
				require.NotContains(t, steps, "dispatch_work_coordinator_summary.cjs")
				require.NotContains(t, steps, "Download activation artifact for work queue summary")
				return
			}
			require.Contains(t, job.Permissions, "contents: read")
			require.Contains(t, job.Needs, "safe_outputs")
			require.Contains(t, steps, "Download activation artifact for work queue summary\n        if: always()")
			require.Contains(t, steps, "Summarize work queue activity\n        if: always()")
			require.Contains(t, steps, "Summarize work queue activity\n        if: always()\n        continue-on-error: true")
			require.Contains(t, steps, "dispatch_work_coordinator_summary.cjs")
			require.Contains(t, steps, "await main({ core, githubClient: github, context });")
			require.Less(t, strings.Index(steps, "Download activation artifact for work queue summary"), strings.Index(steps, "Summarize work queue activity"))
		})
	}
}

func TestConclusionWorkQueueSummaryPreservesWritePermissions(t *testing.T) {
	data := &WorkflowData{
		Tools: map[string]any{"work-queue": true},
		SafeOutputs: &SafeOutputsConfig{
			CreatePullRequests: &CreatePullRequestsConfig{},
		},
	}
	level, ok := computeConclusionJobPermissions(data).Get(PermissionContents)
	require.True(t, ok)
	require.Equal(t, PermissionWrite, level)
}
