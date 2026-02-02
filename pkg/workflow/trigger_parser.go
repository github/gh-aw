package workflow

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var triggerParserLog = logger.New("workflow:trigger_parser")

// TriggerIR represents the intermediate representation of a parsed trigger
type TriggerIR struct {
	// Event is the main GitHub Actions event type (e.g., "push", "pull_request", "issues")
	Event string

	// Types contains the activity types for the event (e.g., ["opened", "edited"])
	Types []string

	// Filters contains additional event filters (branches, paths, tags, labels, etc.)
	Filters map[string]any

	// Conditions contains job-level conditions for complex filtering
	Conditions []string

	// AdditionalEvents contains other events to include (e.g., workflow_dispatch)
	AdditionalEvents map[string]any
}

// ParseTriggerShorthand parses a human-readable trigger shorthand string
// and returns a structured intermediate representation that can be converted to YAML.
// Returns nil if the input is not a recognized trigger shorthand.
func ParseTriggerShorthand(input string) (*TriggerIR, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, fmt.Errorf("trigger shorthand cannot be empty")
	}

	triggerParserLog.Printf("Parsing trigger shorthand: %s", input)

	// Try parsers in order of specificity:

	// 1. Slash command shorthand (starts with /)
	if ir, err := parseSlashCommandTrigger(input); ir != nil || err != nil {
		return ir, err
	}

	// 2. Label trigger shorthand (entity labeled label1 label2...)
	if ir, err := parseLabelTrigger(input); ir != nil || err != nil {
		return ir, err
	}

	// 3. Source control patterns (push, pull request, etc.)
	if ir, err := parseSourceControlTrigger(input); ir != nil || err != nil {
		return ir, err
	}

	// 4. Issue and discussion patterns
	if ir, err := parseIssueDiscussionTrigger(input); ir != nil || err != nil {
		return ir, err
	}

	// 5. Manual invocation patterns
	if ir, err := parseManualTrigger(input); ir != nil || err != nil {
		return ir, err
	}

	// 6. Comment patterns
	if ir, err := parseCommentTrigger(input); ir != nil || err != nil {
		return ir, err
	}

	// 7. Release and repository patterns
	if ir, err := parseReleaseRepositoryTrigger(input); ir != nil || err != nil {
		return ir, err
	}

	// 8. Security patterns
	if ir, err := parseSecurityTrigger(input); ir != nil || err != nil {
		return ir, err
	}

	// 9. External integration patterns
	if ir, err := parseExternalTrigger(input); ir != nil || err != nil {
		return ir, err
	}

	// Not a recognized trigger shorthand
	return nil, nil
}

// ToYAMLMap converts a TriggerIR to a map structure suitable for YAML generation
func (ir *TriggerIR) ToYAMLMap() map[string]any {
	result := make(map[string]any)

	// Add the main event
	if ir.Event != "" {
		eventConfig := make(map[string]any)

		// Add types if specified
		if len(ir.Types) > 0 {
			eventConfig["types"] = ir.Types
		}

		// Add filters
		for key, value := range ir.Filters {
			eventConfig[key] = value
		}

		// If event config has content, add it; otherwise omit the event entirely for simple triggers
		if len(eventConfig) > 0 {
			result[ir.Event] = eventConfig
		} else {
			// For events with no configuration, use an empty map instead of nil
			// This ensures proper YAML generation without "null" values
			result[ir.Event] = map[string]any{}
		}
	}

	// Add additional events
	for event, config := range ir.AdditionalEvents {
		result[event] = config
	}

	return result
}

// parseSlashCommandTrigger parses slash command triggers like "/test"
func parseSlashCommandTrigger(input string) (*TriggerIR, error) {
	commandName, isSlashCommand, err := parseSlashCommandShorthand(input)
	if err != nil {
		return nil, err
	}
	if !isSlashCommand {
		return nil, nil
	}

	triggerParserLog.Printf("Parsed slash command trigger: %s", commandName)

	// Note: slash_command is handled specially in the compiler, not as a standard GitHub event
	// We return nil here to let the existing slash command processing handle it
	return nil, nil
}

// parseLabelTrigger parses label triggers like "issue labeled bug" or "pull_request labeled needs-review"
func parseLabelTrigger(input string) (*TriggerIR, error) {
	entityType, labelNames, isLabelTrigger, err := parseLabelTriggerShorthand(input)
	if err != nil {
		return nil, err
	}
	if !isLabelTrigger {
		return nil, nil
	}

	triggerParserLog.Printf("Parsed label trigger: %s labeled %v", entityType, labelNames)

	// Note: Label triggers are handled specially via expandLabelTriggerShorthand
	// We return nil here to let the existing label trigger processing handle it
	return nil, nil
}

