//go:build !integration

package workflow

import (
	"bytes"
	"compress/flate"
	"encoding/base64"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJobManager_AddJob(t *testing.T) {
	jm := NewJobManager()

	tests := []struct {
		name    string
		job     *Job
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid job",
			job: &Job{
				Name:   "test-job",
				RunsOn: "ubuntu-latest",
			},
			wantErr: false,
		},
		{
			name: "empty job name",
			job: &Job{
				Name:   "",
				RunsOn: "ubuntu-latest",
			},
			wantErr: true,
			errMsg:  "job name cannot be empty",
		},
		{
			name: "duplicate job name",
			job: &Job{
				Name:   "test-job", // Same name as first test
				RunsOn: "windows-latest",
			},
			wantErr: true,
			errMsg:  "job 'test-job' already exists",
		},
		{
			name: "reusable workflow caller cannot set timeout-minutes",
			job: &Job{
				Name:           "call-reusable",
				Uses:           "./.github/workflows/reusable.yml",
				TimeoutMinutes: 10,
			},
			wantErr: true,
			errMsg:  "uses a reusable workflow and cannot set timeout-minutes",
		},
		{
			name: "reusable workflow caller cannot set templated timeout-minutes",
			job: &Job{
				Name:                     "call-reusable-templated",
				Uses:                     "./.github/workflows/reusable.yml",
				TimeoutMinutesExpression: "${{ inputs.timeout }}",
			},
			wantErr: true,
			errMsg:  "uses a reusable workflow and cannot set timeout-minutes",
		},
		{
			name: "cannot set timeout-minutes as both integer and expression",
			job: &Job{
				Name:                     "invalid-timeout-job",
				TimeoutMinutes:           10,
				TimeoutMinutesExpression: "${{ inputs.timeout }}",
			},
			wantErr: true,
			errMsg:  "has timeout-minutes set as both integer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := jm.AddJob(tt.job)
			if tt.wantErr {
				if err == nil {
					t.Errorf("AddJob() expected error but got nil")
					return
				}
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("AddJob() error = %v, want error containing %v", err, tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("AddJob() unexpected error = %v", err)
				}
			}
		})
	}
}

func TestJobManager_ValidateDependencies(t *testing.T) {
	tests := []struct {
		name    string
		jobs    []*Job
		wantErr bool
		errMsg  string
	}{
		{
			name: "no dependencies",
			jobs: []*Job{
				{Name: "job1", RunsOn: "ubuntu-latest"},
				{Name: "job2", RunsOn: "ubuntu-latest"},
			},
			wantErr: false,
		},
		{
			name: "valid dependencies",
			jobs: []*Job{
				{Name: "job1", RunsOn: "ubuntu-latest"},
				{Name: "job2", RunsOn: "ubuntu-latest", Needs: []string{"job1"}},
				{Name: "job3", RunsOn: "ubuntu-latest", Needs: []string{"job1", "job2"}},
			},
			wantErr: false,
		},
		{
			name: "missing dependency",
			jobs: []*Job{
				{Name: "job1", RunsOn: "ubuntu-latest"},
				{Name: "job2", RunsOn: "ubuntu-latest", Needs: []string{"nonexistent"}},
			},
			wantErr: true,
			errMsg:  "depends on non-existent job 'nonexistent'",
		},
		{
			name: "simple cycle",
			jobs: []*Job{
				{Name: "job1", RunsOn: "ubuntu-latest", Needs: []string{"job2"}},
				{Name: "job2", RunsOn: "ubuntu-latest", Needs: []string{"job1"}},
			},
			wantErr: true,
			errMsg:  "cycle detected",
		},
		{
			name: "complex cycle",
			jobs: []*Job{
				{Name: "job1", RunsOn: "ubuntu-latest", Needs: []string{"job2"}},
				{Name: "job2", RunsOn: "ubuntu-latest", Needs: []string{"job3"}},
				{Name: "job3", RunsOn: "ubuntu-latest", Needs: []string{"job1"}},
			},
			wantErr: true,
			errMsg:  "cycle detected",
		},
		{
			name: "self-dependency cycle",
			jobs: []*Job{
				{Name: "job1", RunsOn: "ubuntu-latest", Needs: []string{"job1"}},
			},
			wantErr: true,
			errMsg:  "cycle detected",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jm := NewJobManager()
			for _, job := range tt.jobs {
				if err := jm.AddJob(job); err != nil {
					t.Fatalf("Failed to add job %s: %v", job.Name, err)
				}
			}

			err := jm.ValidateDependencies()
			if tt.wantErr {
				if err == nil {
					t.Errorf("ValidateDependencies() expected error but got nil")
					return
				}
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("ValidateDependencies() error = %v, want error containing %v", err, tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("ValidateDependencies() unexpected error = %v", err)
				}
			}
		})
	}
}

