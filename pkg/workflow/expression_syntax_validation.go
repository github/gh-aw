// This file provides syntax-level validation for GitHub Actions expressions.
// It validates structural correctness: balanced braces, balanced quotes,
// empty expressions, and parenthesis pairing.
// For security/allowlist validation, see expression_safety_validation.go.
// For runtime-import file validation, see runtime_import_validation.go.

package workflow

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/workflow/actionslanguageservice"
)

// validateBalancedBraces checks that all ${{ }} braces are balanced and properly closed
func validateBalancedBraces(group string) error {
	expressionValidationLog.Print("Checking balanced braces in expression")
	expressions, err := actionslanguageservice.ExtractExpressions(group)
	if err != nil {
		return NewValidationError(
			"expression",
			"unclosed expression braces",
			err.Error(),
			"Ensure all '${{' have a corresponding closing '}}'. Add the missing closing braces.",
		)
	}
	offset := 0
	for _, expression := range expressions {
		if strings.Contains(group[offset:expression.Start], "}}") {
			return unexpectedClosingBraces(group, offset)
		}
		offset = expression.End
	}
	if strings.Contains(group[offset:], "}}") {
		return unexpectedClosingBraces(group, offset)
	}
	expressionValidationLog.Print("Brace balance check passed")
	return nil
}

func unexpectedClosingBraces(group string, offset int) error {
	return NewValidationError(
		"expression",
		"unbalanced closing braces",
		fmt.Sprintf("found '}}' at position %d without matching opening '${{' in expression: %s", offset+strings.Index(group[offset:], "}}"), group),
		"Ensure all '}}' have a corresponding opening '${{'. Check for typos or missing opening braces.",
	)
}

// validateExpressionSyntax validates the syntax of expressions within ${{ }}
func validateExpressionSyntax(group string) error {
	expressions, err := actionslanguageservice.ExtractExpressions(group)
	if err != nil {
		return err
	}
	expressionValidationLog.Printf("Found %d expression(s) to validate", len(expressions))
	for _, expression := range expressions {
		exprContent := strings.TrimSpace(expression.Content)
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
	parseErr := actionslanguageservice.ValidateExpression(expr)
	if parseErr == nil {
		return nil
	}

	// Retain the existing actionable diagnostics for common structural errors.
	// Successful official-grammar parsing takes precedence over these heuristics.
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

	return NewValidationError(
		"expression",
		"invalid expression syntax",
		"failed to parse expression: "+parseErr.Error(),
		"Fix the syntax error in your expression. Full expression: "+fullGroup,
	)
}

// validateBalancedQuotes checks for balanced quotes in an expression
func validateBalancedQuotes(expr string) error {
	var quote rune
	escaped := false

	for _, ch := range expr {
		if escaped {
			escaped = false
			continue
		}

		if ch == '\\' {
			escaped = true
			continue
		}

		if ch == quote {
			quote = 0
		} else if quote == 0 && (ch == '\'' || ch == '"' || ch == '`') {
			quote = ch
		}
	}
	switch quote {
	case '\'':
		return NewValidationError(
			"expression",
			"unclosed single quote",
			"found unclosed single quote in expression: "+expr,
			"Add the missing closing single quote (') to your expression.",
		)
	case '"':
		return NewValidationError(
			"expression",
			"unclosed double quote",
			"found unclosed double quote in expression: "+expr,
			"Add the missing closing double quote (\") to your expression.",
		)
	case '`':
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
