//go:build !integration

package workflow

import (
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveJobRunnerTimeouts(t *testing.T) {
	tests := []struct {
		name       string
		job        Job
		data       WorkflowData
		wantRunner string
		wantError  bool
	}{
		{
			name: "fifteen minutes is supported",
			job:  Job{Name: "custom", RunsOn: "runs-on: ubuntu-slim", TimeoutMinutes: 15},
		},
		{
			name:       "default framework runner switches for longer timeout",
			job:        Job{Name: string(constants.SafeOutputsJobName), RunsOn: "runs-on: ubuntu-slim", TimeoutMinutes: 16},
			data:       WorkflowData{SafeOutputs: &SafeOutputsConfig{TimeoutMinutes: 16}},
			wantRunner: "runs-on: ubuntu-latest # Automatically selected because the configured timeout exceeds ubuntu-slim's 15-minute limit.",
		},
		{
			name:       "numeric timeout in expression field is checked",
			job:        Job{Name: string(constants.SafeOutputsJobName), RunsOn: "runs-on: ubuntu-slim", TimeoutMinutesExpression: "45"},
			data:       WorkflowData{SafeOutputs: &SafeOutputsConfig{TimeoutMinutes: 45}},
			wantRunner: "runs-on: ubuntu-latest # Automatically selected because the configured timeout exceeds ubuntu-slim's 15-minute limit.",
		},
		{
			name:      "explicit slim runner is not silently replaced",
			job:       Job{Name: string(constants.SafeOutputsJobName), RunsOn: "runs-on: ubuntu-slim", TimeoutMinutes: 16},
			data:      WorkflowData{RunsOnSlim: "runs-on: ubuntu-slim"},
			wantError: true,
		},
		{
			name:      "safe output override is preserved",
			job:       Job{Name: string(constants.SafeOutputsJobName), RunsOn: "runs-on: ubuntu-slim", TimeoutMinutes: 45},
			data:      WorkflowData{SafeOutputs: &SafeOutputsConfig{RunsOn: "runs-on: ubuntu-slim"}},
			wantError: true,
		},
		{
			name:      "custom job is checked",
			job:       Job{Name: "custom", RunsOn: "runs-on: ubuntu-slim", TimeoutMinutes: 16},
			wantError: true,
		},
		{
			name:      "agent explicit runner is checked",
			job:       Job{Name: string(constants.AgentJobName), RunsOn: "runs-on: ubuntu-slim", TimeoutMinutesExpression: "60"},
			wantError: true,
		},
		{
			name:      "quoted label is checked",
			job:       Job{Name: "custom", RunsOn: "runs-on: 'ubuntu-slim'", TimeoutMinutes: 16},
			wantError: true,
		},
		{
			name:      "label array is checked",
			job:       Job{Name: "custom", RunsOn: "runs-on:\n      - ubuntu-slim", TimeoutMinutes: 16},
			wantError: true,
		},
		{
			name:      "group labels are checked",
			job:       Job{Name: "custom", RunsOn: "runs-on:\n      group: hosted\n      labels: ubuntu-slim", TimeoutMinutes: 16},
			wantError: true,
		},
		{
			name: "group name is not a runner label",
			job:  Job{Name: "custom", RunsOn: "runs-on:\n      group: ubuntu-slim\n      labels: linux", TimeoutMinutes: 30},
		},
		{
			name: "timeout expression is ignored",
			job:  Job{Name: "custom", RunsOn: "runs-on: ubuntu-slim", TimeoutMinutesExpression: "${{ inputs.timeout }}"},
		},
		{
			name: "runner expression is ignored",
			job:  Job{Name: "custom", RunsOn: "runs-on: ${{ inputs.runner }}", TimeoutMinutes: 30},
		},
		{
			name: "other runner is unchanged",
			job:  Job{Name: "custom", RunsOn: "runs-on: ubuntu-latest", TimeoutMinutes: 30},
		},
		{
			name: "unspecified timeout is unchanged",
			job:  Job{Name: "custom", RunsOn: "runs-on: ubuntu-slim"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewCompiler()
			if tt.wantRunner == "" {
				tt.wantRunner = tt.job.RunsOn
			}
			require.NoError(t, c.jobManager.AddJob(&tt.job))
			err := c.resolveJobRunnerTimeouts(&tt.data)
			if tt.wantError {
				require.ErrorContains(t, err, "ubuntu-slim supports at most 15 minutes")
				assert.Contains(t, err.Error(), tt.job.Name)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantRunner, c.jobManager.jobs[tt.job.Name].RunsOn)
		})
	}
}

func TestCompileUbuntuSlimTimeout(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter string
		jobName     string
		wantRunner  string
		wantComment bool
		wantError   bool
	}{
		{
			name:        "safe outputs default timeout stays on slim runner",
			frontmatter: "safe-outputs:\n  add-comment:\n",
			jobName:     "safe_outputs",
			wantRunner:  "ubuntu-slim",
		},
		{
			name:        "safe outputs fifteen minutes stays slim",
			frontmatter: "safe-outputs:\n  timeout-minutes: 15\n  add-comment:\n",
			jobName:     "safe_outputs",
			wantRunner:  "ubuntu-slim",
		},
		{
			name:        "safe outputs longer timeout selects larger runner",
			frontmatter: "safe-outputs:\n  timeout-minutes: 16\n  add-comment:\n",
			jobName:     "safe_outputs",
			wantRunner:  "ubuntu-latest",
			wantComment: true,
		},
		{
			name:        "explicit other runner is preserved",
			frontmatter: "runs-on-slim: ubuntu-24.04\nsafe-outputs:\n  timeout-minutes: 30\n  add-comment:\n",
			jobName:     "safe_outputs",
			wantRunner:  "ubuntu-24.04",
		},
		{
			name:        "explicit slim override is rejected",
			frontmatter: "runs-on-slim: ubuntu-slim\nsafe-outputs:\n  timeout-minutes: 30\n  add-comment:\n",
			wantError:   true,
		},
		{
			name:        "custom job over limit is rejected",
			frontmatter: "jobs:\n  custom:\n    runs-on: ubuntu-slim\n    timeout-minutes: 16\n    steps:\n      - run: echo hello\n",
			wantError:   true,
		},
		{
			name:        "custom job timeout expression is ignored",
			frontmatter: "jobs:\n  custom:\n    runs-on: ubuntu-slim\n    timeout-minutes: ${{ inputs.timeout }}\n    steps:\n      - run: echo hello\n",
			jobName:     "custom",
			wantRunner:  "ubuntu-slim",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewCompiler(WithSkipValidation(true))
			content := "---\non: workflow_dispatch\nengine: copilot\npermissions:\n  contents: read\n" + tt.frontmatter + "---\nTest workflow\n"
			data, err := c.ParseWorkflowString(content, "slim-timeout.md")
			require.NoError(t, err)
			output, err := c.CompileToYAML(data, "slim-timeout.md")
			if tt.wantError {
				require.ErrorContains(t, err, "ubuntu-slim supports at most 15 minutes")
				return
			}
			require.NoError(t, err)
			var lock struct {
				Jobs map[string]struct {
					RunsOn string `yaml:"runs-on"`
				} `yaml:"jobs"`
			}
			require.NoError(t, yaml.Unmarshal([]byte(output), &lock))
			assert.Equal(t, tt.wantRunner, lock.Jobs[tt.jobName].RunsOn)
			if tt.wantComment {
				assert.Contains(t, output, "runs-on: ubuntu-latest # Automatically selected because the configured timeout exceeds ubuntu-slim's 15-minute limit.")
			} else {
				assert.NotContains(t, output, "Automatically selected because the configured timeout exceeds ubuntu-slim's 15-minute limit.")
			}
		})
	}
}
