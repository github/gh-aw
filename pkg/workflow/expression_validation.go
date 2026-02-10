// This file provides GitHub Actions expression security validation.
//
// # Expression Safety Validation
//
// This file validates that GitHub Actions expressions used in workflow markdown
// are safe and authorized. It prevents injection attacks and ensures workflows
// only use approved expression patterns.
//
// # Validation Functions
//
//   - validateExpressionSafety() - Validates all expressions in markdown content
//   - validateSingleExpression() - Validates individual expression syntax
//
// # Validation Pattern: Allowlist Security
//
// Expression validation uses a strict allowlist approach:
//   - Only pre-approved GitHub context expressions are allowed
//   - Unauthorized expressions cause compilation to fail
//   - Prevents injection of secrets or environment variables
//   - Uses regex patterns to match allowed expression formats
//
// # Allowed Expression Patterns
//
// Expressions must match one of these patterns:
//   - github.event.* (event context properties)
//   - github.actor, github.repository, etc. (core GitHub context)
//   - needs.*.outputs.* (job dependencies)
//   - steps.*.outputs.* (step outputs)
//   - github.event.inputs.* (workflow_dispatch inputs)
//
// See pkg/constants for the complete list of allowed expressions.
//
// # When to Add Validation Here
//
// Add validation to this file when:
//   - It validates GitHub Actions expression parsing
//   - It enforces expression security policies
//   - It prevents expression injection attacks
//   - It validates expression syntax and structure
//
// For general validation, see validation.go.
// For detailed documentation, see scratchpad/validation-architecture.md

package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
)

var expressionValidationLog = logger.New("workflow:expression_validation")

// maxFuzzyMatchSuggestions is the maximum number of similar expressions to suggest
// when an unauthorized expression is found
const maxFuzzyMatchSuggestions = 7

// Pre-compiled regexes for expression validation (performance optimization)
var (
	expressionRegex         = regexp.MustCompile(`(?s)\$\{\{(.*?)\}\}`)
	needsStepsRegex         = regexp.MustCompile(`^(needs|steps)\.[a-zA-Z0-9_-]+(\.[a-zA-Z0-9_-]+)*$`)
	inputsRegex             = regexp.MustCompile(`^github\.event\.inputs\.[a-zA-Z0-9_-]+$`)
	workflowCallInputsRegex = regexp.MustCompile(`^inputs\.[a-zA-Z0-9_-]+$`)
	awInputsRegex           = regexp.MustCompile(`^github\.aw\.inputs\.[a-zA-Z0-9_-]+$`)
	envRegex                = regexp.MustCompile(`^env\.[a-zA-Z0-9_-]+$`)
	// comparisonExtractionRegex extracts property accesses from comparison expressions
	// Matches patterns like "github.workflow == 'value'" and extracts "github.workflow"
	comparisonExtractionRegex = regexp.MustCompile(`([a-zA-Z_][a-zA-Z0-9_.]*)\s*(?:==|!=|<|>|<=|>=)\s*`)
)

// validateExpressionSafety checks that all GitHub Actions expressions in the markdown content
// are in the allowed list and returns an error if any unauthorized expressions are found
func validateExpressionSafety(markdownContent string) error {
	expressionValidationLog.Print("Validating expression safety in markdown content")

	// Regular expression to match GitHub Actions expressions: ${{ ... }}
	// Use (?s) flag to enable dotall mode so . matches newlines to capture multiline expressions
	// Use non-greedy matching with .*? to handle nested braces properly

	// Find all expressions in the markdown content
	matches := expressionRegex.FindAllStringSubmatch(markdownContent, -1)
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
		parsed, parseErr := ParseExpression(expression)
		if parseErr == nil {
			// If we can parse it, validate each literal expression in the tree
			validationErr := VisitExpressionTree(parsed, func(expr *ExpressionNode) error {
				return validateSingleExpression(expr.Expression, ExpressionValidationOptions{
					NeedsStepsRe:            needsStepsRegex,
					InputsRe:                inputsRegex,
					WorkflowCallInputsRe:    workflowCallInputsRegex,
					AwInputsRe:              awInputsRegex,
					EnvRe:                   envRegex,
					UnauthorizedExpressions: &unauthorizedExpressions,
				})
			})
			if validationErr != nil {
				return validationErr
			}
		} else {
			// If parsing fails, fall back to validating the whole expression as a literal
			err := validateSingleExpression(expression, ExpressionValidationOptions{
				NeedsStepsRe:            needsStepsRegex,
				InputsRe:                inputsRegex,
				WorkflowCallInputsRe:    workflowCallInputsRegex,
				AwInputsRe:              awInputsRegex,
				EnvRe:                   envRegex,
				UnauthorizedExpressions: &unauthorizedExpressions,
			})
			if err != nil {
				return err
			}
		}
	}

	// If we found unauthorized expressions, return an error
	if len(unauthorizedExpressions) > 0 {
		expressionValidationLog.Printf("Expression safety validation failed: %d unauthorized expressions found", len(unauthorizedExpressions))
		// Format unauthorized expressions list with fuzzy match suggestions
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

		// Format allowed expressions list
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
		allowedList.WriteString("  - inputs.* (workflow_call)\n")
		allowedList.WriteString("  - env.*\n")

		return NewValidationError(
			"expressions",
			fmt.Sprintf("%d unauthorized expressions found", len(unauthorizedExpressions)),
			fmt.Sprintf("expressions are not in the allowed list:%s", unauthorizedList.String()),
			fmt.Sprintf("Use only allowed expressions:%s\nFor more details, see the expression security documentation.", allowedList.String()),
		)
	}

	expressionValidationLog.Print("Expression safety validation passed")
	return nil
}

