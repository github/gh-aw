package workflow

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var expressionBuilderLog = logger.New("workflow:expression_builder")

// Expression Builder Functions
//
// This file provides a functional builder pattern for constructing GitHub Actions
// expression trees. Rather than using a stateful fluent builder, we use composable
// functions that return immutable ConditionNode interfaces.
//
// Design Principles:
// - Composable: Functions can be nested and combined naturally
// - Type-safe: Compile-time guarantees through the ConditionNode interface
// - Immutable: No shared mutable state, thread-safe by design
// - Testable: Pure functions are easy to unit test
// - Clear: Each function has a single, well-defined responsibility
//
// Example Usage:
//
//	condition := BuildAnd(
//	    BuildEventTypeEquals("pull_request"),
//	    BuildLabelContains("deploy"),
//	)
//	expression := condition.Render()
//
// All Build* functions return ConditionNode instances that can be:
// - Combined with BuildAnd() and BuildOr()
// - Rendered to GitHub Actions expression syntax with .Render()
// - Nested to create complex logical expressions

// BuildConditionTree creates a condition tree from existing if condition and new draft condition
func BuildConditionTree(existingCondition string, draftCondition string) ConditionNode {
	expressionBuilderLog.Printf("Building condition tree: existing=%q, draft=%q", existingCondition, draftCondition)
	draftNode := &ExpressionNode{Expression: draftCondition}

	if existingCondition == "" {
		expressionBuilderLog.Print("No existing condition, using draft only")
		return draftNode
	}

	expressionBuilderLog.Print("Combining existing and draft conditions with AND")
	existingNode := &ExpressionNode{Expression: existingCondition}
	return &AndNode{Left: existingNode, Right: draftNode}
}

// BuildOr creates an OR node combining two conditions
func BuildOr(left ConditionNode, right ConditionNode) ConditionNode {
	return &OrNode{Left: left, Right: right}
}

// BuildAnd creates an AND node combining two conditions
func BuildAnd(left ConditionNode, right ConditionNode) ConditionNode {
	expressionBuilderLog.Print("Building AND condition node")
	return &AndNode{Left: left, Right: right}
}

// BuildReactionConditionForTargets creates a condition tree for reactions scoped to target groups.
func BuildReactionConditionForTargets(includeIssues bool, includePullRequests bool, includeDiscussions bool, includeWorkflowDispatch bool) ConditionNode {
	expressionBuilderLog.Printf(
		"Building reaction condition: includeIssues=%t includePullRequests=%t includeDiscussions=%t includeWorkflowDispatch=%t",
		includeIssues,
		includePullRequests,
		includeDiscussions,
		includeWorkflowDispatch,
	)
	return buildReactionLikeCondition(includeIssues, includePullRequests, includeDiscussions, includeWorkflowDispatch)
}

// BuildStatusCommentCondition creates a condition tree for activation status comments.
// When includeIssues is false, issues and issue_comment events are excluded.
// When includePullRequests is false, pull_request, pull_request_review, and pull_request_review_comment events are excluded.
// When includeDiscussions is false, discussion and discussion_comment events are excluded.
func BuildStatusCommentCondition(includeIssues bool, includePullRequests bool, includeDiscussions bool, includeWorkflowDispatch bool) ConditionNode {
	expressionBuilderLog.Printf(
		"Building status comment condition: includeIssues=%t includePullRequests=%t includeDiscussions=%t includeWorkflowDispatch=%t",
		includeIssues,
		includePullRequests,
		includeDiscussions,
		includeWorkflowDispatch,
	)
	return buildReactionLikeCondition(includeIssues, includePullRequests, includeDiscussions, includeWorkflowDispatch)
}

