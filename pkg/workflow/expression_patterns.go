// This file provides centralized regex patterns for GitHub Actions expression matching.
//
// # Expression Patterns
//
// This file consolidates regular expression patterns used across multiple validation and
// extraction files to provide a single source of truth for expression matching logic.
//
// # Available Pattern Categories
//
// ## Core Expression Patterns
//   - ExpressionPattern - Matches GitHub Actions expressions: ${{ ... }}
//   - ExpressionPatternDotAll - Matches expressions with dotall mode (multiline)
//
// ## Context Access Patterns
//   - NeedsStepsPattern - Matches needs.* and steps.* patterns
//   - InputsPattern - Matches github.event.inputs.* patterns
//   - WorkflowCallInputsPattern - Matches inputs.* patterns (workflow_call)
//   - AWInputsPattern - Matches github.aw.inputs.* patterns
//   - EnvPattern - Matches env.* patterns
//
// ## Secret Patterns
//   - SecretExpressionPattern - Matches ${{ secrets.SECRET_NAME }} expressions
//   - SecretsExpressionPattern - Validates secrets expression syntax
//
// ## Template Patterns
//   - InlineExpressionPattern - Matches inline expressions in templates
//   - UnsafeContextPattern - Matches potentially unsafe context patterns
//   - TemplateIfPattern - Matches {{#if ...}} template conditionals
//
// ## Utility Patterns
//   - ComparisonExtractionPattern - Extracts properties from comparison expressions
//   - StringLiteralPattern - Matches string literals ('...', "...", `...`)
//   - NumberLiteralPattern - Matches numeric literals
//   - RangePattern - Matches numeric ranges (e.g., "1-10")
//
// # Design Rationale
//
// Centralizing regex patterns provides several benefits:
//   - Single source of truth for expression matching logic
//   - Consistent behavior across validation and extraction
//   - Easier to maintain and update patterns
//   - Better performance through pre-compilation
//   - Reduced code duplication across files
//
// # Migration Notes
//
// This file consolidates patterns previously scattered across:
//   - expression_validation.go
//   - expression_extraction.go
//   - secret_extraction.go
//   - secrets_validation.go
//   - template.go
//   - template_injection_validation.go
//
// Files are gradually being migrated to use these centralized patterns.

package workflow

