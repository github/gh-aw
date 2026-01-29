//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

func TestAddCommentJobDependencies(t *testing.T) {
	tests := []struct {
		name                     string
		createIssueJobName       string
		createDiscussionJobName  string
		createPullRequestJobName string
		expectedNeeds            []string
		expectedEnvVars          []string
		expectIssueEnvVars       bool
		expectDiscussionEnvVars  bool
		expectPullRequestEnvVars bool
	}{
		{
			name:                     "No dependencies",
			createIssueJobName:       "",
			createDiscussionJobName:  "",
			createPullRequestJobName: "",
			expectedNeeds:            []string{"main"},
			expectedEnvVars:          []string{},
			expectIssueEnvVars:       false,
			expectDiscussionEnvVars:  false,
			expectPullRequestEnvVars: false,
		},
		{
			name:                     "Only create_issue dependency",
			createIssueJobName:       "create_issue",
			createDiscussionJobName:  "",
			createPullRequestJobName: "",
			expectedNeeds:            []string{"main", "create_issue"},
			expectedEnvVars: []string{
				"GH_AW_CREATED_ISSUE_URL",
				"GH_AW_CREATED_ISSUE_NUMBER",
			},
			expectIssueEnvVars:       true,
			expectDiscussionEnvVars:  false,
			expectPullRequestEnvVars: false,
		},
		{
			name:                     "Only create_discussion dependency",
			createIssueJobName:       "",
			createDiscussionJobName:  "create_discussion",
			createPullRequestJobName: "",
			expectedNeeds:            []string{"main", "create_discussion"},
			expectedEnvVars: []string{
				"GH_AW_CREATED_DISCUSSION_URL",
				"GH_AW_CREATED_DISCUSSION_NUMBER",
			},
			expectIssueEnvVars:       false,
			expectDiscussionEnvVars:  true,
			expectPullRequestEnvVars: false,
		},
		{
			name:                     "Only create_pull_request dependency",
			createIssueJobName:       "",
			createDiscussionJobName:  "",
			createPullRequestJobName: "create_pull_request",
			expectedNeeds:            []string{"main", "create_pull_request"},
			expectedEnvVars: []string{
				"GH_AW_CREATED_PULL_REQUEST_URL",
				"GH_AW_CREATED_PULL_REQUEST_NUMBER",
			},
			expectIssueEnvVars:       false,
			expectDiscussionEnvVars:  false,
			expectPullRequestEnvVars: true,
		},
		{
			name:                     "All dependencies",
			createIssueJobName:       "create_issue",
			createDiscussionJobName:  "create_discussion",
			createPullRequestJobName: "create_pull_request",
			expectedNeeds:            []string{"main", "create_issue", "create_discussion", "create_pull_request"},
			expectedEnvVars: []string{
				"GH_AW_CREATED_ISSUE_URL",
				"GH_AW_CREATED_ISSUE_NUMBER",
				"GH_AW_CREATED_DISCUSSION_URL",
				"GH_AW_CREATED_DISCUSSION_NUMBER",
				"GH_AW_CREATED_PULL_REQUEST_URL",
				"GH_AW_CREATED_PULL_REQUEST_NUMBER",
			},
			expectIssueEnvVars:       true,
			expectDiscussionEnvVars:  true,
			expectPullRequestEnvVars: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := &Compiler{}
			workflowData := &WorkflowData{
				Name: "Test Workflow",
				SafeOutputs: &SafeOutputsConfig{
					AddComments: &AddCommentsConfig{
						BaseSafeOutputConfig: BaseSafeOutputConfig{},
					},
				},
			}

			job, err := compiler.buildCreateOutputAddCommentJob(
				workflowData,
				"main",
				tt.createIssueJobName,
				tt.createDiscussionJobName,
				tt.createPullRequestJobName,
			)
			if err != nil {
				t.Fatalf("Failed to build safe_outputs job: %v", err)
			}

			// Check job dependencies (needs)
			if len(job.Needs) != len(tt.expectedNeeds) {
				t.Errorf("Expected %d needs, got %d", len(tt.expectedNeeds), len(job.Needs))
			}
			for _, expectedNeed := range tt.expectedNeeds {
				found := false
				for _, need := range job.Needs {
					if need == expectedNeed {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected need '%s' not found in job.Needs: %v", expectedNeed, job.Needs)
				}
			}

			// Convert steps to string to check for environment variables
			stepsStr := strings.Join(job.Steps, "")

			// Check for expected environment variables
			for _, envVar := range tt.expectedEnvVars {
				if !strings.Contains(stepsStr, envVar) {
					t.Errorf("Expected environment variable '%s' not found in job steps", envVar)
				}
			}

			// Check for issue-specific environment variables
			if tt.expectIssueEnvVars {
				if !strings.Contains(stepsStr, "GH_AW_CREATED_ISSUE_URL: ${{ needs.create_issue.outputs.issue_url }}") {
					t.Error("Expected issue_url environment variable declaration not found in job steps")
				}
				if !strings.Contains(stepsStr, "GH_AW_CREATED_ISSUE_NUMBER: ${{ needs.create_issue.outputs.issue_number }}") {
					t.Error("Expected issue_number environment variable declaration not found in job steps")
				}
			} else {
				if strings.Contains(stepsStr, "GH_AW_CREATED_ISSUE_URL: ${{ needs.create_issue.outputs.issue_url }}") {
					t.Error("Unexpected GH_AW_CREATED_ISSUE_URL environment variable declaration found in job steps")
				}
			}

			// Check for discussion-specific environment variables
			if tt.expectDiscussionEnvVars {
				if !strings.Contains(stepsStr, "GH_AW_CREATED_DISCUSSION_URL: ${{ needs.create_discussion.outputs.discussion_url }}") {
					t.Error("Expected discussion_url environment variable declaration not found in job steps")
				}
				if !strings.Contains(stepsStr, "GH_AW_CREATED_DISCUSSION_NUMBER: ${{ needs.create_discussion.outputs.discussion_number }}") {
					t.Error("Expected discussion_number environment variable declaration not found in job steps")
				}
			} else {
				if strings.Contains(stepsStr, "GH_AW_CREATED_DISCUSSION_URL: ${{ needs.create_discussion.outputs.discussion_url }}") {
					t.Error("Unexpected GH_AW_CREATED_DISCUSSION_URL environment variable declaration found in job steps")
				}
			}

			// Check for pull request-specific environment variables
			if tt.expectPullRequestEnvVars {
				if !strings.Contains(stepsStr, "GH_AW_CREATED_PULL_REQUEST_URL: ${{ needs.create_pull_request.outputs.pull_request_url }}") {
					t.Error("Expected pull_request_url environment variable declaration not found in job steps")
				}
				if !strings.Contains(stepsStr, "GH_AW_CREATED_PULL_REQUEST_NUMBER: ${{ needs.create_pull_request.outputs.pull_request_number }}") {
					t.Error("Expected pull_request_number environment variable declaration not found in job steps")
				}
			} else {
				if strings.Contains(stepsStr, "GH_AW_CREATED_PULL_REQUEST_URL: ${{ needs.create_pull_request.outputs.pull_request_url }}") {
					t.Error("Unexpected GH_AW_CREATED_PULL_REQUEST_URL environment variable declaration found in job steps")
				}
			}
		})
	}
}
