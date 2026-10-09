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

func TestMaintenanceWorkQueueCompactionUsesSingleJob(t *testing.T) {
	opts := buildMaintenanceWorkflowYAMLOptions{runsOnValue: "ubuntu-latest"}
	jobs := buildMaintenanceWorkQueueCompactionJobs(opts, "./actions/setup")
	assert.Contains(t, jobs, "  work_queue_compaction:")
	assert.Contains(t, jobs, "contents: write")
	assert.Contains(t, jobs, "work_queue_compaction_plan.cjs")
	assert.Contains(t, jobs, "work_queue_compaction_apply.cjs")
	assert.Equal(t, 1, strings.Count(jobs, "script: |"))
	assert.NotContains(t, jobs, "work_queue_compaction_plan:")
	assert.NotContains(t, jobs, "work_queue_compaction_apply:")
	assert.NotContains(t, jobs, "work-queue-compaction-plan")
	assert.NotContains(t, jobs, "actions/upload-artifact")
	assert.NotContains(t, jobs, "actions/download-artifact")
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
	assert.NotEmpty(t, jobs["work_queue_compaction"])
	assert.Empty(t, jobs["work_queue_compaction_plan"])
	assert.Empty(t, jobs["work_queue_compaction_apply"])
	assert.Empty(t, jobs["ledger_compaction_plan"])
}
