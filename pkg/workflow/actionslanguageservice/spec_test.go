//go:build !integration

package actionslanguageservice_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-aw/pkg/workflow/actionslanguageservice"
)

// TestSpec_PublicAPI_Tokenize follows README.md's spelling and byte-offset
// contract, including tokens following multibyte text.
func TestSpec_PublicAPI_Tokenize(t *testing.T) {
	for _, source := range []string{"github.ref == 'main'", "'é' == github.ref"} {
		t.Run(source, func(t *testing.T) {
			tokens, err := actionslanguageservice.Tokenize(source)
			require.NoError(t, err, "valid Actions syntax must tokenize")
			require.NotEmpty(t, tokens, "tokenization must retain source tokens")
			var spelling []string
			for _, token := range tokens {
				if token.Text == "" {
					continue
				}
				require.GreaterOrEqual(t, token.Offset, 0, "token byte offset must be within the source")
				require.LessOrEqual(t, token.Offset+len(token.Text), len(source), "token spelling must fit in the source")
				assert.Equal(t, token.Text, source[token.Offset:token.Offset+len(token.Text)], "token spelling must equal its original source bytes")
				spelling = append(spelling, token.Text)
			}
			assert.Equal(t, strings.ReplaceAll(source, " ", ""), strings.Join(spelling, ""), "tokens must retain every non-whitespace source character")
		})
	}
}

// TestSpec_PublicAPI_ExpressionParsing follows README.md's syntax validation
// and AST construction contracts without evaluating expressions.
func TestSpec_PublicAPI_ExpressionParsing(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source string
		valid  bool
	}{
		{name: "context reference", source: "github.ref", valid: true},
		{name: "comparison", source: "github.ref == 'main'", valid: true},
		{name: "unclosed grouping", source: "(github.ref"},
		{name: "missing operand", source: "github.ref =="},
	} {
		t.Run(tc.name, func(t *testing.T) {
			expression, parseErr := actionslanguageservice.ParseSyntax(tc.source)
			validateErr := actionslanguageservice.ValidateExpression(tc.source)
			if !tc.valid {
				require.Error(t, parseErr, "invalid syntax must fail AST construction")
				require.Error(t, validateErr, "invalid syntax must fail validation")
				return
			}
			require.NoError(t, parseErr, "valid syntax must construct an AST")
			require.NoError(t, validateErr, "valid syntax must validate")
			require.NotNil(t, expression, "parsing must return an expression")
			assert.Equal(t, tc.source, tc.source[expression.Start:expression.End], "the root source range must span the expression")
		})
	}
}

// TestSpec_PublicAPI_ParseExpression follows README.md's allowed-context check.
func TestSpec_PublicAPI_ParseExpression(t *testing.T) {
	for _, tc := range []struct {
		name    string
		allowed []string
		valid   bool
	}{
		{name: "allowed context", allowed: []string{"github"}, valid: true},
		{name: "unavailable context", allowed: []string{"runner"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			expression, err := actionslanguageservice.ParseExpression("github.ref", tc.allowed)
			if tc.valid {
				require.NoError(t, err, "references to an allowed context must parse")
				assert.NotNil(t, expression, "allowed references must produce an AST")
			} else {
				assert.Error(t, err, "references outside the allowed contexts must fail")
			}
		})
	}
}

// TestSpec_PublicAPI_References follows README.md's AST traversal and context
// reference extraction contracts.
func TestSpec_PublicAPI_References(t *testing.T) {
	expression, err := actionslanguageservice.ParseSyntax("github.ref == inputs.branch")
	require.NoError(t, err, "a comparison of context references must parse")
	assert.ElementsMatch(t, [][]string{{"github", "ref"}, {"inputs", "branch"}}, actionslanguageservice.References(expression), "reference extraction must include both context paths")
	var references [][]string
	actionslanguageservice.Walk(expression, func(node *actionslanguageservice.Expression) bool {
		if path, ok := actionslanguageservice.Reference(node); ok {
			references = append(references, path)
		}
		return true
	})
	// SPEC_AMBIGUITY: traversal granularity is not specified; visiting
	// component references as well as complete paths is permitted.
	assert.Contains(t, references, []string{"github", "ref"}, "walking must expose the complete github reference")
	assert.Contains(t, references, []string{"inputs", "branch"}, "walking must expose the complete inputs reference")
}