// TestJobManager_WriteJobsYAML verifies that WriteJobsYAML correctly appends
// the jobs section to an already-populated builder (mimicking generateWorkflowBody).
func TestJobManager_WriteJobsYAML(t *testing.T) {
	tests := []struct {
		name     string
		prefix   string
		jobs     []*Job
		expected []string
	}{
		{
			name:     "appends to non-empty builder",
			prefix:   "name: \"my-workflow\"\non: issues\npermissions: {}\n\n",
			jobs:     []*Job{},
			expected: []string{"name: \"my-workflow\"", "jobs:"},
		},
		{
			name:   "jobs follow existing header content",
			prefix: "name: \"test\"\n",
			jobs: []*Job{
				{
					Name:    "build",
					RunsOn:  "runs-on: ubuntu-latest",
					Steps:   []string{"      - name: Build\n        run: make build\n"},
					Outputs: map[string]string{"sha": "${{ steps.build.outputs.sha }}"},
				},
			},
			expected: []string{
				"name: \"test\"",
				"jobs:",
				"  build:",
				"    runs-on: ubuntu-latest",
				"    outputs:",
				"      sha: ${{ steps.build.outputs.sha }}",
				"    steps:",
				"      - name: Build",
				"        run: make build",
			},
		},
		{
			name:   "multiple jobs appended to header - order and separators correct",
			prefix: "on: push\n\n",
			jobs: []*Job{
				{
					Name:   "test",
					RunsOn: "runs-on: ubuntu-latest",
					Steps:  []string{"      - name: Test\n        run: echo test\n"},
				},
				{
					Name:   "deploy",
					RunsOn: "runs-on: ubuntu-latest",
					Needs:  []string{"test"},
					Steps:  []string{"      - name: Deploy\n        run: echo deploy\n"},
				},
			},
			expected: []string{
				"on: push",
				"jobs:",
				"  deploy:",
				"    needs: test",
				"  test:",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jm := NewJobManager()
			for _, job := range tt.jobs {
				if err := jm.AddJob(job); err != nil {
					t.Fatalf("Failed to add job %s: %v", job.Name, err)
				}
			}

			// Write to a builder that already has content.
			var b strings.Builder
			b.WriteString(tt.prefix)
			jm.WriteJobsYAML(&b)
			result := b.String()

			for _, expected := range tt.expected {
				if !strings.Contains(result, expected) {
					t.Errorf("WriteJobsYAML() result does not contain %q\nFull result:\n%s", expected, result)
				}
			}
		})
	}
}