// parseSourceControlTrigger parses source control triggers
func parseSourceControlTrigger(input string) (*TriggerIR, error) {
	tokens := strings.Fields(input)
	if len(tokens) == 0 {
		return nil, nil
	}

	switch tokens[0] {
	case "push":
		return parsePushTrigger(tokens)
	case "pull", "pull_request":
		// Normalize "pull" to "pull_request"
		normalizedTokens := append([]string{"pull_request"}, tokens[1:]...)
		return parsePullRequestTrigger(normalizedTokens)
	default:
		return nil, nil
	}
}

// parsePushTrigger parses push-related triggers
func parsePushTrigger(tokens []string) (*TriggerIR, error) {
	if len(tokens) == 1 {
		// Simple "push" trigger - leave as simple string, don't convert
		// GitHub Actions supports simple event names as strings: on: push
		return nil, nil
	}

	if len(tokens) >= 3 && tokens[1] == "to" {
		// "push to <branch>"
		branch := strings.Join(tokens[2:], " ")
		return &TriggerIR{
			Event: "push",
			Filters: map[string]any{
				"branches": []string{branch},
			},
			AdditionalEvents: map[string]any{
				"workflow_dispatch": nil,
			},
		}, nil
	}

	if len(tokens) >= 3 && tokens[1] == "tags" {
		// "push tags <pattern>"
		pattern := strings.Join(tokens[2:], " ")
		return &TriggerIR{
			Event: "push",
			Filters: map[string]any{
				"tags": []string{pattern},
			},
			AdditionalEvents: map[string]any{
				"workflow_dispatch": nil,
			},
		}, nil
	}

	return nil, fmt.Errorf("invalid push trigger format: '%s'. Expected format: 'push to <branch>' or 'push tags <pattern>'. Example: 'push to main' or 'push tags v*'", strings.Join(tokens, " "))
}

// parsePullRequestTrigger parses pull request triggers
func parsePullRequestTrigger(tokens []string) (*TriggerIR, error) {
	if len(tokens) == 1 {
		// Simple "pull_request" trigger - leave as simple string
		// GitHub Actions supports: on: pull_request
		return nil, nil
	}

	// Check for activity type: "pull_request opened", "pull_request merged", etc.
	activityType := tokens[1]

	// Map common activity types
	validTypes := map[string]bool{
		"opened":           true,
		"edited":           true,
		"closed":           true,
		"reopened":         true,
		"synchronize":      true,
		"assigned":         true,
		"unassigned":       true,
		"labeled":          true,
		"unlabeled":        true,
		"review_requested": true,
	}

	// Special case: "merged" is not a real type, it's a condition on "closed"
	if activityType == "merged" {
		return &TriggerIR{
			Event:      "pull_request",
			Types:      []string{"closed"},
			Conditions: []string{"github.event.pull_request.merged == true"},
			AdditionalEvents: map[string]any{
				"workflow_dispatch": nil,
			},
		}, nil
	}

	if validTypes[activityType] {
		ir := &TriggerIR{
			Event: "pull_request",
			Types: []string{activityType},
			AdditionalEvents: map[string]any{
				"workflow_dispatch": nil,
			},
		}

		// Check for path filter: "pull_request opened affecting <path>"
		if len(tokens) >= 4 && tokens[2] == "affecting" {
			path := strings.Join(tokens[3:], " ")
			ir.Filters = map[string]any{
				"paths": []string{path},
			}
		}

		return ir, nil
	}

	// Check for "affecting" without activity type: "pull_request affecting <path>"
	if activityType == "affecting" && len(tokens) >= 3 {
		path := strings.Join(tokens[2:], " ")
		return &TriggerIR{
			Event: "pull_request",
			Types: []string{"opened", "synchronize", "reopened"},
			Filters: map[string]any{
				"paths": []string{path},
			},
			AdditionalEvents: map[string]any{
				"workflow_dispatch": nil,
			},
		}, nil
	}

	return nil, fmt.Errorf("invalid pull_request trigger format: '%s'. Expected format: 'pull_request <type>' or 'pull_request affecting <path>'. Valid types: opened, edited, closed, reopened, synchronize, merged, labeled, unlabeled. Example: 'pull_request opened' or 'pull_request affecting src/**'", strings.Join(tokens, " "))
}

// parseIssueDiscussionTrigger parses issue and discussion triggers
func parseIssueDiscussionTrigger(input string) (*TriggerIR, error) {
	tokens := strings.Fields(input)
	if len(tokens) < 2 {
		return nil, nil
	}

	switch tokens[0] {
	case "issue":
		return parseIssueTrigger(tokens)
	case "discussion":
		return parseDiscussionTrigger(tokens)
	default:
		return nil, nil
	}
}

