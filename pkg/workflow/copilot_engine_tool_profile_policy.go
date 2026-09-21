package workflow

import (
	"errors"
	"fmt"
	"slices"
)

type copilotSDKToolProfile struct {
	ID                      string         `json:"id"`
	RepositoryDefaultBranch string         `json:"repositoryDefaultBranch"`
	Policy                  map[string]any `json:"policy"`
}

const goRepositoryDefaultBranchExpression = "${{ github.event.repository.default_branch }}"
const goRepositoryDefaultBranchEnv = "GH_AW_GITHUB_EVENT_REPOSITORY_DEFAULT_BRANCH"

func projectGoRepositoryPolicy(data *WorkflowData) (map[string]any, error) {
	if data == nil || data.SafeOutputs == nil || data.SafeOutputs.CreatePullRequests == nil {
		return nil, errors.New("engine.tool-profile: go-repository requires safe-outputs.create-pull-request")
	}
	handlers := make(map[string]any)
	// Project before generateSafeOutputsConfig neutralizes later-job expressions.
	addStandardHandlerConfigs(handlers, data)
	pr, ok := handlers["create_pull_request"].(map[string]any)
	if !ok {
		return nil, errors.New("engine.tool-profile: go-repository could not derive the create_pull_request policy")
	}
	policy := make(map[string]any)
	for _, field := range []string{
		"target-repo", "patch_workspace_path", "current_checkout_repo",
		"base_branch", "allowed_base_branches", "allowed_branches", "branch_prefix",
		"preserve_branch_name", "recreate_ref",
		"allowed_files", "excluded_files", "protected_files_policy", "protected_files",
		"protected_path_prefixes", "protect_top_level_dot_folders", "protected_dot_folder_excludes",
		"max_patch_size", "max_patch_files",
	} {
		if value, exists := pr[field]; exists {
			policy[field] = value
		}
	}
	return policy, nil
}

func addGoRepositoryToolProfile(config *copilotSDKToolConfig, data *WorkflowData) {
	if engineToolProfile(data) != copilotGoRepositoryToolProfile {
		return
	}
	policy, err := projectGoRepositoryPolicy(data)
	if err != nil {
		panic(fmt.Sprintf("BUG: invalid validated Go repository tool profile: %v", err))
	}
	config.Version = 2
	config.Profile = &copilotSDKToolProfile{
		ID:                      copilotGoRepositoryToolProfile,
		RepositoryDefaultBranch: goRepositoryDefaultBranchExpression,
		Policy:                  policy,
	}
	config.Capabilities.Bash = false
	config.Capabilities.CLIProxy = false
	config.Permissions.AllowedTools = append(config.Permissions.AllowedTools, copilotGoRepositoryToolName)
	slices.Sort(config.Permissions.AllowedTools)
	config.Permissions.AllowedTools = slices.Compact(config.Permissions.AllowedTools)
	config.ExplicitlyDisabledTools = append(config.ExplicitlyDisabledTools, "bash", "cli-proxy")
	slices.Sort(config.ExplicitlyDisabledTools)
	config.ExplicitlyDisabledTools = slices.Compact(config.ExplicitlyDisabledTools)
}
