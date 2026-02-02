package workflow

import (
	"encoding/json"
	"fmt"

	"github.com/github/gh-aw/pkg/logger"
)

var addCommentLog = logger.New("workflow:add_comment")

// AddCommentConfig holds configuration for creating GitHub issue/PR comments from agent output (deprecated, use AddCommentsConfig)
type AddCommentConfig struct {
	// Empty struct for now, as per requirements, but structured for future expansion
}

// AddCommentsConfig holds configuration for creating GitHub issue/PR comments from agent output
type AddCommentsConfig struct {
	BaseSafeOutputConfig `yaml:",inline"`
	Target               string   `yaml:"target,omitempty"`              // Target for comments: "triggering" (default), "*" (any issue), or explicit issue number
	TargetRepoSlug       string   `yaml:"target-repo,omitempty"`         // Target repository in format "owner/repo" for cross-repository comments
	AllowedRepos         []string `yaml:"allowed-repos,omitempty"`       // List of additional repositories that comments can be added to (additionally to the target-repo)
	Discussion           *bool    `yaml:"discussion,omitempty"`          // Target discussion comments instead of issue/PR comments. Must be true if present.
	HideOlderComments    bool     `yaml:"hide-older-comments,omitempty"` // When true, minimizes/hides all previous comments from the same workflow before creating the new comment
	AllowedReasons       []string `yaml:"allowed-reasons,omitempty"`     // List of allowed reasons for hiding older comments (default: all reasons allowed)
}