func TestRenderStepForRunner(t *testing.T) {
	windowsStep := "    steps:\n      - name: Run script\n        run: echo \"${RUNNER_TEMP}/gh-aw/actions/setup.sh\" --verbose\n"
	got := renderStepForRunner(windowsStep, "runs-on: windows-latest", "agent")
	if !strings.Contains(got, "        run: |\n") || !strings.Contains(got, "wsl.exe --distribution Ubuntu --user root --exec bash --noprofile --norc -e -o pipefail") {
		t.Fatalf("Windows step did not invoke the POSIX script through WSL:\n%s", got)
	}
	if !strings.Contains(got, "        shell: pwsh\n") {
		t.Fatalf("Windows step did not use PowerShell to launch WSL:\n%s", got)
	}
	for _, pathVariable := range []string{
		"'GITHUB_WORKSPACE'",
		"'RUNNER_TEMP'",
		"'RUNNER_TOOL_CACHE'",
		"'GITHUB_OUTPUT'",
		"'GH_AW_WSL_WORKING_DIRECTORY'",
	} {
		if !strings.Contains(got, pathVariable) {
			t.Errorf("Windows step did not map %s into WSL:\n%s", pathVariable, got)
		}
	}
	if !strings.Contains(got, "$pathVariableSet.Contains($_) -or $value -match") || !strings.Contains(got, "\"$_/p\"") {
		t.Fatalf("Windows step did not mark host paths for WSL conversion:\n%s", got)
	}
	if !strings.Contains(got, "$environmentNames = $allowedEnvironmentVariables | Where-Object") ||
		strings.Contains(got, "$processEnvironment.Keys | Where-Object") {
		t.Fatalf("Windows step did not restrict WSL environment forwarding:\n%s", got)
	}
	if !strings.Contains(got, `cd "$GH_AW_WSL_WORKING_DIRECTORY"`) ||
		!strings.Contains(got, "+ \"`n\" +") ||
		strings.Contains(got, "[System.Environment]::NewLine") {
		t.Fatalf("Windows step did not preserve its working directory with LF line endings:\n%s", got)
	}
	for _, expected := range []string{
		"& $wsl.Source --status",
		"--install --web-download --distribution Ubuntu --no-launch",
		"apt-get install -y --no-install-recommends ca-certificates curl docker.io gh git jq nodejs npm xz-utils",
		"WSL is unavailable",
	} {
		if !strings.Contains(got, expected) {
			t.Errorf("Windows job lacks WSL bootstrap requirement %q:\n%s", expected, got)
		}
	}
	if script := decodeWindowsWSLRunScript(t, got); script != "echo \"${RUNNER_TEMP}/gh-aw-setup/actions/setup.sh\" --verbose" {
		t.Fatalf("WSL adapter changed the POSIX script: %q", script)
	}

	multiline := "    steps:\n      - name: Run script\n        run: |\n          SCRIPT=helper.sh\n          ./helper.sh\n          cat <<'EOF'\n          helper.sh\n          EOF\n"
	got = renderStepForRunner(multiline, "windows-latest", "agent")
	if script := decodeWindowsWSLRunScript(t, got); script != "SCRIPT=helper.sh\n./helper.sh\ncat <<'EOF'\nhelper.sh\nEOF\n" {
		t.Fatalf("WSL adapter changed the multiline POSIX script: %q", script)
	}

	withShell := "    steps:\n      - name: Run script\n        run: ./script.ps1\n        shell: pwsh\n"
	got = renderStepForRunner(withShell, "runs-on: windows-latest", "agent")
	if got != withShell {
		t.Fatalf("Windows step changed its explicit shell:\n%s", got)
	}

	nodeSetup := `    steps:
      - name: Setup Node.js
        uses: actions/setup-node@v7
        with:
          node-version: '24'
      - name: Install runtime
        run: npm install -g example
`
	got = renderStepForRunner(nodeSetup, "runs-on: windows-latest", "agent")
	for _, expected := range []string{
		"id: wsl_setup_node",
		"name: Setup Node.js in WSL",
		"${{ steps.wsl_setup_node.outputs.node-version }}",
		"SHASUMS256.txt",
		"ln -sfn \"$install_dir\" /usr/local/lib/nodejs/current",
		`export PATH="/usr/local/lib/nodejs/current/bin:$PATH"`,
	} {
		if !strings.Contains(got, expected) {
			t.Errorf("Windows job did not bootstrap the Linux Node.js runtime (%q):\n%s", expected, got)
		}
	}

	ubuntuStep := "    steps:\n      - name: Run script\n        run: ./script.sh\n"
	if got = renderStepForRunner(ubuntuStep, "runs-on: ubuntu-latest", "agent"); got != ubuntuStep {
		t.Fatalf("Non-Windows step changed:\n%s", got)
	}

	if got = renderStepForRunner(windowsStep, "runs-on: windows-latest", "release"); got != windowsStep {
		t.Fatalf("Unrelated Windows job changed:\n%s", got)
	}
}

