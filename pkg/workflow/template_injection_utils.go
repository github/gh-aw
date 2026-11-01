package workflow

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/stringutil"
	"mvdan.cc/sh/v3/syntax"
)

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

	if len(runBlocks) > 0 {
		templateInjectionValidationLog.Printf("Extracted %d run block(s) from YAML tree", len(runBlocks))
	}
	return runBlocks
}

type shellHeredocRegion struct {
	start, end int
	quoted     bool
}

// shellHeredocRegions uses Bash syntax rather than delimiter heuristics. Masking
// Actions expressions preserves byte offsets while avoiding shell parse errors.
// A parse failure grants no heredoc exemption.
func shellHeredocRegions(content string) ([]shellHeredocRegion, bool) {
	if !strings.Contains(content, "<<") {
		return nil, true
	}
	masked := []byte(content)
	for _, loc := range ExpressionPattern.FindAllStringSubmatchIndex(content, -1) {
		if len(loc) < 2 {
			continue
		}
		for i := loc[0]; i < loc[1]; i++ { //nolint:uncheckedsliceindex // Match index pairs are byte offsets into this same content.
			if masked[i] != '\n' && masked[i] != '\r' { //nolint:uncheckedsliceindex // The matching span bounds i within masked.
				masked[i] = 'a' //nolint:uncheckedsliceindex // The matching span bounds i within masked.
			}
		}
	}
	file, err := syntax.NewParser().Parse(bytes.NewReader(masked), "")
	if err != nil {
		templateInjectionValidationLog.Printf("Cannot identify safe heredoc regions; scanning entire script: %v", err)
		return nil, false
	}
	var regions []shellHeredocRegion
	syntax.Walk(file, func(node syntax.Node) bool {
		redirect, ok := node.(*syntax.Redirect)
		if !ok || redirect.Hdoc == nil || len(redirect.Hdoc.Parts) == 0 {
			return true
		}
		start, end := int(redirect.Hdoc.Pos().Offset()), int(redirect.Hdoc.End().Offset())
		delimiter := content[redirect.Word.Pos().Offset():redirect.Word.End().Offset()]
		regions = append(regions, shellHeredocRegion{
			start: start, end: end, quoted: !mayContainInlineExpression(delimiter) && strings.ContainsAny(delimiter, "'\"\\"),
		})
		// Nested substitutions belong to the containing unquoted body.
		return false
	})
	slices.SortFunc(regions, func(a, b shellHeredocRegion) int { return a.start - b.start })
	return regions, true
}

// transformShellContent preserves quoted heredoc bodies, processes unquoted
// bodies without shell-comment filtering, and filters comments only in commands.
func transformShellContent(content string, shell, heredoc func(string) string, skipQuoted bool) string {
	regions, parsed := shellHeredocRegions(content)
	if !parsed {
		// Invalid shell syntax must not hide expressions behind apparent comments.
		return heredoc(content)
	}
	var result strings.Builder
	pos := 0
	for _, region := range regions {
		result.WriteString(shell(content[pos:region.start]))
		switch {
		case region.quoted && skipQuoted:
			result.WriteByte('\n')
		case region.quoted:
			result.WriteString(content[region.start:region.end])
		default:
			result.WriteString(heredoc(content[region.start:region.end]))
		}
		pos = region.end
	}
	result.WriteString(shell(content[pos:]))
	return result.String()
}

// removeHeredocContent removes only quoted heredoc bodies. Unquoted bodies
// undergo command substitution and must remain visible to injection checks.
func removeHeredocContent(content string) string {
	return transformShellContent(content, func(s string) string { return s }, func(s string) string { return s }, true)
}

func executableShellContent(content string) string {
	return transformShellContent(content, stripShellLineComments, func(s string) string { return s }, true)
}

// stripShellLineComments removes bash-style # line comments while preserving text
// inside single/double quotes and escaped # characters.
func stripShellLineComments(content string) string {
	var out strings.Builder
	out.Grow(len(content))

	inSingleQuote := false
	inDoubleQuote := false
	escaped := false

	for i := 0; i < len(content); i++ {
		ch := content[i]

		// Preserve newlines and reset escape state across lines.
		if ch == '\n' {
			out.WriteByte(ch)
			escaped = false
			continue
		}

		if escaped {
			out.WriteByte(ch)
			escaped = false
			continue
		}

		if ch == '\\' && !inSingleQuote {
			out.WriteByte(ch)
			escaped = true
			continue
		}

		if ch == '\'' && !inDoubleQuote {
			inSingleQuote = !inSingleQuote
			out.WriteByte(ch)
			continue
		}

		if ch == '"' && !inSingleQuote {
			inDoubleQuote = !inDoubleQuote
			out.WriteByte(ch)
			continue
		}

		if ch == '#' && !inSingleQuote && !inDoubleQuote && isShellCommentStart(content, i) {
			for i+1 < len(content) && content[i+1] != '\n' {
				i++
			}
			continue
		}

		out.WriteByte(ch)
	}

	return out.String()
}

