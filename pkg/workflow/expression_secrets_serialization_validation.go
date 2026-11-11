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
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/github/gh-aw/pkg/workflow/actionslanguageservice"
)

var expressionSecretsSerializationLog = logger.New("workflow:expression_secrets_serialization_validation")

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
		spans, _ := actionslanguageservice.ExtractExpressions(match)
		if len(spans) != 1 {
			return match
		}

		exprContent := spans[0].Content
		callRanges := findSecretsSerializationCallRanges(exprContent)
		if len(callRanges) == 0 {
			return match
		}

		neutralized := replaceStringRanges(exprContent, callRanges, "false")
		return "${{" + neutralized + "}}"
	})
}

// findSecretsSerializationCallRanges returns AST call ranges, never quoted data.
func findSecretsSerializationCallRanges(expr string) [][]int {
	expression, err := actionslanguageservice.ParseSyntax(expr)
	if err != nil {
		return nil
	}
	var ranges [][]int
	actionslanguageservice.Walk(expression, func(node *actionslanguageservice.Expression) bool {
		if node.Kind == "call" && strings.EqualFold(node.Token.Text, "toJSON") && len(node.Children) == 1 {
			argument := node.Child(0)
			for argument.Kind == "group" && len(argument.Children) == 1 {
				argument = argument.Child(0)
			}
			path, ok := actionslanguageservice.Reference(argument)
			if ok && len(path) == 1 && strings.EqualFold(path[0], "secrets") {
				ranges = append(ranges, []int{node.Start, node.End})
				return false
			}
		}
		return true
	})
	return ranges
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

// findFrontmatterSecretsSerializationExpressions scans decoded YAML scalars, not
// their source spelling: YAML quoting and folding can change expression syntax.
func findFrontmatterSecretsSerializationExpressions(value any) []string {
	var found []string
	switch value := value.(type) {
	case string:
		found = findSecretsSerializationExpressions(value)
	case map[string]any:
		for _, child := range value {
			found = append(found, findFrontmatterSecretsSerializationExpressions(child)...)
		}
	case []any:
		for _, child := range value {
			found = append(found, findFrontmatterSecretsSerializationExpressions(child)...)
		}
	}
	return found
}

// validateSecretsSerializationExpressions scans markdown content and frontmatter
// decoded YAML values for expressions that serialize all secrets, such as
// ${{ toJSON(secrets) }}.
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

	allFound := findSecretsSerializationExpressions(workflowData.MarkdownContent)
	allFound = append(allFound, findFrontmatterSecretsSerializationExpressions(workflowData.RawFrontmatter)...)

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
