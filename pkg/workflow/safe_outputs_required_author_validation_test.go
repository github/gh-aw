package workflow

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateSafeOutputsRequiredAuthors(t *testing.T) {
	tests := []struct {
		name    string
		config  *SafeOutputsConfig
		wantErr string
	}{
		{
			name: "literal author",
			config: &SafeOutputsConfig{
				PushToPullRequestBranch: &PushToPullRequestBranchConfig{RequiredAuthor: "workflow-bot"},
			},
		},
		{
			name: "whole expression",
			config: &SafeOutputsConfig{
				UpdatePullRequests: &UpdatePullRequestsConfig{RequiredAuthor: "${{ inputs.author }}"},
			},
		},
		{
			name: "whitespace-only push author",
			config: &SafeOutputsConfig{
				PushToPullRequestBranch: &PushToPullRequestBranchConfig{RequiredAuthor: "   "},
			},
			wantErr: "safe-outputs.push-to-pull-request-branch.required-author cannot be empty or whitespace-only",
		},
		{
			name: "whitespace-only update author",
			config: &SafeOutputsConfig{
				UpdatePullRequests: &UpdatePullRequestsConfig{RequiredAuthor: "   "},
			},
			wantErr: "safe-outputs.update-pull-request.required-author cannot be empty or whitespace-only",
		},
		{
			name: "mixed expression",
			config: &SafeOutputsConfig{
				PushToPullRequestBranch: &PushToPullRequestBranchConfig{RequiredAuthor: "app/${{ inputs.author }}"},
			},
			wantErr: "safe-outputs.push-to-pull-request-branch.required-author must be a complete GitHub Actions expression",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSafeOutputsRequiredAuthors(tt.config)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}
