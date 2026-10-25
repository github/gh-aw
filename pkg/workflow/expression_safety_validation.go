// This file provides GitHub Actions expression security validation.
// It enforces an allowlist of approved expressions to prevent injection attacks.
// For syntax helpers, see expression_syntax_validation.go.
// For runtime-import validation, see runtime_import_validation.go.

package workflow

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
)

var expressionValidationLog = logger.New("workflow:expression_safety_validation")

// maxFuzzyMatchSuggestions is the maximum number of similar expressions to suggest
// when an unauthorized expression is found
const maxFuzzyMatchSuggestions = 7

// Pre-compiled regexes for expression safety validation (performance optimization)
var (
	// comparisonExpressionPattern matches a full comparison expression so both sides can be
	// validated recursively instead of allowing a safe-looking prefix to bypass validation.
	comparisonExpressionPattern = regexp.MustCompile(`^(.+?)\s*(?:==|!=|<|>|<=|>=)\s*(.+)$`)
	// orExpressionPattern matches "left || right" for fallback literal/expression checking
	orExpressionPattern = regexp.MustCompile(`^(.+?)\s*\|\|\s*(.+)$`)
)

// validateExpressionSafety checks that all GitHub Actions expressions in the markdown content
// are in the allowed list and returns an error if any unauthorized expressions are found
func validateExpressionSafety(markdownContent string) error {
	expressionValidationLog.Print("Validating expression safety in markdown content")

	matches := ExpressionPatternDotAll.FindAllStringSubmatch(markdownContent, -1)
	expressionValidationLog.Printf("Found %d expressions to validate", len(matches))

	var unauthorizedExpressions []string

	for _, match := range matches {
		if len(match) < 2 {
			continue
		}

		// Extract the expression content (everything between ${{ and }})
		expression := strings.TrimSpace(match[1])

		// Reject expressions that span multiple lines (contain newlines)
		if strings.Contains(match[1], "\n") {
			unauthorizedExpressions = append(unauthorizedExpressions, expression)
			continue
		}

		// Try to parse the expression using the parser
		if err := validateMarkdownExpression(expression, &unauthorizedExpressions); err != nil {
			return err
		}
	}

	if len(unauthorizedExpressions) > 0 {
		return unauthorizedExpressionError(unauthorizedExpressions)
	}
	expressionValidationLog.Print("Expression safety validation passed")
	return nil
}

func validateMarkdownExpression(expression string, unauthorizedExpressions *[]string) error {
	opts := ExpressionValidationOptions{
		NeedsStepsRe:            NeedsStepsPattern,
		InputsRe:                InputsPattern,
		WorkflowCallInputsRe:    WorkflowCallInputsPattern,
		AwInputsRe:              AWInputsPattern,
		AwImportInputsRe:        AWImportInputsPattern,
		EnvRe:                   EnvPattern,
		UnauthorizedExpressions: unauthorizedExpressions,
	}
	parsed, parseErr := ParseExpression(expression)
	if parseErr == nil {
		// If we can parse it, validate each literal expression in the tree
		return VisitExpressionTree(parsed, func(expr *ExpressionNode) error {
			return validateSingleExpression(expr.Expression, opts)
		})
	}
	// If parsing fails, fall back to validating the whole expression as a literal.
	return validateSingleExpression(expression, opts)
}

func unauthorizedExpressionError(unauthorizedExpressions []string) error {
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
	NeedsStepsRe            *regexp.Regexp
	InputsRe                *regexp.Regexp
	WorkflowCallInputsRe    *regexp.Regexp
	AwInputsRe              *regexp.Regexp
	AwImportInputsRe        *regexp.Regexp
	EnvRe                   *regexp.Regexp
	UnauthorizedExpressions *[]string
}

// validateExpressionForDangerousProps checks if an expression contains dangerous JavaScript
// property names that could be used for prototype pollution or traversal attacks.
// This matches the JavaScript runtime validation in actions/setup/js/runtime_import.cjs
// Returns an error if dangerous properties are found.
func validateExpressionForDangerousProps(expression string) error {
	expressionValidationLog.Printf("Checking expression for dangerous properties: %s", expression)
	trimmed := strings.TrimSpace(expression)

	// Split expression into parts using both dot and bracket notation;
	// filter out numeric indices (e.g., "0" in "assets[0]")
	parts := exprPartSplitRe.Split(trimmed, -1)

	for _, part := range parts {
		if part == "" || exprNumericPartRe.MatchString(part) {
			continue
		}

		if _, isDangerous := constants.DangerousPropertyNamesSet[part]; isDangerous {
			return NewValidationError(
				"expressions",
				fmt.Sprintf("dangerous property name %q found in expression", part),
				fmt.Sprintf("expression %q contains the dangerous property name %q", expression, part),
				fmt.Sprintf("Remove the dangerous property %q from the expression. Property names like constructor, __proto__, prototype, and similar JavaScript built-ins are blocked to prevent prototype pollution attacks. See PR #14826 for more details.", part),
			)
		}
	}

	return nil
}

