package workflow

import (
	"fmt"

	"github.com/githubnext/gh-aw/pkg/constants"
	"github.com/githubnext/gh-aw/pkg/logger"
)

var createPRLog = logger.New("workflow:create_pull_request")

// CreatePullRequestsConfig holds configuration for creating GitHub pull requests from agent output
type CreatePullRequestsConfig struct {
	BaseSafeOutputConfig `yaml:",inline"`
	TitlePrefix          string   `yaml:"title-prefix,omitempty"`
	Labels               []string `yaml:"labels,omitempty"`
	AllowedLabels        []string `yaml:"allowed-labels,omitempty"` // Optional list of allowed labels. If omitted, any labels are allowed (including creating new ones).
	Reviewers            []string `yaml:"reviewers,omitempty"`      // List of users/bots to assign as reviewers to the pull request
	Draft                *bool    `yaml:"draft,omitempty"`          // Pointer to distinguish between unset (nil) and explicitly false
	IfNoChanges          string   `yaml:"if-no-changes,omitempty"`  // Behavior when no changes to push: "warn" (default), "error", or "ignore"
	AllowEmpty           bool     `yaml:"allow-empty,omitempty"`    // Allow creating PR without patch file or with empty patch (useful for preparing feature branches)
	TargetRepoSlug       string   `yaml:"target-repo,omitempty"`    // Target repository in format "owner/repo" for cross-repository pull requests
	AllowedRepos         []string `yaml:"allowed-repos,omitempty"`  // List of additional repositories that pull requests can be created in (additionally to the target-repo)
	Expires              int      `yaml:"expires,omitempty"`        // Hours until the pull request expires and should be automatically closed (only for same-repo PRs)
	AutoMerge            bool     `yaml:"auto-merge,omitempty"`     // Enable auto-merge for the pull request when all required checks pass
}

// buildCreateOutputPullRequestJob creates the create_pull_request job
func (c *Compiler) buildCreateOutputPullRequestJob(data *WorkflowData, mainJobName string) (*Job, error) {
	if data.SafeOutputs == nil || data.SafeOutputs.CreatePullRequests == nil {
		return nil, fmt.Errorf("safe-outputs.create-pull-request configuration is required")
	}

	if createPRLog.Enabled() {
		draftValue := true // Default
		if data.SafeOutputs.CreatePullRequests.Draft != nil {
			draftValue = *data.SafeOutputs.CreatePullRequests.Draft
		}
		createPRLog.Printf("Building create-pull-request job: workflow=%s, main_job=%s, draft=%v, reviewers=%d",
			data.Name, mainJobName, draftValue, len(data.SafeOutputs.CreatePullRequests.Reviewers))
	}

	// Build pre-steps for patch download, checkout, and git config
	var preSteps []string

	// Step 1: Download patch artifact from unified agent-artifacts
	preSteps = append(preSteps, "      - name: Download patch artifact\n")
	preSteps = append(preSteps, "        continue-on-error: true\n")
	preSteps = append(preSteps, fmt.Sprintf("        uses: %s\n", GetActionPin("actions/download-artifact")))
	preSteps = append(preSteps, "        with:\n")
	preSteps = append(preSteps, "          name: agent-artifacts\n")
	preSteps = append(preSteps, "          path: /tmp/gh-aw/\n")

	// Step 2: Checkout repository
	preSteps = buildCheckoutRepository(preSteps, c)

	// Step 3: Configure Git credentials
	preSteps = append(preSteps, c.generateGitConfigurationSteps()...)

	// Build custom environment variables specific to create-pull-request
	var customEnvVars []string
	// Pass the workflow ID for branch naming
	customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_WORKFLOW_ID: %q\n", mainJobName))
	// Pass the base branch from GitHub context
	customEnvVars = append(customEnvVars, "          GH_AW_BASE_BRANCH: ${{ github.ref_name }}\n")
	customEnvVars = append(customEnvVars, buildTitlePrefixEnvVar("GH_AW_PR_TITLE_PREFIX", data.SafeOutputs.CreatePullRequests.TitlePrefix)...)
	customEnvVars = append(customEnvVars, buildLabelsEnvVar("GH_AW_PR_LABELS", data.SafeOutputs.CreatePullRequests.Labels)...)
	customEnvVars = append(customEnvVars, buildLabelsEnvVar("GH_AW_PR_ALLOWED_LABELS", data.SafeOutputs.CreatePullRequests.AllowedLabels)...)
	// Pass draft setting - default to true for backwards compatibility
	draftValue := true // Default value
	if data.SafeOutputs.CreatePullRequests.Draft != nil {
		draftValue = *data.SafeOutputs.CreatePullRequests.Draft
	}
	customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_PR_DRAFT: %q\n", fmt.Sprintf("%t", draftValue)))

	// Pass the if-no-changes configuration
	ifNoChanges := data.SafeOutputs.CreatePullRequests.IfNoChanges
	if ifNoChanges == "" {
		ifNoChanges = "warn" // Default value
	}
	customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_PR_IF_NO_CHANGES: %q\n", ifNoChanges))

	// Pass the allow-empty configuration
	customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_PR_ALLOW_EMPTY: %q\n", fmt.Sprintf("%t", data.SafeOutputs.CreatePullRequests.AllowEmpty)))

	// Pass the auto-merge configuration
	customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_PR_AUTO_MERGE: %q\n", fmt.Sprintf("%t", data.SafeOutputs.CreatePullRequests.AutoMerge)))

	// Pass the maximum patch size configuration
	maxPatchSize := 1024 // Default value
	if data.SafeOutputs != nil && data.SafeOutputs.MaximumPatchSize > 0 {
		maxPatchSize = data.SafeOutputs.MaximumPatchSize
	}
	customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_MAX_PATCH_SIZE: %d\n", maxPatchSize))

	// Pass activation comment information if available (for updating the comment with PR link)
	// These outputs are only available when reaction is configured in the workflow
	if data.AIReaction != "" && data.AIReaction != "none" {
		customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_COMMENT_ID: ${{ needs.%s.outputs.comment_id }}\n", constants.ActivationJobName))
		customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_COMMENT_REPO: ${{ needs.%s.outputs.comment_repo }}\n", constants.ActivationJobName))
	}

	// Add expires value if set (only for same-repo PRs - when target-repo is not set)
	if data.SafeOutputs.CreatePullRequests.Expires > 0 && data.SafeOutputs.CreatePullRequests.TargetRepoSlug == "" {
		customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_PR_EXPIRES: \"%d\"\n", data.SafeOutputs.CreatePullRequests.Expires))
	}

	// Add standard environment variables (metadata + staged/target repo)
	customEnvVars = append(customEnvVars, c.buildStandardSafeOutputEnvVars(data, data.SafeOutputs.CreatePullRequests.TargetRepoSlug)...)

	// Build post-steps for reviewers if configured
	var postSteps []string
	if len(data.SafeOutputs.CreatePullRequests.Reviewers) > 0 {
		// Get the effective GitHub token to use for gh CLI
		var safeOutputsToken string
		if data.SafeOutputs != nil {
			safeOutputsToken = data.SafeOutputs.GitHubToken
		}

		postSteps = buildCopilotParticipantSteps(CopilotParticipantConfig{
			Participants:       data.SafeOutputs.CreatePullRequests.Reviewers,
			ParticipantType:    "reviewer",
			CustomToken:        data.SafeOutputs.CreatePullRequests.GitHubToken,
			SafeOutputsToken:   safeOutputsToken,
			WorkflowToken:      data.GitHubToken,
			ConditionStepID:    "create_pull_request",
			ConditionOutputKey: "pull_request_url",
		})
	}

	// Create outputs for the job
	outputs := map[string]string{
		"pull_request_number": "${{ steps.create_pull_request.outputs.pull_request_number }}",
		"pull_request_url":    "${{ steps.create_pull_request.outputs.pull_request_url }}",
		"issue_number":        "${{ steps.create_pull_request.outputs.issue_number }}",
		"issue_url":           "${{ steps.create_pull_request.outputs.issue_url }}",
		"branch_name":         "${{ steps.create_pull_request.outputs.branch_name }}",
		"fallback_used":       "${{ steps.create_pull_request.outputs.fallback_used }}",
		"error_message":       "${{ steps.create_pull_request.outputs.error_message }}",
	}

	// Use the shared builder function to create the job
	return c.buildSafeOutputJob(data, SafeOutputJobConfig{
		JobName:        "create_pull_request",
		StepName:       "Create Pull Request",
		StepID:         "create_pull_request",
		MainJobName:    mainJobName,
		CustomEnvVars:  customEnvVars,
		Script:         "", // Legacy - handler manager uses require() to load handler from /tmp/gh-aw/actions
		Permissions:    NewPermissionsContentsWriteIssuesWritePRWrite(),
		Outputs:        outputs,
		PreSteps:       preSteps,
		PostSteps:      postSteps,
		Token:          data.SafeOutputs.CreatePullRequests.GitHubToken,
		TargetRepoSlug: data.SafeOutputs.CreatePullRequests.TargetRepoSlug,
	})
}

