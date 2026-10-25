package workflow

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/setutil"
)

var commandLog = logger.New("workflow:command")

func isWildcardCommandName(commandName string) bool {
	return len(commandName) > 1 && strings.HasSuffix(commandName, "*")
}

// buildEventAwareCommandCondition creates a condition that only applies command checks to comment-related events
// commandNames: list of command names that can trigger this workflow
// commandEvents: list of event identifiers where command should be active (nil = all events)
func buildEventAwareCommandCondition(commandNames []string, commandEvents []string, hasOtherEvents bool) (ConditionNode, error) {
	commandLog.Printf("Building event-aware command condition: commands=%v, event_count=%d, has_other_events=%t",
		commandNames, len(commandEvents), hasOtherEvents)

	if len(commandNames) == 0 {
		return nil, errors.New("no command names provided")
	}

	// Get the filtered events where command should be active
	filteredEvents := FilterCommentEvents(commandEvents)
	eventNames := GetCommentEventNames(filteredEvents)
	commandLog.Printf("Filtered command events: commands=%v, filtered_count=%d", commandNames, len(eventNames))

	commandChecks := buildCommandEventChecks(commandNames, eventNames)

	// Combine all command checks with OR
	var commandCondition ConditionNode
	if len(commandChecks) == 0 {
		return nil, fmt.Errorf("no valid comment events specified for commands %v - at least one event must be enabled", commandNames)
	}
	commandLog.Printf("Built %d command check(s) for commands: %v", len(commandChecks), commandNames)
	commandCondition = BuildDisjunction(false, commandChecks...)

	if !hasOtherEvents {
		commandLog.Print("Using simple command condition (no other events)")
		return commandCondition, nil
	}
	commandLog.Print("Using event-aware condition (mixed command and non-command events)")
	return allowNonCommandEvents(eventNames, commandCondition), nil
}

func buildMultiCommandCheck(commandNames []string, bodyAccessor string) ConditionNode {
	var commandOrChecks []ConditionNode
	for _, commandName := range commandNames {
		if isWildcardCommandName(commandName) {
			commandPrefix := "/" + strings.TrimSuffix(commandName, "*")
			commandOrChecks = append(commandOrChecks, BuildFunctionCall("startsWith",
				BuildPropertyAccess(bodyAccessor),
				BuildStringLiteral(commandPrefix),
			))
			continue
		}

		commandOrChecks = append(commandOrChecks, buildExactCommandCheck(commandName, bodyAccessor))
	}
	if len(commandOrChecks) == 1 {
		return commandOrChecks[0]
	}
	return BuildDisjunction(false, commandOrChecks...)
}

func buildExactCommandCheck(commandName, bodyAccessor string) ConditionNode {
	commandText := "/" + commandName
	commandWithSpace := fmt.Sprintf("/%s ", commandName)
	commandWithNewline := fmt.Sprintf("/%s\n", commandName)
	// Covers both "\r\n" (CRLF, as used by the GitHub web editor) and a bare "\r"
	commandWithCR := fmt.Sprintf("/%s\r", commandName)

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

	// Check for command followed by a newline (e.g. bot comments that append metadata after the command)
	startsWithNewlineMatch := BuildFunctionCall("startsWith",
		BuildPropertyAccess(bodyAccessor),
		BuildStringLiteral(commandWithNewline),
	)

	// Check for command followed by a carriage return (CRLF line endings, e.g. GitHub web editor)
	startsWithCRMatch := BuildFunctionCall("startsWith",
		BuildPropertyAccess(bodyAccessor),
		BuildStringLiteral(commandWithCR),
	)

	// Combine: exact match OR starts with space OR starts with newline OR starts with carriage return
	return &OrNode{
		Left: &OrNode{
			Left: &OrNode{
				Left:  startsWithMatch,
				Right: startsWithNewlineMatch,
			},
			Right: startsWithCRMatch,
		},
		Right: exactMatch,
	}

}

func buildCommandEventChecks(commandNames, eventNames []string) []ConditionNode {
	var checks []ConditionNode
	for _, eventName := range []string{"issues", "issue_comment", "pull_request_comment", "pull_request_review_comment", "pull_request", "discussion", "discussion_comment"} {
		if slices.Contains(eventNames, eventName) {
			checks = append(checks, buildCommandEventCheck(commandNames, eventName))
		}
	}
	return checks
}

func buildCommandEventCheck(commandNames []string, eventName string) ConditionNode {
	bodyAccessor := "github.event.comment.body"
	switch eventName {
	case "issues":
		bodyAccessor = "github.event.issue.body"
	case "pull_request":
		bodyAccessor = "github.event.pull_request.body"
	case "discussion":
		bodyAccessor = "github.event.discussion.body"
	}
	bodyCheck := buildMultiCommandCheck(commandNames, bodyAccessor)
	if eventName == "issue_comment" || eventName == "pull_request_comment" {
		var pullRequestCheck ConditionNode = BuildEquals(
			BuildPropertyAccess("github.event.issue.pull_request"), BuildNullLiteral())
		if eventName == "pull_request_comment" {
			pullRequestCheck = BuildNotEquals(
				BuildPropertyAccess("github.event.issue.pull_request"), BuildNullLiteral())
		}
		bodyCheck = &AndNode{Left: bodyCheck, Right: pullRequestCheck}
		eventName = "issue_comment"
	}
	return &AndNode{Left: BuildEventTypeEquals(eventName), Right: bodyCheck}
}

func allowNonCommandEvents(eventNames []string, commandCondition ConditionNode) ConditionNode {
	// Define which events should be checked for command
	var commentEventTerms []ConditionNode
	actualEventNames := make(map[string]struct { // Use map to deduplicate
	})
	for _, eventName := range eventNames {
		actualName := GetActualGitHubEventName(eventName)
		if !setutil.Contains(actualEventNames, actualName) {
			actualEventNames[actualName] = struct {
			}{}
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
	}
}
