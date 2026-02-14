package workflow

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var toolsValidationLog = logger.New("workflow:tools_validation")

// validateBashToolConfig validates that bash tool configuration is explicit (not nil/anonymous)
func validateBashToolConfig(tools *Tools, workflowName string) error {
	if tools == nil {
		return nil
	}

	// Check if bash is present in the raw map but Bash field is nil
	// This indicates the anonymous syntax (bash:) was used
	if rawMap := tools.ToMap(); rawMap != nil {
		if _, hasBash := rawMap["bash"]; hasBash && tools.Bash == nil {
			toolsValidationLog.Printf("Invalid bash tool configuration in workflow: %s", workflowName)
			return fmt.Errorf("invalid bash tool configuration: anonymous syntax 'bash:' is not supported. Use 'bash: true' (enable all commands), 'bash: false' (disable), or 'bash: [\"cmd1\", \"cmd2\"]' (specific commands). Run 'gh aw fix' to automatically migrate")
		}
	}

	return nil
}

// isGitToolAllowed checks if git commands are allowed in bash tool configuration
func isGitToolAllowed(tools *Tools) bool {
	if tools == nil {
		// No tools configured - defaults will be applied which include git for PR operations
		return true
	}

	if tools.Bash == nil {
		// No bash tool configured - defaults will be applied which include git for PR operations
		return true
	}

	// If AllowedCommands is nil or empty, check which case it is:
	// - nil AllowedCommands = bash: true (all commands allowed, including git)
	// - empty slice = bash: false (explicitly disabled)
	if tools.Bash.AllowedCommands == nil {
		// bash: true - all commands allowed
		return true
	}

	if len(tools.Bash.AllowedCommands) == 0 {
		// bash: false or bash: [] - explicitly disabled or no commands
		return false
	}

	// Check if git is in the allowed commands list
	for _, cmd := range tools.Bash.AllowedCommands {
		if cmd == "*" {
			// Wildcard allows all commands
			return true
		}
		if cmd == "git" {
			// Exact match for git command
			return true
		}
		// Check for git with wildcards: "git *", "git:*", "git checkout:*", etc.
		if strings.HasPrefix(cmd, "git ") || strings.HasPrefix(cmd, "git:") {
			return true
		}
	}

	return false
}

// validateGitToolForSafeOutputs validates that workflows using create-pull-request or
// push-to-pull-request-branch have git tool allowed in their bash configuration
func validateGitToolForSafeOutputs(tools *Tools, safeOutputs *SafeOutputsConfig, workflowName string) error {
	if safeOutputs == nil {
		return nil
	}

	// Check if workflow uses create-pull-request or push-to-pull-request-branch
	usesCreatePR := safeOutputs.CreatePullRequests != nil
	usesPushToBranch := safeOutputs.PushToPullRequestBranch != nil

	if !usesCreatePR && !usesPushToBranch {
		// Workflow doesn't use these features, no validation needed
		return nil
	}

	// Check if git tool is allowed
	if !isGitToolAllowed(tools) {
		var feature string
		if usesCreatePR && usesPushToBranch {
			feature = "create-pull-request and push-to-pull-request-branch"
		} else if usesCreatePR {
			feature = "create-pull-request"
		} else {
			feature = "push-to-pull-request-branch"
		}

		toolsValidationLog.Printf("Workflow %s uses %s but git tool is not allowed", workflowName, feature)
		return fmt.Errorf("workflow uses %s but git tool is not allowed in bash configuration. Add 'bash: true' (all commands), 'bash: [\"git\"]' (git only), or 'bash: [\"*\"]' (wildcard) to enable git commands", feature)
	}

	return nil
}