// buildCreateOutputAddCommentJob creates the add_comment job
func (c *Compiler) buildCreateOutputAddCommentJob(data *WorkflowData, mainJobName string, createIssueJobName string, createDiscussionJobName string, createPullRequestJobName string) (*Job, error) {
	addCommentLog.Printf("Building add_comment job: target=%s, discussion=%v", data.SafeOutputs.AddComments.Target, data.SafeOutputs.AddComments.Discussion != nil && *data.SafeOutputs.AddComments.Discussion)
	if data.SafeOutputs == nil || data.SafeOutputs.AddComments == nil {
		return nil, fmt.Errorf("safe-outputs.add-comment configuration is required")
	}

	// Build pre-steps for debugging output
	var preSteps []string
	preSteps = append(preSteps, "      - name: Debug agent outputs\n")
	preSteps = append(preSteps, "        env:\n")
	preSteps = append(preSteps, fmt.Sprintf("          AGENT_OUTPUT: ${{ needs.%s.outputs.output }}\n", mainJobName))
	preSteps = append(preSteps, fmt.Sprintf("          AGENT_OUTPUT_TYPES: ${{ needs.%s.outputs.output_types }}\n", mainJobName))
	preSteps = append(preSteps, "        run: |\n")
	preSteps = append(preSteps, "          echo \"Output: $AGENT_OUTPUT\"\n")
	preSteps = append(preSteps, "          echo \"Output types: $AGENT_OUTPUT_TYPES\"\n")

	// Build custom environment variables specific to add-comment
	var customEnvVars []string

	// Pass the comment target configuration
	if data.SafeOutputs.AddComments.Target != "" {
		customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_COMMENT_TARGET: %q\n", data.SafeOutputs.AddComments.Target))
	}
	// Pass the discussion flag configuration
	if data.SafeOutputs.AddComments.Discussion != nil && *data.SafeOutputs.AddComments.Discussion {
		customEnvVars = append(customEnvVars, "          GITHUB_AW_COMMENT_DISCUSSION: \"true\"\n")
	}
	// Pass the hide-older-comments flag configuration
	if data.SafeOutputs.AddComments.HideOlderComments {
		customEnvVars = append(customEnvVars, "          GH_AW_HIDE_OLDER_COMMENTS: \"true\"\n")
	}
	// Pass the allowed-reasons list configuration
	if len(data.SafeOutputs.AddComments.AllowedReasons) > 0 {
		reasonsJSON, err := json.Marshal(data.SafeOutputs.AddComments.AllowedReasons)
		if err == nil {
			customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_ALLOWED_REASONS: %q\n", string(reasonsJSON)))
		}
	}
	// Add environment variables for the URLs from other safe output jobs if they exist
	if createIssueJobName != "" {
		customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_CREATED_ISSUE_URL: ${{ needs.%s.outputs.issue_url }}\n", createIssueJobName))
		customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_CREATED_ISSUE_NUMBER: ${{ needs.%s.outputs.issue_number }}\n", createIssueJobName))
		customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_TEMPORARY_ID_MAP: ${{ needs.%s.outputs.temporary_id_map }}\n", createIssueJobName))
	}
	if createDiscussionJobName != "" {
		customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_CREATED_DISCUSSION_URL: ${{ needs.%s.outputs.discussion_url }}\n", createDiscussionJobName))
		customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_CREATED_DISCUSSION_NUMBER: ${{ needs.%s.outputs.discussion_number }}\n", createDiscussionJobName))
	}
	if createPullRequestJobName != "" {
		customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_CREATED_PULL_REQUEST_URL: ${{ needs.%s.outputs.pull_request_url }}\n", createPullRequestJobName))
		customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_CREATED_PULL_REQUEST_NUMBER: ${{ needs.%s.outputs.pull_request_number }}\n", createPullRequestJobName))
	}

	// Add standard environment variables (metadata + staged/target repo)
	customEnvVars = append(customEnvVars, c.buildStandardSafeOutputEnvVars(data, data.SafeOutputs.AddComments.TargetRepoSlug)...)

	// Create outputs for the job
	outputs := map[string]string{
		"comment_id":  "${{ steps.add_comment.outputs.comment_id }}",
		"comment_url": "${{ steps.add_comment.outputs.comment_url }}",
	}

	// Build job condition with event check if target is not specified
	jobCondition := BuildSafeOutputType("add_comment")
	if data.SafeOutputs.AddComments != nil && data.SafeOutputs.AddComments.Target == "" {
		eventCondition := BuildOr(
			BuildOr(
				BuildPropertyAccess("github.event.issue.number"),
				BuildPropertyAccess("github.event.pull_request.number"),
			),
			BuildPropertyAccess("github.event.discussion.number"),
		)
		jobCondition = BuildAnd(jobCondition, eventCondition)
	}

	// Build the needs list - always depend on mainJobName, and conditionally on the other jobs
	needs := []string{mainJobName}
	if createIssueJobName != "" {
		needs = append(needs, createIssueJobName)
	}
	if createDiscussionJobName != "" {
		needs = append(needs, createDiscussionJobName)
	}
	if createPullRequestJobName != "" {
		needs = append(needs, createPullRequestJobName)
	}

	// Use the shared builder function to create the job
	return c.buildSafeOutputJob(data, SafeOutputJobConfig{
		JobName:        "add_comment",
		StepName:       "Add Issue Comment",
		StepID:         "add_comment",
		MainJobName:    mainJobName,
		CustomEnvVars:  customEnvVars,
		Script:         getAddCommentScript(),
		Permissions:    NewPermissionsContentsReadIssuesWritePRWriteDiscussionsWrite(),
		Outputs:        outputs,
		Condition:      jobCondition,
		Needs:          needs,
		PreSteps:       preSteps,
		Token:          data.SafeOutputs.AddComments.GitHubToken,
		TargetRepoSlug: data.SafeOutputs.AddComments.TargetRepoSlug,
	})
}

// parseCommentsConfig handles add-comment configuration
func (c *Compiler) parseCommentsConfig(outputMap map[string]any) *AddCommentsConfig {
	// Check if the key exists
	if _, exists := outputMap["add-comment"]; !exists {
		return nil
	}

	addCommentLog.Print("Parsing add-comment configuration")

	// Unmarshal into typed config struct
	var config AddCommentsConfig
	if err := unmarshalConfig(outputMap, "add-comment", &config, addCommentLog); err != nil {
		addCommentLog.Printf("Failed to unmarshal config: %v", err)
		// For backward compatibility, handle nil/empty config
		config = AddCommentsConfig{}
	}

	// Set default max if not specified
	if config.Max == 0 {
		config.Max = 1
	}

	// Validate target-repo (wildcard "*" is not allowed)
	if validateTargetRepoSlug(config.TargetRepoSlug, addCommentLog) {
		return nil // Invalid configuration, return nil to cause validation error
	}

	// Validate discussion field - must be true if present
	if config.Discussion != nil && !*config.Discussion {
		addCommentLog.Print("Invalid discussion: must be true if present")
		return nil // Invalid configuration, return nil to cause validation error
	}

	return &config
}
