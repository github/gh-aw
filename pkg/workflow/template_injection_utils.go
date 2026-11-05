package workflow

import (
	"bytes"
	"errors"
	"slices"
	"strconv"
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

func maskShellActionsExpressions(content string) []byte {
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
	return masked
}

// shellHeredocRegions uses Bash syntax rather than delimiter heuristics. Masking
// Actions expressions preserves byte offsets while avoiding shell parse errors.
// A parse failure grants no heredoc exemption.
func shellHeredocRegions(content string, includeComments bool) ([]shellHeredocRegion, bool) {
	if !strings.Contains(content, "<<") {
		return nil, true
	}
	masked := maskShellActionsExpressions(content)
	file, err := syntax.NewParser(syntax.KeepComments(true)).Parse(bytes.NewReader(masked), "")
	if err != nil {
		templateInjectionValidationLog.Printf("Cannot identify safe heredoc regions; scanning entire script")
		return nil, false
	}
	var regions []shellHeredocRegion
	valid := true
	syntax.Walk(file, func(node syntax.Node) bool {
		if comment, ok := node.(*syntax.Comment); ok && includeComments {
			regions = append(regions, shellHeredocRegion{
				start: int(comment.Pos().Offset()), end: int(comment.End().Offset()),
			})
		}
		redirect, ok := node.(*syntax.Redirect)
		if !ok || redirect.Hdoc == nil || len(redirect.Hdoc.Parts) == 0 {
			return true
		}
		start, end := int(redirect.Hdoc.Pos().Offset()), int(redirect.Hdoc.End().Offset())
		delimiter := content[redirect.Word.Pos().Offset():redirect.Word.End().Offset()]
		quoted, delimiterValid := shellHeredocDelimiterQuoted(redirect.Word, content)
		valid = valid && delimiterValid
		if quoted && !mayContainInlineExpression(delimiter) {
			// mvdan can treat prefix-quoted delimiters as expanding, joining
			// body lines across continuations. Bash instead stops at the
			// first literal delimiter line. Never exempt an overlong body.
			if !quotedHeredocBoundaryMatches(content, start, end, redirect.Op == syntax.DashHdoc) {
				valid = false
			}
			regions = append(regions, shellHeredocRegion{start: start, end: end, quoted: true})
		}
		// Walk expanding bodies too: they may contain quoted inner heredocs.
		return true
	})
	slices.SortFunc(regions, func(a, b shellHeredocRegion) int {
		if a.start == b.start {
			return b.end - a.end
		}
		return a.start - b.start
	})
	return regions, valid
}

func shellHeredocDelimiterQuoted(word *syntax.Word, content string) (bool, bool) {
	quoted := false
	for _, part := range word.Parts {
		switch part := part.(type) {
		case *syntax.Lit:
			// The parser removes unquoted backslash-newline continuations.
			quoted = quoted || strings.Contains(part.Value, "\\")
		case *syntax.SglQuoted:
			quoted = true
			// Bash decodes ANSI-C escapes that mvdan may retain.
			if part.Dollar && strings.Contains(part.Value, "\\") {
				return true, false
			}
		case *syntax.DblQuoted:
			quoted = true
			// Double-quoted escapes can likewise disagree with Bash.
			if strings.Contains(content[part.Pos().Offset():part.End().Offset()], "\\") {
				return true, false
			}
		default:
			return false, false
		}
	}
	return quoted, true
}

func quotedHeredocBoundaryMatches(content string, start, end int, stripTabs bool) bool {
	if start < 0 || end < start || end > len(content) {
		return false
	}
	if end < len(content) && !strings.HasPrefix(content[end:], "\n") {
		return false
	}
	// Hdoc.End includes the closing delimiter, but not its newline.
	closeStart := strings.LastIndex(content[:end], "\n") + 1
	if closeStart < start {
		return false
	}
	stop := content[closeStart:end]
	if stripTabs {
		stop = strings.TrimLeft(stop, "\t")
	}
	for line := range strings.SplitSeq(strings.TrimSuffix(content[start:closeStart], "\n"), "\n") {
		if stripTabs {
			line = strings.TrimLeft(line, "\t")
		}
		if line == stop {
			return false
		}
	}
	return true
}

// transformShellContent applies transformations outside parser-identified
// quoted bodies and comments, retaining nested quoted bodies in expanding ones.
func transformShellContent(content string, transform func(string) string, skipQuoted, includeComments bool) string {
	regions, parsed := shellHeredocRegions(content, includeComments)
	if !parsed {
		// Scan everything, but do not rewrite bodies whose boundaries are unknown.
		// The unchanged expressions will be rejected by validation.
		return content
	}
	return transformShellRegions(content, regions, transform, skipQuoted)
}

func transformShellRegions(content string, regions []shellHeredocRegion, transform func(string) string, skipQuoted bool) string {
	var result strings.Builder
	pos := 0
	for _, region := range regions {
		if region.start < pos {
			continue // A containing protected region already covered this span.
		}
		result.WriteString(transform(content[pos:region.start]))
		if skipQuoted {
			result.WriteByte('\n')
		} else {
			result.WriteString(content[region.start:region.end])
		}
		pos = region.end
	}
	result.WriteString(transform(content[pos:]))
	return result.String()
}

// TransformExecutableShellContent shares the compiler's Bash heredoc and comment
// boundaries with source migrations. Unproven boundaries return the original
// script and an actionable, value-free error before invoking transform.
func TransformExecutableShellContent(content string, transform func(string) string) (string, error) {
	regions, parsed := shellHeredocRegions(content, true)
	if !parsed {
		return content, errors.New("cannot prove Bash heredoc boundaries; manually use a literal, simply quoted delimiter with an exact closing line, or move dynamic data into env bindings")
	}
	if !strings.Contains(content, "<<") {
		return transformOutsideShellLineComments(content, transform), nil
	}
	return transformShellRegions(content, regions, transform, false), nil
}

// shellExpansionContent excludes regions where Bash does not perform parameter
// expansion. It is for sanitizer decisions only; Actions expressions are
// substituted before Bash interprets these regions.
func shellExpansionContent(content string) string {
	if !strings.Contains(content, "<<") {
		return stripShellLineComments(content)
	}
	return transformShellContent(content, func(s string) string { return s }, true, true)
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
		ch := content[i] //nolint:uncheckedsliceindex // i is bounded by the loop condition.

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
			for i+1 < len(content) && content[i+1] != '\n' { //nolint:uncheckedsliceindex // i+1 is bounded by the loop condition.
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
	if index < 0 || index >= len(content) {
		return false
	}
	prev := content[index-1] //nolint:uncheckedsliceindex // index is checked above.
	return prev == ' ' || prev == '\t' || prev == '\n' || prev == '\r' || prev == ';'
}

func transformOutsideShellLineComments(content string, transform func(string) string) string {
	var result strings.Builder
	result.Grow(len(content))

	inSingleQuote := false
	inDoubleQuote := false
	escaped := false
	segmentStart := 0

	for i := 0; i < len(content); {
		ch := content[i] //nolint:uncheckedsliceindex // i is bounded by the loop condition.

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
			result.WriteString(transform(content[segmentStart:i]))

			commentEnd := i
			for commentEnd < len(content) && content[commentEnd] != '\n' { //nolint:uncheckedsliceindex // commentEnd is bounded by the loop condition.
				commentEnd++
			}
			result.WriteString(content[i:commentEnd])
			segmentStart = commentEnd
			i = commentEnd
			continue
		}

		i++
	}

	result.WriteString(transform(content[segmentStart:]))
	return result.String()
}

// TemplateInjectionViolation represents a detected template injection risk
type TemplateInjectionViolation struct {
	Expression string // The unsafe expression (e.g., "${{ github.event.issue.title }}")
	Snippet    string // Code snippet showing the violation context
	Context    string // Expression context (e.g., "github.event", "steps.*.outputs")
}

func writeQuotedHeredocMigrationGuidance(builder *strings.Builder, violations []TemplateInjectionViolation) {
	for _, violation := range violations {
		if strings.HasPrefix(violation.Context, "quoted heredoc") || violation.Context == "unproven heredoc boundary" {
			builder.WriteString("Quoted heredoc contents are still interpolated by GitHub Actions before Bash. Bind dynamic values in the step's env: block, then use an unquoted heredoc or printf '%s\\n' \"$VALUE\" instead.\n\n")
			return
		}
	}
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

	builder.WriteString("template injection: unsafe Actions expressions found in authored run: content\n")
	builder.WriteString("GitHub Actions interpolates expressions before Bash parses shell syntax, including comments and heredoc delimiters.\n")
	builder.WriteString("Remove expressions from comments. For heredoc values, move the expression to a step env: entry and use an unquoted heredoc so Bash can expand the shell variable.\n\n")
	builder.WriteString("Actions expands these expressions before shell execution; attacker-controlled values can become shell input in executable contexts:\n\n")

	// Group violations by context for clearer reporting
	contextGroups := make(map[string][]TemplateInjectionViolation)
	for _, v := range violations {
		contextGroups[v.Context] = append(contextGroups[v.Context], v)
	}

	// Report violations grouped by context
	for context, contextViolations := range contextGroups {
		builder.WriteString("  " + context + " context (" + strconv.Itoa(len(contextViolations)) + " occurrence(s)):\n")

		// Show up to 3 examples per context to keep error message manageable
		maxExamples := 3
		for i, v := range contextViolations {
			if i >= maxExamples {
				builder.WriteString("    ... and " + strconv.Itoa(len(contextViolations)-maxExamples) + " more\n")
				break
			}
			builder.WriteString("    - " + v.Expression + "\n")
			builder.WriteString("      in: " + v.Snippet + "\n")
		}
		builder.WriteString("\n")
	}

	writeQuotedHeredocMigrationGuidance(&builder, violations)

	builder.WriteString("Security Risk:\n")
	builder.WriteString("  Direct interpolation into executable shell syntax can let attacker-controlled text alter commands.\n")
	builder.WriteString("  Comments and quoted heredocs do not prevent Actions substitution; remove comment expressions and\n")
	builder.WriteString("  pass heredoc values through environment data.\n\n")

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

// formatRunScriptExpressionValidationError explains why Actions expressions are
// prohibited in raw run: content, including shell comments and quoted heredocs.
func formatRunScriptExpressionValidationError(violations []TemplateInjectionViolation) error {
	var builder strings.Builder

	builder.WriteString("template injection: GitHub Actions expressions are not allowed in authored run: content, including shell comments and quoted heredocs.\n")
	builder.WriteString("Actions interpolates ${{ ... }} before Bash parses shell syntax.\n\n")
	builder.WriteString("Remove expressions from comments. For heredoc values, move the expression to a step env: entry and use an unquoted heredoc so Bash can expand the shell variable.\n\n")
	builder.WriteString("Examples found:\n")

	maxExamples := min(5, len(violations))
	for i, violation := range violations {
		if i >= maxExamples {
			break
		}
		builder.WriteString("  - " + violation.Expression + "\n")
		builder.WriteString("    in: " + violation.Snippet + "\n")
	}
	if len(violations) > maxExamples {
		builder.WriteString("  ... and " + strconv.Itoa(len(violations)-maxExamples) + " more\n")
	}

	writeQuotedHeredocMigrationGuidance(&builder, violations)

	builder.WriteString("\nSafe pattern:\n")
	builder.WriteString("  env:\n")
	builder.WriteString("    EXPR_VALUE: ${{ github.event.issue.title }}\n")
	builder.WriteString("  run: |\n")
	builder.WriteString("    printf '%s\\n' \"$EXPR_VALUE\"\n")

	return errors.New(builder.String())
}
