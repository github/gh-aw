package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSafeOutputRequiredAuthorRuntimeContract(t *testing.T) {
	config := &SafeOutputsConfig{
		PushToPullRequestBranch: &PushToPullRequestBranchConfig{RequiredAuthor: "workflow-bot"},
		UpdatePullRequests:      &UpdatePullRequestsConfig{RequiredAuthor: "workflow-bot"},
	}
	handlers := []struct {
		name        string
		runtimeFile string
		runtimeCode string
	}{
		{
			name:        "push_to_pull_request_branch",
			runtimeFile: "push_to_pull_request_branch.cjs",
			runtimeCode: "matchesRequiredAuthor(pullRequest.user, requiredAuthor)",
		},
		{
			name:        "update_pull_request",
			runtimeFile: "update_pull_request.cjs",
			runtimeCode: "checkRequiredFilter(githubClient, repoParts, prNumber, requiredLabels, requiredTitlePrefix, \"update_pull_request\", requiredAuthor)",
		},
	}

	for _, handler := range handlers {
		t.Run(handler.name, func(t *testing.T) {
			buildConfig, ok := pullRequestHandlerRegistry[handler.name]
			require.True(t, ok)
			require.Equal(t, "workflow-bot", buildConfig(config)["required_author"])

			runtimePath := filepath.Join("..", "..", "actions", "setup", "js", handler.runtimeFile)
			runtimeSource, err := os.ReadFile(runtimePath)
			require.NoError(t, err)
			require.Contains(t, string(runtimeSource), "config.required_author")
			require.Contains(t, string(runtimeSource), handler.runtimeCode)
		})
	}

	helperPath := filepath.Join("..", "..", "actions", "setup", "js", "safe_output_helpers.cjs")
	helperSource, err := os.ReadFile(helperPath)
	require.NoError(t, err)
	require.Contains(t, string(helperSource), "function matchesRequiredAuthor(user, requiredAuthor)")
}
