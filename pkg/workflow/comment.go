package workflow

import "github.com/github/gh-aw/pkg/logger"

var commentLog = logger.New("workflow:comment")

// CommentEventMapping defines the mapping between event identifiers and their GitHub Actions event configurations
type CommentEventMapping struct {
	EventName      string   // GitHub Actions event name (e.g., "issues", "issue_comment")
	Types          []string // Event types (e.g., ["opened", "edited", "reopened"])
	IsPRComment    bool     // True if this is pull_request_comment (issue_comment on PRs only)
	IsIssueComment bool     // True if this is issue_comment (issue_comment on issues only)
}

// GetAllCommentEvents returns all possible comment-related events for command triggers
func GetAllCommentEvents() []CommentEventMapping {
	return []CommentEventMapping{
		{
			EventName: "issues",
			Types:     []string{"opened", "edited", "reopened"},
		},
		{
			EventName:      "issue_comment",
			Types:          []string{"created", "edited"},
			IsIssueComment: true, // Only comments on issues (not PRs)
		},
		{
			EventName:   "pull_request_comment",
			Types:       []string{"created", "edited"},
			IsPRComment: true, // Only comments on PRs (uses issue_comment event with filter)
		},
		{
			EventName: "pull_request",
			Types:     []string{"opened", "edited", "reopened"},
		},
		{
			EventName: "pull_request_review_comment",
			Types:     []string{"created", "edited"},
		},
		{
			EventName: "discussion",
			Types:     []string{"created", "edited"},
		},
		{
			EventName: "discussion_comment",
			Types:     []string{"created", "edited"},
		},
	}
}

// GetCommentEventByIdentifier returns the event mapping for a given identifier
// Uses GitHub Actions event names (e.g., "issues", "issue_comment", "pull_request_comment", "pull_request", "pull_request_review_comment")
func GetCommentEventByIdentifier(identifier string) *CommentEventMapping {
	// Find and return the matching event mapping using GitHub Actions event names
	allEvents := GetAllCommentEvents()
	for i := range allEvents {
		if allEvents[i].EventName == identifier {
			return &allEvents[i]
		}
	}

	return nil
}

// ParseCommandEvents parses the events field from command configuration
// Returns a list of event identifiers to enable, or nil for default (all events)
func ParseCommandEvents(eventsValue any) []string {
	if eventsValue == nil {
		commentLog.Print("Parsing command events: nil value, using default (all events)")
		return nil // Default: all events
	}

	// Handle string value (e.g., "*" or single event)
	if str, ok := eventsValue.(string); ok {
		if str == "*" {
			commentLog.Print("Parsing command events: wildcard, enabling all events")
			return nil // Explicit all events
		}
		commentLog.Printf("Parsing command events: single event: %s", str)
		return []string{str}
	}

	// Handle array of strings
	if arr, ok := eventsValue.([]any); ok {
		result := make([]string, 0, len(arr))
		for _, item := range arr {
			if str, ok := item.(string); ok {
				result = append(result, str)
			}
		}
		if len(result) > 0 {
			commentLog.Printf("Parsing command events: array of %d events", len(result))
			return result
		}
	}

	commentLog.Print("Parsing command events: parsing failed, using default (all events)")
	return nil // Default if parsing fails
}

// FilterCommentEvents returns only the comment events specified by the identifiers
// If identifiers is nil or empty, returns all comment events
func FilterCommentEvents(identifiers []string) []CommentEventMapping {
	// len() for nil slices returns 0, so this handles both nil and empty slices
	if len(identifiers) == 0 {
		commentLog.Print("Filtering comment events: no identifiers specified, returning all events")
		return GetAllCommentEvents()
	}

	commentLog.Printf("Filtering comment events: %d identifiers specified", len(identifiers))
	var result []CommentEventMapping
	for _, identifier := range identifiers {
		if mapping := GetCommentEventByIdentifier(identifier); mapping != nil {
			result = append(result, *mapping)
		}
	}

	return result
}

// GetCommentEventNames returns just the event names from a list of mappings
func GetCommentEventNames(mappings []CommentEventMapping) []string {
	names := make([]string, len(mappings))
	for i, mapping := range mappings {
		names[i] = mapping.EventName
	}
	return names
}

// GetActualGitHubEventName returns the actual GitHub Actions event name for a given identifier
// This maps pull_request_comment to issue_comment since that's the actual event in GitHub Actions
func GetActualGitHubEventName(identifier string) string {
	if identifier == "pull_request_comment" || identifier == "issue_comment" {
		return "issue_comment"
	}
	return identifier
}

// MergeEventsForYAML merges comment events for YAML generation, combining pull_request_comment and issue_comment
func MergeEventsForYAML(mappings []CommentEventMapping) []CommentEventMapping {
	var result []CommentEventMapping
	hasIssueComment := false
	hasPRComment := false

	for _, mapping := range mappings {
		switch mapping.EventName {
		case "issue_comment":
			hasIssueComment = true
		case "pull_request_comment":
			hasPRComment = true
		}
	}

	// If both issue_comment and pull_request_comment are present, merge them
	if hasIssueComment && hasPRComment {
		for _, mapping := range mappings {
			if mapping.EventName == "issue_comment" || mapping.EventName == "pull_request_comment" {
				// Skip individual ones, we'll add merged version
				continue
			}
			result = append(result, mapping)
		}
		// Add merged issue_comment (covers both issues and PRs)
		result = append(result, CommentEventMapping{
			EventName: "issue_comment",
			Types:     []string{"created", "edited"},
		})
	} else {
		// Map pull_request_comment to issue_comment
		for _, mapping := range mappings {
			if mapping.EventName == "pull_request_comment" {
				result = append(result, CommentEventMapping{
					EventName:   "issue_comment",
					Types:       mapping.Types,
					IsPRComment: true,
				})
			} else {
				result = append(result, mapping)
			}
		}
	}

	return result
}
