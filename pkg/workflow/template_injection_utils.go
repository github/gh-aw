package workflow

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/stringutil"
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

// heredocPattern holds pre-compiled regexp patterns for a single heredoc delimiter suffix.
type heredocPattern struct {
	quoted   *regexp.Regexp
	unquoted *regexp.Regexp
}

// heredocPatterns are compiled once at program start for performance.
// Each entry covers one of the common delimiter suffixes used by heredocs in shell scripts.
// Since Go regex doesn't support backreferences, we match common heredoc delimiter suffixes explicitly.
// Matches both exact delimiters (EOF) and prefixed delimiters (GH_AW_SAFE_OUTPUTS_CONFIG_EOF).
var heredocPatterns = func() []heredocPattern {
	suffixes := []string{"EOF", "EOL", "END", "HEREDOC", "JSON", "YAML", "SQL"}
	patterns := make([]heredocPattern, 0, len(suffixes))
	for _, suffix := range suffixes {
		// Pattern for quoted delimiter ending with suffix: << 'PREFIX_SUFFIX' or << "PREFIX_SUFFIX"
		// \w* matches zero or more word characters (allowing both exact match and prefixes)
		// (?ms) enables multiline and dotall modes, .*? is non-greedy
		// \s*\w*%s\s*$ allows for leading/trailing whitespace on the closing delimiter
		//nolint:regexpdynamicpattern // Suffixes are fixed internal values and compilation failures are skipped.
		quoted, quotedErr := regexp.Compile(fmt.Sprintf(`(?ms)<<\s*['"]\w*%s['"].*?\n\s*\w*%s\s*$`, suffix, suffix))
		//nolint:regexpdynamicpattern // Suffixes are fixed internal values and compilation failures are skipped.
		unquoted, unquotedErr := regexp.Compile(fmt.Sprintf(`(?ms)<<\s*\w*%s.*?\n\s*\w*%s\s*$`, suffix, suffix))
		if quotedErr == nil && unquotedErr == nil {
			patterns = append(patterns, heredocPattern{quoted: quoted, unquoted: unquoted})
		}
	}
	return patterns
}()

// removeHeredocContent removes heredoc sections from shell commands.
// Heredocs (e.g., cat > file << 'EOF' ... EOF) are safe for template expressions
// because the content is written to files, not executed in the shell.
func removeHeredocContent(content string) string {
	templateInjectionValidationLog.Printf("Removing heredoc content from shell command: input_size=%d bytes", len(content))
	result := content
	for _, p := range heredocPatterns {
		result = p.quoted.ReplaceAllString(result, "# heredoc removed")
		result = p.unquoted.ReplaceAllString(result, "# heredoc removed")
	}
	if len(result) != len(content) {
		templateInjectionValidationLog.Printf("Heredoc content removed: output_size=%d bytes (reduced by %d bytes)", len(result), len(content)-len(result))
	}
	return result
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
// Assumption: heredoc regions do not overlap.  Shell syntax does not allow
// nested or overlapping heredocs, so this is always true for valid scripts.
// Malformed scripts with overlapping heredoc delimiters may not be handled
// correctly, but the compiler will surface any such errors during later YAML
// schema validation.
//
// When there are no quoted heredocs the function is equivalent to
// strings.ReplaceAll(s, old, new).
func replaceOutsideQuotedHeredocs(s, old, new string) string {
	type region struct{ start, end int }

	// Collect all quoted-heredoc intervals.
	var quotedRegions []region
	for _, p := range heredocPatterns {
		for _, loc := range p.quoted.FindAllStringIndex(s, -1) {
			quotedRegions = append(quotedRegions, region{loc[0], loc[1]})
		}
	}

	if len(quotedRegions) == 0 {
		return replaceOutsideShellLineComments(s, old, new)
	}

	templateInjectionValidationLog.Printf("Replacing outside %d quoted heredoc region(s): replacing %q with %q", len(quotedRegions), old, new)

	// Sort regions by start position so we can walk left-to-right.
	slices.SortFunc(quotedRegions, func(a, b region) int {
		switch {
		case a.start < b.start:
			return -1
		case a.start > b.start:
			return 1
		default:
			return 0
		}
	})

	var result strings.Builder
	pos := 0
	for _, r := range quotedRegions {
		// Replace in the non-heredoc segment before this region.
		if pos < r.start {
			result.WriteString(replaceOutsideShellLineComments(s[pos:r.start], old, new))
		}
		// Write the quoted-heredoc region verbatim.
		result.WriteString(s[r.start:r.end])
		pos = r.end
	}
	// Replace in the trailing non-heredoc segment.
	if pos < len(s) {
		result.WriteString(replaceOutsideShellLineComments(s[pos:], old, new))
	}
	return result.String()
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

			commentEnd := writeShellLineComment(&result, content, i)

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

func writeShellLineComment(result *strings.Builder, content string, start int) int {
	commentEnd := start
	for commentEnd < len(content) && content[commentEnd] != '\n' {
		commentEnd++
	}
	result.WriteString(content[start:commentEnd])
	if commentEnd < len(content) {
		result.WriteByte('\n')
		commentEnd++
	}
	return commentEnd
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
