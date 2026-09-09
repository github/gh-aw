// This file provides validation for GitHub Actions event types in the 'on:' section.
//
// # Event Type Validation
//
// This file validates that event types specified in the workflow's 'on:' section
// are recognized GitHub Actions event names. When an unknown event name is found
// that closely resembles a valid event (Levenshtein distance ≤ 3), a "Did you mean?"
// suggestion is provided so users can quickly fix typos.
//
// Only potential typos are flagged — event names that are too different from any
// known event are silently ignored, allowing workflows to use new GitHub Actions
// events that are not yet in our list.
//
// # Validation Functions
//
//   - ValidateEventTypes() - Main entry point for event type validation
//
// # When to Add Validation Here
//
// Add validation to this file when:
//   - It validates GitHub Actions event names in the 'on:' section
//   - It checks for typos or unrecognized event types
//
// For event filter mutual-exclusivity validation, see compiler_filters_validation.go.
// For general validation, see validation.go.

package workflow

import (
	"fmt"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/stringutil"
)

var eventValidationLog = logger.New("workflow:event_validation")

// validGitHubEventTypes is the list of all supported GitHub Actions event types.
// Source: https://docs.github.com/en/actions/using-workflows/events-that-trigger-workflows
var validGitHubEventTypes = []string{
	"branch_protection_rule",
	"check_run",
	"check_suite",
	"create",
	"delete",
	"deployment",
	"deployment_status",
	"discussion",
	"discussion_comment",
	"fork",
	"gollum",
	"issue_comment",
	"issues",
	"label",
	"merge_group",
	"milestone",
	"page_build",
	"project",
	"project_card",
	"project_column",
	"public",
	"pull_request",
	"pull_request_review",
	"pull_request_review_comment",
	"pull_request_target",
	"push",
	"registry_package",
	"release",
	"repository_dispatch",
	"schedule",
	"status",
	"watch",
	"workflow_call",
	"workflow_dispatch",
	"workflow_run",
}

// ghAwOnSectionKeys contains gh-aw-specific extensions to the 'on:' section that
// are not standard GitHub Actions event types and should be excluded from event
// type validation.
var ghAwOnSectionKeys = map[string]bool{
	"allow-bot-authored-trigger-comment": true,
	"bots":                               true,
	"command":                            true,
	"github-app":                         true,
	"github-token":                       true,
	"label_command":                      true,
	"labels":                             true,
	"needs":                              true,
	"reaction":                           true,
	"report-blocked-version":             true,
	"roles":                              true,
	"skip-author-associations":           true,
	"skip-if-match":                      true,
	"skip-if-no-match":                   true,
	"slash_command":                      true,
	"stale-check":                        true,
	"status-comment":                     true,
	"steps":                              true,
	"stop-after":                         true,
}

// ValidateEventTypes validates that the event types in the 'on:' section of a
// workflow are recognized GitHub Actions events. It only warns about potential
// typos (when the unknown name is close to a known event), rather than erroring
// on all unknown events, to avoid false positives for new GitHub event types or
// gh-aw-specific on: section extensions.
func ValidateEventTypes(frontmatter map[string]any) error {
	on, exists := frontmatter["on"]
	if !exists {
		eventValidationLog.Print("No 'on' section found, skipping event type validation")
		return nil
	}

	// Extract event names from the on: section (handles string, []any, map formats)
	var eventNames []string
	switch v := on.(type) {
	case string:
		eventNames = []string{v}
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok {
				eventNames = append(eventNames, s)
			}
		}
	case map[string]any:
		for key := range v {
			eventNames = append(eventNames, key)
		}
	default:
		eventValidationLog.Printf("'on' section has unexpected type %T, skipping event type validation", on)
		return nil
	}

	// Check each event name against the list of known valid events
	for _, eventName := range eventNames {
		if isKnownGitHubEvent(eventName) {
			continue
		}

		// Skip gh-aw-specific on: section extensions
		if ghAwOnSectionKeys[eventName] {
			eventValidationLog.Printf("Skipping gh-aw extension key: %q", eventName)
			continue
		}

		eventValidationLog.Printf("Unknown event type: %q", eventName)

		// Check for a case-only difference first (e.g. "Push" → "push")
		lowerEventName := strings.ToLower(eventName)
		if lowerEventName != eventName && isKnownGitHubEvent(lowerEventName) {
			return fmt.Errorf(
				"unknown event type %q in 'on:' section.\n\nDid you mean: %s?\n\nValid event types include: %s\n\nSee: https://docs.github.com/en/actions/using-workflows/events-that-trigger-workflows",
				eventName,
				lowerEventName,
				strings.Join(validGitHubEventTypes[:10], ", ")+"...",
			)
		}

		// Only flag as a typo when there is a close match
		suggestions := stringutil.FindClosestMatches(eventName, validGitHubEventTypes, 3)
		if len(suggestions) == 0 {
			eventValidationLog.Printf("No close matches found for unknown event %q, skipping", eventName)
			continue
		}

		return fmt.Errorf(
			"unknown event type %q in 'on:' section.\n\nDid you mean: %s?\n\nValid event types include: %s\n\nSee: https://docs.github.com/en/actions/using-workflows/events-that-trigger-workflows",
			eventName,
			strings.Join(suggestions, ", "),
			strings.Join(validGitHubEventTypes[:10], ", ")+"...",
		)
	}

	return nil
}

// isKnownGitHubEvent returns true if the event name is in the list of valid GitHub Actions event types.
func isKnownGitHubEvent(eventName string) bool {
	return slices.Contains(validGitHubEventTypes, eventName)
}
