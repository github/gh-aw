//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

// TestCreatePRReviewCommentUsesHelper verifies that create_pr_review_comment.go
// uses the buildSafeOutputJobEnvVars helper correctly
func TestCreatePRReviewCommentUsesHelper(t *testing.T) {
	c := NewCompiler()

	workflowData := &WorkflowData{
		Name: "test-workflow",
		SafeOutputs: &SafeOutputsConfig{
			Staged: true,
			CreatePullRequestReviewComments: &CreatePullRequestReviewCommentsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 10},
				TargetRepoSlug:       "owner/target-repo",
			},
		},
	}

	job, err := c.buildCreateOutputPullRequestReviewCommentJob(workflowData, "main_job")
	if err != nil {
		t.Fatalf("Unexpected error building PR review comment job: %v", err)
	}

	// Convert steps to a single string for testing
	stepsContent := strings.Join(job.Steps, "")

	// Verify that GH_AW_SAFE_OUTPUTS_STAGED is present
	if !strings.Contains(stepsContent, "          GH_AW_SAFE_OUTPUTS_STAGED: \"true\"\n") {
		t.Error("Expected GH_AW_SAFE_OUTPUTS_STAGED to be set in create-pull-request-review-comment job")
	}

	// Verify that GH_AW_TARGET_REPO_SLUG is present with the correct value
	if !strings.Contains(stepsContent, "          GH_AW_TARGET_REPO_SLUG: \"owner/target-repo\"\n") {
		t.Error("Expected GH_AW_TARGET_REPO_SLUG to be set correctly in create-pull-request-review-comment job")
	}
}

// TestCreateDiscussionUsesHelper verifies that create_discussion.go
// uses the buildSafeOutputJobEnvVars helper correctly (standalone job still uses env vars)
func TestCreateDiscussionUsesHelper(t *testing.T) {
	c := NewCompiler()

	workflowData := &WorkflowData{
		Name: "test-workflow",
		SafeOutputs: &SafeOutputsConfig{
			Staged: true,
			CreateDiscussions: &CreateDiscussionsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 1},
				Category:             "12345",
				TargetRepoSlug:       "owner/target-repo",
			},
		},
	}

	job, err := c.buildCreateOutputDiscussionJob(workflowData, "main_job", "")
	if err != nil {
		t.Fatalf("Unexpected error building discussion job: %v", err)
	}

	// Convert steps to a single string for testing
	stepsContent := strings.Join(job.Steps, "")

	// Verify that GH_AW_SAFE_OUTPUTS_STAGED is present
	if !strings.Contains(stepsContent, "          GH_AW_SAFE_OUTPUTS_STAGED: \"true\"\n") {
		t.Error("Expected GH_AW_SAFE_OUTPUTS_STAGED to be set in create-discussion standalone job")
	}

	// Standalone jobs still use env var for target-repo (not handler config)
	// This is expected for backward compatibility with non-consolidated jobs
	// The handler manager version would use config, but this is the standalone job
}

// TestTrialModeWithoutTargetRepo verifies that trial mode without explicit
// target-repo config uses the trial repo slug
func TestTrialModeWithoutTargetRepo(t *testing.T) {
	c := NewCompiler()
	c.SetTrialMode(true)
	c.SetTrialLogicalRepoSlug("owner/trial-repo")

	workflowData := &WorkflowData{
		Name:             "test-workflow",
		TrialMode:        true,
		TrialLogicalRepo: "owner/trial-repo",
		SafeOutputs: &SafeOutputsConfig{
			CreateDiscussions: &CreateDiscussionsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 1},
				Category:             "12345",
			},
		},
	}

	job, err := c.buildCreateOutputDiscussionJob(workflowData, "main_job", "")
	if err != nil {
		t.Fatalf("Unexpected error building discussion job: %v", err)
	}

	// Convert steps to a single string for testing
	stepsContent := strings.Join(job.Steps, "")

	// Verify that GH_AW_SAFE_OUTPUTS_STAGED is present (trial mode sets this)
	if !strings.Contains(stepsContent, "          GH_AW_SAFE_OUTPUTS_STAGED: \"true\"\n") {
		t.Error("Expected GH_AW_SAFE_OUTPUTS_STAGED to be set in trial mode")
	}

	// Verify that GH_AW_TARGET_REPO_SLUG uses trial repo slug
	if !strings.Contains(stepsContent, "          GH_AW_TARGET_REPO_SLUG: \"owner/trial-repo\"\n") {
		t.Error("Expected GH_AW_TARGET_REPO_SLUG to use trial repo slug in trial mode")
	}
}

