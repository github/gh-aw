//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUsesPatchesAndCheckouts(t *testing.T) {
	tests := []struct {
		name        string
		safeOutputs *SafeOutputsConfig
		expected    bool
	}{
		{
			name:        "returns false for nil SafeOutputsConfig",
			safeOutputs: nil,
			expected:    false,
		},
		{
			name:        "returns false for empty SafeOutputsConfig",
			safeOutputs: &SafeOutputsConfig{},
			expected:    false,
		},
		{
			name: "returns true when CreatePullRequests is set",
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequests: &CreatePullRequestsConfig{},
			},
			expected: true,
		},
		{
			name: "returns true when PushToPullRequestBranch is set",
			safeOutputs: &SafeOutputsConfig{
				PushToPullRequestBranch: &PushToPullRequestBranchConfig{},
			},
			expected: true,
		},
		{
			name: "returns true when both CreatePullRequests and PushToPullRequestBranch are set",
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequests:      &CreatePullRequestsConfig{},
				PushToPullRequestBranch: &PushToPullRequestBranchConfig{},
			},
			expected: true,
		},
		{
			name: "returns false when only CreateIssues is set",
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{},
			},
			expected: false,
		},
		{
			name: "returns false when only AddComments is set",
			safeOutputs: &SafeOutputsConfig{
				AddComments: &AddCommentsConfig{},
			},
			expected: false,
		},
		{
			name: "returns false when only UpdatePullRequests is set",
			safeOutputs: &SafeOutputsConfig{
				UpdatePullRequests: &UpdatePullRequestsConfig{},
			},
			expected: false,
		},
		{
			name: "returns true when CreatePullRequests is set alongside other outputs",
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequests: &CreatePullRequestsConfig{},
				CreateIssues:       &CreateIssuesConfig{},
				AddComments:        &AddCommentsConfig{},
			},
			expected: true,
		},
		{
			name: "returns true when PushToPullRequestBranch is set alongside other outputs",
			safeOutputs: &SafeOutputsConfig{
				PushToPullRequestBranch: &PushToPullRequestBranchConfig{},
				CreateIssues:            &CreateIssuesConfig{},
			},
			expected: true,
		},
		{
			name: "returns false when CreatePullRequests is globally staged",
			safeOutputs: &SafeOutputsConfig{
				Staged:             templatableBoolPtr("true"),
				CreatePullRequests: &CreatePullRequestsConfig{},
			},
			expected: false,
		},
		{
			name: "returns false when PushToPullRequestBranch is globally staged",
			safeOutputs: &SafeOutputsConfig{
				Staged:                  templatableBoolPtr("true"),
				PushToPullRequestBranch: &PushToPullRequestBranchConfig{},
			},
			expected: false,
		},
		{
			name: "returns false when both PR handlers are globally staged",
			safeOutputs: &SafeOutputsConfig{
				Staged:                  templatableBoolPtr("true"),
				CreatePullRequests:      &CreatePullRequestsConfig{},
				PushToPullRequestBranch: &PushToPullRequestBranchConfig{},
			},
			expected: false,
		},
		{
			name: "returns false when CreatePullRequests is per-handler staged",
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequests: &CreatePullRequestsConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Staged: templatableBoolPtr("true")}},
			},
			expected: false,
		},
		{
			name: "returns false when both PR handlers are per-handler staged",
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequests:      &CreatePullRequestsConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Staged: templatableBoolPtr("true")}},
				PushToPullRequestBranch: &PushToPullRequestBranchConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Staged: templatableBoolPtr("true")}},
			},
			expected: false,
		},
		{
			name: "returns true when CreatePullRequests is not staged but PushToPullRequestBranch is staged",
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequests:      &CreatePullRequestsConfig{},
				PushToPullRequestBranch: &PushToPullRequestBranchConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Staged: templatableBoolPtr("true")}},
			},
			expected: true,
		},
		{
			name: "returns true when PushToPullRequestBranch is not staged but CreatePullRequests is staged",
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequests:      &CreatePullRequestsConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Staged: templatableBoolPtr("true")}},
				PushToPullRequestBranch: &PushToPullRequestBranchConfig{},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, usesPatchesAndCheckouts(tt.safeOutputs))
		})
	}
}
