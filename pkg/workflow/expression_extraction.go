package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var expressionExtractionLog = logger.New("workflow:expression_extraction")

// Pre-compiled regexes for performance (avoid recompilation in hot paths)
var (
	// expressionExtractionRegex matches GitHub Actions expressions: ${{ ... }}
	// Uses (?s) flag for dotall mode, non-greedy matching
	expressionExtractionRegex = regexp.MustCompile(`\$\{\{(.*?)\}\}`)
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

// ExtractExpressions extracts all ${{ ... }} expressions from the markdown content
// and creates environment variable mappings for each unique expression
func (e *ExpressionExtractor) ExtractExpressions(markdown string) ([]*ExpressionMapping, error) {
	expressionExtractionLog.Printf("Extracting expressions from markdown: content_length=%d", len(markdown))

	// Use pre-compiled regex from package level for performance
	matches := expressionExtractionRegex.FindAllStringSubmatch(markdown, -1)
	expressionExtractionLog.Printf("Found %d expression matches", len(matches))

	for _, match := range matches {
		if len(match) < 2 {
			continue
		}

		// Extract the full original expression including ${{ }}
		originalExpr := match[0]

		// Extract the content (without ${{ }})
		content := strings.TrimSpace(match[1])

		// Skip if we've already seen this expression
		if _, exists := e.mappings[originalExpr]; exists {
			continue
		}

		// Generate environment variable name
		envVar := e.generateEnvVarName(content)

		// Create mapping
		mapping := &ExpressionMapping{
			Original: originalExpr,
			EnvVar:   envVar,
			Content:  content,
		}

		e.mappings[originalExpr] = mapping
	}

	// Convert map to sorted slice for consistent ordering
	var result []*ExpressionMapping
	for _, mapping := range e.mappings {
		result = append(result, mapping)
	}

	// Sort by original expression for deterministic output
	sort.Slice(result, func(i, j int) bool {
		return result[i].Original < result[j].Original
	})

	expressionExtractionLog.Printf("Extracted %d unique expressions", len(result))

	return result, nil
}

// simpleIdentifierRegex matches simple JavaScript property access chains like
// "github.event.issue.number" or "needs.activation.outputs.text"
// Each identifier must start with a letter or underscore, followed by alphanumeric or underscore
var simpleIdentifierRegex = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*(\.[a-zA-Z_][a-zA-Z0-9_]*)*$`)

// generateEnvVarName generates a unique environment variable name for an expression
// For simple JavaScript property access chains (e.g., "github.event.issue.number"),
// it generates a pretty name like "GH_AW_GITHUB_EVENT_ISSUE_NUMBER".
// For complex expressions, it falls back to a hash-based name.
func (e *ExpressionExtractor) generateEnvVarName(content string) string {
	// Check if the expression is a simple JavaScript property access chain
	if simpleIdentifierRegex.MatchString(content) {
		// Convert dots to underscores and uppercase
		prettyName := strings.ToUpper(strings.ReplaceAll(content, ".", "_"))
		return fmt.Sprintf("GH_AW_%s", prettyName)
	}

	// Fall back to hash-based name for complex expressions
	// Use SHA256 hash to generate a unique identifier
	hash := sha256.Sum256([]byte(content))
	hashStr := hex.EncodeToString(hash[:])

	// Use first 8 characters of hash for brevity
	shortHash := hashStr[:8]

	// Create environment variable name
	return fmt.Sprintf("GH_AW_EXPR_%s", strings.ToUpper(shortHash))
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
	sort.Slice(mappings, func(i, j int) bool {
		return len(mappings[i].Original) > len(mappings[j].Original)
	})

	// Replace each expression with its environment variable reference
	// Use __VAR__ placeholder format to prevent template injection
	for _, mapping := range mappings {
		placeholder := fmt.Sprintf("__%s__", mapping.EnvVar)
		result = strings.ReplaceAll(result, mapping.Original, placeholder)
	}

	return result
}

// GetMappings returns all expression mappings
func (e *ExpressionExtractor) GetMappings() []*ExpressionMapping {
	var result []*ExpressionMapping
	for _, mapping := range e.mappings {
		result = append(result, mapping)
	}

	// Sort by environment variable name for consistent ordering
	sort.Slice(result, func(i, j int) bool {
		return result[i].EnvVar < result[j].EnvVar
	})

	return result
}

// awInputsExprRegex matches ${{ github.aw.inputs.<key> }} expressions
var awInputsExprRegex = regexp.MustCompile(`\$\{\{\s*github\.aw\.inputs\.([a-zA-Z0-9_-]+)\s*\}\}`)

// SubstituteImportInputs replaces ${{ github.aw.inputs.<key> }} expressions
// with the corresponding values from the importInputs map.
// This is called before expression extraction to inject import input values.
func SubstituteImportInputs(content string, importInputs map[string]any) string {
	if len(importInputs) == 0 {
		return content
	}

	expressionExtractionLog.Printf("Substituting import inputs: %d inputs available", len(importInputs))

	result := awInputsExprRegex.ReplaceAllStringFunc(content, func(match string) string {
		// Extract the key name from the expression
		matches := awInputsExprRegex.FindStringSubmatch(match)
		if len(matches) < 2 {
			return match
		}

		key := matches[1]
		if value, exists := importInputs[key]; exists {
			// Convert value to string
			strValue := fmt.Sprintf("%v", value)
			expressionExtractionLog.Printf("Substituting github.aw.inputs.%s with value: %s", key, strValue)
			return strValue
		}

		// If the key doesn't exist in importInputs, keep the original expression
		expressionExtractionLog.Printf("Import input key not found: %s", key)
		return match
	})

	return result
}