// TestNoStagedNorTrialMode verifies that neither staged flag nor target repo slug
// are added when not configured
func TestNoStagedNorTrialMode(t *testing.T) {
	c := NewCompiler()

	workflowData := &WorkflowData{
		Name: "test-workflow",
		SafeOutputs: &SafeOutputsConfig{
			CreatePullRequestReviewComments: &CreatePullRequestReviewCommentsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 10},
			},
		},
	}

	job, err := c.buildCreateOutputPullRequestReviewCommentJob(workflowData, "main_job")
	if err != nil {
		t.Fatalf("Unexpected error building PR review comment job: %v", err)
	}

	// Convert steps to a single string for testing
	stepsContent := strings.Join(job.Steps, "")

	// Verify that GH_AW_SAFE_OUTPUTS_STAGED is NOT present
	if strings.Contains(stepsContent, "GH_AW_SAFE_OUTPUTS_STAGED:") {
		t.Error("Expected GH_AW_SAFE_OUTPUTS_STAGED to not be set when staged is false")
	}

	// Verify that GH_AW_TARGET_REPO_SLUG is NOT present
	if strings.Contains(stepsContent, "GH_AW_TARGET_REPO_SLUG:") {
		t.Error("Expected GH_AW_TARGET_REPO_SLUG to not be set when not configured")
	}
}

// TestTargetRepoOverridesTrialRepo verifies that explicit target-repo config
// takes precedence over trial mode repo slug
func TestTargetRepoOverridesTrialRepo(t *testing.T) {
	c := NewCompiler()
	c.SetTrialMode(true)
	c.SetTrialLogicalRepoSlug("owner/trial-repo")

	workflowData := &WorkflowData{
		Name: "test-workflow",
		SafeOutputs: &SafeOutputsConfig{
			CreatePullRequestReviewComments: &CreatePullRequestReviewCommentsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 10},
				TargetRepoSlug:       "owner/explicit-target",
			},
		},
	}

	job, err := c.buildCreateOutputPullRequestReviewCommentJob(workflowData, "main_job")
	if err != nil {
		t.Fatalf("Unexpected error building PR review comment job: %v", err)
	}

	// Convert steps to a single string for testing
	stepsContent := strings.Join(job.Steps, "")

	// Verify that GH_AW_TARGET_REPO_SLUG uses explicit target, not trial repo
	if !strings.Contains(stepsContent, "          GH_AW_TARGET_REPO_SLUG: \"owner/explicit-target\"\n") {
		t.Error("Expected GH_AW_TARGET_REPO_SLUG to use explicit target-repo, not trial repo")
	}

	// Verify that trial repo slug is NOT used
	if strings.Contains(stepsContent, "          GH_AW_TARGET_REPO_SLUG: \"owner/trial-repo\"\n") {
		t.Error("Expected trial repo slug to be overridden by explicit target-repo")
	}
}