// parsePullRequestsConfig handles only create-pull-request (singular) configuration
func (c *Compiler) parsePullRequestsConfig(outputMap map[string]any) *CreatePullRequestsConfig {
	// Check for singular form only
	if _, exists := outputMap["create-pull-request"]; !exists {
		createPRLog.Print("No create-pull-request configuration found")
		return nil
	}

	createPRLog.Print("Parsing create-pull-request configuration")

	// Get the config data to check for special cases before unmarshaling
	configData, _ := outputMap["create-pull-request"].(map[string]any)

	// Pre-process the reviewers field to convert single string to array BEFORE unmarshaling
	// This prevents YAML unmarshal errors when reviewers is a string instead of []string
	if configData != nil {
		if reviewers, exists := configData["reviewers"]; exists {
			if reviewerStr, ok := reviewers.(string); ok {
				// Convert single string to array
				configData["reviewers"] = []string{reviewerStr}
				createPRLog.Printf("Converted single reviewer string to array before unmarshaling")
			}
		}
	}

	// Pre-process the expires field if it's a string (convert to int before unmarshaling)
	if configData != nil {
		if expires, exists := configData["expires"]; exists {
			if _, ok := expires.(string); ok {
				// Parse the string format and replace with int
				expiresInt := parseExpiresFromConfig(configData)
				if expiresInt > 0 {
					configData["expires"] = expiresInt
					createPRLog.Printf("Converted expires from relative time format to hours: %d", expiresInt)
				}
			}
		}
	}

	// Unmarshal into typed config struct
	var config CreatePullRequestsConfig
	if err := unmarshalConfig(outputMap, "create-pull-request", &config, createPRLog); err != nil {
		createPRLog.Printf("Failed to unmarshal config: %v", err)
		// For backward compatibility, handle nil/empty config
		config = CreatePullRequestsConfig{}
	}

	// Validate target-repo (wildcard "*" is not allowed)
	if validateTargetRepoSlug(config.TargetRepoSlug, createPRLog) {
		return nil // Invalid configuration, return nil to cause validation error
	}

	// Log expires if configured
	if config.Expires > 0 {
		createPRLog.Printf("Pull request expiration configured: %d hours", config.Expires)
	}

	// Note: max parameter is not supported for pull requests (always limited to 1)
	// Override any user-specified max value to enforce the limit
	config.Max = 1

	return &config
}
