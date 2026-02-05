package workflow

import (
	"fmt"

	"github.com/github/gh-aw/pkg/logger"
)

var createIssueLog = logger.New("workflow:create_issue")

// CreateIssuesConfig holds configuration for creating GitHub issues from agent output
type CreateIssuesConfig struct {
	BaseSafeOutputConfig `yaml:",inline"`
	TitlePrefix          string   `yaml:"title-prefix,omitempty"`
	Labels               []string `yaml:"labels,omitempty"`
	AllowedLabels        []string `yaml:"allowed-labels,omitempty"`     // Optional list of allowed labels. If omitted, any labels are allowed (including creating new ones).
	Assignees            []string `yaml:"assignees,omitempty"`          // List of users/bots to assign the issue to
	TargetRepoSlug       string   `yaml:"target-repo,omitempty"`        // Target repository in format "owner/repo" for cross-repository issues
	AllowedRepos         []string `yaml:"allowed-repos,omitempty"`      // List of additional repositories that issues can be created in
	CloseOlderIssues     bool     `yaml:"close-older-issues,omitempty"` // When true, close older issues with same title prefix or labels as "not planned"
	Expires              int      `yaml:"expires,omitempty"`            // Hours until the issue expires and should be automatically closed
	Group                bool     `yaml:"group,omitempty"`              // If true, group issues as sub-issues under a parent issue (workflow ID is used as group identifier)
}

// parseIssuesConfig handles create-issue configuration
func (c *Compiler) parseIssuesConfig(outputMap map[string]any) *CreateIssuesConfig {
	// Check if the key exists
	if _, exists := outputMap["create-issue"]; !exists {
		return nil
	}

	createIssueLog.Print("Parsing create-issue configuration")

	// Get the config data to check for special cases before unmarshaling
	configData, _ := outputMap["create-issue"].(map[string]any)

	// Pre-process the expires field (convert to hours before unmarshaling)
	expiresDisabled := false
	if configData != nil {
		if expires, exists := configData["expires"]; exists {
			// Always parse the expires value through parseExpiresFromConfig
			// This handles: integers (days), strings (time specs like "48h"), and boolean false
			expiresInt := parseExpiresFromConfig(configData)
			if expiresInt == -1 {
				// Explicitly disabled with false
				expiresDisabled = true
				configData["expires"] = 0
			} else if expiresInt > 0 {
				configData["expires"] = expiresInt
			} else {
				// Invalid or missing - set to 0
				configData["expires"] = 0
			}
			createIssueLog.Printf("Parsed expires value %v to %d hours (disabled=%t)", expires, expiresInt, expiresDisabled)
		}
	}

	// Unmarshal into typed config struct
	var config CreateIssuesConfig
	if err := unmarshalConfig(outputMap, "create-issue", &config, createIssueLog); err != nil {
		createIssueLog.Printf("Failed to unmarshal config: %v", err)
		// For backward compatibility, handle nil/empty config
		config = CreateIssuesConfig{}
	}

	// Handle single string assignee (YAML unmarshaling won't convert string to []string)
	if len(config.Assignees) == 0 && configData != nil {
		if assignees, exists := configData["assignees"]; exists {
			if assigneeStr, ok := assignees.(string); ok {
				config.Assignees = []string{assigneeStr}
				createIssueLog.Printf("Converted single assignee string to array: %v", config.Assignees)
			}
		}
	}

	// Set default max if not specified
	if config.Max == 0 {
		config.Max = 1
	}

	// Validate target-repo (wildcard "*" is not allowed)
	if validateTargetRepoSlug(config.TargetRepoSlug, createIssueLog) {
		return nil // Invalid configuration, return nil to cause validation error
	}

	// Log expires if configured or explicitly disabled
	if expiresDisabled {
		createIssueLog.Print("Issue expiration explicitly disabled")
	} else if config.Expires > 0 {
		createIssueLog.Printf("Issue expiration configured: %d hours", config.Expires)
	}

	return &config
}

// hasCopilotAssignee checks if "copilot" is in the assignees list
func hasCopilotAssignee(assignees []string) bool {
	for _, a := range assignees {
		if a == "copilot" {
			return true
		}
	}
	return false
}

// filterNonCopilotAssignees returns assignees excluding "copilot"
func filterNonCopilotAssignees(assignees []string) []string {
	var result []string
	for _, a := range assignees {
		if a != "copilot" {
			result = append(result, a)
		}
	}
	return result
}

// buildCopilotAssignmentStep generates a post-step for assigning copilot to created issues
// This step uses the agent token with full precedence chain
func buildCopilotAssignmentStep(configToken, safeOutputsToken, workflowToken string) []string {
	var steps []string

	// Get the effective agent token with full precedence chain
	effectiveToken := getEffectiveAgentGitHubToken(configToken, getEffectiveAgentGitHubToken(safeOutputsToken, workflowToken))

	steps = append(steps, "      - name: Assign Copilot to created issues\n")
	steps = append(steps, "        if: steps.create_issue.outputs.issues_to_assign_copilot != ''\n")
	steps = append(steps, fmt.Sprintf("        uses: %s\n", GetActionPin("actions/github-script")))
	steps = append(steps, "        with:\n")
	steps = append(steps, fmt.Sprintf("          github-token: %s\n", effectiveToken))
	steps = append(steps, "          script: |\n")
	steps = append(steps, "            const { setupGlobals } = require('"+SetupActionDestination+"/setup_globals.cjs');\n")
	steps = append(steps, "            setupGlobals(core, github, context, exec, io);\n")
	// Load script from external file using require()
	steps = append(steps, "            const { main } = require('/opt/gh-aw/actions/assign_copilot_to_created_issues.cjs');\n")
	steps = append(steps, "            await main({ github, context, core, exec, io });\n")

	return steps
}

