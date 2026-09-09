//go:build !integration

package workflow

import (
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
	windowsStep := "      - name: Run script\n        run: \"${RUNNER_TEMP}/gh-aw/actions/setup.sh\" --verbose\n"
	got := renderStepForRunner(windowsStep, "runs-on: windows-latest")
	if !strings.Contains(got, "        run: bash \"${RUNNER_TEMP}/gh-aw/actions/setup.sh\" --verbose\n") {
		t.Fatalf("Windows step did not invoke the shell script with Bash:\n%s", got)
	}
	if !strings.Contains(got, "        shell: bash\n") {
		t.Fatalf("Windows step did not explicitly select Bash:\n%s", got)
	}

	quotedPath := "      - name: Run script\n        run: './my script.sh' --verbose\n"
	got = renderStepForRunner(quotedPath, "windows-latest")
	if !strings.Contains(got, "        run: bash './my script.sh' --verbose\n") {
		t.Fatalf("Windows step did not invoke a quoted shell script with Bash:\n%s", got)
	}

	multiline := "      - name: Run script\n        run: |\n          SCRIPT=helper.sh\n          ./helper.sh\n          cat <<'EOF'\n          helper.sh\n          EOF\n"
	got = renderStepForRunner(multiline, "windows-latest")
	if strings.Contains(got, "bash SCRIPT=helper.sh") || strings.Contains(got, "bash helper.sh\n          EOF") {
		t.Fatalf("Windows step rewrote an assignment or heredoc:\n%s", got)
	}
	if !strings.Contains(got, "          bash ./helper.sh\n") {
		t.Fatalf("Windows step did not invoke the actual shell script with Bash:\n%s", got)
	}

	withShell := "      - name: Run script\n        run: ./script.sh\n        shell: pwsh\n"
	got = renderStepForRunner(withShell, "runs-on: windows-latest")
	if !strings.Contains(got, "        shell: pwsh\n") {
		t.Fatalf("Windows step changed its explicit shell:\n%s", got)
	}

	ubuntuStep := "      - name: Run script\n        run: ./script.sh\n"
	if got = renderStepForRunner(ubuntuStep, "runs-on: ubuntu-latest"); got != ubuntuStep {
		t.Fatalf("Non-Windows step changed:\n%s", got)
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