func buildReactionLikeCondition(includeIssues bool, includePullRequests bool, includeDiscussions bool, includeWorkflowDispatch bool) ConditionNode {
	if !includeIssues && !includePullRequests && !includeDiscussions {
		return BuildBooleanLiteral(false)
	}

	// Build a list of event types that should trigger reactions/status-comments using expression nodes.
	var terms []ConditionNode

	if includeIssues {
		terms = append(terms, BuildEventTypeEquals("issues"))
		terms = append(terms, BuildEventTypeEquals("issue_comment"))
	}
	if includePullRequests {
		terms = append(terms, BuildEventTypeEquals("pull_request_review_comment"))
	}
	if includeDiscussions {
		terms = append(terms, BuildEventTypeEquals("discussion"))
		terms = append(terms, BuildEventTypeEquals("discussion_comment"))
	}

	// For pull_request events, we need to ensure it's not from a forked repository since
	// forked pull requests have read-only permissions and cannot perform write operations
	// like adding reactions or workflow run status-comments.
	if includePullRequests {
		pullRequestCondition := &AndNode{
			Left:  BuildEventTypeEquals("pull_request"),
			Right: BuildNotFromFork(),
		}
		terms = append(terms, pullRequestCondition)
	}

	expressionBuilderLog.Printf("Created native disjunction with %d event type terms", len(terms))
	nativeCondition := BuildDisjunction(false, terms...)

	if !includeWorkflowDispatch {
		return nativeCondition
	}

	dispatchSourceCondition := buildDispatchSourceEventCondition(includeIssues, includePullRequests, includeDiscussions)
	dispatchCondition := BuildAnd(
		BuildEventTypeEquals("workflow_dispatch"),
		dispatchSourceCondition,
	)
	return BuildOr(nativeCondition, dispatchCondition)
}

func buildDispatchSourceEventCondition(includeIssues bool, includePullRequests bool, includeDiscussions bool) ConditionNode {
	eventExpr := BuildPropertyAccess("fromJSON(github.event.inputs.aw_context || '{}').event_type")
	var terms []ConditionNode

	if includeIssues {
		terms = append(terms, BuildEquals(eventExpr, BuildStringLiteral("issues")))
		terms = append(terms, BuildEquals(eventExpr, BuildStringLiteral("issue_comment")))
	}
	if includePullRequests {
		terms = append(terms, BuildEquals(eventExpr, BuildStringLiteral("pull_request_review_comment")))
		terms = append(terms, BuildEquals(eventExpr, BuildStringLiteral("pull_request")))
	}
	if includeDiscussions {
		terms = append(terms, BuildEquals(eventExpr, BuildStringLiteral("discussion")))
		terms = append(terms, BuildEquals(eventExpr, BuildStringLiteral("discussion_comment")))
	}
	if len(terms) == 0 {
		return BuildBooleanLiteral(false)
	}
	return BuildDisjunction(false, terms...)
}

// buildCommentAuthorAssociationCondition returns a ConditionNode that passes for non-comment
// events and for comment events whose author is an OWNER, MEMBER, or COLLABORATOR.
// Actors listed in bots (from on.bots) are also exempted so that bot/app-triggered workflows
// continue to work even though bots rarely carry an OWNER/MEMBER/COLLABORATOR association.
//
// activeCommentEvents lists which comment events are actually in the compiled on: section
// (e.g. ["issue_comment", "pull_request_review_comment"]). Only events present in this list
// get a "not equal" guard clause; omitting an event that is not a trigger avoids dead-code
// conditions and keeps the generated expression consistent with the workflow's actual triggers.
//
// The generated expression for activeCommentEvents=["issue_comment","pull_request_review_comment"]
// (without bots) is:
//
//	(github.event_name != 'issue_comment' && github.event_name != 'pull_request_review_comment')
//	|| contains(fromJSON('["OWNER","MEMBER","COLLABORATOR"]'), github.event.comment.author_association)
//
// For activeCommentEvents=["issue_comment"] the expression simplifies to:
//
//	github.event_name != 'issue_comment'
//	|| contains(fromJSON('["OWNER","MEMBER","COLLABORATOR"]'), github.event.comment.author_association)
//
// With one or more bots an additional OR clause is appended for each bot:
//
//	|| github.actor == 'dependabot[bot]'
//
// This satisfies the RGS-004 rule (explicit author_association check for comment-triggered
// workflows) while remaining transparent to non-comment events such as push or schedule,
// and preserves existing on.bots allow-list behaviour.
func buildCommentAuthorAssociationCondition(bots []string, activeCommentEvents []string) ConditionNode {
	// Build "not equal" guards only for comment events that are active in this workflow.
	// The canonical guarded events are issue_comment and pull_request_review_comment.
	guardedEvents := []string{"issue_comment", "pull_request_review_comment"}
	var notCommentTerms []ConditionNode
	for _, ev := range guardedEvents {
		if slices.Contains(activeCommentEvents, ev) {
			notCommentTerms = append(notCommentTerms, BuildNotEquals(
				BuildPropertyAccess("github.event_name"),
				BuildStringLiteral(ev),
			))
		}
	}

	var notCommentEvent ConditionNode
	switch len(notCommentTerms) {
	case 0:
		// No guarded comment events are active; the condition is always true.
		notCommentEvent = BuildBooleanLiteral(true)
	case 1:
		notCommentEvent = notCommentTerms[0]
	default:
		// Combine all terms with AND regardless of how many there are, so that
		// adding a third guarded event type in the future doesn't silently drop it.
		combined := notCommentTerms[0]
		for _, term := range notCommentTerms[1:] {
			combined = BuildAnd(combined, term)
		}
		notCommentEvent = combined
	}

	authorizedAssoc := BuildFunctionCall(
		"contains",
		BuildFunctionCall("fromJSON", BuildStringLiteral(`["OWNER","MEMBER","COLLABORATOR"]`)),
		BuildPropertyAccess("github.event.comment.author_association"),
	)

	result := BuildOr(notCommentEvent, authorizedAssoc)
	if len(bots) > 0 {
		botTerms := make([]ConditionNode, len(bots))
		for i, bot := range bots {
			botTerms[i] = BuildEquals(
				BuildPropertyAccess("github.actor"),
				BuildStringLiteral(bot),
			)
		}
		result = BuildOr(result, BuildDisjunction(false, botTerms...))
	}

	return result
}

