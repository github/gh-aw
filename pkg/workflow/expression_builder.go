package workflow

import (
	"fmt"
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
	return &AndNode{Left: left, Right: right}
}

// BuildReactionCondition creates a condition tree for the add_reaction job
func BuildReactionCondition() ConditionNode {
	expressionBuilderLog.Print("Building reaction condition for multiple event types")
	// Build a list of event types that should trigger reactions using the new expression nodes
	var terms []ConditionNode

	terms = append(terms, BuildEventTypeEquals("issues"))
	terms = append(terms, BuildEventTypeEquals("issue_comment"))
	terms = append(terms, BuildEventTypeEquals("pull_request_review_comment"))
	terms = append(terms, BuildEventTypeEquals("discussion"))
	terms = append(terms, BuildEventTypeEquals("discussion_comment"))

	// For pull_request events, we need to ensure it's not from a forked repository
	// since forked repositories have read-only permissions and cannot add reactions
	pullRequestCondition := &AndNode{
		Left:  BuildEventTypeEquals("pull_request"),
		Right: BuildNotFromFork(),
	}
	terms = append(terms, pullRequestCondition)

	// Use DisjunctionNode to avoid deep nesting
	return &DisjunctionNode{Terms: terms}
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

// BuildNumberLiteral creates a number literal node
func BuildNumberLiteral(value string) *NumberLiteralNode {
	return &NumberLiteralNode{Value: value}
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

// BuildContains creates a contains() function call node
func BuildContains(array ConditionNode, value ConditionNode) *ContainsNode {
	return &ContainsNode{Array: array, Value: value}
}

// BuildFunctionCall creates a function call node
func BuildFunctionCall(functionName string, args ...ConditionNode) *FunctionCallNode {
	return &FunctionCallNode{FunctionName: functionName, Arguments: args}
}

// BuildTernary creates a ternary conditional expression
func BuildTernary(condition ConditionNode, trueValue ConditionNode, falseValue ConditionNode) *TernaryNode {
	return &TernaryNode{Condition: condition, TrueValue: trueValue, FalseValue: falseValue}
}

// BuildLabelContains creates a condition to check if an issue/PR contains a specific label
func BuildLabelContains(labelName string) *ContainsNode {
	return BuildContains(
		BuildPropertyAccess("github.event.issue.labels.*.name"),
		BuildStringLiteral(labelName),
	)
}

// BuildActionEquals creates a condition to check if the event action equals a specific value
func BuildActionEquals(action string) *ComparisonNode {
	return BuildEquals(
		BuildPropertyAccess("github.event.action"),
		BuildStringLiteral(action),
	)
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

// BuildRefStartsWith creates a condition to check if github.ref starts with a prefix
func BuildRefStartsWith(prefix string) *FunctionCallNode {
	return BuildFunctionCall("startsWith",
		BuildPropertyAccess("github.ref"),
		BuildStringLiteral(prefix),
	)
}

// BuildExpressionWithDescription creates an expression node with an optional description
func BuildExpressionWithDescription(expression, description string) *ExpressionNode {
	return &ExpressionNode{
		Expression:  expression,
		Description: description,
	}
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

// BuildPRCommentCondition creates a condition to check if the event is a comment on a pull request
// This checks for:
// - issue_comment on a PR (github.event.issue.pull_request != null)
// - pull_request_review_comment
// - pull_request_review
func BuildPRCommentCondition() ConditionNode {
	// issue_comment event on a PR
	issueCommentOnPR := BuildAnd(
		BuildEventTypeEquals("issue_comment"),
		BuildComparison(
			BuildPropertyAccess("github.event.issue.pull_request"),
			"!=",
			&ExpressionNode{Expression: "null"},
		),
	)

	// pull_request_review_comment event
	prReviewComment := BuildEventTypeEquals("pull_request_review_comment")

	// pull_request_review event
	prReview := BuildEventTypeEquals("pull_request_review")

	// Combine all conditions with OR
	return &DisjunctionNode{
		Terms: []ConditionNode{
			issueCommentOnPR,
			prReviewComment,
			prReview,
		},
	}
}

// RenderConditionAsIf renders a ConditionNode as an 'if' condition with proper YAML indentation
func RenderConditionAsIf(yaml *strings.Builder, condition ConditionNode, indent string) {
	yaml.WriteString("        if: |\n")
	conditionStr := condition.Render()

	// Format the condition with proper indentation
	lines := strings.Split(conditionStr, "\n")
	for _, line := range lines {
		yaml.WriteString(indent + line + "\n")
	}
}

// AddDetectionSuccessCheck adds a check for detection job success to an existing condition
// This ensures safe output jobs only run when threat detection passes
func AddDetectionSuccessCheck(existingCondition string) string {
	// Build the detection success check
	detectionSuccess := BuildComparison(
		BuildPropertyAccess(fmt.Sprintf("needs.%s.outputs.success", constants.DetectionJobName)),
		"==",
		BuildStringLiteral("true"),
	)

	// If there's an existing condition, AND it with the detection check
	if existingCondition != "" {
		return fmt.Sprintf("(%s) && (%s)", existingCondition, detectionSuccess.Render())
	}

	// If no existing condition, just return the detection check
	return detectionSuccess.Render()
}