var gatewayEventsAccessorPattern = regexp.MustCompile(`^gateway\.events(?:\.[a-zA-Z_][a-zA-Z0-9_-]*|\.\*|\[[0-9]+\]|\['[a-zA-Z0-9_.-]+'\])*$`)

func isGatewayEventsExpression(expression string) bool {
	if strings.HasPrefix(expression, "toJSON(") && strings.HasSuffix(expression, ")") {
		expression = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(expression, "toJSON("), ")"))
	}
	if !gatewayEventsAccessorPattern.MatchString(expression) {
		return false
	}
	for _, part := range strings.FieldsFunc(expression, func(r rune) bool { return r == '.' || r == '[' || r == ']' || r == '\'' }) {
		if _, dangerous := constants.DangerousPropertyNamesSet[part]; dangerous {
			return false
		}
	}
	return true
}

// validateSingleExpression validates a single literal expression
func validateSingleExpression(expression string, opts ExpressionValidationOptions) error {
	expression = strings.TrimSpace(expression)

	// Allow literal values (string, number, boolean) — safe leaf nodes in compound expressions.
	if stringLiteralRegex.MatchString(expression) ||
		numberLiteralRegex.MatchString(expression) ||
		expression == "true" || expression == "false" {
		return nil
	}

	// Check for dangerous JavaScript property names (prototype pollution, PR #14826)
	if err := validateExpressionForDangerousProps(expression); err != nil {
		return err
	}

	allowed := isAllowedSingleExpression(expression, opts)
	if !allowed {
		allowed = isAllowedOrExpression(expression, opts)
	}
	if !allowed {
		allowed = isAllowedComparisonExpression(expression, opts)
	}
	if !allowed {
		*opts.UnauthorizedExpressions = append(*opts.UnauthorizedExpressions, expression)
	}
	return nil
}

func isAllowedSingleExpression(expression string, opts ExpressionValidationOptions) bool {
	// Check if this expression is in the allowed list
	allowed := false

	if opts.NeedsStepsRe.MatchString(expression) {
		allowed = true
	} else if opts.InputsRe.MatchString(expression) {
		allowed = true
	} else if opts.WorkflowCallInputsRe.MatchString(expression) {
		allowed = true
	} else if opts.AwInputsRe.MatchString(expression) {
		allowed = true
	} else if opts.AwImportInputsRe != nil && opts.AwImportInputsRe.MatchString(expression) {
		allowed = true
	} else if opts.EnvRe.MatchString(expression) {
		allowed = true
	} else if isGatewayEventsExpression(expression) {
		allowed = true
	} else if _, ok := constants.AllowedExpressionsSet[expression]; ok {
		allowed = true
	}
	return allowed
}

func isAllowedOrExpression(expression string, opts ExpressionValidationOptions) bool {
	allowed := false
	// Check for OR expressions with literals (e.g., "inputs.repository || 'default'")
	if !allowed {
		orMatch := orExpressionPattern.FindStringSubmatch(expression)
		if len(orMatch) > 2 {
			leftExpr := strings.TrimSpace(orMatch[1])
			rightExpr := strings.TrimSpace(orMatch[2])

			leftErr := validateSingleExpression(leftExpr, opts)
			leftIsSafe := leftErr == nil && !containsExpressionInList(opts.UnauthorizedExpressions, leftExpr)

			if leftIsSafe {
				// Check if right side is a literal string (single, double, or backtick quotes)
				// Note: Using (?:) for non-capturing group and checking each quote type separately
				isStringLiteral := stringLiteralRegex.MatchString(rightExpr)
				// Check if right side is a number literal
				isNumberLiteral := numberLiteralRegex.MatchString(rightExpr)
				// Check if right side is a boolean literal
				isBooleanLiteral := rightExpr == "true" || rightExpr == "false"

				if isStringLiteral || isNumberLiteral || isBooleanLiteral {
					allowed = true
				} else {
					// If right side is also a safe expression, recursively check it
					rightErr := validateSingleExpression(rightExpr, opts)
					if rightErr == nil && !containsExpressionInList(opts.UnauthorizedExpressions, rightExpr) {
						allowed = true
					}
				}
			}
		}
	}
	return allowed
}

func isAllowedComparisonExpression(expression string, opts ExpressionValidationOptions) bool {
	allowed := false
	// Validate both sides of comparison expressions recursively.
	if !allowed {
		comparisonMatch := comparisonExpressionPattern.FindStringSubmatch(expression)
		if len(comparisonMatch) > 2 {
			leftExpr := strings.TrimSpace(comparisonMatch[1])
			rightExpr := strings.TrimSpace(comparisonMatch[2])
			leftErr := validateSingleExpression(leftExpr, opts)
			rightErr := validateSingleExpression(rightExpr, opts)
			if leftExpr != "" && rightExpr != "" &&
				leftErr == nil && !containsExpressionInList(opts.UnauthorizedExpressions, leftExpr) &&
				rightErr == nil && !containsExpressionInList(opts.UnauthorizedExpressions, rightExpr) {
				allowed = true
			}
		}
	}

	return allowed
}

// containsExpressionInList checks if an expression is in the list.
func containsExpressionInList(list *[]string, expr string) bool {
	return slices.Contains(*list, expr)
}
