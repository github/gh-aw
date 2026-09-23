//go:build !integration

package workflow

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/require"
)

func TestParseSafeJobsConfig(t *testing.T) {
	c := NewCompiler()

	// Test parseSafeJobsConfig internal function which now expects a jobs map directly.
	// Note: User workflows should use "safe-outputs.jobs" syntax; this test validates
	// the internal parsing logic used by extractSafeJobsFromFrontmatter and safe_outputs.go.
	jobsMap := map[string]any{
		"deploy": map[string]any{
			"runs-on": "ubuntu-latest",
			"if":      "github.event.issue.number",
			"needs":   []any{"task"},
			"env": map[string]any{
				"DEPLOY_ENV": "production",
			},
			"permissions": map[string]any{
				"contents": "write",
				"issues":   "read",
			},
			"github-token": "${{ secrets.CUSTOM_TOKEN }}",
			"inputs": map[string]any{
				"environment": map[string]any{
					"description": "Target deployment environment",
					"required":    true,
					"type":        "choice",
					"options":     []any{"staging", "production"},
				},
				"force": map[string]any{
					"description": "Force deployment even if tests fail",
					"required":    false,
					"type":        "boolean",
					"default":     "false",
				},
			},
			"steps": []any{
				map[string]any{
					"name": "Deploy application",
					"run":  "echo 'Deploying to ${{ inputs.environment }}'",
				},
			},
		},
	}

	result := c.parseSafeJobsConfig(jobsMap)

	if result == nil {
		t.Fatal("Expected safe-jobs config to be parsed, got nil")
	}

	if len(result) != 1 {
		t.Fatalf("Expected 1 safe job, got %d", len(result))
	}

	deployJob, exists := result["deploy"]
	if !exists {
		t.Fatal("Expected 'deploy' job to exist")
	}

	// Test runs-on
	if deployJob.RunsOn != "runs-on: ubuntu-latest" {
		t.Errorf("Expected runs-on to be 'ubuntu-latest', got %v", deployJob.RunsOn)
	}

	// Test if condition
	if deployJob.If != "github.event.issue.number" {
		t.Errorf("Expected if condition to be 'github.event.issue.number', got %s", deployJob.If)
	}

	// Test needs
	if len(deployJob.Needs) != 1 || deployJob.Needs[0] != "task" {
		t.Errorf("Expected needs to be ['task'], got %v", deployJob.Needs)
	}

	// Test env
	if len(deployJob.Env) != 1 || deployJob.Env["DEPLOY_ENV"] != "production" {
		t.Errorf("Expected env to contain DEPLOY_ENV=production, got %v", deployJob.Env)
	}

	// Test permissions
	if len(deployJob.Permissions) != 2 || deployJob.Permissions["contents"] != "write" || deployJob.Permissions["issues"] != "read" {
		t.Errorf("Expected specific permissions, got %v", deployJob.Permissions)
	}

	// Test github-token
	if deployJob.GitHubToken != "${{ secrets.CUSTOM_TOKEN }}" {
		t.Errorf("Expected github-token to be '${{ secrets.CUSTOM_TOKEN }}', got %s", deployJob.GitHubToken)
	}

	// Test inputs
	if len(deployJob.Inputs) != 2 {
		t.Fatalf("Expected 2 inputs, got %d", len(deployJob.Inputs))
	}

	envInput, exists := deployJob.Inputs["environment"]
	if !exists {
		t.Fatal("Expected 'environment' input to exist")
	}

	if envInput.Description != "Target deployment environment" {
		t.Errorf("Expected environment input description, got %s", envInput.Description)
	}

	if !envInput.Required {
		t.Error("Expected environment input to be required")
	}

	if envInput.Type != "choice" {
		t.Errorf("Expected environment input type to be 'choice', got %s", envInput.Type)
	}

	if len(envInput.Options) != 2 || envInput.Options[0] != "staging" || envInput.Options[1] != "production" {
		t.Errorf("Expected environment input options to be ['staging', 'production'], got %v", envInput.Options)
	}

	forceInput, exists := deployJob.Inputs["force"]
	if !exists {
		t.Fatal("Expected 'force' input to exist")
	}

	if forceInput.Required {
		t.Error("Expected force input to not be required")
	}

	if forceInput.Type != "boolean" {
		t.Errorf("Expected force input type to be 'boolean', got %s", forceInput.Type)
	}

	if forceInput.Default != "false" {
		t.Errorf("Expected force input default to be 'false', got %s", forceInput.Default)
	}

	// Test steps
	if len(deployJob.Steps) != 1 {
		t.Fatalf("Expected 1 step, got %d", len(deployJob.Steps))
	}
}

func TestBuildSafeJobsDefaultsEmptyRunsOn(t *testing.T) {
	for name, runsOn := range map[string]any{
		"empty scalar": "",
		"empty array":  []any{},
		"empty label":  []any{""},
	} {
		t.Run(name, func(t *testing.T) {
			c := NewCompiler()
			safeJobs := c.parseSafeJobsConfig(map[string]any{
				"notify": map[string]any{"runs-on": runsOn},
			})
			_, err := c.buildSafeJobs(&WorkflowData{
				Name:        "test-workflow",
				SafeOutputs: &SafeOutputsConfig{Jobs: safeJobs},
			}, false)

			require.NoError(t, err)
			job, exists := c.jobManager.GetJob("notify")
			require.True(t, exists)
			require.Equal(t, "runs-on: ubuntu-latest", job.RunsOn)
		})
	}
}