// TestSpec_PublicAPI_RewriteReferences follows README.md's source-preserving
// reference rewriting contract.
func TestSpec_PublicAPI_RewriteReferences(t *testing.T) {
	source := "github.ref  ==  'github.ref' && inputs.branch != ''"
	rewritten := actionslanguageservice.RewriteReferences(source, func(path []string) (string, bool) {
		if strings.Join(path, ".") == "github.ref" {
			return "github.head_ref", true
		}
		return "", false
	})
	assert.Equal(t, "github.head_ref  ==  'github.ref' && inputs.branch != ''", rewritten, "rewriting a reference must retain whitespace, string literals, and unrelated references")
}

// TestSpec_PublicAPI_WrappedExpression follows README.md's complete wrapped
// expression extraction contract.
func TestSpec_PublicAPI_WrappedExpression(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source string
		valid  bool
	}{
		{name: "complete expression", source: "${{ github.ref }}", valid: true},
		{name: "surrounding text", source: "ref=${{ github.ref }}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			expression, ok := actionslanguageservice.WrappedExpression(tc.source)
			assert.Equal(t, tc.valid, ok, "only a complete wrapped expression must be extracted")
			if tc.valid {
				require.NotNil(t, expression, "a complete wrapper must yield an AST")
				path, ok := actionslanguageservice.Reference(expression)
				require.True(t, ok, "the wrapped context reference must remain a reference")
				assert.Equal(t, []string{"github", "ref"}, path, "wrapper extraction must retain the context path")
			}
		})
	}
}

// TestSpec_PublicAPI_ExtractExpressions follows README.md's interpolation
// extraction contract, including quoted strings containing closing markers.
func TestSpec_PublicAPI_ExtractExpressions(t *testing.T) {
	source := "first=${{ '}}' }}; second=${{ github.ref }}"
	interpolations, err := actionslanguageservice.ExtractExpressions(source)
	require.NoError(t, err, "closing markers inside a quoted string must not end an interpolation")
	require.Len(t, interpolations, 2, "both interpolations must be extracted")
	for i, expected := range []string{"${{ '}}' }}", "${{ github.ref }}"} {
		item := interpolations[i]
		assert.Equal(t, expected, source[item.Start:item.End], "interpolation %d must cover its complete source range", i)
		assert.Contains(t, expected, strings.TrimSpace(item.Content), "interpolation %d must retain its expression content", i)
	}
}

// SPEC_AMBIGUITY: Pattern and ReferencePattern flag/filter and capture semantics
// are not documented. The no-match tests below avoid assuming those semantics.

// TestSpec_PublicAPI_ParseWorkflow follows README.md's per-YAML-node expression
// recording contract.
func TestSpec_PublicAPI_ParseWorkflow(t *testing.T) {
	source := "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo '${{ github.ref }}'\n"
	workflow, err := actionslanguageservice.ParseWorkflow("spec.yml", source)
	require.NoError(t, err, "workflow YAML containing an expression must parse")
	require.NotNil(t, workflow.Document, "workflow parsing must retain the YAML document")
	require.NotEmpty(t, workflow.Expressions, "workflow parsing must record expressions by YAML node")
	var references [][]string
	for node, expressions := range workflow.Expressions {
		require.NotNil(t, node, "each recorded expression must belong to a YAML node")
		for _, expression := range expressions {
			references = append(references, actionslanguageservice.References(expression)...)
		}
	}
	assert.Contains(t, references, []string{"github", "ref"}, "the recorded expression must retain its context reference")
}

// TestSpec_PublicAPI_ExpressionBounds follows README.md's length and nesting
// limits. SPEC_AMBIGUITY: exact boundary measurement is unspecified, so these
// cases exceed the published bounds rather than asserting inclusive limits.
func TestSpec_PublicAPI_ExpressionBounds(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source string
	}{
		{name: "excessive length", source: "'" + strings.Repeat("x", actionslanguageservice.MaxExpressionLength+1) + "'"},
		{name: "excessive depth", source: strings.Repeat("(", actionslanguageservice.MaxExpressionDepth+1) + "true" + strings.Repeat(")", actionslanguageservice.MaxExpressionDepth+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := actionslanguageservice.ParseSyntax(tc.source)
			assert.Error(t, err, "parsing must enforce the documented %s bound", tc.name)
		})
	}
}

