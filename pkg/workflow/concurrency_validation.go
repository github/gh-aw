// This file provides validation for custom concurrency group expressions.
//
// # Concurrency Group Expression Validation
//
// This file validates that custom concurrency group expressions specified by users
// have correct syntax and can be safely compiled into GitHub Actions workflows.
//
// # Validation Functions
//
//   - validateConcurrencyGroupExpression() - Validates syntax of a single group expression
//   - extractGroupExpression() - Extracts group value from concurrency configuration
//
// # Validation Coverage
//
// The validation detects common syntactic errors at compile time:
//   - Unbalanced ${{ }} braces
//   - Missing closing braces
//   - Malformed GitHub Actions expressions
//   - Invalid logical operators placement
//   - Unclosed parentheses or quotes
//
// # When to Add Validation Here
//
// Add validation to this file when:
//   - Adding new concurrency group syntax checks
//   - Detecting new types of expression syntax errors
//   - Improving error messages for concurrency configuration

package workflow

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var concurrencyValidationLog = logger.New("workflow:concurrency_validation")

// validateConcurrencyGroupExpression validates the syntax of a custom concurrency group expression.
// It checks for common syntactic errors that would cause runtime failures:
//   - Unbalanced ${{ }} braces
//   - Missing closing braces
//   - Malformed GitHub Actions expressions
//   - Invalid operator placement
//
// Returns an error if validation fails, nil otherwise.
func validateConcurrencyGroupExpression(group string) error {
	if strings.TrimSpace(group) == "" {
		return NewValidationError(
			"concurrency",
			"empty concurrency group expression",
			"the concurrency group expression is empty or contains only whitespace",
			"Provide a non-empty concurrency group name or expression. Example: 'my-workflow-${{ github.ref }}'",
		)
	}

	concurrencyValidationLog.Printf("Validating concurrency group expression: %s", group)

	// Check for balanced ${{ }} braces
	if err := validateBalancedBraces(group); err != nil {
		return err
	}

	// Extract and validate each GitHub Actions expression within ${{ }}
	if err := validateExpressionSyntax(group); err != nil {
		return err
	}

	concurrencyValidationLog.Print("Concurrency group expression validation passed")
	return nil
}

// validateBalancedBraces checks that all ${{ }} braces are balanced and properly closed
func validateBalancedBraces(group string) error {
	openCount := 0
	i := 0
	positions := []int{} // Track positions of opening braces for error reporting

	for i < len(group) {
		// Check for opening ${{
		if i+2 < len(group) && group[i:i+3] == "${{" {
			openCount++
			positions = append(positions, i)
			i += 3
			continue
		}

		// Check for closing }}
		if i+1 < len(group) && group[i:i+2] == "}}" {
			if openCount == 0 {
				return NewValidationError(
					"concurrency",
					"unbalanced closing braces",
					fmt.Sprintf("found '}}' at position %d without matching opening '${{' in expression: %s", i, group),
					"Ensure all '}}' have a corresponding opening '${{'. Check for typos or missing opening braces.",
				)
			}
			openCount--
			if len(positions) > 0 {
				positions = positions[:len(positions)-1]
			}
			i += 2
			continue
		}

		i++
	}

	if openCount > 0 {
		// Find the position of the first unclosed opening brace
		pos := positions[0]
		return NewValidationError(
			"concurrency",
			"unclosed expression braces",
			fmt.Sprintf("found opening '${{' at position %d without matching closing '}}' in expression: %s", pos, group),
			"Ensure all '${{' have a corresponding closing '}}'. Add the missing closing braces.",
		)
	}

	return nil
}

// validateExpressionSyntax validates the syntax of expressions within ${{ }}
func validateExpressionSyntax(group string) error {
	// Pattern to extract content between ${{ and }}
	expressionPattern := regexp.MustCompile(`\$\{\{([^}]*)\}\}`)
	matches := expressionPattern.FindAllStringSubmatch(group, -1)

	for _, match := range matches {
		if len(match) < 2 {
			continue
		}

		exprContent := strings.TrimSpace(match[1])
		if exprContent == "" {
			return NewValidationError(
				"concurrency",
				"empty expression content",
				fmt.Sprintf("found empty expression '${{ }}' in concurrency group: %s", group),
				"Provide a valid GitHub Actions expression inside '${{ }}'. Example: '${{ github.ref }}'",
			)
		}

		// Check for common syntax errors
		if err := validateExpressionContent(exprContent, group); err != nil {
			return err
		}
	}

	return nil
}

// validateExpressionContent validates the content inside ${{ }}
func validateExpressionContent(expr string, fullGroup string) error {
	// Check for unbalanced parentheses
	parenCount := 0
	for i, ch := range expr {
		switch ch {
		case '(':
			parenCount++
		case ')':
			parenCount--
			if parenCount < 0 {
				return NewValidationError(
					"concurrency",
					"unbalanced parentheses in expression",
					fmt.Sprintf("found closing ')' without matching opening '(' at position %d in expression: %s", i, expr),
					"Ensure all parentheses are properly balanced in your concurrency group expression.",
				)
			}
		}
	}

	if parenCount > 0 {
		return NewValidationError(
			"concurrency",
			"unclosed parentheses in expression",
			fmt.Sprintf("found %d unclosed opening '(' in expression: %s", parenCount, expr),
			"Add the missing closing ')' to balance parentheses in your expression.",
		)
	}

	// Check for unbalanced quotes (single, double, backtick)
	if err := validateBalancedQuotes(expr); err != nil {
		return err
	}

	// Try to parse complex expressions with logical operators
	if containsLogicalOperators(expr) {
		if _, err := ParseExpression(expr); err != nil {
			return NewValidationError(
				"concurrency",
				"invalid expression syntax",
				fmt.Sprintf("failed to parse expression in concurrency group: %s", err.Error()),
				fmt.Sprintf("Fix the syntax error in your concurrency group expression. Full expression: %s", fullGroup),
			)
		}
	}

	return nil
}