func TestParseSafeJobsConfigMax(t *testing.T) {
	c := NewCompiler()

	jobsMap := map[string]any{
		"emit-finding": map[string]any{
			"description": "Emit a quality finding",
			"max":         float64(25), // YAML numbers are float64 when parsed generically
			"inputs": map[string]any{
				"message": map[string]any{
					"description": "The finding message",
					"required":    true,
					"type":        "string",
				},
			},
		},
		"single-output": map[string]any{
			"description": "Default max (no max field)",
			"inputs": map[string]any{
				"body": map[string]any{
					"type": "string",
				},
			},
		},
	}

	result := c.parseSafeJobsConfig(jobsMap)

	if result == nil {
		t.Fatal("Expected safe-jobs config to be parsed, got nil")
	}

	finding := result["emit-finding"]
	if finding == nil {
		t.Fatal("Expected 'emit-finding' job to exist")
	}
	if finding.Max != 25 {
		t.Errorf("Expected Max to be 25, got %d", finding.Max)
	}

	single := result["single-output"]
	if single == nil {
		t.Fatal("Expected 'single-output' job to exist")
	}
	if single.Max != 0 {
		t.Errorf("Expected Max to be 0 (unset) when not specified, got %d", single.Max)
	}
}

func TestParseSafeJobsConfigMaxInvalidIgnored(t *testing.T) {
	c := NewCompiler()

	jobsMap := map[string]any{
		"bad-max": map[string]any{
			"max": float64(-5), // negative — should be ignored
			"inputs": map[string]any{
				"x": map[string]any{"type": "string"},
			},
		},
		"zero-max": map[string]any{
			"max": float64(0), // zero — should be ignored
			"inputs": map[string]any{
				"x": map[string]any{"type": "string"},
			},
		},
	}

	result := c.parseSafeJobsConfig(jobsMap)

	if result["bad-max"].Max != 0 {
		t.Errorf("Expected negative max to be ignored (Max=0), got %d", result["bad-max"].Max)
	}
	if result["zero-max"].Max != 0 {
		t.Errorf("Expected zero max to be ignored (Max=0), got %d", result["zero-max"].Max)
	}
}

func TestBuildSafeJobs(t *testing.T) {
	c := NewCompiler()

	workflowData := &WorkflowData{
		Name: "test-workflow",
		SafeOutputs: &SafeOutputsConfig{
			Jobs: map[string]*SafeJobConfig{
				"deploy": {
					RunsOn: "runs-on: ubuntu-latest",
					If:     "github.event.issue.number",
					Env: map[string]string{
						"DEPLOY_ENV": "production",
					},
					Inputs: map[string]*InputDefinition{
						"environment": {
							Description: "Target deployment environment",
							Required:    true,
							Type:        "choice",
							Options:     []string{"staging", "production"},
						},
					},
					Steps: []any{
						map[string]any{
							"name": "Deploy",
							"run":  "echo 'Deploying'",
						},
					},
				},
			},
			Env: map[string]string{
				"GLOBAL_VAR": "global_value",
			},
		},
	}

	_, err := c.buildSafeJobs(workflowData, false)
	if err != nil {
		t.Fatalf("Unexpected error building safe jobs: %v", err)
	}

	jobs := c.jobManager.GetAllJobs()
	if len(jobs) != 1 {
		t.Fatalf("Expected 1 job to be created, got %d", len(jobs))
	}

	var job *Job
	for _, j := range jobs {
		job = j
		break
	}

	// Check job name
	if job.Name != "deploy" {
		t.Errorf("Expected job name to be 'deploy', got %s", job.Name)
	}

	// Check dependencies - should include agent job and any additional needs
	expectedNeeds := []string{"agent"}
	if len(job.Needs) != len(expectedNeeds) {
		t.Errorf("Expected needs %v, got %v", expectedNeeds, job.Needs)
	}

	// Check if condition - should now combine safe output type check with user condition
	expectedIf := "(!cancelled()) && needs.agent.result != 'skipped' && contains(needs.agent.outputs.output_types, 'deploy') && (github.event.issue.number)"
	if job.If != expectedIf {
		t.Errorf("Expected if condition to be '%s', got '%s'", expectedIf, job.If)
	}

	// Check runs-on
	if job.RunsOn != "runs-on: ubuntu-latest" {
		t.Errorf("Expected runs-on to be 'runs-on: ubuntu-latest', got %s", job.RunsOn)
	}

	// Check that steps were generated
	if len(job.Steps) == 0 {
		t.Error("Expected steps to be generated")
	}

	// Check that environment setup step is created but no longer includes input variables
	stepsContent := strings.Join(job.Steps, "")
	if strings.Contains(stepsContent, "GH_AW_SAFE_JOB_ENVIRONMENT") {
		t.Error("Input-specific environment variables should no longer be set (inputs should be processed from agent output via jq)")
	}

	if !strings.Contains(stepsContent, "GH_AW_AGENT_OUTPUT") {
		t.Error("Expected main job output to be available as environment variable")
	}

	if strings.Contains(stepsContent, "Configure Safe Outputs Job Environment Variables") {
		t.Error("Configure Safe Outputs Job Environment Variables step should have been removed")
	}

	if strings.Contains(stepsContent, "GLOBAL_VAR=global_value") {
		t.Error("Safe-jobs should not inherit environment variables from safe-outputs.env (they are now independent)")
	}
}

