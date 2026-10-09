package workflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMaintenanceWorkQueueCompactionJobsSeparatePlanAndApply(t *testing.T) {
	opts := buildMaintenanceWorkflowYAMLOptions{runsOnValue: "ubuntu-latest"}
	jobs := buildMaintenanceWorkQueueCompactionJobs(opts, "./actions/setup")
	plan := strings.Split(jobs, "  work_queue_compaction_apply:")[0]
	applyJob := strings.Split(jobs, "  work_queue_compaction_apply:")[1]

	assert.Contains(t, plan, "contents: read")
	assert.Contains(t, plan, "work_queue_compaction_plan.cjs")
	assert.Contains(t, plan, "steps.plan.outputs.plan_created == 'true'")
	assert.NotContains(t, plan, "work_queue_compaction_apply.cjs")
	assert.Contains(t, applyJob, "needs: work_queue_compaction_plan")
	assert.Contains(t, applyJob, "contents: write")
	assert.Contains(t, applyJob, "work_queue_compaction_apply.cjs")
	assert.NotContains(t, applyJob, "work_queue_compaction_plan.cjs")
}

func TestMaintenanceGeneratedForWorkQueueOnly(t *testing.T) {
	directory := t.TempDir()
	err := GenerateMaintenanceWorkflow(context.Background(), GenerateMaintenanceWorkflowOptions{
		WorkflowDataList: []*WorkflowData{{Name: "queue", WorkflowID: "queue", Tools: map[string]any{"work-queue": true}}},
		WorkflowDir:      directory,
		Version:          "v1.0.0",
		ActionMode:       ActionModeDev,
	})
	require.NoError(t, err)
	content, err := os.ReadFile(filepath.Join(directory, "agentics-maintenance.yml"))
	require.NoError(t, err)
	jobs := parseMaintenanceJobs(t, string(content))
	assert.NotEmpty(t, jobs["work_queue_compaction_plan"])
	assert.NotEmpty(t, jobs["work_queue_compaction_apply"])
	assert.Empty(t, jobs["ledger_compaction_plan"])
}
