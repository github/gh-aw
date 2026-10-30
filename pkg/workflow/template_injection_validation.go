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
//   - validateNoTemplateInjectionFromParsed() - Validates parsed YAML for template injection risks
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
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow/actionslanguageservice"
)

var templateInjectionValidationLog = logger.New("workflow:template_injection_validation")

// Pre-compiled regex patterns for template injection detection
var (
	// allowedRunScriptExpressionRegex matches trusted compiler-owned expressions that are
	// intentionally rendered in generated run scripts and are not user-controlled.
	allowedRunScriptExpressionRegex = regexp.MustCompile(`^\$\{\{\s*(env\.[^}]+|vars\.[^}]+|runner\.[^}]+|github\.(repository|run_id|workspace)|steps\.parse-guard-vars\.outputs\.(approval_labels|blocked_users|trusted_users)|job\.services\[[^]]+\]\.ports\[[^]]+\])\s*\}\}$`)
	runKeyPattern                   = regexp.MustCompile(`(?:^|[\s{,])(?:run|["']run["']):`)
)

// runContentExpressionScan captures the two signals needed by the skip-validation
// template-injection fast path before deciding whether to fall back to YAML parsing.
type runContentExpressionScan struct {
	hasUnsafe     bool
	hasDisallowed bool
}

// Include incomplete wrappers so malformed executable expressions fail closed.
func mayContainInlineExpression(s string) bool {
	return strings.Contains(s, "${{")
}

func runScriptExpressions(source string) []string {
	spans, err := actionslanguageservice.ExtractExpressions(source)
	var expressions []string
	last := 0
	for _, span := range spans {
		expressions = append(expressions, source[span.Start:span.End])
		last = span.End
	}
	if err != nil {
		if start := strings.Index(source[last:], "${{"); start >= 0 {
			expressions = append(expressions, source[last+start:])
		}
	}
	return expressions
}

// Unknown computed selectors are wildcards for this deny policy, not trusted
// static references. In particular, steps[vars.step].outputs.* is user data.
func runScriptAccessPath(expression *actionslanguageservice.Expression) []string {
	switch expression.Kind {
	case "group":
		return runScriptAccessPath(expression.Child(0))
	case "context":
		return []string{expression.Token.Text}
	case "index":
		path := runScriptAccessPath(expression.Child(0))
		if path == nil {
			return nil
		}
		index := expression.Child(1)
		if index.Kind == "literal" && (index.Token.Kind == "identifier" || index.Token.Kind == "string") {
			if value, ok := index.Token.Value.(string); ok {
				return append(path, value)
			}
		}
		return append(path, "*")
	default:
		return nil
	}
}

func unsafeRunScriptExpressionContext(source string) string {
	expression, ok := actionslanguageservice.WrappedExpression(source)
	if !ok {
		return "invalid expression"
	}
	context := ""
	actionslanguageservice.Walk(expression, func(node *actionslanguageservice.Expression) bool {
		if context != "" {
			return false
		}
		path := runScriptAccessPath(node)
		switch {
		case len(path) >= 1 && strings.EqualFold(path[0], "inputs"):
			context = "workflow inputs"
		case len(path) >= 2 && strings.EqualFold(path[0], "github") && (strings.EqualFold(path[1], "event") || path[1] == "*"):
			context = "github.event"
		case len(path) >= 4 && strings.EqualFold(path[0], "steps") && (strings.EqualFold(path[2], "outputs") || path[2] == "*"):
			context = "steps.*.outputs"
		}
		return context == ""
	})
	return context
}

func findRunValue(keyPart string) (string, bool) {
	// Fast pre-check: every regex alternative embeds "run:" (unquoted),
	// "run\":" (double-quoted close), or "run':" (single-quoted close), so
	// skip the regex entirely when none of those substrings is present.
	if !strings.Contains(keyPart, "run:") &&
		!strings.Contains(keyPart, `run":`) &&
		!strings.Contains(keyPart, "run':") {
		return "", false
	}
	loc := runKeyPattern.FindStringIndex(keyPart)
	if loc == nil {
		return "", false
	}
	return strings.TrimSpace(keyPart[loc[1]:]), true
}