func TestCompileWorkflowConclusionNeedsSafeJobsDeterministically(t *testing.T) {
	tmpDir := testutil.TempDir(t, "safe-jobs-ordering")

	testContent := `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
engine: copilot
safe-outputs:
  jobs:
    zebra-job:
      steps:
        - name: Zebra
          run: echo "zebra"
    alpha-job:
      steps:
        - name: Alpha
          run: echo "alpha"
---

# Test deterministic safe-job ordering
`

	testFile := filepath.Join(tmpDir, "test-safe-jobs-ordering.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0o644))

	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(testFile))

	lockFile := filepath.Join(tmpDir, "test-safe-jobs-ordering.lock.yml")
	compiled, err := os.ReadFile(lockFile)
	require.NoError(t, err)

	conclusionSection := extractJobSection(string(compiled), "conclusion")
	require.NotEmpty(t, conclusionSection)

	alphaIdx := indexInNonCommentLines(conclusionSection, "- alpha_job")
	zebraIdx := indexInNonCommentLines(conclusionSection, "- zebra_job")
	require.NotEqual(t, -1, alphaIdx, "conclusion job should depend on alpha_job")
	require.NotEqual(t, -1, zebraIdx, "conclusion job should depend on zebra_job")
	require.Less(t, alphaIdx, zebraIdx, "conclusion job should list safe-jobs in deterministic sorted order")
}

func TestParseAndBuildSafeJobsRunsOnList(t *testing.T) {
	c := NewCompiler()

	safeJobs := c.parseSafeJobsConfig(map[string]any{
		"deploy": map[string]any{
			"runs-on": []any{"self-hosted", "linux"},
			"steps": []any{
				map[string]any{"run": "echo 'Deploying'"},
			},
		},
	})

	require.Equal(t, "runs-on:\n      - self-hosted\n      - linux", safeJobs["deploy"].RunsOn)

	workflowData := &WorkflowData{
		Name:        "test-workflow",
		SafeOutputs: &SafeOutputsConfig{Jobs: safeJobs},
	}

	_, err := c.buildSafeJobs(workflowData, false)
	require.NoError(t, err)

	jobs := c.jobManager.GetAllJobs()
	require.Len(t, jobs, 1)

	var job *Job
	for _, j := range jobs {
		job = j
		break
	}

	// Multiple labels parsed as a list are rendered as a YAML list.
	require.Equal(t, "runs-on:\n      - self-hosted\n      - linux", job.RunsOn)
}

func TestParseAndBuildSafeJobsRunsOnObject(t *testing.T) {
	tests := []struct {
		name     string
		runsOn   map[string]any
		expected string
	}{
		{
			name:     "group only",
			runsOn:   map[string]any{"group": "safe-job-runners"},
			expected: "runs-on:\n      group: safe-job-runners",
		},
		{
			name: "group and labels",
			runsOn: map[string]any{
				"group":  "safe-job-runners",
				"labels": []any{"linux", "x64"},
			},
			expected: "runs-on:\n      group: safe-job-runners\n      labels:\n      - linux\n      - x64",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewCompiler()
			safeJobs := c.parseSafeJobsConfig(map[string]any{
				"deploy": map[string]any{
					"runs-on": tt.runsOn,
					"steps":   []any{map[string]any{"run": "echo 'Deploying'"}},
				},
			})

			_, err := c.buildSafeJobs(&WorkflowData{
				Name:        "test-workflow",
				SafeOutputs: &SafeOutputsConfig{Jobs: safeJobs},
			}, false)
			require.NoError(t, err)

			jobs := c.jobManager.GetAllJobs()
			require.Len(t, jobs, 1)
			for _, job := range jobs {
				require.Equal(t, tt.expected, job.RunsOn)
			}
		})
	}
}

func TestBuildSafeJobsRejectsEmptyRunsOnObject(t *testing.T) {
	c := NewCompiler()
	safeJobs := c.parseSafeJobsConfig(map[string]any{
		"deploy": map[string]any{
			"runs-on": map[string]any{},
			"steps":   []any{map[string]any{"run": "echo 'Deploying'"}},
		},
	})

	_, err := c.buildSafeJobs(&WorkflowData{
		Name:        "test-workflow",
		SafeOutputs: &SafeOutputsConfig{Jobs: safeJobs},
	}, false)
	require.ErrorContains(t, err, "invalid runs-on for safe-job 'deploy'")
	require.ErrorContains(t, err, "runs-on object is empty")
}