func TestRenderStepForRunnerPreservesWorkingDirectoryAndExplicitEnvironment(t *testing.T) {
	job := `  agent:
    env:
      JOB_SETTING: value
    steps:
      - name: Run framework
        working-directory: ${{ github.workspace }}/subdirectory
        env:
          FRAMEWORK_TOKEN: ${{ secrets.FRAMEWORK_TOKEN }}
        run: pwd
`
	got := renderStepForRunner(job, "runs-on: windows-latest", "agent")
	for _, expected := range []string{
		"'JOB_SETTING'",
		"'FRAMEWORK_TOKEN'",
		`cd "$GH_AW_WSL_WORKING_DIRECTORY"`,
		"wslpath -u (Get-Location).Path",
	} {
		if !strings.Contains(got, expected) {
			t.Errorf("Windows WSL wrapper lacks %q:\n%s", expected, got)
		}
	}
	for _, excluded := range []string{"'GITHUB_TOKEN'", "UNRELATED_SECRET"} {
		if strings.Contains(got, excluded) {
			t.Errorf("Windows WSL wrapper unexpectedly forwards %q:\n%s", excluded, got)
		}
	}
}

func decodeWindowsWSLRunScript(t *testing.T, rendered string) string {
	t.Helper()
	const start = "FromBase64String('"
	index := strings.Index(rendered, start)
	if index < 0 {
		t.Fatalf("WSL run wrapper does not contain its encoded script:\n%s", rendered)
	}
	encoded := rendered[index+len(start):]
	end := strings.IndexByte(encoded, '\'')
	if end < 0 {
		t.Fatalf("WSL run wrapper has an unterminated encoded script:\n%s", rendered)
	}
	compressed, err := base64.StdEncoding.DecodeString(encoded[:end])
	if err != nil {
		t.Fatalf("decode compressed WSL run script: %v", err)
	}
	reader := flate.NewReader(bytes.NewReader(compressed))
	defer reader.Close()
	script, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("decompress WSL run script: %v", err)
	}
	return string(script)
}

func TestRenderStepForRunnerMapsArtifactTempPaths(t *testing.T) {
	job := `    steps:
      - name: Setup Scripts
        uses: ./actions/setup
        with:
          destination: ${{ runner.temp }}/gh-aw/actions
      - name: Set runtime paths
        run: |
          echo "GH_AW_SAFE_OUTPUTS=${RUNNER_TEMP}/gh-aw/safeoutputs/outputs.jsonl" >> "$GITHUB_OUTPUT"
          echo "GH_AW_SAFE_OUTPUTS_CONFIG_PATH=${RUNNER_TEMP}/gh-aw/safeoutputs/config.json" >> "$GITHUB_OUTPUT"
          echo "GH_AW_SAFE_OUTPUTS_TOOLS_PATH=${RUNNER_TEMP}/gh-aw/safeoutputs/tools.json" >> "$GITHUB_OUTPUT"
      - name: Upload generated data
        uses: actions/upload-artifact@v4
        with:
          path: /tmp/gh-aw/agent_output.json
      - name: Run framework
        shell: bash
        run: |
          mkdir -p /tmp/gh-aw
`
	got := renderStepForRunner(job, "runs-on: windows-latest", "agent")
	if !strings.Contains(got, "path: ${{ runner.temp }}/gh-aw/agent_output.json") {
		t.Fatalf("Windows artifact path was not mapped to runner temp:\n%s", got)
	}
	if !strings.Contains(got, `ln -s "$RUNNER_TEMP/gh-aw" /tmp/gh-aw`) {
		t.Fatalf("WSL bootstrap did not map /tmp/gh-aw to runner temp:\n%s", got)
	}
	if !strings.Contains(got, "destination: ${{ runner.temp }}/gh-aw-setup/actions") {
		t.Fatalf("Windows setup scripts were not separated from writable runtime artifacts:\n%s", got)
	}
	if !strings.Contains(got, "uses: actions/upload-artifact@v4") {
		t.Fatalf("Windows artifact action was not preserved:\n%s", got)
	}
	if script := decodeWindowsWSLRunScript(t, got); !strings.Contains(script, `GH_AW_SAFE_OUTPUTS=$(wslpath -w "${RUNNER_TEMP}/gh-aw/safeoutputs/outputs.jsonl")`) {
		t.Fatalf("Windows native helpers were not given host paths for runtime outputs:\n%s", script)
	}
}

