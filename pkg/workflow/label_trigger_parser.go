package workflow

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var labelTriggerParserLog = logger.New("workflow:label_trigger_parser")

// parseLabelTriggerShorthand parses a string in the format
// "issue labeled label1 label2 ...", "pull_request labeled label1 label2 ...",
// "pull-request labeled label1 label2 ...", or "discussion labeled label1 label2 ..."
// and returns the entity type and label names.
// Returns an empty string for entityType if not a valid label trigger shorthand.
// Returns an error if the format is invalid.
func parseLabelTriggerShorthand(input string) (entityType string, labelNames []string, isLabelTrigger bool, err error) {
	input = strings.TrimSpace(input)

	// Split into tokens
	tokens := strings.Fields(input)
	if len(tokens) < 3 {
		// Need at least: "issue/pull_request/discussion labeled label1"
		return "", nil, false, nil
	}

	// Check for different patterns:
	// 1. "issue labeled label1 label2 ..." or "issue labeled label1, label2, ..."
	// 2. "pull_request labeled label1 label2 ..." or "pull-request labeled label1, label2, ..."
	// 3. "discussion labeled label1 label2 ..." or "discussion labeled label1, label2, ..."

	var startIdx int

	if tokens[0] == "issue" && tokens[1] == "labeled" {
		// Pattern 1: "issue labeled label1 label2 ..." or "issue labeled label1, label2, ..."
		entityType = "issues"
		startIdx = 2
	} else if (tokens[0] == "pull_request" || tokens[0] == "pull-request") && tokens[1] == "labeled" {
		// Pattern 2: "pull_request labeled label1 label2 ..." or "pull-request labeled label1, label2, ..."
		entityType = "pull_request"
		startIdx = 2
	} else if tokens[0] == "discussion" && tokens[1] == "labeled" {
		// Pattern 3: "discussion labeled label1 label2 ..." or "discussion labeled label1, label2, ..."
		entityType = "discussion"
		startIdx = 2
	} else {
		// Not a label trigger shorthand
		return "", nil, false, nil
	}

	// Extract label names
	if len(tokens) <= startIdx {
		return "", nil, true, fmt.Errorf("label trigger shorthand requires at least one label name")
	}

	// Process label names: handle both space-separated and comma-separated formats
	rawLabels := tokens[startIdx:]
	for _, token := range rawLabels {
		// Split on commas to handle "label1,label2,label3" format
		parts := strings.Split(token, ",")
		for _, part := range parts {
			cleanLabel := strings.TrimSpace(part)
			if cleanLabel != "" {
				labelNames = append(labelNames, cleanLabel)
			}
		}
	}

	// Validate we have at least one label after processing
	if len(labelNames) == 0 {
		return "", nil, true, fmt.Errorf("label trigger shorthand requires at least one label name")
	}

	labelTriggerParserLog.Printf("Parsed label trigger shorthand: %s -> entity: %s, labels: %v", input, entityType, labelNames)

	return entityType, labelNames, true, nil
}

// expandLabelTriggerShorthand takes an entity type and label names and returns a map that represents
// the expanded label trigger + workflow_dispatch configuration with item_number input.
// Note: For discussion events, GitHub Actions doesn't support the `names` field,
// so we use the native label filter marker but the names will be filtered via job conditions.
func expandLabelTriggerShorthand(entityType string, labelNames []string) map[string]any {
	// Create the trigger configuration based on entity type
	var triggerKey string
	switch entityType {
	case "issues":
		triggerKey = "issues"
	case "pull_request":
		triggerKey = "pull_request"
	case "discussion":
		triggerKey = "discussion"
	default:
		triggerKey = "issues" // Default to issues (though this shouldn't happen with our parser)
	}

	// Build the trigger configuration
	// Add a marker to indicate this uses native GitHub Actions label filtering
	// (not job condition filtering), so names should not be commented out
	// Note: For discussion events, GitHub Actions doesn't support names field,
	// so we don't include it but still use the marker to indicate shorthand expansion
	triggerConfig := map[string]any{
		"types": []any{"labeled"},
	}

	// Only add names field for issues and pull_request (GitHub Actions supports it)
	// For discussions, names field is not supported by GitHub Actions
	if entityType == "issues" || entityType == "pull_request" {
		triggerConfig["names"] = labelNames
		triggerConfig["__gh_aw_native_label_filter__"] = true // Marker to prevent commenting out names
	}

	// Create workflow_dispatch with item_number input
	workflowDispatchConfig := map[string]any{
		"inputs": map[string]any{
			"item_number": map[string]any{
				"description": "The number of the " + getItemTypeName(entityType),
				"required":    true,
				"type":        "string",
			},
		},
	}

	return map[string]any{
		triggerKey:          triggerConfig,
		"workflow_dispatch": workflowDispatchConfig,
	}
}

// getItemTypeName returns the human-readable item type name for the entity type
func getItemTypeName(entityType string) string {
	switch entityType {
	case "issues":
		return "issue"
	case "pull_request":
		return "pull request"
	case "discussion":
		return "discussion"
	default:
		return "item" // Fallback (though this shouldn't happen with our parser)
	}
}