// buildCreateOutputIssueJob creates the create_issue job
func (c *Compiler) buildCreateOutputIssueJob(data *WorkflowData, mainJobName string) (*Job, error) {
	if data.SafeOutputs == nil || data.SafeOutputs.CreateIssues == nil {
		return nil, fmt.Errorf("safe-outputs.create-issue configuration is required")
	}

	if createIssueLog.Enabled() {
		createIssueLog.Printf("Building create-issue job: workflow=%s, main_job=%s, assignees=%d, labels=%d",
			data.Name, mainJobName, len(data.SafeOutputs.CreateIssues.Assignees), len(data.SafeOutputs.CreateIssues.Labels))
	}

	// Build custom environment variables specific to create-issue using shared helpers
	var customEnvVars []string
	customEnvVars = append(customEnvVars, buildTitlePrefixEnvVar("GH_AW_ISSUE_TITLE_PREFIX", data.SafeOutputs.CreateIssues.TitlePrefix)...)
	customEnvVars = append(customEnvVars, buildLabelsEnvVar("GH_AW_ISSUE_LABELS", data.SafeOutputs.CreateIssues.Labels)...)
	customEnvVars = append(customEnvVars, buildLabelsEnvVar("GH_AW_ISSUE_ALLOWED_LABELS", data.SafeOutputs.CreateIssues.AllowedLabels)...)
	customEnvVars = append(customEnvVars, buildAllowedReposEnvVar("GH_AW_ALLOWED_REPOS", data.SafeOutputs.CreateIssues.AllowedRepos)...)

	// Add expires value if set
	if data.SafeOutputs.CreateIssues.Expires > 0 {
		customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_ISSUE_EXPIRES: \"%d\"\n", data.SafeOutputs.CreateIssues.Expires))
	}

	// Add group flag if set
	if data.SafeOutputs.CreateIssues.Group {
		customEnvVars = append(customEnvVars, "          GH_AW_ISSUE_GROUP: \"true\"\n")
		createIssueLog.Print("Issue grouping enabled - issues will be grouped as sub-issues under parent")
	}

	// Add close-older-issues flag if enabled
	if data.SafeOutputs.CreateIssues.CloseOlderIssues {
		customEnvVars = append(customEnvVars, "          GH_AW_CLOSE_OLDER_ISSUES: \"true\"\n")
		createIssueLog.Print("Close older issues enabled - older issues with same title prefix or labels will be closed")
	}

	// Add standard environment variables (metadata + staged/target repo)
	customEnvVars = append(customEnvVars, c.buildStandardSafeOutputEnvVars(data, data.SafeOutputs.CreateIssues.TargetRepoSlug)...)

	// Check if copilot is in assignees - if so, we'll output issues for assign_to_agent job
	assignCopilot := hasCopilotAssignee(data.SafeOutputs.CreateIssues.Assignees)
	if assignCopilot {
		customEnvVars = append(customEnvVars, "          GH_AW_ASSIGN_COPILOT: \"true\"\n")
		createIssueLog.Print("Copilot assignment requested - will output issues_to_assign_copilot for assign_to_agent job")
	}

	// Build post-steps for non-copilot assignees only
	// Copilot assignment must be done in a separate step with the agent token
	var postSteps []string

	// Get the effective GitHub token to use for gh CLI
	var safeOutputsToken string
	if data.SafeOutputs != nil {
		safeOutputsToken = data.SafeOutputs.GitHubToken
	}

	nonCopilotAssignees := filterNonCopilotAssignees(data.SafeOutputs.CreateIssues.Assignees)
	if len(nonCopilotAssignees) > 0 {
		postSteps = buildCopilotParticipantSteps(CopilotParticipantConfig{
			Participants:       nonCopilotAssignees,
			ParticipantType:    "assignee",
			CustomToken:        data.SafeOutputs.CreateIssues.GitHubToken,
			SafeOutputsToken:   safeOutputsToken,
			WorkflowToken:      data.GitHubToken,
			ConditionStepID:    "create_issue",
			ConditionOutputKey: "issue_number",
		})
	}

	// Add post-step for copilot assignment using agent token
	if assignCopilot {
		postSteps = append(postSteps, buildCopilotAssignmentStep(data.SafeOutputs.CreateIssues.GitHubToken, safeOutputsToken, data.GitHubToken)...)
	}

	// Create outputs for the job
	outputs := map[string]string{
		"issue_number":     "${{ steps.create_issue.outputs.issue_number }}",
		"issue_url":        "${{ steps.create_issue.outputs.issue_url }}",
		"temporary_id_map": "${{ steps.create_issue.outputs.temporary_id_map }}",
	}

	// Add issues_to_assign_copilot output if copilot assignment is requested
	if assignCopilot {
		outputs["issues_to_assign_copilot"] = "${{ steps.create_issue.outputs.issues_to_assign_copilot }}"
	}

	// Use the shared builder function to create the job
	return c.buildSafeOutputJob(data, SafeOutputJobConfig{
		JobName:        "create_issue",
		StepName:       "Create Output Issue",
		StepID:         "create_issue",
		MainJobName:    mainJobName,
		CustomEnvVars:  customEnvVars,
		Script:         getCreateIssueScript(),
		ScriptName:     "create_issue", // For custom action mode
		Permissions:    NewPermissionsContentsReadIssuesWrite(),
		Outputs:        outputs,
		PostSteps:      postSteps,
		Token:          data.SafeOutputs.CreateIssues.GitHubToken,
		TargetRepoSlug: data.SafeOutputs.CreateIssues.TargetRepoSlug,
	})
}
