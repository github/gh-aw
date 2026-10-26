//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveJobRunnerTimeouts(t *testing.T) {
	tests := []struct {
		name        string
		job         Job
		data        WorkflowData
		wantRunner  string
		wantTimeout int
		wantComment string
		wantError   bool
	}{
		{
			name: "fifteen minutes is supported",
			job:  Job{Name: "custom", RunsOn: "runs-on: ubuntu-slim", TimeoutMinutes: 15},
		},
		{
			name:        "default framework runner switches for longer timeout",
			job:         Job{Name: string(constants.SafeOutputsJobName), RunsOn: "runs-on: ubuntu-slim", TimeoutMinutes: 16},
			wantRunner:  "runs-on: ubuntu-latest # ubuntu-slim supports job timeouts of at most 15 minutes.",
			wantComment: "ubuntu-slim supports job timeouts of at most 15 minutes.",
		},
		{
			name:        "timeout expression is ignored",
			job:         Job{Name: string(constants.SafeOutputsJobName), RunsOn: "runs-on: ubuntu-slim", TimeoutMinutesExpression: "${{ inputs.timeout }}"},
			wantRunner:  "runs-on: ubuntu-slim",
			wantTimeout: 0,
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
			data:      WorkflowData{SafeOutputs: &SafeOutputsConfig{RunsOn: "runs-on: ubuntu-slim", TimeoutMinutes: 45}},
			wantError: true,
		},
		{
			name:        "API-only safe outputs cap the implicit timeout and stay slim",
			job:         Job{Name: string(constants.SafeOutputsJobName), RunsOn: "runs-on: ubuntu-slim", TimeoutMinutes: 45},
			data:        WorkflowData{SafeOutputs: &SafeOutputsConfig{}},
			wantRunner:  "runs-on: ubuntu-slim",
			wantTimeout: 15,
		},
		{
			name:        "code publication selects a full runner by default",
			job:         Job{Name: string(constants.SafeOutputsJobName), RunsOn: "runs-on: ubuntu-slim", TimeoutMinutes: 45},
			data:        WorkflowData{SafeOutputs: &SafeOutputsConfig{CreatePullRequests: &CreatePullRequestsConfig{}}},
			wantRunner:  "runs-on: ubuntu-latest # Safe-output code publication checks out repository files; ubuntu-slim is limited to 15 minutes.",
			wantTimeout: 45,
			wantComment: "Safe-output code publication checks out repository files; ubuntu-slim is limited to 15 minutes.",
		},
		{
			name:        "implicit literal default timeout is capped",
			job:         Job{Name: string(constants.AgentJobName), RunsOn: "runs-on: ubuntu-slim", TimeoutMinutesExpression: "60"},
			wantRunner:  "runs-on: ubuntu-slim",
			wantTimeout: 15,
		},
		{
			name:      "custom job is checked",
			job:       Job{Name: "custom", RunsOn: "runs-on: ubuntu-slim", TimeoutMinutes: 16},
			wantError: true,
		},
		{
			name:      "agent explicit runner is checked",
			job:       Job{Name: string(constants.AgentJobName), RunsOn: "runs-on: ubuntu-slim", TimeoutMinutesExpression: "60"},
			data:      WorkflowData{Jobs: map[string]any{"agent": map[string]any{"timeout-minutes": 60}}},
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
			data := &tt.data
			if tt.data.SafeOutputs == nil && tt.job.Name == string(constants.SafeOutputsJobName) && tt.job.TimeoutMinutes > 0 {
				tt.data.SafeOutputs = &SafeOutputsConfig{TimeoutMinutes: tt.job.TimeoutMinutes}
			}
			if tt.data.Jobs == nil && tt.job.Name != string(constants.SafeOutputsJobName) && tt.job.TimeoutMinutes > 0 {
				tt.data.Jobs = map[string]any{tt.job.Name: map[string]any{"timeout-minutes": tt.job.TimeoutMinutes}}
			}
			if tt.wantRunner == "" {
				tt.wantRunner = tt.job.RunsOn
			}
			if tt.wantTimeout == 0 {
				tt.wantTimeout = tt.job.TimeoutMinutes
			}
			require.NoError(t, c.jobManager.AddJob(&tt.job))
			err := c.resolveJobRunnerTimeouts(data)
			if tt.wantError {
				require.ErrorContains(t, err, "ubuntu-slim supports at most 15 minutes")
				assert.Contains(t, err.Error(), tt.job.Name)
				assert.Contains(t, err.Error(), runnerConfigurationField(data, tt.job.Name))
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantRunner, c.jobManager.jobs[tt.job.Name].RunsOn)
			assert.Equal(t, tt.wantTimeout, c.jobManager.jobs[tt.job.Name].TimeoutMinutes)
		})
	}
}

