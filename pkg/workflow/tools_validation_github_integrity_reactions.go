package workflow

import (
	"errors"
	"fmt"

	"github.com/github/gh-aw/pkg/constants"
)

// validReactionContents is the set of valid GitHub ReactionContent enum values.
var validReactionContents = map[string]bool{
	"THUMBS_UP":   true,
	"THUMBS_DOWN": true,
	"HEART":       true,
	"HOORAY":      true,
	"CONFUSED":    true,
	"ROCKET":      true,
	"EYES":        true,
	"LAUGH":       true,
}

// validDisapprovalIntegrityLevels is the set of valid integrity levels for disapproval-integrity.
var validDisapprovalIntegrityLevels = map[GitHubIntegrityLevel]bool{
	GitHubIntegrityNone:       true,
	GitHubIntegrityUnapproved: true,
	GitHubIntegrityApproved:   true,
	GitHubIntegrityMerged:     true,
}

// validEndorserMinIntegrityLevels is the set of valid integrity levels for endorser-min-integrity.
var validEndorserMinIntegrityLevels = map[GitHubIntegrityLevel]bool{
	GitHubIntegrityUnapproved: true,
	GitHubIntegrityApproved:   true,
	GitHubIntegrityMerged:     true,
}

// validateIntegrityReactions validates the integrity-reactions feature configuration.
// It checks that:
//   - endorsement-reactions and disapproval-reactions contain valid ReactionContent values
//   - the integrity-reactions feature flag requires min-integrity to be set (defaults will be injected)
//   - disapproval-integrity and endorser-min-integrity use valid integrity levels
//   - the integrity-reactions feature flag requires MCPG >= v0.2.18
func validateIntegrityReactions(tools *Tools, workflowName string, data *WorkflowData, gatewayConfig *MCPGatewayRuntimeConfig) error {
	if tools == nil || tools.GitHub == nil {
		return nil
	}

	github := tools.GitHub

	hasEndorsementReactions := len(github.EndorsementReactions) > 0
	hasDisapprovalReactions := len(github.DisapprovalReactions) > 0
	hasDisapprovalIntegrity := github.DisapprovalIntegrity != ""
	hasEndorserMinIntegrity := github.EndorserMinIntegrity != ""
	hasExplicitReactionFields := hasEndorsementReactions || hasDisapprovalReactions || hasDisapprovalIntegrity || hasEndorserMinIntegrity
	featureEnabled := isFeatureEnabled(constants.IntegrityReactionsFeatureFlag, data)

	// If none of the reaction fields are set and the feature flag is not enabled, nothing to validate
	if !hasExplicitReactionFields && !featureEnabled {
		return nil
	}

	// Explicit reaction fields require the integrity-reactions feature flag
	if hasExplicitReactionFields && !featureEnabled {
		toolsValidationLog.Printf("Reaction fields present but integrity-reactions feature flag not enabled in workflow: %s", workflowName)
		return errors.New("guard policy fields 'endorsement-reactions', 'disapproval-reactions', 'disapproval-integrity', and 'endorser-min-integrity' require the 'integrity-reactions' feature flag. Example: features:\n  integrity-reactions: true")
	}

	// Feature flag requires MCPG >= v0.2.18
	if !mcpgSupportsIntegrityReactions(gatewayConfig) {
		version := string(constants.DefaultMCPGatewayVersion)
		if gatewayConfig != nil && gatewayConfig.Version != "" {
			version = gatewayConfig.Version
		}
		toolsValidationLog.Printf("integrity-reactions feature flag enabled but MCPG version %s < %s in workflow: %s", version, constants.MCPGIntegrityReactionsMinVersion, workflowName)
		return fmt.Errorf("guard policy feature 'integrity-reactions' requires MCPG >= %s, but the configured version is %s. Expected a gateway version at or above the minimum. Example:\ntools:\n  github:\n    mcp-gateway:\n      version: \"%s\"",
			constants.MCPGIntegrityReactionsMinVersion, version, constants.MCPGIntegrityReactionsMinVersion)
	}

	// Feature flag requires min-integrity (defaults for reaction lists will be injected at compile time)
	if github.MinIntegrity == "" {
		toolsValidationLog.Printf("integrity-reactions feature flag enabled without min-integrity in workflow: %s", workflowName)
		return errors.New("guard policy feature 'integrity-reactions' requires 'github.min-integrity' to be set. Expected one of: 'unapproved', 'approved', 'merged'. Example:\ntools:\n  github:\n    min-integrity: approved")
	}

	return validateIntegrityReactionValues(github, workflowName)
}

func validateIntegrityReactionValues(github *GitHubToolConfig, workflowName string) error {
	// Validate endorsement-reactions values (if explicitly provided)
	for i, reaction := range github.EndorsementReactions {
		if !validReactionContents[reaction] {
			toolsValidationLog.Printf("Invalid endorsement-reactions value '%s' at index %d in workflow: %s", reaction, i, workflowName)
			return fmt.Errorf("guard policy 'endorsement-reactions' value '%s' is not supported. Expected one of: THUMBS_UP, THUMBS_DOWN, HEART, HOORAY, CONFUSED, ROCKET, EYES, LAUGH. Example:\ntools:\n  github:\n    endorsement-reactions: [THUMBS_UP]", reaction)
		}
	}

	// Validate disapproval-reactions values (if explicitly provided)
	for i, reaction := range github.DisapprovalReactions {
		if !validReactionContents[reaction] {
			toolsValidationLog.Printf("Invalid disapproval-reactions value '%s' at index %d in workflow: %s", reaction, i, workflowName)
			return fmt.Errorf("guard policy 'disapproval-reactions' value '%s' is not supported. Expected one of: THUMBS_UP, THUMBS_DOWN, HEART, HOORAY, CONFUSED, ROCKET, EYES, LAUGH. Example:\ntools:\n  github:\n    disapproval-reactions: [THUMBS_DOWN]", reaction)
		}
	}

	// Validate disapproval-integrity value
	if github.DisapprovalIntegrity != "" && !validDisapprovalIntegrityLevels[github.DisapprovalIntegrity] {
		toolsValidationLog.Printf("Invalid disapproval-integrity value '%s' in workflow: %s", github.DisapprovalIntegrity, workflowName)
		return fmt.Errorf("guard policy 'disapproval-integrity' value '%s' is not supported. Expected one of: 'none', 'unapproved', 'approved', 'merged'. Example: disapproval-integrity: approved", github.DisapprovalIntegrity)
	}

	// Validate endorser-min-integrity value
	if github.EndorserMinIntegrity != "" && !validEndorserMinIntegrityLevels[github.EndorserMinIntegrity] {
		toolsValidationLog.Printf("Invalid endorser-min-integrity value '%s' in workflow: %s", github.EndorserMinIntegrity, workflowName)
		return fmt.Errorf("guard policy 'endorser-min-integrity' value '%s' is not supported. Expected one of: 'unapproved', 'approved', 'merged'. Example: endorser-min-integrity: approved", github.EndorserMinIntegrity)
	}

	return nil
}