// detectHeredocDelimiter extracts the heredoc closing delimiter from a line that
// opens a heredoc (contains "<<"). It handles the common forms used in compiler-
// generated run blocks:
//
//   - Unquoted:       cat << DELIM ...    → "DELIM"
//   - Strip-tab:      cat <<- DELIM ...   → "DELIM"
//   - Single-quoted:  cat << 'DELIM' ...  → "DELIM"
//   - Double-quoted:  cat << "DELIM" ...  → "DELIM"
//
// Returns ("", false) when no heredoc opening is found on the line, including
// for bash here-strings (<<<) and arithmetic bitshifts (1 << 2).
func detectHeredocDelimiter(trimmed string) (string, bool) {
	_, after, found := strings.Cut(trimmed, "<<")
	if !found {
		return "", false
	}
	// Reject bash here-strings (<<<): the character immediately after << is another <.
	if after != "" && after[0] == '<' {
		return "", false
	}
	// Handle <<- (strip-tab variant): the dash must immediately follow << with no
	// intervening whitespace. Strip exactly one dash; <<-- and similar are not valid
	// shell and are treated conservatively as non-heredoc.
	if after != "" && after[0] == '-' {
		after = after[1:]
	}
	rest := strings.TrimSpace(after)
	if rest == "" {
		return "", false
	}
	// Quoted delimiter: << 'DELIM' or << "DELIM"
	if rest[0] == '\'' || rest[0] == '"' {
		quoteChar := rest[0]
		end := strings.IndexByte(rest[1:], quoteChar)
		if end >= 0 {
			delim := rest[1 : end+1]
			if delim != "" {
				return delim, true
			}
		}
		return "", false
	}
	// Unquoted delimiter: take the first whitespace-delimited token.
	// Require the delimiter to be a valid shell identifier (letters, digits,
	// underscores; must start with a letter or underscore) to avoid treating
	// arithmetic bitshifts (e.g. "1 << 2") as heredoc openings.
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return "", false
	}
	delim := strings.TrimRight(fields[0], "|;&")
	if !isShellIdentifier(delim) {
		return "", false
	}
	return delim, true
}

// isShellIdentifier reports whether s is a valid shell identifier (letters,
// digits, and underscores; must not start with a digit). Heredoc delimiters
// must match this pattern; non-matching tokens indicate non-heredoc uses of <<
// such as arithmetic bitshifts.
func isShellIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, c := range s {
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c == '_':
			// always valid
		case c >= '0' && c <= '9':
			if i == 0 {
				return false // identifiers must not start with a digit
			}
		default:
			return false
		}
	}
	return true
}

// walkRunBlockLines scans raw YAML text and visits each inline run value or line inside a
// multiline run block. It recognizes plain run: keys as well as quoted and flow-style forms
// so Path B stays aligned with the parsed-YAML validators.
//
// Heredoc content is skipped so that expressions embedded inside heredocs (which are written
// to files and never executed directly by the shell) do not trigger false positives in the
// caller's expression checks. This matches the behaviour of removeHeredocContent, which
// validateNoGitHubExpressionsInRunScriptsFromParsed uses on the parsed path.
func walkRunBlockLines(yamlContent string, visit func(line string) bool) bool {
	inRunBlock := false
	runBlockIndent := 0
	inHeredoc := false
	heredocDelimiter := ""

	for line := range strings.SplitSeq(yamlContent, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if trimmed == "" {
			continue
		}
		indent := len(line) - len(trimmed)

		if inRunBlock {
			if indent <= runBlockIndent {
				inRunBlock = false
				inHeredoc = false
				heredocDelimiter = ""
				// Fall through: check whether this line starts a new run: block.
			} else {
				// If we are inside a heredoc, look for the closing delimiter.
				if inHeredoc {
					if strings.TrimSpace(line) == heredocDelimiter {
						inHeredoc = false
						heredocDelimiter = ""
					}
					continue // always skip heredoc content
				}
				// Check whether this line opens a heredoc.
				if delim, ok := detectHeredocDelimiter(trimmed); ok {
					inHeredoc = true
					heredocDelimiter = delim
					// The opening line itself is not heredoc body; visit it so callers
					// can see the surrounding shell context, but do not recurse.
					if visit(line) {
						return true
					}
					continue
				}
				if visit(line) {
					return true
				}
				continue
			}
		}

		keyPart := trimmed
		if strings.HasPrefix(keyPart, "-") {
			keyPart = strings.TrimSpace(keyPart[1:])
		}

		rest, ok := findRunValue(keyPart)
		if !ok {
			continue
		}

		if rest == "" || rest[0] == '|' || rest[0] == '>' {
			inRunBlock = true
			runBlockIndent = indent
			continue
		}

		if visit(rest) {
			return true
		}
	}

	return false
}