// parseIssueTrigger parses issue triggers
func parseIssueTrigger(tokens []string) (*TriggerIR, error) {
	if len(tokens) < 2 {
		return nil, fmt.Errorf("issue trigger requires an activity type. Expected format: 'issue <type>'. Valid types: opened, edited, closed, reopened, assigned, unassigned, labeled, unlabeled, deleted, transferred. Example: 'issue opened'")
	}

	activityType := tokens[1]

	// Map common activity types
	validTypes := map[string]bool{
		"opened":      true,
		"edited":      true,
		"closed":      true,
		"reopened":    true,
		"assigned":    true,
		"unassigned":  true,
		"labeled":     true,
		"unlabeled":   true,
		"deleted":     true,
		"transferred": true,
	}

	if !validTypes[activityType] {
		return nil, fmt.Errorf("invalid issue activity type: '%s'. Valid types: opened, edited, closed, reopened, assigned, unassigned, labeled, unlabeled, deleted, transferred. Example: 'issue opened'", activityType)
	}

	ir := &TriggerIR{
		Event: "issues",
		Types: []string{activityType},
		AdditionalEvents: map[string]any{
			"workflow_dispatch": nil,
		},
	}

	// Check for label filter: "issue opened labeled <label>"
	if len(tokens) >= 4 && tokens[2] == "labeled" {
		label := strings.Join(tokens[3:], " ")
		ir.Conditions = []string{
			fmt.Sprintf("contains(github.event.issue.labels.*.name, '%s')", label),
		}
	}

	return ir, nil
}

// parseDiscussionTrigger parses discussion triggers
func parseDiscussionTrigger(tokens []string) (*TriggerIR, error) {
	if len(tokens) < 2 {
		return nil, fmt.Errorf("discussion trigger requires an activity type. Expected format: 'discussion <type>'. Valid types: created, edited, deleted, transferred, pinned, unpinned, labeled, unlabeled, locked, unlocked, category_changed, answered, unanswered. Example: 'discussion created'")
	}

	activityType := tokens[1]

	// Map common activity types
	validTypes := map[string]bool{
		"created":          true,
		"edited":           true,
		"deleted":          true,
		"transferred":      true,
		"pinned":           true,
		"unpinned":         true,
		"labeled":          true,
		"unlabeled":        true,
		"locked":           true,
		"unlocked":         true,
		"category_changed": true,
		"answered":         true,
		"unanswered":       true,
	}

	if !validTypes[activityType] {
		return nil, fmt.Errorf("invalid discussion activity type: '%s'. Valid types: created, edited, deleted, transferred, pinned, unpinned, labeled, unlabeled, locked, unlocked, category_changed, answered, unanswered. Example: 'discussion created'", activityType)
	}

	return &TriggerIR{
		Event: "discussion",
		Types: []string{activityType},
		AdditionalEvents: map[string]any{
			"workflow_dispatch": nil,
		},
	}, nil
}

// parseManualTrigger parses manual invocation triggers
func parseManualTrigger(input string) (*TriggerIR, error) {
	tokens := strings.Fields(input)
	if len(tokens) == 0 {
		return nil, nil
	}

	if tokens[0] == "manual" {
		ir := &TriggerIR{
			AdditionalEvents: map[string]any{
				"workflow_dispatch": nil,
			},
		}

		// Check for input specification: "manual with input <name>"
		if len(tokens) >= 4 && tokens[1] == "with" && tokens[2] == "input" {
			inputName := tokens[3]
			ir.AdditionalEvents["workflow_dispatch"] = map[string]any{
				"inputs": map[string]any{
					inputName: map[string]any{
						"description": fmt.Sprintf("Input for %s", inputName),
						"required":    false,
						"type":        "string",
					},
				},
			}
		}

		return ir, nil
	}

	if len(tokens) >= 3 && tokens[0] == "workflow" && tokens[1] == "completed" {
		// "workflow completed <workflow-name>"
		workflowName := strings.Join(tokens[2:], " ")
		return &TriggerIR{
			Event: "workflow_run",
			Types: []string{"completed"},
			Filters: map[string]any{
				"workflows": []string{workflowName},
			},
		}, nil
	}

	return nil, nil
}

// parseCommentTrigger parses comment triggers
func parseCommentTrigger(input string) (*TriggerIR, error) {
	tokens := strings.Fields(input)
	if len(tokens) < 2 {
		return nil, nil
	}

	if tokens[0] == "comment" && tokens[1] == "created" {
		// "comment created" - supports both issue and PR comments
		return &TriggerIR{
			Event: "issue_comment",
			Types: []string{"created"},
			AdditionalEvents: map[string]any{
				"workflow_dispatch": nil,
			},
		}, nil
	}

	return nil, nil
}

