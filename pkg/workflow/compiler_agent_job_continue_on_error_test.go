//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeneratedAgentJobContinueOnError(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter string
		want        bool
		wantPresent bool
	}{
		{
			name:        "true",
			frontmatter: "jobs:\n  agent:\n    continue-on-error: true\n",
			want:        true,
			wantPresent: true,
		},
		{
			name:        "explicit false",
			frontmatter: "jobs:\n  agent:\n    continue-on-error: false\n",
			wantPresent: true,
		},
		{
			name: "omitted",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := compileContinueOnErrorTestWorkflow(t, tt.frontmatter)[string(constants.AgentJobName)]
			value, present := agent["continue-on-error"]
			assert.Equal(t, tt.wantPresent, present)
			if tt.wantPresent {
				assert.Equal(t, tt.want, value)
			}
		})
	}
}

func TestGeneratedAgentJobContinueOnErrorFromImport(t *testing.T) {
	tmpDir := testutil.TempDir(t, "agent-continue-on-error-import")
	sharedPath := filepath.Join(tmpDir, "shared.md")
	require.NoError(t, os.WriteFile(sharedPath, []byte("---\njobs:\n  agent:\n    continue-on-error: true\n---\n"), 0o644))

	workflowPath := filepath.Join(tmpDir, "imported.md")
	workflow := "---\non: workflow_dispatch\npermissions:\n  contents: read\nengine: copilot\nstrict: false\nimports:\n  - ./shared.md\n---\n\nTest workflow.\n"
	require.NoError(t, os.WriteFile(workflowPath, []byte(workflow), 0o644))

	jobs := compileContinueOnErrorWorkflowFile(t, workflowPath)
	assert.Equal(t, true, jobs[string(constants.AgentJobName)]["continue-on-error"])
}

func TestGeneratedAgentJobContinueOnErrorImportAppliesWhenMainOmitsField(t *testing.T) {
	tmpDir := testutil.TempDir(t, "agent-continue-on-error-import-merge")
	sharedPath := filepath.Join(tmpDir, "shared.md")
	require.NoError(t, os.WriteFile(sharedPath, []byte("---\njobs:\n  agent:\n    continue-on-error: true\n---\n"), 0o644))

	workflowPath := filepath.Join(tmpDir, "imported.md")
	workflow := "---\non: workflow_dispatch\npermissions:\n  contents: read\nengine: copilot\nstrict: false\nimports:\n  - ./shared.md\njobs:\n  agent:\n    timeout-minutes: 30\n---\n\nTest workflow.\n"
	require.NoError(t, os.WriteFile(workflowPath, []byte(workflow), 0o644))

	jobs := compileContinueOnErrorWorkflowFile(t, workflowPath)
	assert.Equal(t, true, jobs[string(constants.AgentJobName)]["continue-on-error"])
	assert.Equal(t, uint64(30), jobs[string(constants.AgentJobName)]["timeout-minutes"])
}

func TestGeneratedAgentJobContinueOnErrorMainWinsOverImport(t *testing.T) {
	tmpDir := testutil.TempDir(t, "agent-continue-on-error-main-wins")
	sharedPath := filepath.Join(tmpDir, "shared.md")
	require.NoError(t, os.WriteFile(sharedPath, []byte("---\njobs:\n  agent:\n    continue-on-error: true\n---\n"), 0o644))

	workflowPath := filepath.Join(tmpDir, "imported.md")
	workflow := "---\non: workflow_dispatch\npermissions:\n  contents: read\nengine: copilot\nstrict: false\nimports:\n  - ./shared.md\njobs:\n  agent:\n    continue-on-error: false\n---\n\nTest workflow.\n"
	require.NoError(t, os.WriteFile(workflowPath, []byte(workflow), 0o644))

	jobs := compileContinueOnErrorWorkflowFile(t, workflowPath)
	assert.Equal(t, false, jobs[string(constants.AgentJobName)]["continue-on-error"])
}

func TestContinueOnErrorRejectedForUnsupportedBuiltinJob(t *testing.T) {
	tmpDir := testutil.TempDir(t, "activation-continue-on-error")
	workflowPath := filepath.Join(tmpDir, "unsupported.md")
	workflow := "---\non: workflow_dispatch\npermissions:\n  contents: read\nengine: copilot\nstrict: false\njobs:\n  activation:\n    continue-on-error: true\n---\n\nTest workflow.\n"
	require.NoError(t, os.WriteFile(workflowPath, []byte(workflow), 0o644))

	err := NewCompiler().CompileWorkflow(workflowPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "jobs.activation.continue-on-error is supported only for the generated agent job")
}

func TestCustomJobContinueOnErrorRemainsSupported(t *testing.T) {
	jobs := compileContinueOnErrorTestWorkflow(t, "jobs:\n  optional:\n    runs-on: ubuntu-latest\n    continue-on-error: true\n    steps:\n      - run: echo optional\n")
	assert.Equal(t, true, jobs["optional"]["continue-on-error"])
}

func compileContinueOnErrorTestWorkflow(t *testing.T, frontmatter string) map[string]map[string]any {
	t.Helper()
	tmpDir := testutil.TempDir(t, "job-continue-on-error")
	workflowPath := filepath.Join(tmpDir, "workflow.md")
	workflow := "---\non: workflow_dispatch\npermissions:\n  contents: read\nengine: copilot\nstrict: false\n" + frontmatter + "---\n\nTest workflow.\n"
	require.NoError(t, os.WriteFile(workflowPath, []byte(workflow), 0o644))
	return compileContinueOnErrorWorkflowFile(t, workflowPath)
}

func compileContinueOnErrorWorkflowFile(t *testing.T, workflowPath string) map[string]map[string]any {
	t.Helper()
	require.NoError(t, NewCompiler().CompileWorkflow(workflowPath))

	lockContent, err := os.ReadFile(workflowPath[:len(workflowPath)-len(filepath.Ext(workflowPath))] + ".lock.yml")
	require.NoError(t, err)
	var compiled struct {
		Jobs map[string]map[string]any `yaml:"jobs"`
	}
	require.NoError(t, yaml.Unmarshal(lockContent, &compiled))
	return compiled.Jobs
}