func TestCompileSafeJobRunsOnObject(t *testing.T) {
	tmpDir := testutil.TempDir(t, "safe-job-runs-on-object")
	workflowPath := filepath.Join(tmpDir, "safe-job-runs-on-object.md")
	content := `---
on: workflow_dispatch
permissions: read-all
engine: copilot
safe-outputs:
  jobs:
    notify:
      runs-on:
        group: safe-job-runners
        labels: [linux]
      inputs:
        message:
          description: Message
      steps:
        - run: echo hi
---

# Test
`
	require.NoError(t, os.WriteFile(workflowPath, []byte(content), 0o644))

	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(workflowPath))

	compiled, err := os.ReadFile(filepath.Join(tmpDir, "safe-job-runs-on-object.lock.yml"))
	require.NoError(t, err)
	notifyJob := extractJobSection(string(compiled), "notify")
	require.Contains(t, notifyJob, "    runs-on:\n      group: safe-job-runners\n      labels:\n      - linux")
}

func TestParseAndBuildSafeJobsSingleRunsOnList(t *testing.T) {
	c := NewCompiler()

	safeJobs := c.parseSafeJobsConfig(map[string]any{
		"deploy": map[string]any{
			"runs-on": []any{"self-hosted"},
			"steps": []any{
				map[string]any{"run": "echo 'Deploying'"},
			},
		},
	})

	require.Equal(t, "runs-on:\n      - self-hosted", safeJobs["deploy"].RunsOn)

	workflowData := &WorkflowData{
		Name:        "test-workflow",
		SafeOutputs: &SafeOutputsConfig{Jobs: safeJobs},
	}

	_, err := c.buildSafeJobs(workflowData, false)
	require.NoError(t, err)

	jobs := c.jobManager.GetAllJobs()
	require.Len(t, jobs, 1)

	var job *Job
	for _, j := range jobs {
		job = j
		break
	}

	require.Equal(t, "runs-on:\n      - self-hosted", job.RunsOn)
}

func TestBuildSafeJobsWithNoConfiguration(t *testing.T) {
	c := NewCompiler()

	// Test with no SafeJobs
	workflowData := &WorkflowData{
		Name: "test-workflow",
	}

	_, err := c.buildSafeJobs(workflowData, false)
	if err != nil {
		t.Errorf("Expected no error with no safe-jobs, got %v", err)
	}

	// Test with empty SafeOutputs.Jobs
	workflowData.SafeOutputs = &SafeOutputsConfig{
		Jobs: map[string]*SafeJobConfig{},
	}

	_, err = c.buildSafeJobs(workflowData, false)
	if err != nil {
		t.Errorf("Expected no error with empty safe-jobs, got %v", err)
	}

	jobs := c.jobManager.GetAllJobs()
	if len(jobs) != 0 {
		t.Errorf("Expected no jobs to be created, got %d", len(jobs))
	}
}

func TestBuildSafeJobsWithoutCustomIfCondition(t *testing.T) {
	c := NewCompiler()

	workflowData := &WorkflowData{
		Name: "test-workflow",
		SafeOutputs: &SafeOutputsConfig{
			Jobs: map[string]*SafeJobConfig{
				"notify": {
					RunsOn: "runs-on: ubuntu-latest",
					// No custom 'if' condition
					Inputs: map[string]*InputDefinition{
						"message": {
							Description: "Message to send",
							Required:    true,
							Type:        "string",
						},
					},
					Steps: []any{
						map[string]any{
							"name": "Send notification",
							"run":  "echo 'Sending notification'",
						},
					},
				},
			},
		},
	}

	_, err := c.buildSafeJobs(workflowData, false)
	if err != nil {
		t.Fatalf("Unexpected error building safe jobs: %v", err)
	}

	jobs := c.jobManager.GetAllJobs()
	if len(jobs) != 1 {
		t.Fatalf("Expected 1 job to be created, got %d", len(jobs))
	}

	var job *Job
	for _, j := range jobs {
		job = j
		break
	}

	// Check if condition - should only have safe output type check (no custom condition)
	expectedIf := "(!cancelled()) && needs.agent.result != 'skipped' && contains(needs.agent.outputs.output_types, 'notify')"
	if job.If != expectedIf {
		t.Errorf("Expected if condition to be '%s', got '%s'", expectedIf, job.If)
	}
}

func TestBuildSafeJobsWithDashesInName(t *testing.T) {
	c := NewCompiler()

	workflowData := &WorkflowData{
		Name: "test-workflow",
		SafeOutputs: &SafeOutputsConfig{
			Jobs: map[string]*SafeJobConfig{
				"send-notification": {
					RunsOn: "runs-on: ubuntu-latest",
					Steps: []any{
						map[string]any{
							"name": "Send notification",
							"run":  "echo 'Sending notification'",
						},
					},
				},
			},
		},
	}

	_, err := c.buildSafeJobs(workflowData, false)
	if err != nil {
		t.Fatalf("Unexpected error building safe jobs: %v", err)
	}

	jobs := c.jobManager.GetAllJobs()
	if len(jobs) != 1 {
		t.Fatalf("Expected 1 job to be created, got %d", len(jobs))
	}

	var job *Job
	for _, j := range jobs {
		job = j
		break
	}

	// Job name should be normalized to underscores
	if job.Name != "send_notification" {
		t.Errorf("Expected job name to be 'send_notification', got '%s'", job.Name)
	}

	// Check if condition - should check for underscore version in output_types
	expectedIf := "(!cancelled()) && needs.agent.result != 'skipped' && contains(needs.agent.outputs.output_types, 'send_notification')"
	if job.If != expectedIf {
		t.Errorf("Expected if condition to be '%s', got '%s'", expectedIf, job.If)
	}
}

