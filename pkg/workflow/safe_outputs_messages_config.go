package workflow

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var safeOutputMessagesLog = logger.New("workflow:safe_outputs_config_messages")

const disclosureHeaderDefaultSentinel = "true"

// ========================================
// Safe Output Messages Configuration
// ========================================

// parseMessagesConfig parses the messages configuration from safe-outputs frontmatter
func parseMessagesConfig(messagesMap map[string]any) *SafeOutputMessagesConfig {
	safeOutputMessagesLog.Printf("Parsing messages configuration with %d fields", len(messagesMap))
	config := &SafeOutputMessagesConfig{}

	if appendOnly, exists := messagesMap["append-only-comments"]; exists {
		if appendOnlyBool, ok := appendOnly.(bool); ok {
			config.AppendOnlyComments = appendOnlyBool
			safeOutputMessagesLog.Printf("Set append-only-comments: %t", appendOnlyBool)
		}
	}

	config.Footer = extractStringFromMap(messagesMap, "footer", nil)
	config.FooterInstall = extractStringFromMap(messagesMap, "footer-install", nil)
	config.FooterWorkflowRecompile = extractStringFromMap(messagesMap, "footer-workflow-recompile", nil)
	config.FooterWorkflowRecompileComment = extractStringFromMap(messagesMap, "footer-workflow-recompile-comment", nil)
	config.StagedTitle = extractStringFromMap(messagesMap, "staged-title", nil)
	config.StagedDescription = extractStringFromMap(messagesMap, "staged-description", nil)
	config.RunStarted = extractStringFromMap(messagesMap, "run-started", nil)
	config.RunSuccess = extractStringFromMap(messagesMap, "run-success", nil)
	config.RunFailure = extractStringFromMap(messagesMap, "run-failure", nil)
	config.DetectionFailure = extractStringFromMap(messagesMap, "detection-failure", nil)
	config.PullRequestCreated = extractStringFromMap(messagesMap, "pull-request-created", nil)
	config.IssueCreated = extractStringFromMap(messagesMap, "issue-created", nil)
	config.CommitPushed = extractStringFromMap(messagesMap, "commit-pushed", nil)
	config.AgentFailureIssue = extractStringFromMap(messagesMap, "agent-failure-issue", nil)
	config.AgentFailureComment = extractStringFromMap(messagesMap, "agent-failure-comment", nil)
	config.BodyHeader = extractStringFromMap(messagesMap, "body-header", nil)

	// Handle disclosure-header: can be bool (true for default built-in text) or custom string
	if dh, exists := messagesMap["disclosure-header"]; exists {
		switch v := dh.(type) {
		case bool:
			if v {
				config.DisclosureHeader = disclosureHeaderDefaultSentinel
			}
		case string:
			config.DisclosureHeader = v
		}
	}

	return config
}

// parseMentionsConfig parses the mentions configuration from safe-outputs frontmatter
// Mentions can be:
// - false: always escapes mentions
// - true: always allows mentions (error in strict mode)
// - object: detailed configuration with allowed-collaborators, allow-context, allowed, max
func parseMentionsConfig(mentions any) *MentionsConfig {
	safeOutputMessagesLog.Printf("Parsing mentions configuration: type=%T", mentions)
	config := &MentionsConfig{}

	// Handle boolean value
	if boolVal, ok := mentions.(bool); ok {
		config.Enabled = &boolVal
		safeOutputMessagesLog.Printf("Mentions configured as boolean: %t", boolVal)
		return config
	}

	// Handle object configuration
	if mentionsMap, ok := mentions.(map[string]any); ok {
		// Parse allowed-collaborators (preferred) with fallback to deprecated allow-team-members
		if allowedCollaborators, exists := mentionsMap["allowed-collaborators"]; exists {
			if val, ok := allowedCollaborators.(bool); ok {
				config.AllowedCollaborators = &val
			}
		} else if allowTeamMembers, exists := mentionsMap["allow-team-members"]; exists {
			if val, ok := allowTeamMembers.(bool); ok {
				config.AllowedCollaborators = &val
			}
		}

		// Parse allow-context
		if allowContext, exists := mentionsMap["allow-context"]; exists {
			if val, ok := allowContext.(bool); ok {
				config.AllowContext = &val
			}
		}

		config.Allowed = parseMentionNames(mentionsMap["allowed"])

		// Parse allowed-teams list
		config.AllowedTeams = parseMentionNames(mentionsMap["allowed-teams"])

		// Parse max as a templatable integer.
		if err := preprocessIntFieldAsString(mentionsMap, "max", safeOutputMessagesLog); err != nil {
			safeOutputMessagesLog.Printf("mentions.max: %v", err)
		} else if maxValue, exists := mentionsMap["max"]; exists {
			if max, ok := maxValue.(string); ok {
				if templatableIntValue(&max) >= 1 || isExpression(max) {
					config.Max = &max
				} else {
					safeOutputMessagesLog.Printf("mentions.max must be at least 1, got %q", max)
				}
			}
		}
	}

	return config
}

func parseMentionNames(value any) []string {
	names, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(names))
	for _, item := range names {
		name, ok := item.(string)
		if !ok {
			continue
		}
		normalized := strings.TrimPrefix(name, "@")
		if normalized != name {
			safeOutputMessagesLog.Printf("Normalized mention '%s' to '%s'", name, normalized)
		}
		result = append(result, normalized)
	}
	return result
}

// serializeMessagesConfig converts SafeOutputMessagesConfig to JSON for passing as environment variable
func serializeMessagesConfig(messages *SafeOutputMessagesConfig) (string, error) {
	if messages == nil {
		return "", nil
	}
	safeOutputMessagesLog.Print("Serializing messages configuration to JSON")
	jsonBytes, err := json.Marshal(messages)
	if err != nil {
		safeOutputMessagesLog.Printf("Failed to serialize messages config: %v", err)
		return "", fmt.Errorf("failed to serialize messages config: %w", err)
	}
	safeOutputMessagesLog.Printf("Serialized messages config: %d bytes", len(jsonBytes))
	return string(jsonBytes), nil
}
