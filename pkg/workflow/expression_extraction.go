package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/importinpututil"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/setutil"
)

var expressionExtractionLog = logger.New("workflow:expression_extraction")

// Pre-compiled regexes for performance (avoid recompilation in hot paths)
var (
	awContextExpressionRegex = regexp.MustCompile(`github\.aw\.context\.([a-zA-Z0-9_]+)([^a-zA-Z0-9_-]|$)`)
)

// ExpressionMapping represents a mapping between a GitHub expression and its environment variable
type ExpressionMapping struct {
	Original string // The original ${{ ... }} expression
	EnvVar   string // The GH_AW_ prefixed environment variable name
	Content  string // The expression content without ${{ }}
}

// ExpressionExtractor extracts GitHub Actions expressions from markdown content
// and creates environment variable mappings for them
type ExpressionExtractor struct {
	mappings map[string]*ExpressionMapping // key is the original expression
	counter  int
}

// NewExpressionExtractor creates a new ExpressionExtractor
func NewExpressionExtractor() *ExpressionExtractor {
	return &ExpressionExtractor{
		mappings: make(map[string]*ExpressionMapping),
		counter:  0,
	}
}

// contentTransformer is a function that rewrites an expression's content string.
// It receives the current content and returns the (possibly) transformed content.
// If no transformation applies it returns the input unchanged.
type contentTransformer func(string) string

// defaultContentTransformers is the ordered pipeline of content transformations
// applied to every expression before it is mapped to an env var.
// Transformers are applied in sequence; the output of one becomes the input of
// the next. To extend the pipeline without modifying ExtractExpressions, append
// to this slice before calling NewExpressionExtractor.
var defaultContentTransformers = []contentTransformer{
	transformActivationOutputs,
	transformExperimentsExpression,
	transformAwContextExpression,
}

// applyContentTransformers runs content through each transformer in order,
// logging changes, and returns the fully-transformed content.
func applyContentTransformers(content string, transformers []contentTransformer) string {
	for _, t := range transformers {
		if transformed := t(content); transformed != content {
			expressionExtractionLog.Printf("Transformed expression: %s -> %s", content, transformed)
			content = transformed
		}
	}
	return content
}

// addSubExpressionMappings registers a synthetic ExpressionMapping for every
// qualifying terminal sub-expression inside a compound expression so that the
// runtime evaluator can resolve each operand via a deterministic GH_AW_* env var.
//
// For compound expressions (one that is not a simple identifier), the runtime
// evaluator (runtime_import.cjs evaluateExpression()) recurses on || / && operands
// and looks up "GH_AW_" + toUpperCase(expr.replace(/\./g, "_")) for each terminal.
// Without this method only the hash env var for the full compound expression is
// present in the step's env block, so individual operands always appear unresolved.
func (e *ExpressionExtractor) addSubExpressionMappings(content string) {
	if simpleIdentifierRegex.MatchString(content) {
		return
	}
	for _, subExpr := range extractTerminalSubExpressions(content) {
		syntheticOriginal := "${{ " + subExpr + " }}"
		if _, exists := e.mappings[syntheticOriginal]; !exists {
			// Sub-expressions are guaranteed to be simple identifiers by
			// extractTerminalSubExpressions, so generateEnvVarName produces a
			// deterministic pretty name (e.g. GH_AW_STEPS_SANITIZED_OUTPUTS_TEXT).
			e.mappings[syntheticOriginal] = &ExpressionMapping{
				Original: syntheticOriginal,
				EnvVar:   e.generateEnvVarName(subExpr),
				Content:  subExpr,
			}
		}
	}
}

// processMatch handles a single regex match from the expression extraction regex.
// It applies content transformations, emits any deprecation warnings, registers
// the primary mapping, and expands compound expressions into sub-expression
// mappings. It is a no-op for empty content or already-seen expressions.
func (e *ExpressionExtractor) processMatch(originalExpr, rawContent string) {
	content := strings.TrimSpace(rawContent)
	if content == "" {
		expressionExtractionLog.Printf("Skipping empty expression: %s", originalExpr)
		return
	}

	originalContent := content
	content = applyContentTransformers(content, defaultContentTransformers)

	// Skip if we've already seen this expression (also prevents duplicate deprecation warnings)
	if _, exists := e.mappings[originalExpr]; exists {
		return
	}

	// Emit deprecation warning once per unique deprecated activation-output expression
	if content != originalContent && strings.HasPrefix(content, "steps.sanitized.outputs.") {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(
			fmt.Sprintf("Deprecated expression ${{ %s }}: use ${{ %s }} instead.", originalContent, content),
		))
	}

	e.mappings[originalExpr] = &ExpressionMapping{
		Original: originalExpr,
		EnvVar:   e.generateEnvVarName(content),
		Content:  content,
	}

	e.addSubExpressionMappings(content)
}