func TestSafeJobsInSafeOutputsConfig(t *testing.T) {
	workflowData := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			Jobs: map[string]*SafeJobConfig{
				"deploy": {
					Inputs: map[string]*InputDefinition{
						"environment": {
							Description: "Target deployment environment",
							Required:    true,
							Type:        "choice",
							Options:     []string{"staging", "production"},
						},
					},
				},
				"notify": {
					Inputs: map[string]*InputDefinition{
						"message": {
							Description: "Notification message",
							Required:    false,
							Type:        "string",
							Default:     "Deployment completed",
						},
					},
				},
			},
		},
	}

	configJSON, err := generateSafeOutputsConfig(workflowData)
	require.NoError(t, err, "generateSafeOutputsConfig should not return an error")

	if configJSON == "" {
		t.Fatal("Expected safe-outputs config JSON to be generated")
	}

	// Should contain both safe jobs
	if !strings.Contains(configJSON, "deploy") {
		t.Error("Expected config to contain 'deploy' job")
	}

	if !strings.Contains(configJSON, "notify") {
		t.Error("Expected config to contain 'notify' job")
	}

	// Should contain input definitions
	if !strings.Contains(configJSON, "environment") {
		t.Error("Expected config to contain 'environment' input")
	}

	if !strings.Contains(configJSON, "message") {
		t.Error("Expected config to contain 'message' input")
	}
}

func TestExtractSafeJobsFromFrontmatter(t *testing.T) {
	frontmatter := map[string]any{
		"safe-outputs": map[string]any{
			"jobs": map[string]any{
				"deploy": map[string]any{
					"runs-on": "ubuntu-latest",
					"inputs": map[string]any{
						"environment": map[string]any{
							"description": "Target environment",
							"required":    true,
							"type":        "choice",
							"options":     []any{"staging", "production"},
						},
					},
				},
			},
		},
	}

	result := extractSafeJobsFromFrontmatter(frontmatter)

	if len(result) != 1 {
		t.Errorf("Expected 1 safe-job, got %d", len(result))
	}

	deployJob, exists := result["deploy"]
	if !exists {
		t.Error("Expected 'deploy' job to exist")
	}

	if deployJob.RunsOn != "runs-on: ubuntu-latest" {
		t.Errorf("Expected runs-on to be 'ubuntu-latest', got '%v'", deployJob.RunsOn)
	}
}

func TestMergeSafeJobs(t *testing.T) {
	base := map[string]*SafeJobConfig{
		"deploy": {
			RunsOn: "runs-on: ubuntu-latest",
		},
	}

	additional := map[string]*SafeJobConfig{
		"test": {
			RunsOn: "runs-on: ubuntu-latest",
		},
	}

	// Test successful merge
	result, err := mergeSafeJobs(base, additional)
	if err != nil {
		t.Errorf("Expected no error merging safe-jobs, got %v", err)
	}

	if len(result) != 2 {
		t.Errorf("Expected 2 safe-jobs after merge, got %d", len(result))
	}

	// Test conflict detection
	conflicting := map[string]*SafeJobConfig{
		"deploy": {
			RunsOn: "runs-on: windows-latest",
		},
	}

	_, err = mergeSafeJobs(base, conflicting)
	if err == nil {
		t.Error("Expected error when merging conflicting safe-job names")
	}

	if !strings.Contains(err.Error(), "safe-job name conflict") {
		t.Errorf("Expected conflict error message, got '%s'", err.Error())
	}
}

// TestMergeSafeJobsFromIncludedConfigs tests merging safe-jobs from included safe-outputs configurations
func TestMergeSafeJobsFromIncludedConfigs(t *testing.T) {
	c := NewCompiler()

	// Top-level safe-jobs
	topSafeJobs := map[string]*SafeJobConfig{
		"deploy": {
			Name:   "Deploy Application",
			RunsOn: "runs-on: ubuntu-latest",
		},
	}

	// Simulate included safe-outputs configurations (as returned by ExpandIncludesForSafeOutputs)
	includedConfigs := []string{
		`{
"jobs": {
"test": {
"runs-on": "ubuntu-latest",
"inputs": {
"suite": {
"description": "Test suite to run",
"required": true,
"type": "string"
}
}
}
}
}`,
		`{
"jobs": {
"notify": {
"runs-on": "ubuntu-latest",
"output": "Notification sent",
"inputs": {
"message": {
"description": "Notification message",
"required": true,
"type": "string"
}
}
}
}
}`,
	}

	result, err := c.mergeSafeJobsFromIncludedConfigs(topSafeJobs, includedConfigs)
	if err != nil {
		t.Errorf("Expected no error merging from included configs, got %v", err)
	}

	if len(result) != 3 {
		t.Errorf("Expected 3 safe-jobs after merge, got %d", len(result))
	}

	testJob, exists := result["test"]
	if !exists {
		t.Error("Expected 'test' job from includes to exist")
	}

	if testJob.RunsOn != "runs-on: ubuntu-latest" {
		t.Errorf("Expected test job runs-on to be 'ubuntu-latest', got '%v'", testJob.RunsOn)
	}

	notifyJob, exists := result["notify"]
	if !exists {
		t.Error("Expected 'notify' job from includes to exist")
	}

	if notifyJob.Output != "Notification sent" {
		t.Errorf("Expected notify job output to be 'Notification sent', got '%s'", notifyJob.Output)
	}

	// Test conflict detection
	conflictingConfigs := []string{
		`{
"jobs": {
"deploy": {
"runs-on": "windows-latest"
}
}
}`,
	}

	_, err = c.mergeSafeJobsFromIncludedConfigs(topSafeJobs, conflictingConfigs)
	if err == nil {
		t.Error("Expected error when merging conflicting safe-job from included configs")
	}

	if !strings.Contains(err.Error(), "safe-job name conflict") {
		t.Errorf("Expected conflict error message, got '%s'", err.Error())
	}
}