func TestRewriteWindowsActionTempPathMapsNativeEnvironmentPaths(t *testing.T) {
	step := `      - name: Parse agent output
        uses: actions/github-script@v7
        env:
          GH_AW_AGENT_OUTPUT: /tmp/gh-aw/agent-stdio.log
          GH_AW_SAFE_OUTPUTS: ${{ steps.set-runtime-paths.outputs.GH_AW_SAFE_OUTPUTS }}
          GH_AW_RECOVERY_ADDITIONAL_ROOTS: '["${{ runner.temp }}/gh-aw/agent_output.json","${{ runner.temp }}/gh-aw"]'
          GH_AW_ACTIONS_DIR: ${{ runner.temp }}/gh-aw-setup/actions
`
	got := rewriteWindowsActionTempPath(step)
	if !strings.Contains(got, `GH_AW_AGENT_OUTPUT: ${{ runner.temp }}/gh-aw/agent-stdio.log`) {
		t.Fatalf("Native helper output path was not mapped to runner temp:\n%s", got)
	}
	if !strings.Contains(got, `GH_AW_RECOVERY_ADDITIONAL_ROOTS: '["${{ runner.temp }}/gh-aw/agent_output.json","${{ runner.temp }}/gh-aw"]'`) {
		t.Fatalf("Recovery artifact paths were not kept on the Windows host:\n%s", got)
	}
	if !strings.Contains(got, `GH_AW_ACTIONS_DIR: ${{ runner.temp }}/gh-aw-setup/actions`) {
		t.Fatalf("Setup action path was unexpectedly rewritten:\n%s", got)
	}
	if strings.Contains(got, "/tmp/gh-aw") {
		t.Fatalf("Native helper retained an incompatible POSIX path:\n%s", got)
	}
}

func TestRewriteWindowsPathOutputs(t *testing.T) {
	runtimePaths := rewriteWindowsPathOutputs(
		"Set runtime paths",
		`echo "GH_AW_SAFE_OUTPUTS=${RUNNER_TEMP}/gh-aw/safeoutputs/outputs.jsonl" >> "$GITHUB_OUTPUT"`,
	)
	if !strings.Contains(runtimePaths, `GH_AW_SAFE_OUTPUTS=$(wslpath -w "${RUNNER_TEMP}/gh-aw/safeoutputs/outputs.jsonl")`) {
		t.Fatalf("Set runtime paths did not publish a Windows-native output path:\n%s", runtimePaths)
	}

	agentOutput := rewriteWindowsPathOutputs(
		"Setup agent output environment variable",
		"if [ -f \"/tmp/gh-aw/agent_output.json\" ]; then\n  echo \"GH_AW_AGENT_OUTPUT=/tmp/gh-aw/agent_output.json\" >> \"$GITHUB_OUTPUT\"\nfi\n",
	)
	if !strings.Contains(agentOutput, `GH_AW_AGENT_OUTPUT=$(wslpath -w "${RUNNER_TEMP}/gh-aw/agent_output.json")`) {
		t.Fatalf("Agent output setup did not publish a Windows-native path:\n%s", agentOutput)
	}

	configCopy := rewriteWindowsPathOutputs(
		"Run agent",
		`cp "${RUNNER_TEMP}/gh-aw/awf-config.json" /tmp/gh-aw/awf-config.json`,
	)
	if !strings.Contains(configCopy, `if [ "${RUNNER_TEMP}/gh-aw/awf-config.json" -ef "/tmp/gh-aw/awf-config.json" ]; then :; else cp "${RUNNER_TEMP}/gh-aw/awf-config.json" /tmp/gh-aw/awf-config.json; fi`) {
		t.Fatalf("AWF config copy was not guarded against the WSL alias resolving to the same file:\n%s", configCopy)
	}
}

