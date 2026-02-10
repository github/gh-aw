package workflow

import (
	"fmt"
	"slices"

	"github.com/github/gh-aw/pkg/logger"
)

var commandLog = logger.New("workflow:command")

// buildEventAwareCommandCondition creates a condition that only applies command checks to comment-related events
// commandNames: list of command names that can trigger this workflow
// commandEvents: list of event identifiers where command should be active (nil = all events)
func buildEventAwareCommandCondition(commandNames []string, commandEvents []string, hasOtherEvents bool) (ConditionNode, error) {
	commandLog.Printf("Building event-aware command condition: commands=%v, event_count=%d, has_other_events=%t",
		commandNames, len(commandEvents), hasOtherEvents)

	if len(commandNames) == 0 {
		return nil, fmt.Errorf("no command names provided")
	}

	// Get the filtered events where command should be active
	filteredEvents := FilterCommentEvents(commandEvents)
	eventNames := GetCommentEventNames(filteredEvents)
	commandLog.Printf("Filtered command events: commands=%v, filtered_count=%d", commandNames, len(eventNames))

	// Build command checks for different content sources based on filtered events
	var commandChecks []ConditionNode

	// Check which events are enabled
	hasIssues := slices.Contains(eventNames, "issues")
	hasIssueComment := slices.Contains(eventNames, "issue_comment")
	hasPRComment := slices.Contains(eventNames, "pull_request_comment")
	hasPR := slices.Contains(eventNames, "pull_request")
	hasPRReview := slices.Contains(eventNames, "pull_request_review_comment")
	hasDiscussion := slices.Contains(eventNames, "discussion")
	hasDiscussionComment := slices.Contains(eventNames, "discussion_comment")

	// Helper function to build OR condition for multiple command checks
	// Uses strict matching: command at start of line only
	buildMultiCommandCheck := func(bodyAccessor string) ConditionNode {
		var commandOrChecks []ConditionNode
		for _, commandName := range commandNames {
			commandText := fmt.Sprintf("/%s", commandName)
			commandWithSpace := fmt.Sprintf("/%s ", commandName)

			// Check for exact match (command without arguments)
			exactMatch := BuildEquals(
				BuildPropertyAccess(bodyAccessor),
				BuildStringLiteral(commandText),
			)

			// Check for command with arguments (starts with "/<command> ")
			startsWithMatch := BuildFunctionCall("startsWith",
				BuildPropertyAccess(bodyAccessor),
				BuildStringLiteral(commandWithSpace),
			)

			// Combine: exact match OR starts with pattern
			commandCheck := &OrNode{
				Left:  startsWithMatch,
				Right: exactMatch,
			}

			commandOrChecks = append(commandOrChecks, commandCheck)
		}
		// If only one command, return it directly; otherwise combine with OR
		if len(commandOrChecks) == 1 {
			return commandOrChecks[0]
		}
		return BuildDisjunction(false, commandOrChecks...)
	}

	if hasIssues {
		// issues event - check github.event.issue.body only when event is 'issues'
		issueBodyCheck := &AndNode{
			Left:  BuildEventTypeEquals("issues"),
			Right: buildMultiCommandCheck("github.event.issue.body"),
		}
		commandChecks = append(commandChecks, issueBodyCheck)
	}

	if hasIssueComment {
		// issue_comment event only on issues (not PRs)
		commentBodyCheck := &AndNode{
			Left: BuildEventTypeEquals("issue_comment"),
			Right: &AndNode{
				Left: buildMultiCommandCheck("github.event.comment.body"),
				Right: BuildEquals(
					BuildPropertyAccess("github.event.issue.pull_request"),
					BuildNullLiteral(),
				),
			},
		}
		commandChecks = append(commandChecks, commentBodyCheck)
	}

	if hasPRComment {
		// pull_request_comment event only on PRs
		prCommentBodyCheck := &AndNode{
			Left: BuildEventTypeEquals("issue_comment"),
			Right: &AndNode{
				Left: buildMultiCommandCheck("github.event.comment.body"),
				Right: BuildNotEquals(
					BuildPropertyAccess("github.event.issue.pull_request"),
					BuildNullLiteral(),
				),
			},
		}
		commandChecks = append(commandChecks, prCommentBodyCheck)
	}

	if hasPRReview {
		// pull_request_review_comment uses github.event.comment.body
		reviewCommentBodyCheck := &AndNode{
			Left:  BuildEventTypeEquals("pull_request_review_comment"),
			Right: buildMultiCommandCheck("github.event.comment.body"),
		}
		commandChecks = append(commandChecks, reviewCommentBodyCheck)
	}

	if hasPR {
		// pull_request event - check github.event.pull_request.body
		prBodyCheck := &AndNode{
			Left:  BuildEventTypeEquals("pull_request"),
			Right: buildMultiCommandCheck("github.event.pull_request.body"),
		}
		commandChecks = append(commandChecks, prBodyCheck)
	}

	if hasDiscussion {
		// discussion event - check github.event.discussion.body
		discussionBodyCheck := &AndNode{
			Left:  BuildEventTypeEquals("discussion"),
			Right: buildMultiCommandCheck("github.event.discussion.body"),
		}
		commandChecks = append(commandChecks, discussionBodyCheck)
	}

	if hasDiscussionComment {
		// discussion_comment event - check github.event.comment.body
		discussionCommentBodyCheck := &AndNode{
			Left:  BuildEventTypeEquals("discussion_comment"),
			Right: buildMultiCommandCheck("github.event.comment.body"),
		}
		commandChecks = append(commandChecks, discussionCommentBodyCheck)
	}

	// Combine all command checks with OR
	var commandCondition ConditionNode
	if len(commandChecks) == 0 {
		// No events enabled - this indicates a configuration error
		return nil, fmt.Errorf("no valid comment events specified for commands %v - at least one event must be enabled", commandNames)
	}
	commandCondition = BuildDisjunction(false, commandChecks...)

	if !hasOtherEvents {
		// If there are no other events, just use the simple command condition
		return commandCondition, nil
	}

	// Define which events should be checked for command
	var commentEventTerms []ConditionNode
	actualEventNames := make(map[string]bool) // Use map to deduplicate
	for _, eventName := range eventNames {
		actualName := GetActualGitHubEventName(eventName)
		if !actualEventNames[actualName] {
			actualEventNames[actualName] = true
			commentEventTerms = append(commentEventTerms, BuildEventTypeEquals(actualName))
		}
	}

	commentEventChecks := BuildDisjunction(false, commentEventTerms...)

	// For comment events: check command; for other events: allow unconditionally
	commentEventCheck := &AndNode{
		Left:  commentEventChecks,
		Right: commandCondition,
	}

	// Allow all non-comment events to run
	nonCommentEvents := &NotNode{Child: commentEventChecks}

	// Combine: (comment events && command check) || (non-comment events)
	return &OrNode{
		Left:  commentEventCheck,
		Right: nonCommentEvents,
	}, nil
}