func buildAuthorAssociationNodeForEvent(eventName string) ConditionNode {
	switch eventName {
	case "issue_comment", "pull_request_review_comment", "discussion_comment":
		return BuildPropertyAccess("github.event.comment.author_association")
	case "pull_request_review":
		return BuildPropertyAccess("github.event.review.author_association")
	case "issues":
		return BuildPropertyAccess("github.event.issue.author_association")
	case "pull_request", "pull_request_target":
		return BuildPropertyAccess("github.event.pull_request.author_association")
	default:
		return &ExpressionNode{Expression: "github.event.comment.author_association || github.event.review.author_association || github.event.issue.author_association || github.event.pull_request.author_association || github.event.author_association"}
	}
}

// buildSkipAuthorAssociationsCondition returns a condition that evaluates to true when the
// workflow should continue, and false when the run should be skipped based on:
// on.skip-author-associations.<event> containing the event-specific author_association field.
func buildSkipAuthorAssociationsCondition(skipAuthorAssociations map[string][]string) ConditionNode {
	var eventNames []string
	for eventName, associations := range skipAuthorAssociations {
		if len(associations) > 0 {
			eventNames = append(eventNames, eventName)
		}
	}
	sort.Strings(eventNames)

	var skipTerms []ConditionNode
	for _, eventName := range eventNames {
		associations := skipAuthorAssociations[eventName]
		if len(associations) == 0 {
			continue
		}

		associationJSON, err := json.Marshal(associations)
		if err != nil {
			continue
		}

		isConfiguredEvent := BuildEquals(
			BuildPropertyAccess("github.event_name"),
			BuildStringLiteral(eventName),
		)
		associationIsSkipped := BuildFunctionCall(
			"contains",
			BuildFunctionCall("fromJSON", BuildStringLiteral(string(associationJSON))),
			buildAuthorAssociationNodeForEvent(eventName),
		)
		skipTerms = append(skipTerms, BuildAnd(isConfiguredEvent, associationIsSkipped))
	}

	if len(skipTerms) == 0 {
		return BuildBooleanLiteral(true)
	}

	return &NotNode{Child: BuildDisjunction(false, skipTerms...)}
}

// buildDetectionSuccessCondition checks whether the detection job completed successfully.
// Warn mode can report success even when threats were found; use
// buildDetectionAcceptedCondition when consuming or persisting agent output.
func buildDetectionSuccessCondition() ConditionNode {
	return BuildEquals(
		BuildPropertyAccess(fmt.Sprintf("needs.%s.result", constants.DetectionJobName)),
		BuildStringLiteral("success"),
	)
}

// buildDetectionAcceptedCondition also requires explicit acceptance of the agent output.
func buildDetectionAcceptedCondition() ConditionNode {
	return BuildAnd(
		buildDetectionSuccessCondition(),
		BuildEquals(
			BuildPropertyAccess(fmt.Sprintf("needs.%s.outputs.detection_success", constants.DetectionJobName)),
			BuildStringLiteral("true"),
		),
	)
}