// TestBuildSafeJobsEnvExpressionHoisting verifies that no env vars — whether expression-based
// or literal — are ever written to $GITHUB_OUTPUT, and that all values are injected directly
// into each downstream step's env: block.
func TestBuildSafeJobsEnvExpressionHoisting(t *testing.T) {
	c := NewCompiler()

	workflowData := &WorkflowData{
		Name: "test-workflow",
		SafeOutputs: &SafeOutputsConfig{
			Jobs: map[string]*SafeJobConfig{
				"publish": {
					RunsOn: "runs-on: ubuntu-latest",
					Env: map[string]string{
						"GH_TOKEN":   "${{ github.token }}",
						"STATIC_VAR": "literal-value",
					},
					Steps: []any{
						map[string]any{
							"name": "Publish",
							"run":  "echo 'Publishing'",
						},
					},
				},
			},
		},
	}

	_, err := c.buildSafeJobs(workflowData, false)
	if err != nil {
		t.Fatalf("Unexpected error building safe jobs: %v", err)
	}

	jobs := c.jobManager.GetAllJobs()
	if len(jobs) != 1 {
		t.Fatalf("Expected 1 job to be created, got %d", len(jobs))
	}

	var job *Job
	for _, j := range jobs {
		job = j
		break
	}

	stepsContent := strings.Join(job.Steps, "")

	// Neither expression-based nor literal env vars must ever be written to $GITHUB_OUTPUT.
	if strings.Contains(stepsContent, `GITHUB_OUTPUT`) {
		t.Error("No env var must ever be written to $GITHUB_OUTPUT in safe-jobs (secrets must not be stored in outputs)")
	}

	// Expression-based values must be injected directly into downstream step env: blocks.
	if !strings.Contains(stepsContent, "GH_TOKEN: ${{ github.token }}") {
		t.Error("Expected GH_TOKEN expression to be injected directly into a downstream step env: block")
	}

	// Literal values must also be injected directly into downstream step env: blocks.
	if !strings.Contains(stepsContent, "STATIC_VAR: literal-value") {
		t.Error("Expected literal env var STATIC_VAR to be injected directly into a downstream step env: block")
	}
}

// TestSafeJobsInputTypes tests that safe-jobs inputs support all input types
// and share the same InputDefinition type with workflow_dispatch inputs
func TestSafeJobsInputTypes(t *testing.T) {
	c := NewCompiler()

	jobsMap := map[string]any{
		"test-job": map[string]any{
			"runs-on": "ubuntu-latest",
			"inputs": map[string]any{
				"message": map[string]any{
					"description": "String input",
					"type":        "string",
					"default":     "Hello World",
					"required":    true,
				},
				"debug": map[string]any{
					"description": "Boolean input",
					"type":        "boolean",
					"default":     false,
					"required":    false,
				},
				"count": map[string]any{
					"description": "Number input",
					"type":        "number",
					"default":     100,
					"required":    true,
				},
				"environment": map[string]any{
					"description": "Choice input",
					"type":        "choice",
					"default":     "staging",
					"options":     []any{"dev", "staging", "prod"},
				},
				"deploy_env": map[string]any{
					"description": "Environment input",
					"type":        "environment",
					"required":    false,
				},
			},
			"steps": []any{
				map[string]any{
					"name": "Test step",
					"run":  "echo 'Testing inputs'",
				},
			},
		},
	}

	result := c.parseSafeJobsConfig(jobsMap)

	if result == nil {
		t.Fatal("Expected safe-jobs config to be parsed, got nil")
	}

	job, exists := result["test-job"]
	if !exists {
		t.Fatal("Expected 'test-job' to exist")
	}

	if len(job.Inputs) != 5 {
		t.Fatalf("Expected 5 inputs, got %d", len(job.Inputs))
	}

	// Test string input
	stringInput := job.Inputs["message"]
	if stringInput == nil {
		t.Fatal("Expected 'message' input to exist")
	}
	if stringInput.Type != "string" {
		t.Errorf("Expected type 'string', got %s", stringInput.Type)
	}
	if stringInput.Default != "Hello World" {
		t.Errorf("Expected default 'Hello World', got %v", stringInput.Default)
	}

	// Test boolean input
	boolInput := job.Inputs["debug"]
	if boolInput == nil {
		t.Fatal("Expected 'debug' input to exist")
	}
	if boolInput.Type != "boolean" {
		t.Errorf("Expected type 'boolean', got %s", boolInput.Type)
	}
	if boolInput.Default != false {
		t.Errorf("Expected default false, got %v", boolInput.Default)
	}

	// Test number input
	numberInput := job.Inputs["count"]
	if numberInput == nil {
		t.Fatal("Expected 'count' input to exist")
	}
	if numberInput.Type != "number" {
		t.Errorf("Expected type 'number', got %s", numberInput.Type)
	}
	// Note: YAML/JSON may parse numbers as int or float64
	switch v := numberInput.Default.(type) {
	case int:
		if v != 100 {
			t.Errorf("Expected default 100, got %d", v)
		}
	case float64:
		if v != 100.0 {
			t.Errorf("Expected default 100, got %f", v)
		}
	default:
		t.Errorf("Expected default to be numeric, got %T: %v", numberInput.Default, numberInput.Default)
	}

	// Test choice input
	choiceInput := job.Inputs["environment"]
	if choiceInput == nil {
		t.Fatal("Expected 'environment' input to exist")
	}
	if choiceInput.Type != "choice" {
		t.Errorf("Expected type 'choice', got %s", choiceInput.Type)
	}
	if len(choiceInput.Options) != 3 {
		t.Errorf("Expected 3 options, got %d", len(choiceInput.Options))
	}
	if choiceInput.Default != "staging" {
		t.Errorf("Expected default 'staging', got %v", choiceInput.Default)
	}

	// Test environment input
	envInput := job.Inputs["deploy_env"]
	if envInput == nil {
		t.Fatal("Expected 'deploy_env' input to exist")
	}
	if envInput.Type != "environment" {
		t.Errorf("Expected type 'environment', got %s", envInput.Type)
	}
}