func TestCompileWindowsRunnerUsesWslArtifactPaths(t *testing.T) {
	tmpDir := t.TempDir()
	workflowFile := filepath.Join(tmpDir, "windows-artifacts.md")
	workflowContent := `---
on: workflow_dispatch
permissions:
  contents: read
  copilot-requests: write
runs-on: windows-latest
model: copilot/gpt-5.3-codex
engine:
  id: codex
  model-provider: github
network: {}
tools:
  bash:
    - "*"
safe-outputs:
  create-issue:
strict: false
---

# Windows artifact path regression

Compile this workflow on a Windows runner.
`
	if err := os.WriteFile(workflowFile, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("write workflow: %v", err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowFile); err != nil {
		t.Fatalf("compile workflow: %v", err)
	}

	lockFile := filepath.Join(tmpDir, "windows-artifacts.lock.yml")
	lockBytes, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("read lock file: %v", err)
	}
	fallbackStep := extractWorkflowStepByName(t, string(lockBytes), "Upload agent output fallback artifact")

	wantPath := "${{ runner.temp }}/gh-aw/sandbox/firewall-audit-logs/api-proxy-logs/token-usage.jsonl"
	if !strings.Contains(fallbackStep, wantPath) {
		t.Fatalf("compiled Windows artifact fallback did not map the token usage path %q:\n%s", wantPath, fallbackStep)
	}
	if strings.Contains(fallbackStep, "/tmp/gh-aw/") {
		t.Fatalf("compiled Windows artifact fallback retained a host-incompatible POSIX path:\n%s", fallbackStep)
	}
}

func TestJobManager_GetJob(t *testing.T) {
	jm := NewJobManager()

	testJob := &Job{
		Name:   "test-job",
		RunsOn: "ubuntu-latest",
	}

	// Add a job
	err := jm.AddJob(testJob)
	if err != nil {
		t.Fatalf("Failed to add job: %v", err)
	}

	// Test retrieving existing job
	retrievedJob, exists := jm.GetJob("test-job")
	if !exists {
		t.Error("Expected job to exist but it doesn't")
	}
	if retrievedJob.Name != testJob.Name {
		t.Errorf("Retrieved job name = %s, want %s", retrievedJob.Name, testJob.Name)
	}

	// Test retrieving non-existent job
	_, exists = jm.GetJob("nonexistent")
	if exists {
		t.Error("Expected job to not exist but it does")
	}
}

func TestJobManager_GetAllJobs(t *testing.T) {
	jm := NewJobManager()

	jobs := []*Job{
		{Name: "job1", RunsOn: "ubuntu-latest"},
		{Name: "job2", RunsOn: "windows-latest"},
	}

	for _, job := range jobs {
		if err := jm.AddJob(job); err != nil {
			t.Fatalf("Failed to add job %s: %v", job.Name, err)
		}
	}

	allJobs := jm.GetAllJobs()

	if len(allJobs) != len(jobs) {
		t.Errorf("GetAllJobs() returned %d jobs, want %d", len(allJobs), len(jobs))
	}

	for _, originalJob := range jobs {
		retrievedJob, exists := allJobs[originalJob.Name]
		if !exists {
			t.Errorf("Job %s not found in GetAllJobs() result", originalJob.Name)
		}
		if retrievedJob.Name != originalJob.Name {
			t.Errorf("Job name mismatch: got %s, want %s", retrievedJob.Name, originalJob.Name)
		}
	}

	// Test that modifying returned map doesn't affect internal state
	allJobs["new-job"] = &Job{Name: "new-job"}

	// Original manager should not be affected
	if _, exists := jm.GetJob("new-job"); exists {
		t.Error("Internal state was modified by external change to GetAllJobs() result")
	}
}