// parseReleaseRepositoryTrigger parses release and repository lifecycle triggers
func parseReleaseRepositoryTrigger(input string) (*TriggerIR, error) {
	tokens := strings.Fields(input)
	if len(tokens) < 2 {
		return nil, nil
	}

	switch tokens[0] {
	case "release":
		return parseReleaseTrigger(tokens)
	case "repository":
		return parseRepositoryTrigger(tokens)
	default:
		return nil, nil
	}
}

// parseReleaseTrigger parses release triggers
func parseReleaseTrigger(tokens []string) (*TriggerIR, error) {
	if len(tokens) < 2 {
		return nil, fmt.Errorf("release trigger requires an activity type. Expected format: 'release <type>'. Valid types: published, unpublished, created, edited, deleted, prereleased, released. Example: 'release published'")
	}

	activityType := tokens[1]

	validTypes := map[string]bool{
		"published":   true,
		"unpublished": true,
		"created":     true,
		"edited":      true,
		"deleted":     true,
		"prereleased": true,
		"released":    true,
	}

	if !validTypes[activityType] {
		return nil, fmt.Errorf("invalid release activity type: '%s'. Valid types: published, unpublished, created, edited, deleted, prereleased, released. Example: 'release published'", activityType)
	}

	return &TriggerIR{
		Event: "release",
		Types: []string{activityType},
		AdditionalEvents: map[string]any{
			"workflow_dispatch": nil,
		},
	}, nil
}

// parseRepositoryTrigger parses repository lifecycle triggers
func parseRepositoryTrigger(tokens []string) (*TriggerIR, error) {
	if len(tokens) < 2 {
		return nil, fmt.Errorf("repository trigger requires an activity type. Expected format: 'repository <type>'. Valid types: starred, forked. Example: 'repository starred'")
	}

	activityType := tokens[1]

	// Map activity types to events
	switch activityType {
	case "starred":
		// GitHub Actions uses "watch" event for starring
		return &TriggerIR{
			Event: "watch",
			Types: []string{"started"},
			AdditionalEvents: map[string]any{
				"workflow_dispatch": nil,
			},
		}, nil
	case "forked":
		return &TriggerIR{
			Event:   "fork",
			Filters: map[string]any{}, // Empty map to avoid null in YAML
			AdditionalEvents: map[string]any{
				"workflow_dispatch": nil,
			},
		}, nil
	default:
		return nil, fmt.Errorf("invalid repository activity type: '%s'. Valid types: starred, forked. Example: 'repository starred'", activityType)
	}
}

// parseSecurityTrigger parses security-related triggers
func parseSecurityTrigger(input string) (*TriggerIR, error) {
	tokens := strings.Fields(input)
	if len(tokens) < 2 {
		return nil, nil
	}

	if tokens[0] == "dependabot" && len(tokens) >= 3 && tokens[1] == "pull" && tokens[2] == "request" {
		// "dependabot pull request" - filter pull requests by Dependabot author
		return &TriggerIR{
			Event:      "pull_request",
			Types:      []string{"opened", "synchronize", "reopened"},
			Conditions: []string{"github.actor == 'dependabot[bot]'"},
			AdditionalEvents: map[string]any{
				"workflow_dispatch": nil,
			},
		}, nil
	}

	if tokens[0] == "security" && tokens[1] == "alert" {
		// "security alert" - code scanning alert
		return &TriggerIR{
			Event: "code_scanning_alert",
			Types: []string{"created", "reopened", "fixed"},
			AdditionalEvents: map[string]any{
				"workflow_dispatch": nil,
			},
		}, nil
	}

	if len(tokens) >= 3 && tokens[0] == "code" && tokens[1] == "scanning" && tokens[2] == "alert" {
		// "code scanning alert" - explicit code scanning alert
		return &TriggerIR{
			Event: "code_scanning_alert",
			Types: []string{"created", "reopened", "fixed"},
			AdditionalEvents: map[string]any{
				"workflow_dispatch": nil,
			},
		}, nil
	}

	return nil, nil
}

// parseExternalTrigger parses external integration triggers
func parseExternalTrigger(input string) (*TriggerIR, error) {
	tokens := strings.Fields(input)
	if len(tokens) < 3 {
		return nil, nil
	}

	if tokens[0] == "api" && tokens[1] == "dispatch" {
		// "api dispatch <event-type>"
		eventType := strings.Join(tokens[2:], " ")
		return &TriggerIR{
			Event: "repository_dispatch",
			Filters: map[string]any{
				"types": []string{eventType},
			},
		}, nil
	}

	return nil, nil
}