// validateBalancedQuotes checks for balanced quotes in an expression
func validateBalancedQuotes(expr string) error {
	inSingleQuote := false
	inDoubleQuote := false
	inBacktick := false
	escaped := false

	for i, ch := range expr {
		if escaped {
			escaped = false
			continue
		}

		if ch == '\\' {
			escaped = true
			continue
		}

		switch ch {
		case '\'':
			if !inDoubleQuote && !inBacktick {
				inSingleQuote = !inSingleQuote
			}
		case '"':
			if !inSingleQuote && !inBacktick {
				inDoubleQuote = !inDoubleQuote
			}
		case '`':
			if !inSingleQuote && !inDoubleQuote {
				inBacktick = !inBacktick
			}
		}

		// Check if we reached end of string with unclosed quote
		if i == len(expr)-1 {
			if inSingleQuote {
				return NewValidationError(
					"concurrency",
					"unclosed single quote",
					fmt.Sprintf("found unclosed single quote in expression: %s", expr),
					"Add the missing closing single quote (') to your expression.",
				)
			}
			if inDoubleQuote {
				return NewValidationError(
					"concurrency",
					"unclosed double quote",
					fmt.Sprintf("found unclosed double quote in expression: %s", expr),
					"Add the missing closing double quote (\") to your expression.",
				)
			}
			if inBacktick {
				return NewValidationError(
					"concurrency",
					"unclosed backtick",
					fmt.Sprintf("found unclosed backtick in expression: %s", expr),
					"Add the missing closing backtick (`) to your expression.",
				)
			}
		}
	}

	return nil
}

// containsLogicalOperators checks if an expression contains logical operators (&&, ||, !)
// Note: This is a simple string-based check that may return true for expressions containing
// '!=' (not equals) since it includes the '!' character. This is acceptable because the
// function is used to decide whether to parse the expression with the expression parser,
// and expressions with '!=' will be successfully parsed by the parser.
func containsLogicalOperators(expr string) bool {
	return strings.Contains(expr, "&&") || strings.Contains(expr, "||") || strings.Contains(expr, "!")
}

// extractGroupExpression extracts the group value from a concurrency configuration.
// Handles both string format ("group-name") and object format ({group: "group-name"}).
// Returns the group expression string or empty string if not found.
func extractGroupExpression(concurrency any) string {
	if concurrency == nil {
		return ""
	}

	// Handle string format (simple group name)
	if groupStr, ok := concurrency.(string); ok {
		return groupStr
	}

	// Handle object format with group field
	if concurrencyObj, ok := concurrency.(map[string]any); ok {
		if group, hasGroup := concurrencyObj["group"]; hasGroup {
			if groupStr, ok := group.(string); ok {
				return groupStr
			}
		}
	}

	return ""
}

// extractConcurrencyGroupFromYAML extracts the group value from a YAML-formatted concurrency string.
// The input is expected to be in the format generated by the compiler:
//
//	concurrency: "group-name"  # string format
//
// or
//
//	concurrency:
//	  group: "group-name"      # object format
//	  cancel-in-progress: true  # optional
//
// Returns the group value string or empty string if not found.
//
// Note: This function uses a regex pattern that stops at the first quote or newline character.
// Group values containing embedded quotes or newlines will be truncated at that point. However,
// such values are rare in concurrency group expressions, and any resulting syntax errors will be
// caught by the subsequent expression validation.
func extractConcurrencyGroupFromYAML(concurrencyYAML string) string {
	// First, check if it's object format with explicit "group:" field
	// Pattern: group: "value" or group: 'value' or group: value (at start of line or after spaces)
	groupPattern := regexp.MustCompile(`(?m)^\s*group:\s*["']?([^"'\n]+?)["']?\s*$`)
	matches := groupPattern.FindStringSubmatch(concurrencyYAML)
	if len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}

	// If no explicit "group:" field, it might be string format: concurrency: "value"
	// Pattern: concurrency: "value" or concurrency: 'value' or concurrency: value
	// Must be on the first line (not indented, not preceded by other content)
	lines := strings.Split(concurrencyYAML, "\n")
	if len(lines) > 0 {
		firstLine := strings.TrimSpace(lines[0])
		// Only match if it starts with "concurrency:"
		if strings.HasPrefix(firstLine, "concurrency:") {
			value := strings.TrimSpace(strings.TrimPrefix(firstLine, "concurrency:"))
			// Remove quotes if present
			value = strings.Trim(value, `"'`)
			return value
		}
	}

	return ""
}
