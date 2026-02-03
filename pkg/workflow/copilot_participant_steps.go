package workflow

import (
	"fmt"

	"github.com/github/gh-aw/pkg/logger"
)

var copilotParticipantLog = logger.New("workflow:copilot_participant_steps")

// CopilotParticipantConfig holds configuration for generating Copilot participant steps
type CopilotParticipantConfig struct {
	// Participants is the list of users/bots to assign/review
	Participants []string
	// ParticipantType is either "assignee" or "reviewer"
	ParticipantType string
	// CustomToken is the custom GitHub token from the safe output config
	CustomToken string
	// SafeOutputsToken is the GitHub token from the safe-outputs config
	SafeOutputsToken string
	// WorkflowToken is the top-level GitHub token from the workflow
	WorkflowToken string
	// ConditionStepID is the step ID to check for output (e.g., "create_issue", "create_pull_request")
	ConditionStepID string
	// ConditionOutputKey is the output key to check (e.g., "issue_number", "pull_request_url")
	ConditionOutputKey string
}

// buildCopilotParticipantSteps generates steps for adding Copilot participants (assignees or reviewers)
// This function extracts the common logic between issue assignees and PR reviewers
func buildCopilotParticipantSteps(config CopilotParticipantConfig) []string {
	copilotParticipantLog.Printf("Building Copilot participant steps: type=%s, count=%d", config.ParticipantType, len(config.Participants))

	if len(config.Participants) == 0 {
		copilotParticipantLog.Print("No participants to add, returning empty steps")
		return nil
	}

	var steps []string

	// Add checkout step for gh CLI to work
	steps = append(steps, "      - name: Checkout repository for gh CLI\n")
	steps = append(steps, fmt.Sprintf("        if: steps.%s.outputs.%s != ''\n", config.ConditionStepID, config.ConditionOutputKey))
	steps = append(steps, fmt.Sprintf("        uses: %s\n", GetActionPin("actions/checkout")))
	steps = append(steps, "        with:\n")
	steps = append(steps, "          persist-credentials: false\n")

	// Check if any participant is "copilot" to determine token preference
	hasCopilotParticipant := false
	for _, participant := range config.Participants {
		if participant == "copilot" {
			hasCopilotParticipant = true
			break
		}
	}

	// Use agent token preference if adding copilot as participant, otherwise use regular token
	var effectiveToken string
	if hasCopilotParticipant {
		copilotParticipantLog.Print("Using agent token preference")
		effectiveToken = getEffectiveAgentGitHubToken(config.CustomToken, getEffectiveAgentGitHubToken(config.SafeOutputsToken, config.WorkflowToken))
	} else {
		copilotParticipantLog.Print("Using regular GitHub token")
		effectiveToken = getEffectiveGitHubToken(config.CustomToken, getEffectiveGitHubToken(config.SafeOutputsToken, config.WorkflowToken))
	}

	// Generate participant-specific steps
	switch config.ParticipantType {
	case "assignee":
		copilotParticipantLog.Printf("Generating issue assignee steps for %d participants", len(config.Participants))
		steps = append(steps, buildIssueAssigneeSteps(config, effectiveToken)...)
	case "reviewer":
		copilotParticipantLog.Printf("Generating PR reviewer steps for %d participants", len(config.Participants))
		steps = append(steps, buildPRReviewerSteps(config, effectiveToken)...)
	}

	return steps
}

// buildIssueAssigneeSteps generates steps for assigning issues
func buildIssueAssigneeSteps(config CopilotParticipantConfig, effectiveToken string) []string {
	var steps []string

	for i, assignee := range config.Participants {
		// Special handling: "copilot" should be passed as "@copilot" to gh CLI
		actualAssignee := assignee
		if assignee == "copilot" {
			actualAssignee = "@copilot"
		}

		steps = append(steps, fmt.Sprintf("      - name: Assign issue to %s\n", assignee))
		steps = append(steps, fmt.Sprintf("        if: steps.%s.outputs.%s != ''\n", config.ConditionStepID, config.ConditionOutputKey))
		steps = append(steps, fmt.Sprintf("        uses: %s\n", GetActionPin("actions/github-script")))
		steps = append(steps, "        env:\n")
		steps = append(steps, fmt.Sprintf("          GH_TOKEN: %s\n", effectiveToken))
		steps = append(steps, fmt.Sprintf("          ASSIGNEE: %q\n", actualAssignee))
		steps = append(steps, fmt.Sprintf("          ISSUE_NUMBER: ${{ steps.%s.outputs.%s }}\n", config.ConditionStepID, config.ConditionOutputKey))
		steps = append(steps, "        with:\n")
		steps = append(steps, "          script: |\n")
		steps = append(steps, "            const { setupGlobals } = require('"+SetupActionDestination+"/setup_globals.cjs');\n")
		steps = append(steps, "            setupGlobals(core, github, context, exec, io);\n")
		// Load script from external file using require()
		steps = append(steps, "            const { main } = require('/opt/gh-aw/actions/assign_issue.cjs');\n")
		steps = append(steps, "            await main({ github, context, core, exec, io });\n")

		// Add a comment after each assignee step except the last
		if i < len(config.Participants)-1 {
			steps = append(steps, "\n")
		}
	}

	return steps
}

// buildPRReviewerSteps generates steps for adding PR reviewers
func buildPRReviewerSteps(config CopilotParticipantConfig, effectiveToken string) []string {
	var steps []string

	for i, reviewer := range config.Participants {
		// Special handling: "copilot" uses the GitHub API with "copilot-pull-request-reviewer[bot]"
		// because gh pr edit --add-reviewer does not support @copilot
		if reviewer == "copilot" {
			steps = append(steps, fmt.Sprintf("      - name: Add %s as reviewer\n", reviewer))
			steps = append(steps, "        if: steps.create_pull_request.outputs.pull_request_number != ''\n")
			steps = append(steps, fmt.Sprintf("        uses: %s\n", GetActionPin("actions/github-script")))
			steps = append(steps, "        env:\n")
			steps = append(steps, "          PR_NUMBER: ${{ steps.create_pull_request.outputs.pull_request_number }}\n")
			steps = append(steps, "        with:\n")
			steps = append(steps, fmt.Sprintf("          github-token: %s\n", effectiveToken))
			steps = append(steps, "          script: |\n")
			steps = append(steps, "            const { setupGlobals } = require('"+SetupActionDestination+"/setup_globals.cjs');\n")
			steps = append(steps, "            setupGlobals(core, github, context, exec, io);\n")
			// Load script from external file using require()
			steps = append(steps, "            const { main } = require('/opt/gh-aw/actions/add_copilot_reviewer.cjs');\n")
			steps = append(steps, "            await main({ github, context, core, exec, io });\n")
		} else {
			steps = append(steps, fmt.Sprintf("      - name: Add %s as reviewer\n", reviewer))
			steps = append(steps, "        if: steps.create_pull_request.outputs.pull_request_url != ''\n")
			steps = append(steps, "        env:\n")
			steps = append(steps, fmt.Sprintf("          GH_TOKEN: %s\n", effectiveToken))
			steps = append(steps, fmt.Sprintf("          REVIEWER: %q\n", reviewer))
			steps = append(steps, "          PR_URL: ${{ steps.create_pull_request.outputs.pull_request_url }}\n")
			steps = append(steps, "        run: |\n")
			steps = append(steps, "          gh pr edit \"$PR_URL\" --add-reviewer \"$REVIEWER\"\n")
		}

		// Add a comment after each reviewer step except the last
		if i < len(config.Participants)-1 {
			steps = append(steps, "\n")
		}
	}

	return steps
}