// ExpressionValidationOptions contains the options for validating a single expression
type ExpressionValidationOptions struct {
	NeedsStepsRe            *regexp.Regexp
	InputsRe                *regexp.Regexp
	WorkflowCallInputsRe    *regexp.Regexp
	AwInputsRe              *regexp.Regexp
	EnvRe                   *regexp.Regexp
	UnauthorizedExpressions *[]string
}

// validateExpressionForDangerousProps checks if an expression contains dangerous JavaScript
// property names that could be used for prototype pollution or traversal attacks.
// This matches the JavaScript runtime validation in actions/setup/js/runtime_import.cjs
// Returns an error if dangerous properties are found.
func validateExpressionForDangerousProps(expression string) error {
	trimmed := strings.TrimSpace(expression)

	// Split expression into parts handling both dot notation (e.g., "github.event.issue")
	// and bracket notation (e.g., "release.assets[0].id")
	// Filter out numeric indices (e.g., "0" in "assets[0]")
	parts := regexp.MustCompile(`[.\[\]]+`).Split(trimmed, -1)

	for _, part := range parts {
		// Skip empty parts and numeric indices
		if part == "" || regexp.MustCompile(`^\d+$`).MatchString(part) {
			continue
		}

		// Check if this part is a dangerous property name
		for _, dangerousProp := range constants.DangerousPropertyNames {
			if part == dangerousProp {
				return NewValidationError(
					"expressions",
					fmt.Sprintf("dangerous property name '%s' found in expression", dangerousProp),
					fmt.Sprintf("expression '%s' contains the dangerous property name '%s'", expression, dangerousProp),
					fmt.Sprintf("Remove the dangerous property '%s' from the expression. Property names like constructor, __proto__, prototype, and similar JavaScript built-ins are blocked to prevent prototype pollution attacks. See PR #14826 for more details.", dangerousProp),
				)
			}
		}
	}

	return nil
}