func TestMergeSafeJobsFromIncludedConfigsRejectsMacOSRunner(t *testing.T) {
	c := NewCompiler()

	_, err := c.mergeSafeJobsFromIncludedConfigs(nil, []string{`{
"jobs": {
"notify": {
"runs-on": "macos-latest"
}
}
}`})

	require.ErrorContains(t, err, "safe-outputs.jobs.notify.runs-on")
}

func TestParseSafeJobsConfigArtifacts(t *testing.T) {
	c := NewCompiler()

	t.Run("valid artifacts are parsed", func(t *testing.T) {
		result := c.parseSafeJobsConfig(map[string]any{
			"publish": map[string]any{
				"artifacts": []any{
					"/tmp/gh-aw/agent/review-bundles/",
					"/tmp/gh-aw/agent/review-bundles/*.json",
				},
			},
		})

		job, exists := result["publish"]
		require.True(t, exists, "Expected 'publish' job to exist")
		require.Equal(t, []string{
			"/tmp/gh-aw/agent/review-bundles/",
			"/tmp/gh-aw/agent/review-bundles/*.json",
		}, job.Artifacts)
		require.NoError(t, job.artifactsError)
	})

	t.Run("path outside /tmp/gh-aw/ is rejected", func(t *testing.T) {
		result := c.parseSafeJobsConfig(map[string]any{
			"publish": map[string]any{
				"artifacts": []any{"/tmp/other/path"},
			},
		})

		job, exists := result["publish"]
		require.True(t, exists)
		require.Error(t, job.artifactsError)
		require.ErrorContains(t, job.artifactsError, "/tmp/gh-aw/")
	})

	t.Run("non-string entry is rejected", func(t *testing.T) {
		result := c.parseSafeJobsConfig(map[string]any{
			"publish": map[string]any{
				"artifacts": []any{123},
			},
		})

		job, exists := result["publish"]
		require.True(t, exists)
		require.Error(t, job.artifactsError)
	})

	t.Run("path traversal segments are rejected", func(t *testing.T) {
		result := c.parseSafeJobsConfig(map[string]any{
			"publish": map[string]any{
				"artifacts": []any{"/tmp/gh-aw/../outside.json"},
			},
		})

		job, exists := result["publish"]
		require.True(t, exists)
		require.Error(t, job.artifactsError)
		require.ErrorContains(t, job.artifactsError, "..")
	})

	t.Run("GitHub Actions expressions are rejected", func(t *testing.T) {
		result := c.parseSafeJobsConfig(map[string]any{
			"publish": map[string]any{
				"artifacts": []any{"/tmp/gh-aw/${{ '..' }}/outside.json"},
			},
		})

		job, exists := result["publish"]
		require.True(t, exists)
		require.Error(t, job.artifactsError)
		require.ErrorContains(t, job.artifactsError, "must be literal")
	})

	t.Run("control characters are rejected", func(t *testing.T) {
		result := c.parseSafeJobsConfig(map[string]any{
			"publish": map[string]any{
				"artifacts": []any{"/tmp/gh-aw/agent/output.json\n/tmp/outside.json"},
			},
		})

		job, exists := result["publish"]
		require.True(t, exists)
		require.Error(t, job.artifactsError)
		require.ErrorContains(t, job.artifactsError, "control characters")
	})

	t.Run("double dots within a filename are accepted", func(t *testing.T) {
		result := c.parseSafeJobsConfig(map[string]any{
			"publish": map[string]any{
				"artifacts": []any{"/tmp/gh-aw/agent/bundle..json"},
			},
		})

		job, exists := result["publish"]
		require.True(t, exists)
		require.NoError(t, job.artifactsError)
	})

	t.Run("hidden file path is rejected", func(t *testing.T) {
		result := c.parseSafeJobsConfig(map[string]any{
			"publish": map[string]any{
				"artifacts": []any{"/tmp/gh-aw/agent/bundle/.manifest"},
			},
		})

		job, exists := result["publish"]
		require.True(t, exists)
		require.Error(t, job.artifactsError)
		require.ErrorContains(t, job.artifactsError, "hidden")
	})

	t.Run("hidden directory path is rejected", func(t *testing.T) {
		result := c.parseSafeJobsConfig(map[string]any{
			"publish": map[string]any{
				"artifacts": []any{"/tmp/gh-aw/agent/.cache/data.json"},
			},
		})

		job, exists := result["publish"]
		require.True(t, exists)
		require.Error(t, job.artifactsError)
		require.ErrorContains(t, job.artifactsError, "hidden")
	})

	t.Run("unscannable path is rejected", func(t *testing.T) {
		result := c.parseSafeJobsConfig(map[string]any{
			"publish": map[string]any{
				"artifacts": []any{"/tmp/gh-aw/agent/review-bundle-data"},
			},
		})

		job, exists := result["publish"]
		require.True(t, exists)
		require.Error(t, job.artifactsError)
		require.ErrorContains(t, job.artifactsError, "secret redaction")
	})
}