// buildDetectionPassedCondition builds the condition to check if the detection job either
// succeeded (no threats found) or was skipped (agent produced no outputs or patch — nothing
// to detect against). Use this for downstream jobs that must run in both cases.
func buildDetectionPassedCondition() ConditionNode {
	return BuildOr(
		buildDetectionSuccessCondition(),
		BuildEquals(
			BuildPropertyAccess(fmt.Sprintf("needs.%s.result", constants.DetectionJobName)),
			BuildStringLiteral("skipped"),
		),
	)
}

// Helper functions for building common GitHub Actions expression patterns

// BuildPropertyAccess creates a property access node for GitHub context properties
func BuildPropertyAccess(path string) *PropertyAccessNode {
	return &PropertyAccessNode{PropertyPath: path}
}

// BuildStringLiteral creates a string literal node
func BuildStringLiteral(value string) *StringLiteralNode {
	return &StringLiteralNode{Value: value}
}

// BuildBooleanLiteral creates a boolean literal node
func BuildBooleanLiteral(value bool) *BooleanLiteralNode {
	return &BooleanLiteralNode{Value: value}
}

// BuildNullLiteral creates a null literal node
func BuildNullLiteral() *ExpressionNode {
	return &ExpressionNode{Expression: "null"}
}

// BuildComparison creates a comparison node with the specified operator
func BuildComparison(left ConditionNode, operator string, right ConditionNode) *ComparisonNode {
	return &ComparisonNode{Left: left, Operator: operator, Right: right}
}

// BuildEquals creates an equality comparison
func BuildEquals(left ConditionNode, right ConditionNode) *ComparisonNode {
	return BuildComparison(left, "==", right)
}

// BuildNotEquals creates an inequality comparison
func BuildNotEquals(left ConditionNode, right ConditionNode) *ComparisonNode {
	return BuildComparison(left, "!=", right)
}

// BuildFunctionCall creates a function call node
func BuildFunctionCall(functionName string, args ...ConditionNode) *FunctionCallNode {
	return &FunctionCallNode{FunctionName: functionName, Arguments: args}
}

// BuildNotFromFork creates a condition to check that a pull request is not from a forked repository
// This prevents the job from running on forked PRs where write permissions are not available
// Uses repository ID comparison instead of full name for more reliable matching
func BuildNotFromFork() *ComparisonNode {
	return BuildEquals(
		BuildPropertyAccess("github.event.pull_request.head.repo.id"),
		BuildPropertyAccess("github.repository_id"),
	)
}

func BuildSafeOutputType(outputType string) ConditionNode {
	expressionBuilderLog.Printf("Building safe-output condition for output type: %s", outputType)
	// Use !cancelled() && needs.agent.result != 'skipped' to properly handle workflow cancellation
	// !cancelled() allows jobs to run when dependencies fail (for error reporting)
	// needs.agent.result != 'skipped' prevents running when workflow is cancelled (dependencies get skipped)
	notCancelledFunc := &NotNode{
		Child: BuildFunctionCall("cancelled"),
	}

	// Check that agent job was not skipped (happens when workflow is cancelled)
	agentNotSkipped := &ComparisonNode{
		Left:     BuildPropertyAccess(fmt.Sprintf("needs.%s.result", constants.AgentJobName)),
		Operator: "!=",
		Right:    BuildStringLiteral("skipped"),
	}

	// Combine !cancelled() with agent not skipped check
	baseCondition := &AndNode{
		Left:  notCancelledFunc,
		Right: agentNotSkipped,
	}

	// Always check that the output type is present in agent outputs
	// This prevents the job from running when the agent didn't produce any outputs of this type
	// The min constraint is enforced by the job itself, not by skipping this check
	containsFunc := BuildFunctionCall("contains",
		BuildPropertyAccess(fmt.Sprintf("needs.%s.outputs.output_types", constants.AgentJobName)),
		BuildStringLiteral(outputType),
	)

	return &AndNode{
		Left:  baseCondition,
		Right: containsFunc,
	}
}

