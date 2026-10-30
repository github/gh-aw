// This file provides GitHub Actions expression security validation.
// It enforces an allowlist of approved expressions to prevent injection attacks.
// For syntax helpers, see expression_syntax_validation.go.
// For runtime-import validation, see runtime_import_validation.go.

package workflow

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/workflow/actionslanguageservice"
)

var expressionValidationLog = logger.New("workflow:expression_safety_validation")

// maxFuzzyMatchSuggestions is the maximum number of similar expressions to suggest
// when an unauthorized expression is found
const maxFuzzyMatchSuggestions = 7

// validateExpressionSafety checks that all GitHub Actions expressions in the markdown content
// are in the allowed list and returns an error if any unauthorized expressions are found
func validateExpressionSafety(markdownContent string) error {
	expressionValidationLog.Print("Validating expression safety in markdown content")

	matches, err := actionslanguageservice.ExtractExpressions(markdownContent)
	if err != nil {
		return fmt.Errorf("invalid markdown expression: %w", err)
	}
	expressionValidationLog.Printf("Found %d expressions to validate", len(matches))

	var unauthorizedExpressions []string

	for _, match := range matches {
		// Extract the expression content (everything between ${{ and }})
		expression := strings.TrimSpace(match.Content)

		// Reject expressions that span multiple lines (contain newlines)
		if strings.Contains(match.Content, "\n") {
			unauthorizedExpressions = append(unauthorizedExpressions, expression)
			continue
		}

		if err := validateSingleExpression(expression, ExpressionValidationOptions{
			NeedsStepsRe:            NeedsStepsPattern,
			InputsRe:                InputsPattern,
			WorkflowCallInputsRe:    WorkflowCallInputsPattern,
			AwInputsRe:              AWInputsPattern,
			AwImportInputsRe:        AWImportInputsPattern,
			EnvRe:                   EnvPattern,
			UnauthorizedExpressions: &unauthorizedExpressions,
		}); err != nil {
			return err
		}
	}

	if len(unauthorizedExpressions) > 0 {
		return unauthorizedExpressionsError(unauthorizedExpressions)
	}

	expressionValidationLog.Print("Expression safety validation passed")
	return nil
}

func unauthorizedExpressionsError(unauthorizedExpressions []string) error {
	expressionValidationLog.Printf("Expression safety validation failed: %d unauthorized expressions found", len(unauthorizedExpressions))
	var unauthorizedList strings.Builder
	unauthorizedList.WriteString("\n")
	for _, expr := range unauthorizedExpressions {
		unauthorizedList.WriteString("  - ")
		unauthorizedList.WriteString(expr)

		// Find closest matches using fuzzy string matching
		closestMatches := parser.FindClosestMatches(expr, constants.AllowedExpressions, maxFuzzyMatchSuggestions)
		if len(closestMatches) > 0 {
			unauthorizedList.WriteString(" (did you mean: ")
			unauthorizedList.WriteString(strings.Join(closestMatches, ", "))
			unauthorizedList.WriteString("?)")
		}

		unauthorizedList.WriteString("\n")
	}

	var allowedList strings.Builder
	allowedList.WriteString("\n")
	for _, expr := range constants.AllowedExpressions {
		allowedList.WriteString("  - ")
		allowedList.WriteString(expr)
		allowedList.WriteString("\n")
	}
	allowedList.WriteString("  - needs.*\n")
	allowedList.WriteString("  - steps.*\n")
	allowedList.WriteString("  - github.event.inputs.*\n")
	allowedList.WriteString("  - github.aw.inputs.* (shared workflow inputs)\n")
	allowedList.WriteString("  - github.aw.import-inputs.* (import-schema inputs)\n")
	allowedList.WriteString("  - inputs.* (workflow_call)\n")
	allowedList.WriteString("  - env.*\n")

	return NewValidationError(
		"expressions",
		fmt.Sprintf("%d unauthorized expressions found", len(unauthorizedExpressions)),
		"expressions are not in the allowed list:"+unauthorizedList.String(),
		fmt.Sprintf("Use only allowed expressions:%s\nFor more details, see the expression security documentation.", allowedList.String()),
	)
}

// ExpressionValidationOptions contains the options for validating a single expression
type ExpressionValidationOptions struct {
	NeedsStepsRe            expressionMatcher
	InputsRe                expressionMatcher
	WorkflowCallInputsRe    expressionMatcher
	AwInputsRe              expressionMatcher
	AwImportInputsRe        expressionMatcher
	EnvRe                   expressionMatcher
	UnauthorizedExpressions *[]string
}

type expressionMatcher interface {
	MatchString(string) bool
}

