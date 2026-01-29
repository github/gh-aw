//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

func TestCreateIssueJobWithStagedFlag(t *testing.T) {
	// Create a compiler instance
	c := NewCompiler()

	// Test with staged: true
	workflowData := &WorkflowData{
		Name: "test-workflow",
		SafeOutputs: &SafeOutputsConfig{
			CreateIssues: &CreateIssuesConfig{},
			Staged:       true, // pointer to true
		},
	}

	job, err := c.buildCreateOutputIssueJob(workflowData, "main_job")
	if err != nil {
		t.Fatalf("Unexpected error building create issue job: %v", err)
	}

	// Convert steps to a single string for testing
	stepsContent := strings.Join(job.Steps, "")

	// Check that GH_AW_SAFE_OUTPUTS_STAGED is included in the env section
	if !strings.Contains(stepsContent, "          GH_AW_SAFE_OUTPUTS_STAGED: \"true\"\n") {
		t.Error("Expected GH_AW_SAFE_OUTPUTS_STAGED environment variable to be set to true in create-issue job")
	}

	// Test with staged: false
	workflowData.SafeOutputs.Staged = false // pointer to false

	job, err = c.buildCreateOutputIssueJob(workflowData, "main_job")
	if err != nil {
		t.Fatalf("Unexpected error building create issue job: %v", err)
	}

	stepsContent = strings.Join(job.Steps, "")

	// Check that GH_AW_SAFE_OUTPUTS_STAGED is not included in the env section when false
	// We need to be specific to avoid matching the JavaScript code that references the variable
	if strings.Contains(stepsContent, "          GH_AW_SAFE_OUTPUTS_STAGED:") {
		t.Error("Expected GH_AW_SAFE_OUTPUTS_STAGED environment variable not to be set when staged is false")
	}

}

func TestCreateIssueJobWithoutSafeOutputs(t *testing.T) {
	// Create a compiler instance
	c := NewCompiler()

	// Test with no SafeOutputs config - this should fail
	workflowData := &WorkflowData{
		Name:        "test-workflow",
		SafeOutputs: nil,
	}

	_, err := c.buildCreateOutputIssueJob(workflowData, "main_job")
	if err == nil {
		t.Error("Expected error when SafeOutputs is nil")
	}

	expectedError := "safe-outputs.create-issue configuration is required"
	if !strings.Contains(err.Error(), expectedError) {
		t.Errorf("Expected error message to contain '%s', got: %v", expectedError, err)
	}

	// Test with SafeOutputs but no CreateIssues config - this should also fail
	workflowData.SafeOutputs = &SafeOutputsConfig{
		CreatePullRequests: &CreatePullRequestsConfig{},
		Staged:             true,
	}

	_, err = c.buildCreateOutputIssueJob(workflowData, "main_job")
	if err == nil {
		t.Error("Expected error when CreateIssues is nil")
	}

	if !strings.Contains(err.Error(), expectedError) {
		t.Errorf("Expected error message to contain '%s', got: %v", expectedError, err)
	}
}