// validateSingleExpression validates a single literal expression
func validateSingleExpression(expression string, opts ExpressionValidationOptions) error {
	expression = strings.TrimSpace(expression)

	// First, check for dangerous JavaScript property names that could be used for
	// prototype pollution or traversal attacks (PR #14826)
	if err := validateExpressionForDangerousProps(expression); err != nil {
		return err
	}

	// Check if this expression is in the allowed list
	allowed := false

	// Check if this expression starts with "needs." or "steps." and is a simple property access
	if opts.NeedsStepsRe.MatchString(expression) {
		allowed = true
	} else if opts.InputsRe.MatchString(expression) {
		// Check if this expression matches github.event.inputs.* pattern
		allowed = true
	} else if opts.WorkflowCallInputsRe.MatchString(expression) {
		// Check if this expression matches inputs.* pattern (workflow_call inputs)
		allowed = true
	} else if opts.AwInputsRe.MatchString(expression) {
		// Check if this expression matches github.agentics.inputs.* pattern (shared workflow inputs)
		allowed = true
	} else if opts.EnvRe.MatchString(expression) {
		// check if this expression matches env.* pattern
		allowed = true
	} else {
		for _, allowedExpr := range constants.AllowedExpressions {
			if expression == allowedExpr {
				allowed = true
				break
			}
		}
	}

	// Check for OR expressions with literals (e.g., "inputs.repository || 'default'")
	// Pattern: safe_expression || 'literal' or safe_expression || "literal" or safe_expression || `literal`
	// Also supports numbers and booleans as literals
	if !allowed {
		// Match pattern: something || something_else
		orPattern := regexp.MustCompile(`^(.+?)\s*\|\|\s*(.+)$`)
		orMatch := orPattern.FindStringSubmatch(expression)
		if len(orMatch) > 2 {
			leftExpr := strings.TrimSpace(orMatch[1])
			rightExpr := strings.TrimSpace(orMatch[2])

			// Check if left side is safe (recursively validate)
			leftErr := validateSingleExpression(leftExpr, opts)
			leftIsSafe := leftErr == nil && !containsExpression(opts.UnauthorizedExpressions, leftExpr)

			if leftIsSafe {
				// Check if right side is a literal string (single, double, or backtick quotes)
				// Note: Using (?:) for non-capturing group and checking each quote type separately
				isStringLiteral := regexp.MustCompile(`^'[^']*'$|^"[^"]*"$|^` + "`[^`]*`$").MatchString(rightExpr)
				// Check if right side is a number literal
				isNumberLiteral := regexp.MustCompile(`^-?\d+(\.\d+)?$`).MatchString(rightExpr)
				// Check if right side is a boolean literal
				isBooleanLiteral := rightExpr == "true" || rightExpr == "false"

				if isStringLiteral || isNumberLiteral || isBooleanLiteral {
					allowed = true
				} else {
					// If right side is also a safe expression, recursively check it
					rightErr := validateSingleExpression(rightExpr, opts)
					if rightErr == nil && !containsExpression(opts.UnauthorizedExpressions, rightExpr) {
						allowed = true
					}
				}
			}
		}
	}

	// If not allowed as a whole, try to extract and validate property accesses from comparisons
	if !allowed {
		// Extract property accesses from comparison expressions (e.g., "github.workflow == 'value'")
		matches := comparisonExtractionRegex.FindAllStringSubmatch(expression, -1)
		if len(matches) > 0 {
			// Assume it's allowed if all extracted properties are allowed
			allPropertiesAllowed := true
			for _, match := range matches {
				if len(match) > 1 {
					property := strings.TrimSpace(match[1])
					propertyAllowed := false

					// Check if extracted property is allowed
					if opts.NeedsStepsRe.MatchString(property) {
						propertyAllowed = true
					} else if opts.InputsRe.MatchString(property) {
						propertyAllowed = true
					} else if opts.WorkflowCallInputsRe.MatchString(property) {
						propertyAllowed = true
					} else if opts.AwInputsRe.MatchString(property) {
						propertyAllowed = true
					} else if opts.EnvRe.MatchString(property) {
						propertyAllowed = true
					} else {
						for _, allowedExpr := range constants.AllowedExpressions {
							if property == allowedExpr {
								propertyAllowed = true
								break
							}
						}
					}

					if !propertyAllowed {
						allPropertiesAllowed = false
						break
					}
				}
			}

			if allPropertiesAllowed && len(matches) > 0 {
				allowed = true
			}
		}
	}

	if !allowed {
		*opts.UnauthorizedExpressions = append(*opts.UnauthorizedExpressions, expression)
	}

	return nil
}

// containsExpression checks if an expression is in the list
func containsExpression(list *[]string, expr string) bool {
	for _, item := range *list {
		if item == expr {
			return true
		}
	}
	return false
}

// ValidateExpressionSafetyPublic is a public wrapper for validateExpressionSafety
// that allows testing expression validation from external packages
func ValidateExpressionSafetyPublic(markdownContent string) error {
	return validateExpressionSafety(markdownContent)
}