// validateExpressionForDangerousProps checks if an expression contains dangerous JavaScript
// property names that could be used for prototype pollution or traversal attacks.
// This matches the JavaScript runtime validation in actions/setup/js/runtime_import.cjs
// Returns an error if dangerous properties are found.
func validateExpressionForDangerousProps(expression string) error {
	expressionValidationLog.Printf("Checking expression for dangerous properties: %s", expression)
	tree, err := actionslanguageservice.ParseSyntax(expression)
	if err != nil {
		return fmt.Errorf("invalid expression %q: %w", expression, err)
	}
	var dangerous string
	actionslanguageservice.Walk(tree, func(node *actionslanguageservice.Expression) bool {
		var property string
		if node.Kind == "context" {
			property = node.Token.Text
		} else if node.Kind == "index" && node.Child(1).Kind == "literal" {
			if value, ok := node.Child(1).Token.Value.(string); ok {
				property = value
			}
		}
		for part := range strings.SplitSeq(property, ".") {
			if _, blocked := constants.DangerousPropertyNamesSet[part]; blocked {
				dangerous = part
			}
		}
		return dangerous == ""
	})
	if dangerous != "" {
		return NewValidationError(
			"expressions",
			fmt.Sprintf("dangerous property name %q found in expression", dangerous),
			fmt.Sprintf("expression %q contains the dangerous property name %q", expression, dangerous),
			fmt.Sprintf("Remove the dangerous property %q from the expression. Property names like constructor, __proto__, prototype, and similar JavaScript built-ins are blocked to prevent prototype pollution attacks. See PR #14826 for more details.", dangerous),
		)
	}
	return nil
}

func isGatewayEventsExpression(expression string) bool {
	tree, err := actionslanguageservice.ParseSyntax(expression)
	if err != nil || tree == nil {
		return false
	}
	if tree.Kind == "call" && tree.Token.Text == "toJSON" && len(tree.Children) == 1 {
		tree = tree.Child(0)
	}
	path, ok := actionslanguageservice.Reference(tree)
	if !ok || len(path) < 2 || path[0] != "gateway" || path[1] != "events" {
		return false
	}
	for _, property := range path[2:] {
		if property == "*" {
			continue
		}
		for part := range strings.SplitSeq(property, ".") {
			if !namedAccessPath([]string{part}) {
				return false
			}
			if _, dangerous := constants.DangerousPropertyNamesSet[part]; dangerous {
				return false
			}
		}
	}
	return true
}

// validateSingleExpression validates a single literal expression
func validateSingleExpression(expression string, opts ExpressionValidationOptions) error {
	expression = strings.TrimSpace(expression)
	tree, err := actionslanguageservice.ParseSyntax(expression)
	if err != nil || tree == nil {
		*opts.UnauthorizedExpressions = append(*opts.UnauthorizedExpressions, expression)
		return nil
	}

	// Check for dangerous JavaScript property names (prototype pollution, PR #14826)
	if err := validateExpressionForDangerousProps(expression); err != nil {
		return err
	}

	validateSafeExpressionNode(expression, tree, opts)
	return nil
}

var allowedReferencePaths = func() map[string]struct{} {
	result := make(map[string]struct{})
	for _, source := range constants.AllowedExpressions {
		expression, err := actionslanguageservice.ParseSyntax(source)
		if err == nil {
			if path, ok := actionslanguageservice.Reference(expression); ok {
				result[strings.Join(path, "\x00")] = struct{}{}
			}
		}
	}
	return result
}()

func validateSafeExpressionNode(source string, node *actionslanguageservice.Expression, opts ExpressionValidationOptions) {
	switch node.Kind {
	case "literal":
		return
	case "logical", "binary", "unary", "group":
		for _, child := range node.Children {
			validateSafeExpressionNode(source, child, opts)
		}
		return
	}
	expression := source[node.Start:node.End]
	if _, allowed := constants.AllowedExpressionsSet[expression]; allowed || isGatewayEventsExpression(expression) {
		return
	}
	if path, ok := actionslanguageservice.Reference(node); ok {
		if _, allowed := allowedReferencePaths[strings.Join(path, "\x00")]; allowed {
			return
		}
		for _, matcher := range []expressionMatcher{opts.NeedsStepsRe, opts.InputsRe, opts.WorkflowCallInputsRe, opts.AwInputsRe, opts.AwImportInputsRe, opts.EnvRe} {
			if matcher != nil && matcher.MatchString(expression) {
				return
			}
		}
	}
	*opts.UnauthorizedExpressions = append(*opts.UnauthorizedExpressions, expression)
}