// BuildFromAllowedForks creates a condition to check if a pull request is from an allowed fork
// Supports glob patterns like "org/*" and exact matches like "org/repo"
func BuildFromAllowedForks(allowedForks []string) ConditionNode {
	if len(allowedForks) == 0 {
		return BuildNotFromFork()
	}

	var conditions []ConditionNode

	// Always allow PRs from the same repository
	conditions = append(conditions, BuildNotFromFork())

	for _, pattern := range allowedForks {
		if strings.HasSuffix(pattern, "/*") {
			// Glob pattern: org/* matches org/anything
			prefix := strings.TrimSuffix(pattern, "*")
			condition := &FunctionCallNode{
				FunctionName: "startsWith",
				Arguments: []ConditionNode{
					BuildPropertyAccess("github.event.pull_request.head.repo.full_name"),
					BuildStringLiteral(prefix),
				},
			}
			conditions = append(conditions, condition)
		} else {
			// Exact match: org/repo
			condition := BuildEquals(
				BuildPropertyAccess("github.event.pull_request.head.repo.full_name"),
				BuildStringLiteral(pattern),
			)
			conditions = append(conditions, condition)
		}
	}

	if len(conditions) == 1 {
		return conditions[0]
	}

	// Use DisjunctionNode to combine all conditions with OR
	return &DisjunctionNode{Terms: conditions}
}

// BuildEventTypeEquals creates a condition to check if the event type equals a specific value
func BuildEventTypeEquals(eventType string) *ComparisonNode {
	return BuildEquals(
		BuildPropertyAccess("github.event_name"),
		BuildStringLiteral(eventType),
	)
}

// BuildDisjunction creates a disjunction node (OR operation) from the given terms
// Handles arrays of size 0, 1, or more correctly
// The multiline parameter controls whether to render each term on a separate line
func BuildDisjunction(multiline bool, terms ...ConditionNode) *DisjunctionNode {
	return &DisjunctionNode{
		Terms:     terms,
		Multiline: multiline,
	}
}

// RenderCondition optimises a ConditionNode and renders it to a string.
// Use this instead of calling node.Render() directly whenever the result
// will be used as an 'if:' condition in generated YAML.
func RenderCondition(node ConditionNode) string {
	return OptimizeExpression(node).Render()
}

// RenderConditionAsIf renders a ConditionNode as an 'if' condition with proper YAML indentation.
// The condition is automatically optimised with OptimizeExpression before rendering.
func RenderConditionAsIf(yaml *strings.Builder, condition ConditionNode, indent string) {
	yaml.WriteString("        if: |\n")
	conditionStr := RenderCondition(condition)

	// Format the condition with proper indentation
	lines := strings.SplitSeq(conditionStr, "\n")
	for line := range lines {
		yaml.WriteString(indent + line + "\n")
	}
}

// injectStepCondition inserts an `if:` line into each generated step, immediately after
// the step's `- name:` line. Each element of steps is expected to be a complete YAML step
// beginning with a "      - name: ...\n" line. When condition is nil the steps are returned
// unchanged.
//
// This lets a job reuse step generators verbatim while gating every emitted step on a
// shared condition (for example, whether a particular safe output will be processed).
func injectStepCondition(steps []string, condition ConditionNode) []string {
	if condition == nil {
		return steps
	}

	gate := RenderCondition(condition)
	insertLine := fmt.Sprintf("        if: %s\n", gate)

	out := make([]string, 0, len(steps))
	for _, step := range steps {
		// If the step already has an inline `if: ...` line, combine it instead of emitting
		// duplicate YAML keys.
		ifPrefix := "\n        if: "
		ifIdx := strings.Index(step, ifPrefix)
		if ifIdx >= 0 {
			condStart := ifIdx + len(ifPrefix)
			condEndRel := strings.IndexByte(step[condStart:], '\n')
			if condEndRel >= 0 {
				condEnd := condStart + condEndRel
				existingCond := strings.TrimSpace(step[condStart:condEnd])
				// `if: |` is a block scalar; don't try to rewrite it here.
				if existingCond != "|" {
					combined := fmt.Sprintf("        if: %s && (%s)\n", gate, existingCond)
					out = append(out, step[:ifIdx+1]+combined+step[condEnd+1:])
					continue
				}
			}
			out = append(out, step)
			continue
		}

		nl := strings.IndexByte(step, '\n')
		if nl < 0 {
			out = append(out, step)
			continue
		}
		out = append(out, step[:nl+1]+insertLine+step[nl+1:])
	}
	return out
}