// extractRuntimeImportPaths extracts all runtime-import file paths from markdown content.
// Returns a list of file paths (not URLs) referenced in {{#runtime-import}} macros.
// URLs (http:// or https://) are excluded since they are validated separately.
func extractRuntimeImportPaths(markdownContent string) []string {
	if markdownContent == "" {
		return nil
	}

	var paths []string
	seen := make(map[string]bool)

	// Pattern to match {{#runtime-import filepath}} or {{#runtime-import? filepath}}
	// Also handles line ranges like filepath:10-20
	macroPattern := `\{\{#runtime-import\??[ \t]+([^\}]+)\}\}`
	macroRe := regexp.MustCompile(macroPattern)
	matches := macroRe.FindAllStringSubmatch(markdownContent, -1)

	for _, match := range matches {
		if len(match) > 1 {
			pathWithRange := strings.TrimSpace(match[1])

			// Remove line range if present (e.g., "file.md:10-20" -> "file.md")
			filepath := pathWithRange
			if colonIdx := strings.Index(pathWithRange, ":"); colonIdx > 0 {
				// Check if what follows colon looks like a line range (digits-digits)
				afterColon := pathWithRange[colonIdx+1:]
				if regexp.MustCompile(`^\d+-\d+$`).MatchString(afterColon) {
					filepath = pathWithRange[:colonIdx]
				}
			}

			// Skip URLs - they don't need file validation
			if strings.HasPrefix(filepath, "http://") || strings.HasPrefix(filepath, "https://") {
				continue
			}

			// Add to list if not already seen
			if !seen[filepath] {
				paths = append(paths, filepath)
				seen[filepath] = true
			}
		}
	}

	return paths
}

// validateRuntimeImportFiles validates expressions in all runtime-import files at compile time.
// This catches expression errors early, before the workflow runs.
// workspaceDir should be the root of the repository (containing .github folder).
func validateRuntimeImportFiles(markdownContent string, workspaceDir string) error {
	expressionValidationLog.Print("Validating runtime-import files")

	// Extract all runtime-import file paths
	paths := extractRuntimeImportPaths(markdownContent)
	if len(paths) == 0 {
		expressionValidationLog.Print("No runtime-import files to validate")
		return nil
	}

	expressionValidationLog.Printf("Found %d runtime-import file(s) to validate", len(paths))

	var validationErrors []string

	for _, filePath := range paths {
		// Normalize the path to be relative to .github folder
		normalizedPath := filePath
		if strings.HasPrefix(normalizedPath, ".github/") {
			normalizedPath = normalizedPath[8:] // Remove ".github/"
		} else if strings.HasPrefix(normalizedPath, ".github\\") {
			normalizedPath = normalizedPath[8:] // Remove ".github\" (Windows)
		}
		if strings.HasPrefix(normalizedPath, "./") {
			normalizedPath = normalizedPath[2:] // Remove "./"
		} else if strings.HasPrefix(normalizedPath, ".\\") {
			normalizedPath = normalizedPath[2:] // Remove ".\" (Windows)
		}

		// Build absolute path to the file
		githubFolder := filepath.Join(workspaceDir, ".github")
		absolutePath := filepath.Join(githubFolder, normalizedPath)

		// Security check: ensure the resolved path is within the .github folder
		// Use filepath.Rel to check if the path escapes the .github folder
		normalizedGithubFolder := filepath.Clean(githubFolder)
		normalizedAbsolutePath := filepath.Clean(absolutePath)
		relativePath, err := filepath.Rel(normalizedGithubFolder, normalizedAbsolutePath)
		if err != nil || relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) || filepath.IsAbs(relativePath) {
			validationErrors = append(validationErrors, fmt.Sprintf("%s: Security: Path must be within .github folder (resolves to: %s)", filePath, relativePath))
			continue
		}

		// Check if file exists
		if _, err := os.Stat(absolutePath); os.IsNotExist(err) {
			// Skip validation for optional imports ({{#runtime-import? ...}})
			// We can't determine if it's optional here, but missing files will be caught at runtime
			expressionValidationLog.Printf("Skipping validation for non-existent file: %s", filePath)
			continue
		}

		// Read the file content
		content, err := os.ReadFile(absolutePath)
		if err != nil {
			validationErrors = append(validationErrors, fmt.Sprintf("%s: failed to read file: %v", filePath, err))
			continue
		}

		// Validate expressions in the imported file
		if err := validateExpressionSafety(string(content)); err != nil {
			validationErrors = append(validationErrors, fmt.Sprintf("%s: %v", filePath, err))
		} else {
			expressionValidationLog.Printf("✓ Validated expressions in %s", filePath)
		}
	}

	if len(validationErrors) > 0 {
		expressionValidationLog.Printf("Runtime-import validation failed: %d file(s) with errors", len(validationErrors))
		return NewValidationError(
			"runtime-import",
			fmt.Sprintf("%d files with errors", len(validationErrors)),
			fmt.Sprintf("runtime-import files contain expression errors:\n\n%s", strings.Join(validationErrors, "\n\n")),
			"Fix the expression errors in the imported files listed above. Each file must only use allowed GitHub Actions expressions. See expression security documentation for details.",
		)
	}

	expressionValidationLog.Print("All runtime-import files validated successfully")
	return nil
}