// scanRunContentExpressions performs a single pass over run: blocks to detect both
// user-controlled expressions and any non-allowlisted expressions. This avoids
// the duplicate YAML walk used by the skipValidation fast path.
func scanRunContentExpressions(yamlContent string) runContentExpressionScan {
	if !mayContainInlineExpression(yamlContent) {
		return runContentExpressionScan{}
	}

	var scan runContentExpressionScan
	walkRunBlockLines(yamlContent, func(line string) bool {
		if !mayContainInlineExpression(line) {
			return false
		}

		for _, expr := range runScriptExpressions(line) {
			context := unsafeRunScriptExpressionContext(expr)
			if !scan.hasUnsafe && context != "" {
				scan.hasUnsafe = true
			}
			if !scan.hasDisallowed && (context == "invalid expression" || !allowedRunScriptExpressionRegex.MatchString(expr)) {
				scan.hasDisallowed = true
			}
			if scan.hasUnsafe && scan.hasDisallowed {
				return true
			}
		}
		return false
	})

	return scan
}

// validateNoTemplateInjectionFromParsed checks a pre-parsed workflow map for template
// injection vulnerabilities. It is called when the caller already holds a parsed
// representation of the compiled YAML, avoiding a redundant parse.
func validateNoTemplateInjectionFromParsed(workflow map[string]any) error {
	// Extract all run blocks from the workflow
	runBlocks := extractRunBlocks(workflow)
	templateInjectionValidationLog.Printf("Found %d run blocks to scan", len(runBlocks))

	var violations []TemplateInjectionViolation

	for _, runContent := range runBlocks {
		// Check if this run block contains inline expressions
		if !mayContainInlineExpression(runContent) {
			continue
		}

		// Remove non-executable regions from the run block to avoid false positives:
		//   - heredocs are written to files/stdin, not executed directly
		//   - bash # comments are ignored by the shell
		contentWithoutHeredocs := stripShellLineComments(removeHeredocContent(runContent))

		// Extract all inline expressions from this run block (excluding heredocs)
		expressions := runScriptExpressions(contentWithoutHeredocs)

		// Check each expression for unsafe contexts
		for _, expr := range expressions {
			if context := unsafeRunScriptExpressionContext(expr); context != "" {
				// Found an unsafe pattern - extract a snippet for context
				snippet := extractRunSnippet(contentWithoutHeredocs, expr)
				violations = append(violations, TemplateInjectionViolation{
					Expression: expr,
					Snippet:    snippet,
					Context:    context,
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

// validateNoGitHubExpressionsInRunScriptsFromParsed checks a pre-parsed workflow map
// for any GitHub Actions expression usage in run: scripts.
//
// This is a compiler regression guardrail: run: scripts in compiled lock files should
// never contain ${{ ... }} directly because the compiler must rewrite expressions into
// env: variables. It runs after validateNoTemplateInjectionFromParsed as a broader
// catch-all for any remaining expression contexts.
func validateNoGitHubExpressionsInRunScriptsFromParsed(workflow map[string]any) error {
	runBlocks := extractRunBlocks(workflow)
	templateInjectionValidationLog.Printf("Found %d run blocks to scan for raw expressions", len(runBlocks))

	var violations []TemplateInjectionViolation

	for _, runContent := range runBlocks {
		if !mayContainInlineExpression(runContent) {
			continue
		}

		// Align with template-injection validation by excluding non-executable regions:
		// heredoc bodies and bash # comments.
		contentWithoutHeredocs := stripShellLineComments(removeHeredocContent(runContent))
		expressions := runScriptExpressions(contentWithoutHeredocs)
		for _, expr := range expressions {
			if _, valid := actionslanguageservice.WrappedExpression(expr); valid && allowedRunScriptExpressionRegex.MatchString(expr) {
				continue
			}
			snippet := extractRunSnippet(contentWithoutHeredocs, expr)
			violations = append(violations, TemplateInjectionViolation{
				Expression: expr,
				Snippet:    snippet,
				Context:    detectExpressionContext(expr),
			})
		}
	}

	if len(violations) > 0 {
		templateInjectionValidationLog.Printf("Run-script expression guardrail failed: %d violation(s) found", len(violations))
		return formatRunScriptExpressionGuardrailError(violations)
	}

	return nil
}
