// This file provides template injection vulnerability detection.
//
// # Template Injection Detection
//
// This file validates that GitHub Actions expressions are not used directly in
// shell commands where they could enable template injection attacks. It detects
// unsafe patterns where user-controlled data flows into shell execution context.
//
// # Validation Functions
//
//   - validateNoTemplateInjection() - Validates compiled YAML for template injection risks
//
// # Validation Pattern: Security Detection
//
// Template injection validation uses pattern detection:
//   - Scans compiled YAML for run: steps with inline expressions
//   - Identifies unsafe patterns: ${{ ... }} directly in shell commands
//   - Suggests safe patterns: use env: variables instead
//   - Focuses on high-risk contexts: github.event.*, steps.*.outputs.*
//
// # Unsafe Patterns (Template Injection Risk)
//
// Direct expression use in run: commands:
//   - run: echo "${{ github.event.issue.title }}"
//   - run: bash script.sh ${{ steps.foo.outputs.bar }}
//   - run: command "${{ inputs.user_data }}"
//
// # Safe Patterns (No Template Injection)
//
// Expression use through environment variables:
//   - env: { VALUE: "${{ github.event.issue.title }}" }
//     run: echo "$VALUE"
//   - env: { OUTPUT: "${{ steps.foo.outputs.bar }}" }
//     run: bash script.sh "$OUTPUT"
//
// # When to Add Validation Here
//
// Add validation to this file when:
//   - It detects template injection vulnerabilities
//   - It validates expression usage in shell contexts
//   - It enforces safe expression handling patterns
//   - It provides security-focused compile-time checks
//
// For general validation, see validation.go.
// For detailed documentation, see scratchpad/validation-architecture.md and
// scratchpad/template-injection-prevention.md

package workflow

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/goccy/go-yaml"
)

var templateInjectionValidationLog = logger.New("workflow:template_injection_validation")

// Pre-compiled regex patterns for template injection detection
var (
	// inlineExpressionRegex matches GitHub Actions template expressions ${{ ... }}
	inlineExpressionRegex = regexp.MustCompile(`\$\{\{[^}]+\}\}`)

	// unsafeContextRegex matches high-risk context expressions that could contain user input
	// These patterns are particularly dangerous when used directly in shell commands
	unsafeContextRegex = regexp.MustCompile(`\$\{\{\s*(github\.event\.|steps\.[^}]+\.outputs\.|inputs\.)[^}]+\}\}`)
)

// validateNoTemplateInjection checks compiled YAML for template injection vulnerabilities
// It detects cases where GitHub Actions expressions are used directly in shell commands
// instead of being passed through environment variables
func validateNoTemplateInjection(yamlContent string) error {
	templateInjectionValidationLog.Print("Validating compiled YAML for template injection risks")

	// Parse YAML to walk the tree and extract run fields
	var workflow map[string]any
	if err := yaml.Unmarshal([]byte(yamlContent), &workflow); err != nil {
		templateInjectionValidationLog.Printf("Failed to parse YAML: %v", err)
		// Fall back to skipping validation if YAML is malformed
		// (compilation would have already failed if YAML is invalid)
		return nil
	}

	// Extract all run blocks from the workflow
	runBlocks := extractRunBlocks(workflow)
	templateInjectionValidationLog.Printf("Found %d run blocks to scan", len(runBlocks))

	var violations []TemplateInjectionViolation

	for _, runContent := range runBlocks {
		// Check if this run block contains inline expressions
		if !inlineExpressionRegex.MatchString(runContent) {
			continue
		}

		// Remove heredoc content from the run block to avoid false positives
		// Heredocs (e.g., << 'EOF' ... EOF) safely contain template expressions
		// because they're written to files, not executed in shell
		contentWithoutHeredocs := removeHeredocContent(runContent)

		// Extract all inline expressions from this run block (excluding heredocs)
		expressions := inlineExpressionRegex.FindAllString(contentWithoutHeredocs, -1)

		// Check each expression for unsafe contexts
		for _, expr := range expressions {
			if unsafeContextRegex.MatchString(expr) {
				// Found an unsafe pattern - extract a snippet for context
				snippet := extractRunSnippet(contentWithoutHeredocs, expr)
				violations = append(violations, TemplateInjectionViolation{
					Expression: expr,
					Snippet:    snippet,
					Context:    detectExpressionContext(expr),
				})

				templateInjectionValidationLog.Printf("Found template injection risk: %s in run block", expr)
			}
		}
	}

	// If we found violations, return a detailed error
	if len(violations) > 0 {
		templateInjectionValidationLog.Printf("Template injection validation failed: %d violations found", len(violations))
		return formatTemplateInjectionError(violations)
	}

	templateInjectionValidationLog.Print("Template injection validation passed")
	return nil
}

// extractRunBlocks walks the YAML tree and extracts all run: field values
func extractRunBlocks(data any) []string {
	var runBlocks []string

	switch v := data.(type) {
	case map[string]any:
		// Check if this map has a "run" key
		if runValue, ok := v["run"]; ok {
			if runStr, ok := runValue.(string); ok {
				runBlocks = append(runBlocks, runStr)
			}
		}
		// Recursively process all values in the map
		for _, value := range v {
			runBlocks = append(runBlocks, extractRunBlocks(value)...)
		}
	case []any:
		// Recursively process all items in the slice
		for _, item := range v {
			runBlocks = append(runBlocks, extractRunBlocks(item)...)
		}
	}

	return runBlocks
}

