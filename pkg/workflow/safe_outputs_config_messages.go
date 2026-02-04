package workflow

import (
	"encoding/json"
	"fmt"

	"github.com/github/gh-aw/pkg/logger"
)

var safeOutputMessagesLog = logger.New("workflow:safe_outputs_config_messages")

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

	if footer, exists := messagesMap["footer"]; exists {
		if footerStr, ok := footer.(string); ok {
			config.Footer = footerStr
		}
	}

	if footerInstall, exists := messagesMap["footer-install"]; exists {
		if footerInstallStr, ok := footerInstall.(string); ok {
			config.FooterInstall = footerInstallStr
		}
	}

	if footerWorkflowRecompile, exists := messagesMap["footer-workflow-recompile"]; exists {
		if footerWorkflowRecompileStr, ok := footerWorkflowRecompile.(string); ok {
			config.FooterWorkflowRecompile = footerWorkflowRecompileStr
		}
	}

	if footerWorkflowRecompileComment, exists := messagesMap["footer-workflow-recompile-comment"]; exists {
		if footerWorkflowRecompileCommentStr, ok := footerWorkflowRecompileComment.(string); ok {
			config.FooterWorkflowRecompileComment = footerWorkflowRecompileCommentStr
		}
	}

	if stagedTitle, exists := messagesMap["staged-title"]; exists {
		if stagedTitleStr, ok := stagedTitle.(string); ok {
			config.StagedTitle = stagedTitleStr
		}
	}

	if stagedDescription, exists := messagesMap["staged-description"]; exists {
		if stagedDescriptionStr, ok := stagedDescription.(string); ok {
			config.StagedDescription = stagedDescriptionStr
		}
	}

	if runStarted, exists := messagesMap["run-started"]; exists {
		if runStartedStr, ok := runStarted.(string); ok {
			config.RunStarted = runStartedStr
		}
	}

	if runSuccess, exists := messagesMap["run-success"]; exists {
		if runSuccessStr, ok := runSuccess.(string); ok {
			config.RunSuccess = runSuccessStr
		}
	}

	if runFailure, exists := messagesMap["run-failure"]; exists {
		if runFailureStr, ok := runFailure.(string); ok {
			config.RunFailure = runFailureStr
		}
	}

	return config
}

// parseMentionsConfig parses the mentions configuration from safe-outputs frontmatter
// Mentions can be:
// - false: always escapes mentions
// - true: always allows mentions (error in strict mode)
// - object: detailed configuration with allow-team-members, allow-context, allowed, max
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
		// Parse allow-team-members
		if allowTeamMembers, exists := mentionsMap["allow-team-members"]; exists {
			if val, ok := allowTeamMembers.(bool); ok {
				config.AllowTeamMembers = &val
			}
		}

		// Parse allow-context
		if allowContext, exists := mentionsMap["allow-context"]; exists {
			if val, ok := allowContext.(bool); ok {
				config.AllowContext = &val
			}
		}

		// Parse allowed list
		if allowed, exists := mentionsMap["allowed"]; exists {
			if allowedArray, ok := allowed.([]any); ok {
				var allowedStrings []string
				for _, item := range allowedArray {
					if str, ok := item.(string); ok {
						allowedStrings = append(allowedStrings, str)
					}
				}
				config.Allowed = allowedStrings
			}
		}

		// Parse max
		if maxVal, exists := mentionsMap["max"]; exists {
			switch v := maxVal.(type) {
			case int:
				if v >= 1 {
					config.Max = &v
				}
			case int64:
				intVal := int(v)
				if intVal >= 1 {
					config.Max = &intVal
				}
			case uint64:
				intVal := int(v)
				if intVal >= 1 {
					config.Max = &intVal
				}
			case float64:
				intVal := int(v)
				// Warn if truncation occurs
				if v != float64(intVal) {
					safeOutputsConfigLog.Printf("mentions.max: float value %.2f truncated to integer %d", v, intVal)
				}
				if intVal >= 1 {
					config.Max = &intVal
				}
			}
		}
	}

	return config
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