func isShellCommentStart(content string, index int) bool {
	if index == 0 {
		return true
	}
	prev := content[index-1]
	return prev == ' ' || prev == '\t' || prev == '\n' || prev == '\r' || prev == ';'
}

// replaceOutsideQuotedHeredocs replaces all occurrences of old with new in s,
// skipping content inside quoted heredoc blocks (e.g. << 'EOF' ... EOF).
//
// Quoted heredocs suppress variable expansion in the shell, so a replacement
// like "$GH_AW_VAR" would be written literally to the output file rather than
// being expanded.  Unquoted heredoc content and regular script lines are
// processed normally.
//
// When there are no quoted heredocs the function is equivalent to
// strings.ReplaceAll(s, old, new).
func replaceOutsideQuotedHeredocs(s, old, new string) string {
	return transformShellContent(s,
		func(segment string) string { return replaceOutsideShellLineComments(segment, old, new) },
		func(segment string) string { return strings.ReplaceAll(segment, old, new) }, false)
}

// replaceOutsideShellLineComments replaces old with new in shell script content
// while preserving text inside bash-style # line comments.
func replaceOutsideShellLineComments(content, old, new string) string {
	var result strings.Builder
	result.Grow(len(content))

	inSingleQuote := false
	inDoubleQuote := false
	escaped := false
	segmentStart := 0

	for i := 0; i < len(content); {
		ch := content[i]

		if ch == '\n' {
			escaped = false
			i++
			continue
		}

		if escaped {
			escaped = false
			i++
			continue
		}

		if ch == '\\' && !inSingleQuote {
			escaped = true
			i++
			continue
		}

		if ch == '\'' && !inDoubleQuote {
			inSingleQuote = !inSingleQuote
			i++
			continue
		}

		if ch == '"' && !inSingleQuote {
			inDoubleQuote = !inDoubleQuote
			i++
			continue
		}

		if ch == '#' && !inSingleQuote && !inDoubleQuote && isShellCommentStart(content, i) {
			result.WriteString(strings.ReplaceAll(content[segmentStart:i], old, new))

			commentEnd := i
			for commentEnd < len(content) && content[commentEnd] != '\n' {
				commentEnd++
			}
			result.WriteString(content[i:commentEnd])
			if commentEnd < len(content) {
				result.WriteByte('\n')
				commentEnd++
			}

			escaped = false
			segmentStart = commentEnd
			i = commentEnd
			continue
		}

		i++
	}

	result.WriteString(strings.ReplaceAll(content[segmentStart:], old, new))
	return result.String()
}

// TemplateInjectionViolation represents a detected template injection risk
type TemplateInjectionViolation struct {
	Expression string // The unsafe expression (e.g., "${{ github.event.issue.title }}")
	Snippet    string // Code snippet showing the violation context
	Context    string // Expression context (e.g., "github.event", "steps.*.outputs")
}

// extractRunSnippet extracts a relevant snippet from the run block containing the expression
func extractRunSnippet(runContent string, expression string) string {
	lines := strings.SplitSeq(runContent, "\n")

	for line := range lines {
		if strings.Contains(line, expression) {
			// Return the trimmed line containing the expression
			trimmed := strings.TrimSpace(line)
			// Limit snippet length to avoid overwhelming error messages
			return stringutil.Truncate(trimmed, 100)
		}
	}

	// Fallback: return the expression itself
	return expression
}

// detectExpressionContext identifies what type of expression this is
func detectExpressionContext(expression string) string {
	templateInjectionValidationLog.Printf("Detecting expression context for: %s", expression)
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

	return errors.New(builder.String())
}

// formatRunScriptExpressionGuardrailError formats a compiler-regression error for any
// GitHub Actions expressions that remain in run: shell scripts of compiled workflows.
func formatRunScriptExpressionGuardrailError(violations []TemplateInjectionViolation) error {
	var builder strings.Builder

	builder.WriteString("compiler regression detected: GitHub Actions expressions found in run: shell scripts of compiled workflow\n\n")
	builder.WriteString("The compiler must rewrite expressions in run: blocks to env variables.\n")
	builder.WriteString("Use env: assignments and shell variable references instead of inline ${{ ... }} in run: scripts.\n\n")
	builder.WriteString("Examples found:\n")

	maxExamples := min(5, len(violations))
	for i := range maxExamples {
		fmt.Fprintf(&builder, "  - %s\n", violations[i].Expression)
		fmt.Fprintf(&builder, "    in: %s\n", violations[i].Snippet)
	}
	if len(violations) > maxExamples {
		fmt.Fprintf(&builder, "  ... and %d more\n", len(violations)-maxExamples)
	}

	builder.WriteString("\nSafe pattern:\n")
	builder.WriteString("  env:\n")
	builder.WriteString("    EXPR_VALUE: ${{ github.token }}\n")
	builder.WriteString("  run: |\n")
	builder.WriteString("    echo \"$EXPR_VALUE\"\n")

	return errors.New(builder.String())
}
