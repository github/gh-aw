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

	"github.com/github/gh-aw/pkg/logger"
)

var expressionPatternsLog = logger.New("workflow:expression_patterns")

func init() {
	expressionPatternsLog.Print("Initializing expression pattern regex compilation")
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
	TemplateIfPattern = regexp.MustCompile(`\{\{#if\s+((?:\$\{\{[^\}]*\}\}|[^\}])*)\s*\}\}`)
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