import (
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

// expressionPatternsLog is the debug logger for GitHub Actions expression detection.
// Enable with DEBUG=workflow:expression_patterns to trace which values are treated
// as dynamic ${{ ... }} expressions during validation and extraction.
var expressionPatternsLog = logger.New("workflow:expression_patterns")

// hasExpressionMarker reports whether s contains a GitHub Actions expression opening marker.
// This is a permissive check used in scenarios where partial expressions should be treated
// as dynamic values.
func hasExpressionMarker(s string) bool {
	return strings.Contains(s, "${{")
}

// containsExpression reports whether s contains a complete non-empty GitHub Actions expression.
// A complete expression has a "${{" marker that appears before a closing "}}" marker
// with at least one character between them.
func containsExpression(s string) bool {
	_, afterOpen, found := strings.Cut(s, "${{")
	if !found {
		return false
	}
	closeIdx := strings.Index(afterOpen, "}}")
	complete := closeIdx > 0
	if complete {
		expressionPatternsLog.Printf("containsExpression: complete expression detected (input length %d)", len(s))
	}
	return complete
}

// isExpression reports whether the entire string s is a GitHub Actions expression.
func isExpression(s string) bool {
	s = strings.TrimSpace(s)
	result := strings.HasPrefix(s, "${{") && strings.HasSuffix(s, "}}")
	if result {
		expressionPatternsLog.Printf("isExpression: entire value is an expression (length %d)", len(s))
	}
	return result
}

// Core Expression Patterns
var (
	// ExpressionPattern matches GitHub Actions expressions: ${{ ... }}
	// Uses non-greedy matching to handle nested braces properly
	ExpressionPattern = regexp.MustCompile(`\$\{\{(.*?)\}\}`)

	// ExpressionPatternDotAll matches expressions with dotall mode enabled
	// The (?s) flag enables dotall mode where . matches newlines
	ExpressionPatternDotAll = regexp.MustCompile(`(?s)\$\{\{(.*?)\}\}`)
)

// Context Access Patterns
var (
	// NeedsStepsPattern matches needs.* and steps.* context patterns
	// Example: needs.build.outputs.version, steps.setup.outputs.path
	NeedsStepsPattern = regexp.MustCompile(`^(needs|steps)\.[a-zA-Z0-9_-]+(\.[a-zA-Z0-9_-]+)*$`)

	// InputsPattern matches github.event.inputs.* patterns
	// Example: github.event.inputs.workflow_id
	InputsPattern = regexp.MustCompile(`^github\.event\.inputs\.[a-zA-Z0-9_-]+$`)

	// WorkflowCallInputsPattern matches inputs.* patterns for workflow_call
	// Example: inputs.branch_name
	WorkflowCallInputsPattern = regexp.MustCompile(`^inputs\.[a-zA-Z0-9_-]+$`)

	// AWInputsPattern matches github.aw.inputs.* patterns
	// Example: github.aw.inputs.custom_param
	AWInputsPattern = regexp.MustCompile(`^github\.aw\.inputs\.[a-zA-Z0-9_-]+$`)

	// AWInputsExpressionPattern matches full ${{ github.aw.inputs.* }} expressions
	// Used for extraction rather than validation
	AWInputsExpressionPattern = regexp.MustCompile(`\$\{\{\s*github\.aw\.inputs\.([a-zA-Z0-9_-]+)\s*\}\}`)

	// AWImportInputsPattern matches github.aw.import-inputs.* patterns for import-schema form.
	// Supports both scalar inputs and one-level deep object sub-keys:
	//   github.aw.import-inputs.count
	//   github.aw.import-inputs.config.apiKey
	AWImportInputsPattern = regexp.MustCompile(`^github\.aw\.import-inputs\.[a-zA-Z0-9_-]+(?:\.[a-zA-Z0-9_-]+)?$`)

	// AWImportInputsExpressionPattern matches full ${{ github.aw.import-inputs.* }} expressions.
	// Captures the full dotted path after "import-inputs." (e.g. "count" or "config.apiKey").
	// Used for substitution of values provided via the 'with' key in import specifications.
	AWImportInputsExpressionPattern = regexp.MustCompile(`\$\{\{\s*github\.aw\.import-inputs\.([a-zA-Z0-9_-]+(?:\.[a-zA-Z0-9_-]+)?)\s*\}\}`)

	// EnvPattern matches env.* patterns
	// Example: env.NODE_VERSION
	EnvPattern = regexp.MustCompile(`^env\.[a-zA-Z0-9_-]+$`)
)

// Secret Patterns
var (
	// SecretExpressionPattern matches ${{ secrets.SECRET_NAME }} expressions
	// Captures the secret name and supports optional || fallback
	SecretExpressionPattern = regexp.MustCompile(`\$\{\{\s*secrets\.([A-Z_][A-Z0-9_]*)\s*(?:\|\|.*?)?\s*\}\}`)

	// SecretsExpressionPattern validates complete secrets expression syntax
	// Supports chained || fallbacks: ${{ secrets.A || secrets.B }}
	SecretsExpressionPattern = regexp.MustCompile(`^\$\{\{\s*secrets\.[A-Za-z_][A-Za-z0-9_]*(\s*\|\|\s*secrets\.[A-Za-z_][A-Za-z0-9_]*)*\s*\}\}$`)
)

// Template Patterns
var (
	// InlineExpressionPattern matches inline ${{ ... }} expressions in templates
	InlineExpressionPattern = regexp.MustCompile(`\$\{\{[^}]+\}\}`)

	// UnsafeContextPattern matches potentially unsafe context patterns
	// These patterns may allow injection attacks in templates
	UnsafeContextPattern = regexp.MustCompile(`\$\{\{\s*(github\.event\.|steps\.[^}]+\.outputs\.|inputs\.)[^}]+\}\}`)

	// TemplateIfPattern matches {{#if condition }} template conditionals
	// Captures the condition expression (which may contain ${{ ... }})
	//
	// Expression group: (?:\$\{\{[^\}]*\}\}|[^\}\{]|\{[^\{])*
	//   - \$\{\{[^\}]*\}\}  — already-wrapped ${{ ... }} expression
	//   - [^\}\{]           — any character that is not } or {
	//   - \{[^\{]           — a { not immediately followed by another { (handles ${ env refs)
	// Using [^\}\{] (instead of [^\}]) prevents the pattern from greedily consuming
	// {{ sequences that start a nested template tag, which would embed a raw {{elseif
	// or similar token inside the wrapped ${{ }} expression and confuse later validation.
	TemplateIfPattern = regexp.MustCompile(`\{\{#if\s+((?:\$\{\{[^\}]*\}\}|[^\}\{]|\{[^\{])*)\s*\}\}`)

	// TemplateElseIfPattern matches elseif/else-if/else_if template conditionals in all supported
	// syntax variants:
	//   {{#elseif expr}}  {{#else-if expr}}  {{#else_if expr}}
	//   {{elseif expr}}   {{else-if expr}}   {{else_if expr}}
	// Captures the condition expression (which may contain ${{ ... }})
	// See TemplateIfPattern for the expression group design rationale.
	TemplateElseIfPattern = regexp.MustCompile(`\{\{#?else[-_]?if\s+((?:\$\{\{[^\}]*\}\}|[^\}\{]|\{[^\{])*)\s*\}\}`)
)

// Comparison and Literal Patterns
var (
	// ComparisonExtractionPattern extracts property accesses from comparison expressions
	// Matches patterns like "github.workflow == 'value'" and extracts "github.workflow"
	ComparisonExtractionPattern = regexp.MustCompile(`([a-zA-Z_][a-zA-Z0-9_.]*)\s*(?:==|!=|<|>|<=|>=)\s*`)

	// OrPattern matches logical OR expressions
	// Example: value1 || value2
	OrPattern = regexp.MustCompile(`^(.+?)\s*\|\|\s*(.+)$`)

	// StringLiteralPattern matches string literals in single quotes, double quotes, or backticks
	// Example: 'hello', "world", `template`
	StringLiteralPattern = regexp.MustCompile(`^'[^']*'$|^"[^"]*"$|^` + "`[^`]*`$")

	// NumberLiteralPattern matches numeric literals (integers and decimals)
	// Example: 42, -3.14, 0.5
	NumberLiteralPattern = regexp.MustCompile(`^-?\d+(\.\d+)?$`)

	// RangePattern matches numeric range patterns
	// Example: 1-10, 100-200
	RangePattern = regexp.MustCompile(`^\d+-\d+$`)
)