// removeHeredocContent removes heredoc sections from shell commands
// Heredocs (e.g., cat > file << 'EOF' ... EOF) are safe for template expressions
// because the content is written to files, not executed in the shell
func removeHeredocContent(content string) string {
	// Match common heredoc patterns with known delimiters
	// Since Go regex doesn't support backreferences, we match common heredoc delimiters explicitly
	commonDelimiters := []string{"EOF", "EOL", "END", "HEREDOC", "JSON", "YAML", "SQL"}

	result := content
	for _, delimiter := range commonDelimiters {
		// Pattern for quoted delimiter: << 'DELIMITER' or << "DELIMITER"
		// (?ms) enables multiline and dotall modes, .*? is non-greedy
		// \s*%s\s*$ allows for leading/trailing whitespace on the closing delimiter
		quotedPattern := fmt.Sprintf(`(?ms)<<\s*['"]%s['"].*?\n\s*%s\s*$`, delimiter, delimiter)
		quotedRegex := regexp.MustCompile(quotedPattern)
		result = quotedRegex.ReplaceAllString(result, "# heredoc removed")

		// Pattern for unquoted delimiter: << DELIMITER
		unquotedPattern := fmt.Sprintf(`(?ms)<<\s*%s.*?\n\s*%s\s*$`, delimiter, delimiter)
		unquotedRegex := regexp.MustCompile(unquotedPattern)
		result = unquotedRegex.ReplaceAllString(result, "# heredoc removed")
	}

	return result
}

// TemplateInjectionViolation represents a detected template injection risk
type TemplateInjectionViolation struct {
	Expression string // The unsafe expression (e.g., "${{ github.event.issue.title }}")
	Snippet    string // Code snippet showing the violation context
	Context    string // Expression context (e.g., "github.event", "steps.*.outputs")
}

// extractRunSnippet extracts a relevant snippet from the run block containing the expression
func extractRunSnippet(runContent string, expression string) string {
	lines := strings.Split(runContent, "\n")

	for _, line := range lines {
		if strings.Contains(line, expression) {
			// Return the trimmed line containing the expression
			trimmed := strings.TrimSpace(line)
			// Limit snippet length to avoid overwhelming error messages
			if len(trimmed) > 100 {
				return trimmed[:97] + "..."
			}
			return trimmed
		}
	}

	// Fallback: return the expression itself
	return expression
}

// detectExpressionContext identifies what type of expression this is
func detectExpressionContext(expression string) string {
	if strings.Contains(expression, "github.event.") {
		return "github.event"
	}
	if strings.Contains(expression, "steps.") && strings.Contains(expression, ".outputs.") {
		return "steps.*.outputs"
	}
	if strings.Contains(expression, "inputs.") {
		return "workflow inputs"
	}
	return "unknown context"
}

// formatTemplateInjectionError formats a user-friendly error message for template injection violations
func formatTemplateInjectionError(violations []TemplateInjectionViolation) error {
	var builder strings.Builder

	builder.WriteString("template injection vulnerabilities detected in compiled workflow\n\n")
	builder.WriteString("The following expressions are used directly in shell commands, which enables template injection attacks:\n\n")

	// Group violations by context for clearer reporting
	contextGroups := make(map[string][]TemplateInjectionViolation)
	for _, v := range violations {
		contextGroups[v.Context] = append(contextGroups[v.Context], v)
	}

	// Report violations grouped by context
	for context, contextViolations := range contextGroups {
		fmt.Fprintf(&builder, "  %s context (%d occurrence(s)):\n", context, len(contextViolations))

		// Show up to 3 examples per context to keep error message manageable
		maxExamples := 3
		for i, v := range contextViolations {
			if i >= maxExamples {
				fmt.Fprintf(&builder, "    ... and %d more\n", len(contextViolations)-maxExamples)
				break
			}
			fmt.Fprintf(&builder, "    - %s\n", v.Expression)
			fmt.Fprintf(&builder, "      in: %s\n", v.Snippet)
		}
		builder.WriteString("\n")
	}

	builder.WriteString("Security Risk:\n")
	builder.WriteString("  When expressions are used directly in shell commands, an attacker can inject\n")
	builder.WriteString("  malicious code through user-controlled inputs (issue titles, PR descriptions,\n")
	builder.WriteString("  comments, etc.) to execute arbitrary commands, steal secrets, or modify the repository.\n\n")

	builder.WriteString("Safe Pattern - Use environment variables instead:\n")
	builder.WriteString("  env:\n")
	builder.WriteString("    MY_VALUE: ${{ github.event.issue.title }}\n")
	builder.WriteString("  run: |\n")
	builder.WriteString("    echo \"Title: $MY_VALUE\"\n\n")

	builder.WriteString("Unsafe Pattern - Do NOT use expressions directly:\n")
	builder.WriteString("  run: |\n")
	builder.WriteString("    echo \"Title: ${{ github.event.issue.title }}\"  # UNSAFE!\n\n")

	builder.WriteString("References:\n")
	builder.WriteString("  - https://docs.github.com/en/actions/security-guides/security-hardening-for-github-actions\n")
	builder.WriteString("  - https://docs.zizmor.sh/audits/#template-injection\n")
	builder.WriteString("  - scratchpad/template-injection-prevention.md\n")

	return fmt.Errorf("%s", builder.String())
}