func TestCompileUbuntuSlimTimeout(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter string
		jobName     string
		wantRunner  string
		wantTimeout int
		wantComment string
		wantError   bool
	}{
		{
			name:        "API-only safe outputs use slim runner with capped default timeout",
			frontmatter: "safe-outputs:\n  add-comment:\n",
			jobName:     "safe_outputs",
			wantRunner:  "ubuntu-slim",
			wantTimeout: 15,
		},
		{
			name:        "safe outputs fifteen minutes stays slim",
			frontmatter: "safe-outputs:\n  timeout-minutes: 15\n  add-comment:\n",
			jobName:     "safe_outputs",
			wantRunner:  "ubuntu-slim",
			wantTimeout: 15,
		},
		{
			name:        "explicit slim runner caps implicit timeout",
			frontmatter: "runs-on-slim: ubuntu-slim\nsafe-outputs:\n  add-comment:\n",
			jobName:     "safe_outputs",
			wantRunner:  "ubuntu-slim",
			wantTimeout: 15,
		},
		{
			name:        "safe outputs longer timeout selects larger runner",
			frontmatter: "safe-outputs:\n  timeout-minutes: 16\n  add-comment:\n",
			jobName:     "safe_outputs",
			wantRunner:  "ubuntu-latest",
			wantTimeout: 16,
			wantComment: "ubuntu-slim supports job timeouts of at most 15 minutes.",
		},
		{
			name:        "code publication uses larger runner by default",
			frontmatter: "safe-outputs:\n  create-pull-request:\n",
			jobName:     "safe_outputs",
			wantRunner:  "ubuntu-latest",
			wantTimeout: 45,
			wantComment: "Safe-output code publication checks out repository files; ubuntu-slim is limited to 15 minutes.",
		},
		{
			name:        "explicit other runner is preserved",
			frontmatter: "runs-on-slim: ubuntu-24.04\nsafe-outputs:\n  timeout-minutes: 30\n  add-comment:\n",
			jobName:     "safe_outputs",
			wantRunner:  "ubuntu-24.04",
			wantTimeout: 30,
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
					RunsOn  string `yaml:"runs-on"`
					Timeout any    `yaml:"timeout-minutes"`
				} `yaml:"jobs"`
			}
			require.NoError(t, yaml.Unmarshal([]byte(output), &lock))
			assert.Equal(t, tt.wantRunner, lock.Jobs[tt.jobName].RunsOn)
			if tt.wantTimeout > 0 {
				assert.EqualValues(t, tt.wantTimeout, lock.Jobs[tt.jobName].Timeout)
			}
			assert.Equal(t, tt.wantComment, commentForJob(output, tt.jobName))
		})
	}
}

func commentForJob(output, jobName string) string {
	lines := strings.Split(output, "\n")
	for i, line := range lines {
		if line == "  "+jobName+":" {
			for _, candidate := range lines[i+1:] {
				if after, ok := strings.CutPrefix(candidate, "    runs-on:"); ok {
					if _, comment, found := strings.Cut(after, " # "); found {
						return comment
					}
					return ""
				}
			}
		}
	}
	return ""
}