// TestSpec_PublicAPI_ActionsIdentifier follows README.md's identifier
// validation API. SPEC_AMBIGUITY: the README does not list identifier rules.
func TestSpec_PublicAPI_ActionsIdentifier(t *testing.T) {
	for _, name := range []string{"build", "test1"} {
		assert.True(t, actionslanguageservice.ActionsIdentifier(name, false), "ordinary Actions identifier %q must validate", name)
	}
}

// TestSpec_PublicAPI_ValidateWorkflowWithCacheMode follows README.md's schema
// validation API. SPEC_AMBIGUITY: supported cache modes are not enumerated,
// so this validates a native workflow with no explicit cache mode.
func TestSpec_PublicAPI_ValidateWorkflowWithCacheMode(t *testing.T) {
	source := "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ready\n"
	err := actionslanguageservice.ValidateWorkflowWithCacheMode("spec.yml", source)
	require.NoError(t, err, "a native workflow with no explicit cache policy must validate")
}

// TestSpec_PublicAPI_ExpressionChild follows README.md / Public API: Child
// returns a child node by index. Only valid indices are specified.
func TestSpec_PublicAPI_ExpressionChild(t *testing.T) {
	t.Parallel()

	expression, err := actionslanguageservice.ParseSyntax("github.ref == inputs.branch")
	require.NoError(t, err, "a comparison must parse before inspecting its children")
	require.NotEmpty(t, expression.Children, "a comparison must expose its expression children")
	for index, child := range expression.Children {
		assert.Equal(t, child, expression.Child(index), "Child(%d) must return the child at that index", index)
	}
}

// TestSpec_Types_Diagnostic follows README.md / Public API: Diagnostic
// implements Error. Exact diagnostic text and formatting are not specified.
func TestSpec_Types_Diagnostic(t *testing.T) {
	t.Parallel()

	diagnostic := &actionslanguageservice.Diagnostic{
		File: "spec.yml", Line: 1, Column: 1, Message: "Invalid expression",
	}
	var err error = diagnostic
	assert.Equal(t, diagnostic.Error(), err.Error(), "Diagnostic must be usable through Go's error interface")
}

// TestSpec_PublicAPI_ReferencePatternNoMatches follows README.md / Public API:
// searches find expression references and rewrites preserve unaffected source.
// SPEC_AMBIGUITY: flag meanings and capture layouts are unspecified, so every
// flag combination is checked on text containing no expression references.
func TestSpec_PublicAPI_ReferencePatternNoMatches(t *testing.T) {
	t.Parallel()

	const source = "  Plain text without expressions.\r\n"
	for _, tt := range []struct {
		name      string
		whole     bool
		unwrapped bool
	}{
		{name: "both flags false"},
		{name: "whole flag", whole: true},
		{name: "unwrapped flag", unwrapped: true},
		{name: "both flags true", whole: true, unwrapped: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pattern := actionslanguageservice.ReferencePattern(tt.whole, tt.unwrapped, func(path []string) []string {
				return path
			})
			assert.False(t, pattern.MatchString(source), "plain text must not match an expression reference")
			assert.Empty(t, pattern.FindAllString(source, 1), "search must find no reference strings in plain text")
			assert.Empty(t, pattern.FindAllStringSubmatch(source, 1), "plain text must have no reference captures")
			assert.Empty(t, pattern.FindAllStringSubmatchIndex(source, 1), "plain text must have no reference capture offsets")
			assert.Empty(t, pattern.FindStringSubmatch(source), "a single-match search must find no reference in plain text")
			assert.Equal(t, source, pattern.ReplaceAllString(source, "replacement"), "rewriting must preserve all unmatched source bytes")
			calls := 0
			rewritten := pattern.ReplaceAllStringFunc(source, func(string) string {
				calls++
				return "replacement"
			})
			assert.Zero(t, calls, "a replacement callback must not run when no references match")
			assert.Equal(t, source, rewritten, "callback rewriting must preserve all unmatched source bytes")
		})
	}
}

// SPEC_AMBIGUITY: README.md documents a schema property's UnmarshalJSON method,
// but that property type is unexported and cannot be named by external tests.
