// This file provides validation for GitHub Actions expressions that serialize the
// entire secrets context, such as toJSON(secrets).
//
// # Secrets Serialization Validation
//
// Expressions like ${{ toJSON(secrets) }} pass the entire secrets context to a
// function that converts it to a string. This exposes ALL secrets to the agent
// rather than only the specific secret values it requires.
//
// The validation uses the same strict/non-strict pattern as other secret checks:
//   - In strict mode an error is returned.
//   - In non-strict mode a warning is printed and compilation continues.
//
// For strict mode orchestration, see strict_mode_validation.go.
// For environment secrets validation, see strict_mode_env_validation.go.
// For steps secrets validation, see strict_mode_steps_validation.go.

package workflow

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/sliceutil"
)

var expressionSecretsSerializationLog = logger.New("workflow:expression_secrets_serialization_validation")

// secretsSerializationPattern matches function calls that pass the entire secrets
// context as an argument, e.g. toJSON(secrets).
//
// GitHub Actions function names are case-insensitive, so (?i) is used. The
// word-boundary \b before the function name prevents matching substrings
// (e.g. "nottoJSON"). The pattern deliberately does NOT match
// toJSON(secrets.SPECIFIC_KEY) because a dot after "secrets" would be consumed
// by \s*\) only when no further content follows "secrets" before ")".
var secretsSerializationPattern = regexp.MustCompile(`(?i)\btoJSON\s*\(\s*secrets\s*\)`)

// findSecretsSerializationExpressions scans content for GitHub Actions expressions
// that serialize the entire secrets context (e.g. ${{ toJSON(secrets) }}).
// Returns the full ${{ … }} expression strings that matched.
func findSecretsSerializationExpressions(content string) []string {
	if !strings.Contains(content, "${{") {
		return nil
	}

	expressionSecretsSerializationLog.Printf("Scanning content (%d bytes) for secrets serialization expressions", len(content))

	matches := ExpressionPatternDotAll.FindAllStringSubmatchIndex(content, -1)
	var found []string
	for _, match := range matches {
		if len(match) < 4 {
			continue
		}
		exprContent := content[match[2]:match[3]]
		if len(findSecretsSerializationCallRanges(exprContent)) > 0 {
			found = append(found, content[match[0]:match[1]])
		}
	}

	if len(found) > 0 {
		expressionSecretsSerializationLog.Printf("Found %d secrets serialization expression(s)", len(found))
	}
	return found
}

// neutralizeSecretsSerializationExpressions replaces ${{ toJSON(secrets) }} (and
// case/whitespace variants) with ${{ false }} so that the expression allowlist does
// not re-error on a pattern that was already handled by
// validateSecretsSerializationExpressions in non-strict mode.
//
// "${{ false }}" is a valid literal expression and passes the allowlist check.
func neutralizeSecretsSerializationExpressions(content string) string {
	return ExpressionPatternDotAll.ReplaceAllStringFunc(content, func(match string) string {
		groups := ExpressionPatternDotAll.FindStringSubmatchIndex(match)
		if len(groups) < 4 {
			return match
		}

		exprContent := match[groups[2]:groups[3]]
		callRanges := findSecretsSerializationCallRanges(exprContent)
		if len(callRanges) == 0 {
			return match
		}

		neutralized := replaceStringRanges(exprContent, callRanges, "false")
		return match[:groups[2]] + neutralized + match[groups[3]:]
	})
}

// findSecretsSerializationCallRanges returns toJSON(secrets) match ranges in expr,
// excluding quoted string literals.
func findSecretsSerializationCallRanges(expr string) [][]int {
	masked := maskQuotedExpressionLiterals(expr)
	return secretsSerializationPattern.FindAllStringIndex(masked, -1)
}

// maskQuotedExpressionLiterals replaces quoted content with spaces, preserving
// string length and indexes for downstream regex range mapping.
func maskQuotedExpressionLiterals(expr string) string {
	if expr == "" {
		return expr
	}

	masked := []byte(expr)
	var quote byte
	escaped := false
	for i := range masked {
		ch := masked[i]
		if quote == 0 {
			if ch == '\'' || ch == '"' || ch == '`' {
				quote = ch
				masked[i] = ' '
			}
			continue
		}

		masked[i] = ' '
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
		}
	}

	return string(masked)
}

func replaceStringRanges(input string, ranges [][]int, replacement string) string {
	var b strings.Builder
	last := 0
	for _, r := range ranges {
		if len(r) != 2 || r[0] < last || r[1] > len(input) {
			expressionSecretsSerializationLog.Printf("Skipping invalid replacement range %v for input length %d", r, len(input))
			continue
		}
		b.WriteString(input[last:r[0]])
		b.WriteString(replacement)
		last = r[1]
	}
	b.WriteString(input[last:])
	return b.String()
}

// validateSecretsSerializationExpressions scans markdown content and frontmatter
// YAML for expressions that serialize all secrets, such as ${{ toJSON(secrets) }}.
//
// Serializing the entire secrets context exposes ALL secrets to the agent, which
// is a significant security risk. Callers should use specific secret references
// (e.g. secrets.MY_SECRET) instead.
//
// In strict mode this returns an error; in non-strict mode it emits a warning to
// stderr and increments the compiler warning count.
func (c *Compiler) validateSecretsSerializationExpressions(workflowData *WorkflowData) error {
	effectiveStrict := c.effectiveStrictMode(workflowData.RawFrontmatter)
	expressionSecretsSerializationLog.Printf(
		"Validating secrets serialization expressions (strictMode=%t, effectiveStrictMode=%t)",
		c.strictMode,
		effectiveStrict,
	)

	var allFound []string
	for _, content := range []string{workflowData.MarkdownContent, workflowData.FrontmatterYAML} {
		allFound = append(allFound, findSecretsSerializationExpressions(content)...)
	}

	if len(allFound) == 0 {
		expressionSecretsSerializationLog.Printf("No secrets serialization expressions found")
		return nil
	}

	allFound = sliceutil.Deduplicate(allFound)
	sort.Strings(allFound)

	expressionSecretsSerializationLog.Printf("Detected %d secrets serialization expression(s): %v", len(allFound), allFound)

	msg := fmt.Sprintf(
		"secrets serialization expression(s) detected that would expose all secrets to the agent. "+
			"Found: %s. "+
			"Use specific secret references (e.g. secrets.MY_SECRET) instead of passing the entire secrets context.",
		strings.Join(allFound, ", "),
	)

	if effectiveStrict {
		return fmt.Errorf("strict mode: %s", msg)
	}

	fmt.Fprintln(os.Stderr, console.FormatWarningMessage(msg))
	c.IncrementWarningCount()
	return nil
}