// ExtractExpressions extracts all ${{ ... }} expressions from the markdown content
// and creates environment variable mappings for each unique expression.
func (e *ExpressionExtractor) ExtractExpressions(markdown string) ([]*ExpressionMapping, error) {
	expressionExtractionLog.Printf("Extracting expressions from markdown: content_length=%d", len(markdown))

	matches := ExpressionPattern.FindAllStringSubmatch(markdown, -1)
	expressionExtractionLog.Printf("Found %d expression matches", len(matches))

	for _, match := range matches {
		originalExpr, hasOriginal := regexSubmatchAt(match, 0)
		content, hasContent := regexSubmatchAt(match, 1)
		if !hasOriginal || !hasContent {
			continue
		}
		e.processMatch(originalExpr, content)
	}

	// Convert map to sorted slice for consistent ordering
	var result []*ExpressionMapping
	for _, mapping := range e.mappings {
		result = append(result, mapping)
	}

	// Sort by original expression for deterministic output
	slices.SortFunc(result, func(a, b *ExpressionMapping) int {
		switch {
		case a.Original < b.Original:
			return -1
		case a.Original > b.Original:
			return 1
		default:
			return 0
		}
	})

	expressionExtractionLog.Printf("Extracted %d unique expressions", len(result))

	return result, nil
}

// transformActivationOutputs transforms needs.activation.outputs.* expressions to steps.sanitized.outputs.*
// for backward compatibility with existing workflows.
//
// NEW WORKFLOWS should use steps.sanitized.outputs.* directly in their markdown.
//
// The function transforms these specific outputs:
//
//	needs.activation.outputs.text -> steps.sanitized.outputs.text
//	needs.activation.outputs.title -> steps.sanitized.outputs.title
//	needs.activation.outputs.body -> steps.sanitized.outputs.body
//
// Other activation outputs (e.g., comment_id, comment_repo) are not transformed.
//
// Parameters:
//   - expr: The expression content to transform (without ${{ }} wrapper)
//
// Returns:
//   - The transformed expression, or the original if no transformation applies
func transformActivationOutputs(expr string) string {
	// Define the activation outputs that should be transformed
	// These are the outputs generated by the sanitized step (formerly compute-text)
	activationOutputs := []string{"text", "title", "body"}

	for _, output := range activationOutputs {
		// Build the old and new expressions
		oldExpr := "needs.activation.outputs." + output
		newExpr := "steps.sanitized.outputs." + output

		// Use word boundary replacement to avoid partial matches
		// We need to ensure we're replacing complete tokens, not substrings
		// Check for word boundaries: start of string, space, or operator characters
		// This prevents transforming "needs.activation.outputs.text_custom" incorrectly

		// Start searching from the beginning
		searchStart := 0
		for {
			idx := strings.Index(expr[searchStart:], oldExpr)
			if idx == -1 {
				break
			}

			// Convert relative index to absolute index
			idx += searchStart

			// Check if this is a complete token (not part of a larger identifier)
			// Look at the character after the match (if any)
			endIdx := idx + len(oldExpr)
			if endIdx >= 0 && endIdx < len(expr) {
				nextChar := expr[endIdx]
				// If the next character is alphanumeric or underscore, this is a partial match
				if (nextChar >= 'a' && nextChar <= 'z') ||
					(nextChar >= 'A' && nextChar <= 'Z') ||
					(nextChar >= '0' && nextChar <= '9') ||
					nextChar == '_' {
					// This is a partial match like "needs.activation.outputs.text_custom"
					// Skip it and continue searching after this match
					searchStart = endIdx
					continue
				}
			}

			// This is a complete token - replace it
			expr = expr[:idx] + newExpr + expr[endIdx:]

			// Continue searching after the replacement
			searchStart = idx + len(newExpr)
		}
	}

	return expr
}

// experimentNameRegex matches experiments.<name> expressions where name is a simple identifier.
var experimentNameRegex = regexp.MustCompile(`^experiments\.([a-zA-Z_][a-zA-Z0-9_]*)$`)

// experimentComparisonRegex matches experiments.<name> followed by a comparison operator and
// a quoted string value, e.g. `experiments.prompt_style == "concise"` or
// `experiments.prompt_style !== "detailed"`. The value may be enclosed in double quotes or
// single quotes, with no embedded quotes of the same kind. It captures:
//   - group 1: the experiment name
//   - group 2: the remainder of the expression (operator + quoted value), verbatim
var experimentComparisonRegex = regexp.MustCompile(`^experiments\.([a-zA-Z_][a-zA-Z0-9_]*)([ \t]*(?:!==?|===?)[ \t]*(?:"[^"]*"|'[^']*')[ \t]*)$`)

