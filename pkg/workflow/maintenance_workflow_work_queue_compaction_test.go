package workflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

func TestMaintenanceWorkQueueCompactionUsesSingleJob(t *testing.T) {
	opts := buildMaintenanceWorkflowYAMLOptions{runsOnValue: "ubuntu-latest", workQueuePolicy: `{"version":4,"pools":{}}`}
	jobs := buildMaintenanceWorkQueueCompactionJobs(opts, "./actions/setup")
	assert.Contains(t, jobs, "  work_queue_compaction:")
	assert.Contains(t, jobs, "name: Reconcile and checkpoint work queue")
	assert.Contains(t, jobs, "contents: write")
	assert.Contains(t, jobs, "actions: read")
	assert.Contains(t, jobs, "issues: write")
	assert.Contains(t, jobs, "setupGlobals(core, github, context, exec, io, getOctokit);")
	assert.Contains(t, jobs, "const { maintenance } = require('${{ runner.temp }}/gh-aw/actions/reconcile_work_queue_run.cjs');")
	assert.Contains(t, jobs, "await maintenance({ projectIssues: true });")
	assert.Contains(t, jobs, `GH_AW_WORK_QUEUE_POLICY: "{\"version\":4,\"pools\":{}}"`)
	assert.Equal(t, 1, strings.Count(jobs, "script: |"))
	assert.Less(t, strings.Index(jobs, "GH_AW_WORK_QUEUE_POLICY"), strings.Index(jobs, "script: |"))
	assert.NotContains(t, jobs, "work_queue_compaction_plan")
	assert.NotContains(t, jobs, "work_queue_compaction_apply")
	assert.NotContains(t, jobs, "GH_AW_WORK_QUEUE_COMPACTION_PLAN_FILE")
	assert.NotContains(t, jobs, "core.setFailed(")
	assert.NotContains(t, jobs, "actions/upload-artifact")
	assert.NotContains(t, jobs, "actions/download-artifact")

	withoutPolicy := buildMaintenanceWorkQueueCompactionJobs(buildMaintenanceWorkflowYAMLOptions{runsOnValue: "ubuntu-latest"}, "./actions/setup")
	assert.NotContains(t, withoutPolicy, "GH_AW_WORK_QUEUE_POLICY")
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
	var document struct {
		Jobs map[string]any `yaml:"jobs"`
	}
	require.NoError(t, yaml.Unmarshal(content, &document))
	jobs := document.Jobs
	assert.NotEmpty(t, jobs["work_queue_compaction"])
	assert.Empty(t, jobs["work_queue_compaction_plan"])
	assert.Empty(t, jobs["work_queue_compaction_apply"])
	assert.Empty(t, jobs["ledger_compaction_plan"])
}

func TestMaintenanceWorkQueueClosureIsHandledElsewhere(t *testing.T) {
	opts := buildMaintenanceWorkflowYAMLOptions{runsOnValue: "ubuntu-latest", hasWorkQueue: true}
	trigger := buildMaintenanceWorkflowTriggerYAML(opts, false, false, "", "")
	assert.NotContains(t, trigger, "issues:")
	assert.Contains(t, buildMaintenanceWorkflowTriggerYAML(opts, true, false, "", ""), "types: [labeled]")
	assert.NotContains(t, buildMaintenanceWorkflowTriggerYAML(buildMaintenanceWorkflowYAMLOptions{}, false, false, "", ""), "issues:")
}
