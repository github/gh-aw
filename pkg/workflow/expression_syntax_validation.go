// This file provides syntax-level validation for GitHub Actions expressions.
// It validates structural correctness: balanced braces, balanced quotes,
// empty expressions, and parenthesis pairing.
// For security/allowlist validation, see expression_safety_validation.go.
// For runtime-import file validation, see runtime_import_validation.go.

package workflow

import (
	"fmt"
	"regexp"
	"strings"
)

// expressionBracesPattern matches GitHub Actions ${{ }} expressions for syntax validation.
// Uses [^}]* to match non-closing-brace characters within the expression.
var expressionBracesPattern = regexp.MustCompile(`\$\{\{([^}]*)\}\}`)

// Pre-compiled regexes shared between syntax and safety validation
var (
	// stringLiteralRegex matches single-quoted, double-quoted, or backtick-quoted string literals.
	// Note: escape sequences inside strings are not handled; GitHub Actions uses '' for literal quotes.
	stringLiteralRegex = regexp.MustCompile(`^'[^']*'$|^"[^"]*"$|^` + "`[^`]*`$")
	// numberLiteralRegex matches integer and decimal number literals (with optional leading minus)
	numberLiteralRegex = regexp.MustCompile(`^-?\d+(\.\d+)?$`)
	// exprPartSplitRe splits expression strings on dot and bracket characters
	exprPartSplitRe = regexp.MustCompile(`[.\[\]]+`)
	// exprNumericPartRe matches purely numeric expression parts (array indices)
	exprNumericPartRe = regexp.MustCompile(`^\d+$`)
)

// validateBalancedBraces checks that all ${{ }} braces are balanced and properly closed
func validateBalancedBraces(group string) error {
	expressionValidationLog.Print("Checking balanced braces in expression")
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
					"expression",
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
		expressionValidationLog.Printf("Found %d unclosed brace(s) starting at position %d", openCount, pos)
		return NewValidationError(
			"expression",
			"unclosed expression braces",
			fmt.Sprintf("found opening '${{' at position %d without matching closing '}}' in expression: %s", pos, group),
			"Ensure all '${{' have a corresponding closing '}}'. Add the missing closing braces.",
		)
	}

	expressionValidationLog.Print("Brace balance check passed")
	return nil
}

// validateExpressionSyntax validates the syntax of expressions within ${{ }}
func validateExpressionSyntax(group string) error {
	matches := expressionBracesPattern.FindAllStringSubmatch(group, -1)

	expressionValidationLog.Printf("Found %d expression(s) to validate", len(matches))

	for _, match := range matches {
		if len(match) < 2 {
			continue
		}

		exprContent := strings.TrimSpace(match[1])
		if exprContent == "" {
			return NewValidationError(
				"expression",
				"empty expression content",
				"found empty expression '${{ }}' in: "+group,
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
					"expression",
					"unbalanced parentheses in expression",
					fmt.Sprintf("found closing ')' without matching opening '(' at position %d in expression: %s", i, expr),
					"Ensure all parentheses are properly balanced in your expression.",
				)
			}
		}
	}

	if parenCount > 0 {
		return NewValidationError(
			"expression",
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
		expressionValidationLog.Print("Expression contains logical operators, performing deep validation")
		if _, err := ParseExpression(expr); err != nil {
			expressionValidationLog.Printf("Expression parsing failed: %v", err)
			return NewValidationError(
				"expression",
				"invalid expression syntax",
				"failed to parse expression: "+err.Error(),
				"Fix the syntax error in your expression. Full expression: "+fullGroup,
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
			return unclosedExpressionQuoteError(expr, inSingleQuote, inDoubleQuote, inBacktick)
		}
	}

	return nil
}

func unclosedExpressionQuoteError(expr string, inSingleQuote, inDoubleQuote, inBacktick bool) error {
	if inSingleQuote {
		return NewValidationError(
			"expression",
			"unclosed single quote",
			"found unclosed single quote in expression: "+expr,
			"Add the missing closing single quote (') to your expression.",
		)
	}
	if inDoubleQuote {
		return NewValidationError(
			"expression",
			"unclosed double quote",
			"found unclosed double quote in expression: "+expr,
			"Add the missing closing double quote (\") to your expression.",
		)
	}
	if inBacktick {
		return NewValidationError(
			"expression",
			"unclosed backtick",
			"found unclosed backtick in expression: "+expr,
			"Add the missing closing backtick (`) to your expression.",
		)
	}
	return nil
}

// containsLogicalOperators checks if an expression contains logical operators (&&, ||, !).
// Note: '!=' also matches '!' — this is acceptable since the expression parser handles it.
func containsLogicalOperators(expr string) bool {
	return strings.Contains(expr, "&&") || strings.Contains(expr, "||") || strings.Contains(expr, "!")
}