// ExperimentEnvVarName returns the env-var name used for the given experiment.
// The name is uppercased; hyphens are converted to underscores; all other characters
// that are not A-Z, 0-9, or underscore are dropped (not replaced).
// Example: "feature1" → "GH_AW_EXPERIMENTS_FEATURE1"
// Example: "my-flag"  → "GH_AW_EXPERIMENTS_MY_FLAG"
func ExperimentEnvVarName(experimentName string) string {
	return "GH_AW_EXPERIMENTS_" + normalizeJobNameForEnvVar(experimentName)
}

// transformExperimentsExpression detects expressions of the form "experiments.<name>"
// (and the comparison form `experiments.<name> == "value"`) and rewrites them so that the
// placeholder substitution step reads the value from the pick_experiment step output.
//
// Simple form:     experiments.name          → steps.pick-experiment.outputs.name
// Comparison form: experiments.name == "v"  → steps.pick-experiment.outputs.name == 'v'
//
// Double quotes in the comparison value are converted to single quotes because GitHub
// Actions expression syntax only supports single-quoted string literals.
//
// This is used for ${{ experiments.name }} and ${{ experiments.name == "value" }} expressions
// that appear directly in the prompt body (mostly relevant in inline mode; in runtime-import
// mode the {{#if experiments.name == "value"}} conditional is handled by interpolate_prompt.cjs
// step 2.5 which substitutes the variant value directly inside the condition tag).
//
// Without this transformation, the generated env var would contain an invalid GitHub Actions
// expression like `${{ experiments.name == "value" }}` where `experiments` is not a real
// context, causing the expression to always evaluate to false.
func transformExperimentsExpression(expr string) string {
	if m := experimentNameRegex.FindStringSubmatch(expr); m != nil {
		if name, ok := regexSubmatchAt(m, 1); ok {
			return "steps.pick-experiment.outputs." + name
		}
	}
	if m := experimentComparisonRegex.FindStringSubmatch(expr); m != nil {
		name, hasName := regexSubmatchAt(m, 1)
		remainder, hasRemainder := regexSubmatchAt(m, 2)
		if !hasName || !hasRemainder {
			return expr
		}
		// Convert double quotes to single quotes: GitHub Actions expressions only
		// support single-quoted string literals, not double-quoted ones.
		// This replacement is safe because experimentComparisonRegex guarantees
		// that quotes only appear as delimiters around the string literal value;
		// no embedded quotes of the same kind are allowed by the pattern.
		remainder = strings.ReplaceAll(remainder, `"`, `'`)
		return "steps.pick-experiment.outputs." + name + remainder
	}
	return expr
}

func regexSubmatchAt(matches []string, index int) (string, bool) {
	if index < 0 || index >= len(matches) {
		return "", false
	}
	return matches[index], true
}

// transformAwContextExpression rewrites github.aw.context.<field> references to
// parsed aw_context access expressions.
//
// These expressions only ever appear in prompt content rendered by the activation
// job's own steps (see generatePrompt), so they must read the normalized value from
// the step that produces it (steps.generate_aw_info.outputs.aw_context) rather than
// needs.activation.outputs.aw_context: a job cannot reference its own output via
// needs.<self>, and the activation job is the one that both emits and consumes this
// output.
//
// Example:
//
//	github.aw.context.item_number -> fromJSON(steps.generate_aw_info.outputs.aw_context || '{}').item_number
func transformAwContextExpression(expr string) string {
	return awContextExpressionRegex.ReplaceAllString(expr, "fromJSON(steps.generate_aw_info.outputs.aw_context || '{}').$1$2")
}