func TestBuildSafeJobsRejectsInvalidArtifacts(t *testing.T) {
	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			Jobs: map[string]*SafeJobConfig{
				"publish": {
					Inputs: map[string]*InputDefinition{
						"message": {Description: "msg", Type: "string"},
					},
				},
			},
		},
	}
	data.SafeOutputs.Jobs["publish"].artifactsError = errors.New("bad artifact")

	c := NewCompiler()
	_, err := c.buildSafeJobs(data, false)
	require.Error(t, err)
	require.ErrorContains(t, err, "invalid artifacts for safe-job 'publish'")
}

func TestCollectSafeJobArtifactPaths(t *testing.T) {
	paths := collectSafeJobArtifactPaths(map[string]*SafeJobConfig{
		"zeta": {Artifacts: []string{"/tmp/gh-aw/agent/zeta/"}},
		"alpha": {Artifacts: []string{
			"/tmp/gh-aw/agent/alpha/",
			"/tmp/gh-aw/agent/alpha/extra.json",
		}},
	})

	require.Equal(t, []string{
		"/tmp/gh-aw/agent/alpha/**/*.txt",
		"/tmp/gh-aw/agent/alpha/**/*.json",
		"/tmp/gh-aw/agent/alpha/**/*.log",
		"/tmp/gh-aw/agent/alpha/**/*.md",
		"/tmp/gh-aw/agent/alpha/**/*.mdx",
		"/tmp/gh-aw/agent/alpha/**/*.yml",
		"/tmp/gh-aw/agent/alpha/**/*.jsonl",
		"/tmp/gh-aw/agent/alpha/**/*.patch",
		"/tmp/gh-aw/agent/alpha/extra.json",
		"/tmp/gh-aw/agent/zeta/**/*.txt",
		"/tmp/gh-aw/agent/zeta/**/*.json",
		"/tmp/gh-aw/agent/zeta/**/*.log",
		"/tmp/gh-aw/agent/zeta/**/*.md",
		"/tmp/gh-aw/agent/zeta/**/*.mdx",
		"/tmp/gh-aw/agent/zeta/**/*.yml",
		"/tmp/gh-aw/agent/zeta/**/*.jsonl",
		"/tmp/gh-aw/agent/zeta/**/*.patch",
	}, paths)
}

func TestCompileSafeJobArtifactsIncludedInAgentUpload(t *testing.T) {
	tmpDir := testutil.TempDir(t, "safe-job-artifacts")
	workflowPath := filepath.Join(tmpDir, "safe-job-artifacts.md")
	content := `---
on: workflow_dispatch
permissions: read-all
engine: copilot
safe-outputs:
  jobs:
    publish-review-bundle:
      description: "Publish a prepared directory as a workflow artifact"
      runs-on: ubuntu-latest
      artifacts:
        - /tmp/gh-aw/agent/review-bundles/
      inputs:
        source_dir:
          description: "Directory containing prepared files"
          required: true
          type: string
      steps:
        - run: echo hi
---

# Test
`
	require.NoError(t, os.WriteFile(workflowPath, []byte(content), 0o644))

	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(workflowPath))

	compiled, err := os.ReadFile(filepath.Join(tmpDir, "safe-job-artifacts.lock.yml"))
	require.NoError(t, err)
	agentJob := extractJobSection(string(compiled), "agent")
	require.Contains(t, agentJob, "/tmp/gh-aw/agent/review-bundles/**/*.json")
	require.NotContains(t, agentJob, "\n            /tmp/gh-aw/agent/review-bundles/\n")
}
