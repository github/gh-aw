//go:build !integration

package taint_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/rhysd/actionlint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-aw/pkg/workflow/taint"
)

// The README's Public API section promises both parser interfaces.
var (
	_ taint.JavaScriptParser      = (*taint.TypeScriptParser)(nil)
	_ taint.JavaScriptBatchParser = (*taint.TypeScriptParser)(nil)
	_ error                       = (*taint.ExpressionError)(nil)
)

// TestSpec_PublicAPI_JavaScriptNodeChildren checks README / Public API:
// Children returns every projected child, including unmodeled syntax in Other.
// SPEC_AMBIGUITY: Child ordering and duplicate/nil entries are unspecified;
// fixtures use distinct non-nil children and compare without ordering.
func TestSpec_PublicAPI_JavaScriptNodeChildren(t *testing.T) {
	t.Parallel()

	nodeType := reflect.TypeFor[taint.JavaScriptNode]()
	childType := reflect.TypeFor[*taint.JavaScriptNode]()
	childrenType := reflect.TypeFor[[]*taint.JavaScriptNode]()

	// Discover child-bearing exported fields so the documented "all projected
	// child nodes" contract also covers future additions to the public AST.
	for field := range nodeType.Fields() {
		if !field.IsExported() || (field.Type != childType && field.Type != childrenType) {
			continue
		}
		t.Run(field.Name, func(t *testing.T) {
			t.Parallel()
			first := &taint.JavaScriptNode{Text: field.Name + " first"}
			node := &taint.JavaScriptNode{}
			target := reflect.ValueOf(node).Elem().FieldByName(field.Name)
			expected := []*taint.JavaScriptNode{first}
			if field.Type == childType {
				target.Set(reflect.ValueOf(first))
			} else {
				expected = append(expected, &taint.JavaScriptNode{Text: field.Name + " second"})
				target.Set(reflect.ValueOf(expected))
			}
			assert.ElementsMatch(t, expected, node.Children(),
				"Children should include every child projected through %s", field.Name)
		})
	}

	t.Run("modeled and unmodeled children together", func(t *testing.T) {
		t.Parallel()
		expression := &taint.JavaScriptNode{Text: "modeled expression"}
		statement := &taint.JavaScriptNode{Text: "modeled statement"}
		other := &taint.JavaScriptNode{Text: "unmodeled syntax"}
		node := &taint.JavaScriptNode{
			Expression: expression,
			Statements: []*taint.JavaScriptNode{statement},
			Other:      []*taint.JavaScriptNode{other},
		}
		assert.ElementsMatch(t, []*taint.JavaScriptNode{expression, statement, other}, node.Children(),
			"modeled child fields must not hide unmodeled syntax in Other")
	})
}

// TestSpec_PublicAPI_VisitInterpolations checks README / Public API:
// each interpolation is visited without evaluating its symbolic input.
// SPEC_AMBIGUITY: The README does not define callback offsets or visit order,
// so these tests only check visits for ordinary Actions interpolations.
func TestSpec_PublicAPI_VisitInterpolations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		text   string
		visits int
	}{
		{name: "plain authored text", text: "Review the build result.", visits: 0},
		{name: "one symbolic input", text: "Build ${{ inputs.target }}.", visits: 1},
		{
			name:   "distinct symbolic inputs",
			text:   "${{ inputs.target }}\n${{ inputs.configuration }}",
			visits: 2,
		},
		{
			name:   "repeated interpolation occurrences",
			text:   "${{ inputs.target }} / ${{ inputs.target }} / ${{ inputs.target }}",
			visits: 3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			visits := 0
			err := taint.VisitInterpolations(context.Background(), tt.text, "authored.md",
				func(_, _, _ int, node actionlint.ExprNode) error {
					visits++
					assert.NotNil(t, node, "each interpolation visit should expose a parsed expression")
					return nil
				})
			require.NoError(t, err, "ordinary symbolic interpolations should be visitable: %s", tt.name)
			assert.Equal(t, tt.visits, visits, "every interpolation occurrence should be visited: %s", tt.name)
		})
	}
}

// TestSpec_PublicAPI_AnalyzeText checks README / Public API: authored expressions
// are analyzed using the supplied source rule and symbolic source bindings.
// SPEC_AMBIGUITY: The README does not prescribe expression-result ordering or
// how expressions without sources are represented; compare source references.
func TestSpec_PublicAPI_AnalyzeText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		text       string
		source     string
		bindings   map[string][]taint.Source
		references []string
	}{
		{
			name:       "configured symbolic source",
			text:       "Review ${{ inputs.target }}.",
			source:     "inputs.target",
			references: []string{"inputs.target"},
		},
		{
			name:   "unconfigured symbolic input",
			text:   "Review ${{ inputs.configuration }}.",
			source: "inputs.target",
		},
		{
			name:   "source propagated through supplied binding",
			text:   "Review ${{ env.TARGET }}.",
			source: "inputs.target",
			bindings: map[string][]taint.Source{
				"env.TARGET": {{Reference: "inputs.target", Path: "binding.md"}},
			},
			references: []string{"inputs.target"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rule := taint.Rule{
				ID:       "readme-symbolic-input",
				IsSource: func(reference string) bool { return reference == tt.source },
			}
			expressions, err := taint.AnalyzeText(context.Background(), tt.text, "authored.md", rule, tt.bindings)
			require.NoError(t, err, "authored text should support the supplied symbolic source rule: %s", tt.name)
			var references []string
			for _, expression := range expressions {
				for _, source := range expression.Sources {
					references = append(references, source.Reference)
				}
			}
			assert.ElementsMatch(t, tt.references, references,
				"analysis should report sources selected by the rule and supplied bindings: %s", tt.name)
		})
	}
}