// simpleIdentifierRegex matches simple JavaScript property access chains like
// "github.event.issue.number" or "needs.activation.outputs.text"
// Each identifier must start with a letter or underscore, followed by alphanumeric or underscore
var simpleIdentifierRegex = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*(\.[a-zA-Z_][a-zA-Z0-9_]*)*$`)

// runtimeEvalEnvVarPrefixRegex matches the expression prefixes for which the runtime
// evaluator (runtime_import.cjs evaluateExpression) resolves values via deterministic
// GH_AW_* environment variable names (rather than via the GitHub context object).
// See the `if (trimmed.startsWith("needs.") || ...)` block in evaluateExpression().
var runtimeEvalEnvVarPrefixRegex = regexp.MustCompile(`^(?:needs|steps|inputs)\.`)

// isQualifyingSubExpression reports whether expr is a simple property-access chain
// (matching simpleIdentifierRegex) that starts with needs.*, steps.*, or inputs.*.
// These are the sub-expressions for which the runtime evaluator looks up a deterministic
// GH_AW_* environment variable name.
func isQualifyingSubExpression(expr string) bool {
	return expr != "" &&
		simpleIdentifierRegex.MatchString(expr) &&
		runtimeEvalEnvVarPrefixRegex.MatchString(expr)
}

// extractTerminalSubExpressions returns the simple-identifier sub-expressions from a
// compound expression (one containing `||` or `&&` operators, and optionally parentheses)
// that the runtime evaluator resolves via deterministic GH_AW_* environment variable names.
//
// It delegates parsing to the existing ParseExpression / VisitExpressionTree helpers in
// expression_parser.go so that all operator precedence, parenthesis grouping, and quoted
// string handling are handled consistently with the rest of the workflow expression system.
//
// Only leaf ExpressionNode values that:
//
//  1. are valid simple property-access chains (matching simpleIdentifierRegex), and
//  2. start with needs.*, steps.*, or inputs.*
//
// are returned. github.* sub-expressions are deliberately excluded because the runtime
// evaluator resolves them through the GitHub context object, not through env vars.
//
// Examples:
//
//	"steps.sanitized.outputs.text || inputs.command"
//	→ ["steps.sanitized.outputs.text", "inputs.command"]
//
//	"(steps.sanitized.outputs.text || inputs.command) && inputs.flag"
//	→ ["steps.sanitized.outputs.text", "inputs.command", "inputs.flag"]
//
//	"github.event.issue.number || inputs.item_number"
//	→ ["inputs.item_number"]
//
//	"steps.pick-experiment.outputs.name == 'concise'"
//	→ []  (hyphenated segment; not a simpleIdentifier)
func extractTerminalSubExpressions(content string) []string {
	tree, err := ParseExpression(content)
	if err != nil {
		// Unparseable expression (e.g. malformed input) — return empty safely.
		expressionExtractionLog.Printf("Could not parse expression %q for sub-expression extraction (skipping): %v", content, err)
		return nil
	}

	seen := make(map[string]struct {
	})
	var result []string
	_ = VisitExpressionTree(tree, func(node *ExpressionNode) error {
		expr := strings.TrimSpace(node.Expression)
		if isQualifyingSubExpression(expr) && !setutil.Contains(seen, expr) {
			seen[expr] = struct {
			}{}
			result = append(result, expr)
		}
		return nil
	})
	return result
}

// generateEnvVarName generates a unique environment variable name for an expression
// For simple JavaScript property access chains (e.g., "github.event.issue.number"),
// it generates a pretty name like "GH_AW_GITHUB_EVENT_ISSUE_NUMBER".
// For complex expressions, it falls back to a hash-based name.
func (e *ExpressionExtractor) generateEnvVarName(content string) string {
	// Check if the expression is a simple JavaScript property access chain
	if simpleIdentifierRegex.MatchString(content) {
		// Convert dots to underscores and uppercase
		prettyName := strings.ToUpper(strings.ReplaceAll(content, ".", "_"))
		return "GH_AW_" + prettyName
	}

	// Fall back to hash-based name for complex expressions
	// Use SHA256 hash to generate a unique identifier
	hash := sha256.Sum256([]byte(content))
	hashStr := hex.EncodeToString(hash[:])

	// Use first 8 characters of hash for brevity
	shortHash := hashStr[:8]

	// Create environment variable name
	return "GH_AW_EXPR_" + strings.ToUpper(shortHash)
}

// ReplaceExpressionsWithEnvVars replaces all ${{ ... }} expressions in the markdown
// with references to their corresponding environment variables using placeholder format
func (e *ExpressionExtractor) ReplaceExpressionsWithEnvVars(markdown string) string {
	expressionExtractionLog.Printf("Replacing expressions with env vars: mapping_count=%d", len(e.mappings))

	result := markdown

	// Sort mappings by length of original expression (longest first)
	// This ensures we replace longer expressions before shorter ones
	// to avoid partial replacements
	var mappings []*ExpressionMapping
	for _, mapping := range e.mappings {
		mappings = append(mappings, mapping)
	}
	slices.SortFunc(mappings, func(a, b *ExpressionMapping) int {
		switch {
		case len(a.Original) > len(b.Original):
			return -1
		case len(a.Original) < len(b.Original):
			return 1
		default:
			return 0
		}
	})

	// Replace each expression with its environment variable reference
	// Use __VAR__ placeholder format to prevent template injection
	for _, mapping := range mappings {
		placeholder := fmt.Sprintf("__%s__", mapping.EnvVar)
		result = strings.ReplaceAll(result, mapping.Original, placeholder)
	}

	return result
}

// applyWorkflowDispatchFallbacks enhances entity number expressions with an
// "|| inputs.item_number" fallback when the workflow has a workflow_dispatch
// trigger that includes the item_number input (generated by the label trigger
// shorthand). Without this fallback, manually dispatched runs receive an empty
// entity number because the event payload is absent.
//
// Only the three canonical entity number paths are patched:
//
//	github.event.pull_request.number → github.event.pull_request.number || inputs.item_number
//	github.event.issue.number        → github.event.issue.number        || inputs.item_number
//	github.event.discussion.number   → github.event.discussion.number   || inputs.item_number
//
// The EnvVar field is intentionally left unchanged so that callers that already
// hold a reference to an env-var name continue to work.
func applyWorkflowDispatchFallbacks(mappings []*ExpressionMapping, hasItemNumber bool) {
	if !hasItemNumber {
		return
	}

	fallbacks := map[string]string{
		"github.event.pull_request.number": "github.event.pull_request.number || inputs.item_number",
		"github.event.issue.number":        "github.event.issue.number || inputs.item_number",
		"github.event.discussion.number":   "github.event.discussion.number || inputs.item_number",
	}

	for _, mapping := range mappings {
		if enhanced, ok := fallbacks[mapping.Content]; ok {
			expressionExtractionLog.Printf("Applying workflow_dispatch fallback: %s -> %s", mapping.Content, enhanced)
			mapping.Content = enhanced
		}
	}
}

// SubstituteImportInputs replaces ${{ github.aw.inputs.<key> }} and
// ${{ github.aw.import-inputs.<key> }} expressions with the corresponding
// values from the importInputs map.
// This is called before expression extraction to inject import input values.
func SubstituteImportInputs(content string, importInputs map[string]any) string {
	if len(importInputs) == 0 {
		return content
	}

	expressionExtractionLog.Printf("Substituting import inputs: %d inputs available", len(importInputs))

	substituteFunc := func(regex *regexp.Regexp, inputCategory string) func(string) string {
		return func(match string) string {
			matches := regex.FindStringSubmatch(match)
			path, ok := regexSubmatchAt(matches, 1)
			if !ok {
				return match
			}
			// Resolve potentially dotted path (e.g. "config.apiKey" for object inputs)
			if value, found := resolveImportInputByPath(importInputs, path); found {
				strValue := marshalImportInputValue(value)
				expressionExtractionLog.Printf("Substituting github.aw.%s.%s with value: %s", inputCategory, path, strValue)
				return strValue
			}
			expressionExtractionLog.Printf("Import input path not found: %s", path)
			return match
		}
	}

	// Substitute ${{ github.aw.inputs.<key> }} (legacy form)
	result := AWInputsExpressionPattern.ReplaceAllStringFunc(content, substituteFunc(AWInputsExpressionPattern, "inputs"))
	// Substitute ${{ github.aw.import-inputs.<key> }} (import-schema form)
	result = AWImportInputsExpressionPattern.ReplaceAllStringFunc(result, substituteFunc(AWImportInputsExpressionPattern, "import-inputs"))

	return result
}

// marshalImportInputValue serializes an import input value to a string suitable for
// substitution into both YAML frontmatter and markdown prose.
// Arrays and maps are serialized as JSON (which is valid YAML inline syntax).
// Scalar values use Go's default string formatting.
//
// goccy/go-yaml may produce typed slices (e.g. []string) instead of []any, so
// a reflection fallback converts any slice kind to []any before JSON marshaling.
func marshalImportInputValue(value any) string {
	if formatted, ok := importinpututil.FormatResolvedValue(value); ok {
		return formatted
	}
	if value == nil {
		// Null import input — return empty string rather than panicking.
		return ""
	}
	return fmt.Sprintf("%v", value)
}

// resolveImportInputByPath resolves a potentially dotted key path from the importInputs map.
// For scalar inputs ("count"), it looks up importInputs["count"] directly.
// For object sub-key paths ("config.apiKey"), it looks up importInputs["config"]["apiKey"],
// supporting one level of nesting as defined by import-schema object types.
// Returns the resolved value and true on success, or nil and false when the path is not found.
func resolveImportInputByPath(importInputs map[string]any, path string) (any, bool) {
	return importinpututil.ResolvePathValue(importInputs, path)
}