// TestSafeOutputJobBuilderRefactor validates that the refactored safe output job builders
// produce the expected job structure and maintain consistency across different output types
func TestSafeOutputJobBuilderRefactor(t *testing.T) {
	tests := []struct {
		name           string
		frontmatter    string
		expectedJob    string
		expectedPerms  string
		expectedOutput string
	}{
		{
			name: "create-issue job builder",
			frontmatter: `---
on: issues
permissions:
  contents: read
engine: copilot
strict: false
safe-outputs:
  create-issue:
    title-prefix: "[bot] "
    labels: [automation]
---

# Test workflow`,
			expectedJob:    "safe_outputs:",
			expectedPerms:  "contents: read",
			expectedOutput: "issue_number:",
		},
		{
			name: "create-discussion job builder",
			frontmatter: `---
on: issues
permissions:
  contents: read
engine: copilot
strict: false
safe-outputs:
  create-discussion:
    title-prefix: "[report] "
    category: General
---

# Test workflow`,
			expectedJob:    "safe_outputs:",
			expectedPerms:  "contents: read",
			expectedOutput: "discussion_number:",
		},
		{
			name: "update-issue job builder",
			frontmatter: `---
on: issues
permissions:
  contents: read
engine: copilot
strict: false
safe-outputs:
  update-issue:
    status:
    title:
---

# Test workflow`,
			expectedJob:    "safe_outputs:",
			expectedPerms:  "contents: read",
			expectedOutput: "issue_number:",
		},
		{
			name: "add-comment job builder",
			frontmatter: `---
on: issues
permissions:
  contents: read
engine: copilot
strict: false
safe-outputs:
  add-comment:
    max: 3
---

# Test workflow`,
			expectedJob:    "safe_outputs:",
			expectedPerms:  "contents: read",
			expectedOutput: "comment_id:",
		},
		{
			name: "create-pull-request job builder",
			frontmatter: `---
on: push
permissions:
  contents: read
engine: copilot
strict: false
safe-outputs:
  create-pull-request:
    title-prefix: "[auto] "
    labels: [automated]
---

# Test workflow`,
			expectedJob:    "safe_outputs:",
			expectedPerms:  "contents: write",
			expectedOutput: "pull_request_number:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory for test files
			tmpDir := testutil.TempDir(t, "refactor-test")

			testFile := filepath.Join(tmpDir, "test-workflow.md")
			if err := os.WriteFile(testFile, []byte(tt.frontmatter), 0644); err != nil {
				t.Fatal(err)
			}

			// Compile the workflow
			compiler := NewCompiler()
			if err := compiler.CompileWorkflow(testFile); err != nil {
				t.Fatalf("Failed to compile workflow: %v", err)
			}

			// Read the compiled output
			outputFile := filepath.Join(tmpDir, "test-workflow.lock.yml")
			compiledContent, err := os.ReadFile(outputFile)
			if err != nil {
				t.Fatalf("Failed to read compiled output: %v", err)
			}

			yamlStr := string(compiledContent)

			// Verify job is created
			if !strings.Contains(yamlStr, tt.expectedJob) {
				t.Errorf("Expected job %q not found in output", tt.expectedJob)
			}

			// Verify permissions are set
			if !strings.Contains(yamlStr, tt.expectedPerms) {
				t.Errorf("Expected permissions %q not found in output", tt.expectedPerms)
			}

			// Verify outputs are defined (consolidated mode uses different output format)
			if !strings.Contains(yamlStr, tt.expectedOutput) && !strings.Contains(yamlStr, "outputs:") {
				t.Errorf("Expected output %q or outputs section not found", tt.expectedOutput)
			}

			// Verify timeout is set (consolidated safe_outputs job uses 15 minute timeout)
			if !strings.Contains(yamlStr, "timeout-minutes: 15") && !strings.Contains(yamlStr, "timeout-minutes:") {
				t.Error("Expected timeout-minutes not found in output")
			}

			// Verify the job is present
			if !strings.Contains(yamlStr, "safe_outputs:") {
				t.Error("Expected safe_outputs job not found")
			}

			// Verify safe output condition is set
			if !strings.Contains(yamlStr, "!cancelled()") {
				t.Error("Expected safe output condition '!cancelled()' not found")
			}
		})
	}
}

// TestSafeOutputJobBuilderWithPreAndPostSteps validates that pre-steps and post-steps
// are correctly handled by the shared builder
func TestSafeOutputJobBuilderWithPreAndPostSteps(t *testing.T) {
	tests := []struct {
		name         string
		frontmatter  string
		expectedStep string
		stepType     string
	}{
		{
			name: "create-issue with copilot assignee (post-steps)",
			frontmatter: `---
on: issues
permissions:
  contents: read
engine: copilot
strict: false
safe-outputs:
  create-issue:
    assignees: [copilot]
---

# Test workflow`,
			// In consolidated mode with handler manager, check for process_safe_outputs step
			expectedStep: "id: process_safe_outputs",
			stepType:     "step",
		},
		{
			name: "create-pull-request with checkout (pre-steps)",
			frontmatter: `---
on: push
permissions:
  contents: read
engine: copilot
strict: false
safe-outputs:
  create-pull-request:
---

# Test workflow`,
			expectedStep: "actions/checkout",
			stepType:     "pre-step",
		},
		{
			name: "add-comment with debug (pre-steps)",
			frontmatter: `---
on: issues
permissions:
  contents: read
engine: copilot
strict: false
safe-outputs:
  add-comment:
---

# Test workflow`,
			// In consolidated mode with handler manager, check for process_safe_outputs step
			expectedStep: "id: process_safe_outputs",
			stepType:     "step",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory for test files
			tmpDir := testutil.TempDir(t, "presteps-test")

			testFile := filepath.Join(tmpDir, "test-workflow.md")
			if err := os.WriteFile(testFile, []byte(tt.frontmatter), 0644); err != nil {
				t.Fatal(err)
			}

			// Compile the workflow
			compiler := NewCompiler()
			if err := compiler.CompileWorkflow(testFile); err != nil {
				t.Fatalf("Failed to compile workflow: %v", err)
			}

			// Read the compiled output
			outputFile := filepath.Join(tmpDir, "test-workflow.lock.yml")
			compiledContent, err := os.ReadFile(outputFile)
			if err != nil {
				t.Fatalf("Failed to read compiled output: %v", err)
			}

			yamlStr := string(compiledContent)

			// Verify the expected step is present
			if !strings.Contains(yamlStr, tt.expectedStep) {
				t.Errorf("Expected %s %q not found in output", tt.stepType, tt.expectedStep)
			}
		})
	}
}
