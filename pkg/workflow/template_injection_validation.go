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

func runScriptHeredocExpressionSnippets(source string) (map[string]string, bool) {
	regions, parsed := shellHeredocRegions(source, false)
	if !parsed {
		return nil, false
	}
	snippets := make(map[string]string)
	for _, region := range regions {
		if !region.quoted || region.start < 0 || region.end < region.start || region.end > len(source) {
			continue
		}
		body := source[region.start:region.end]
		for _, expression := range runScriptExpressions(body) {
			if _, exists := snippets[expression]; !exists {
				snippets[expression] = extractRunSnippet(body, expression)
			}
		}
	}
	return snippets, true
}

func isRunScriptEnvExpression(source string) bool {
	expression, ok := actionslanguageservice.WrappedExpression(source)
	if !ok {
		return false
	}
	found := false
	actionslanguageservice.Walk(expression, func(node *actionslanguageservice.Expression) bool {
		path := runScriptAccessPath(node)
		first, ok := runScriptAccessPathSegment(path, 0)
		if ok && strings.EqualFold(first, "env") {
			found = true
			return false
		}
		return !found
	})
	return found
}

func unsafeRunScriptExpressionContextInScript(expression string, quotedHeredocSnippets map[string]string, boundariesProven bool) string {
	if context := unsafeRunScriptExpressionContext(expression); context != "" {
		return context
	}
	if isRunScriptEnvExpression(expression) {
		if !boundariesProven {
			return "unproven heredoc boundary"
		}
		if _, quoted := quotedHeredocSnippets[expression]; quoted {
			return "quoted heredoc environment"
		}
	}
	return ""
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

func runScriptAccessPathSegment(path []string, index int) (string, bool) {
	if index < 0 || index >= len(path) {
		return "", false
	}
	return path[index], true //nolint:uncheckedsliceindex // index is checked against len(path) above.
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
		first, ok := runScriptAccessPathSegment(path, 0)
		if !ok {
			return true
		}
		switch {
		case strings.EqualFold(first, "inputs"):
			context = "workflow inputs"
		case strings.EqualFold(first, "github"):
			event, exists := runScriptAccessPathSegment(path, 1)
			if exists && (strings.EqualFold(event, "event") || event == "*") {
				context = "github.event"
			}
		case strings.EqualFold(first, "steps"):
			outputs, exists := runScriptAccessPathSegment(path, 2)
			if len(path) >= 4 && exists && (strings.EqualFold(outputs, "outputs") || outputs == "*") {
				context = "steps.*.outputs"
			}
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
	if len(loc) < 2 {
		return "", false
	}
	return strings.TrimSpace(keyPart[loc[1]:]), true //nolint:uncheckedsliceindex // FindStringIndex returns a pair; checked above.
}

func appendRunBlockLine(block *strings.Builder, line string, indent int, contentIndent *int) {
	if *contentIndent == 0 {
		*contentIndent = indent
	}
	block.WriteString(line[min(*contentIndent, indent):])
	block.WriteByte('\n')
}

func startRunBlock(indent int, inRunBlock *bool, runBlockIndent, contentIndent *int) {
	*inRunBlock = true
	*runBlockIndent = indent
	*contentIndent = 0
}

// walkRunBlocks scans raw YAML text and visits each inline run value or complete
// multiline run block. It recognizes plain run: keys as well as quoted and flow-style
// forms so Path B stays aligned with the parsed-YAML validators.
//
// YAML indentation is removed before expressions are scanned. Shell comments and
// heredoc bodies are included because GitHub evaluates expressions before Bash.
func walkRunBlocks(yamlContent string, visit func(runContent string) bool) bool {
	inRunBlock := false
	runBlockIndent := 0
	contentIndent := 0
	var block strings.Builder
	flush := func() bool {
		content := block.String()
		block.Reset()
		return content != "" && visit(content)
	}
	for line := range strings.SplitSeq(yamlContent, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if trimmed == "" {
			if inRunBlock {
				block.WriteByte('\n')
			}
			continue
		}
		indent := len(line) - len(trimmed)
		if inRunBlock {
			if indent <= runBlockIndent {
				if flush() {
					return true
				}
				inRunBlock = false
				// Fall through: check whether this line starts a new run: block.
			} else {
				appendRunBlockLine(&block, line, indent, &contentIndent)
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
		if rest == "" {
			startRunBlock(indent, &inRunBlock, &runBlockIndent, &contentIndent)
			continue
		}
		if rest[0] == '|' || rest[0] == '>' { //nolint:uncheckedsliceindex // rest is non-empty above.
			startRunBlock(indent, &inRunBlock, &runBlockIndent, &contentIndent)
			continue
		}
		if visit(rest) {
			return true
		}
	}

	return inRunBlock && flush()
}

// scanRunContentExpressions performs a single pass over run: blocks to detect both
// user-controlled expressions and any non-allowlisted expressions. This avoids
// the duplicate YAML walk used by the skipValidation fast path.
func scanRunContentExpressions(yamlContent string) runContentExpressionScan {
	if !mayContainInlineExpression(yamlContent) {
		return runContentExpressionScan{}
	}

	var scan runContentExpressionScan
	walkRunBlocks(yamlContent, func(runContent string) bool {
		if !mayContainInlineExpression(runContent) {
			return false
		}

		quotedHeredocSnippets, boundariesProven := runScriptHeredocExpressionSnippets(runContent)
		for _, expr := range runScriptExpressions(runContent) {
			context := unsafeRunScriptExpressionContextInScript(expr, quotedHeredocSnippets, boundariesProven)
			if !scan.hasUnsafe && context != "" {
				scan.hasUnsafe = true
			}
			if !scan.hasDisallowed && (context != "" || !allowedRunScriptExpressionRegex.MatchString(expr)) {
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

		expressions := runScriptExpressions(runContent)
		quotedHeredocSnippets, boundariesProven := runScriptHeredocExpressionSnippets(runContent)
		shellCodeEvaluation := runStepUsesShellCodeEvaluation(runContent)

		// Check each expression for unsafe contexts
		for _, expr := range expressions {
			context := unsafeRunScriptExpressionContextInScript(expr, quotedHeredocSnippets, boundariesProven)
			if context == "" && shellCodeEvaluation {
				context = "shell code evaluation"
			}
			if context != "" {
				// Found an unsafe pattern - extract a snippet for context
				snippet := extractRunSnippet(runContent, expr)
				if quotedSnippet, quoted := quotedHeredocSnippets[expr]; quoted {
					snippet = quotedSnippet
				}
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
// This raw-content guardrail rejects prohibited expressions left in run: scripts,
// including comments and quoted heredocs that the sanitizer preserves.
func validateNoGitHubExpressionsInRunScriptsFromParsed(workflow map[string]any) error {
	runBlocks := extractRunBlocks(workflow)
	templateInjectionValidationLog.Printf("Found %d run blocks to scan for raw expressions", len(runBlocks))

	var violations []TemplateInjectionViolation

	for _, runContent := range runBlocks {
		if !mayContainInlineExpression(runContent) {
			continue
		}

		expressions := runScriptExpressions(runContent)
		quotedHeredocSnippets, boundariesProven := runScriptHeredocExpressionSnippets(runContent)
		shellCodeEvaluation := runStepUsesShellCodeEvaluation(runContent)
		for _, expr := range expressions {
			_, hasQuotedSnippet := quotedHeredocSnippets[expr]
			quotedEnv := isRunScriptEnvExpression(expr) &&
				(!boundariesProven || hasQuotedSnippet)
			if _, valid := actionslanguageservice.WrappedExpression(expr); valid &&
				allowedRunScriptExpressionRegex.MatchString(expr) && !quotedEnv && !shellCodeEvaluation {
				continue
			}
			snippet := extractRunSnippet(runContent, expr)
			context := detectExpressionContext(expr)
			if shellCodeEvaluation {
				context = "shell code evaluation"
			} else if quotedEnv {
				context = "quoted heredoc environment"
				if quotedSnippet, quoted := quotedHeredocSnippets[expr]; quoted {
					snippet = quotedSnippet
				}
			} else if quotedSnippet, quoted := quotedHeredocSnippets[expr]; quoted {
				snippet = quotedSnippet
				context = "quoted heredoc"
			} else if !boundariesProven && isRunScriptEnvExpression(expr) {
				context = "unproven heredoc boundary"
			}
			violations = append(violations, TemplateInjectionViolation{
				Expression: expr,
				Snippet:    snippet,
				Context:    context,
			})
		}
	}

	if len(violations) > 0 {
		templateInjectionValidationLog.Printf("Run-script expression guardrail failed: %d violation(s) found", len(violations))
		return formatRunScriptExpressionValidationError(violations)
	}

	return nil
}