func TestBuildCustomJobsActivationDependency(t *testing.T) {
	tests := []struct {
		name                 string
		jobs                 map[string]any
		activationJobCreated bool
		expectedDependencies map[string][]string
		description          string
	}{
		{
			name: "custom job without explicit needs should depend on activation",
			jobs: map[string]any{
				"super_linter": map[string]any{
					"runs-on": "ubuntu-latest",
					"steps": []any{
						map[string]any{
							"name": "Run linter",
							"run":  "echo 'linting'",
						},
					},
				},
			},
			activationJobCreated: true,
			expectedDependencies: map[string][]string{
				"super_linter": {"activation"},
			},
			description: "Custom job without explicit needs should automatically depend on activation",
		},
		{
			name: "custom job with explicit needs should not get activation dependency",
			jobs: map[string]any{
				"custom_job": map[string]any{
					"runs-on": "ubuntu-latest",
					"needs":   []any{"other_job"},
					"steps": []any{
						map[string]any{
							"name": "Run custom",
							"run":  "echo 'custom'",
						},
					},
				},
			},
			activationJobCreated: true,
			expectedDependencies: map[string][]string{
				"custom_job": {"other_job"},
			},
			description: "Custom job with explicit needs should keep its own dependencies",
		},
		{
			name: "custom job without activation should have no automatic dependency",
			jobs: map[string]any{
				"custom_job": map[string]any{
					"runs-on": "ubuntu-latest",
					"steps": []any{
						map[string]any{
							"name": "Run custom",
							"run":  "echo 'custom'",
						},
					},
				},
			},
			activationJobCreated: false,
			expectedDependencies: map[string][]string{
				"custom_job": nil,
			},
			description: "Custom job should not have activation dependency when activation job doesn't exist",
		},
		{
			name: "multiple custom jobs without explicit needs",
			jobs: map[string]any{
				"linter": map[string]any{
					"runs-on": "ubuntu-latest",
					"steps": []any{
						map[string]any{"name": "Lint", "run": "lint"},
					},
				},
				"formatter": map[string]any{
					"runs-on": "ubuntu-latest",
					"steps": []any{
						map[string]any{"name": "Format", "run": "fmt"},
					},
				},
			},
			activationJobCreated: true,
			expectedDependencies: map[string][]string{
				"linter":    {"activation"},
				"formatter": {"activation"},
			},
			description: "Multiple custom jobs should all depend on activation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Compiler{
				jobManager: NewJobManager(),
			}

			data := &WorkflowData{
				Jobs: tt.jobs,
			}

			err := c.buildCustomJobs(data, tt.activationJobCreated)
			if err != nil {
				t.Fatalf("%s: buildCustomJobs() error = %v", tt.description, err)
			}

			// Verify each job has expected dependencies
			for jobName, expectedNeeds := range tt.expectedDependencies {
				job, exists := c.jobManager.jobs[jobName]
				if !exists {
					t.Fatalf("%s: job '%s' not found in job manager", tt.description, jobName)
				}

				if len(job.Needs) != len(expectedNeeds) {
					t.Errorf("%s: job '%s' has %d dependencies, expected %d. Got: %v, Expected: %v",
						tt.description, jobName, len(job.Needs), len(expectedNeeds), job.Needs, expectedNeeds)
					continue
				}

				if expectedNeeds == nil {
					if len(job.Needs) > 0 {
						t.Errorf("%s: job '%s' should have no dependencies, got: %v", tt.description, jobName, job.Needs)
					}
					continue
				}

				for i, expected := range expectedNeeds {
					if job.Needs[i] != expected {
						t.Errorf("%s: job '%s' dependency[%d] = %s, expected %s",
							tt.description, jobName, i, job.Needs[i], expected)
					}
				}
			}
		})
	}
}
