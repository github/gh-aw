//go:build !integration

package taint_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/rhysd/actionlint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-aw/pkg/workflow/actions"
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

// TestSpec_PublicAPI_Analyze checks README / Overview and Public API:
// symbolic configured sources reach the selected execution sinks without
// evaluating expressions or executing the workflow's scripts.
// SPEC_AMBIGUITY: Finding order, duplication, and exact path formatting are
// unspecified. Check the selected symbolic reference and sink category only.
func TestSpec_PublicAPI_Analyze(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name         string
		run          string
		selectSink   bool
		wantFindings bool
	}{
		{"configured source and sink", "echo ${{ inputs.target }}\nexit 99", true, true},
		{"unconfigured source", "echo ${{ inputs.configuration }}\nexit 99", true, false},
		{"unselected sink", "echo ${{ inputs.target }}\nexit 99", false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			content := "name: README specification\non: push\njobs:\n  readme:\n" +
				"    runs-on: ubuntu-latest\n    steps:\n      - shell: bash\n        run: |\n          " +
				strings.ReplaceAll(tt.run, "\n", "\n          ") + "\n"
			input, err := actions.Parse([]byte(content))
			require.NoError(t, err, "the fixture must be a parsed Actions workflow")
			rule := taint.Rule{
				ID:       "readme-symbolic-execution",
				IsSource: func(reference string) bool { return reference == "inputs.target" },
				IsSink: func(site taint.Site) bool {
					return tt.selectSink && site.Kind == taint.ExecutableInterpolation
				},
			}
			result, err := taint.Analyze(input, rule)
			require.NoError(t, err, "scripts must be analyzed without executing exit 99")
			if !tt.wantFindings {
				assert.Empty(t, result.Findings, "findings require both a configured source and a selected sink")
				return
			}
			require.NotEmpty(t, result.Findings, "the configured symbolic source must reach the selected execution sink")
			for _, finding := range result.Findings {
				assert.Equal(t, rule.ID, finding.RuleID, "findings must identify the supplied analysis rule")
				assert.Equal(t, taint.ExecutableInterpolation, finding.Site.Kind,
					"findings must identify the selected execution sink")
				require.NotEmpty(t, finding.Sources, "findings must identify symbolic source references")
				for _, source := range finding.Sources {
					assert.Equal(t, "inputs.target", source.Reference,
						"findings must identify the symbolic source without resolving its value")
				}
			}
		})
	}
}

// TestSpec_PublicAPI_ExpressionError checks README / Public API:
// expression errors are value-safe and do not reproduce authored values.
// SPEC_AMBIGUITY: Exact error wording is unspecified; check value omission.
func TestSpec_PublicAPI_ExpressionError(t *testing.T) {
	t.Parallel()

	for _, text := range []string{
		"${{ 'readme-value-marker' + }}",
		"${{ 'readme-value-marker' && }}",
	} {
		t.Run(text, func(t *testing.T) {
			t.Parallel()
			rule := taint.Rule{
				ID:       "readme-expression-error",
				IsSource: func(reference string) bool { return reference == "inputs.target" },
			}
			_, err := taint.AnalyzeText(context.Background(), text, "authored.md", rule, nil)
			require.Error(t, err, "an incomplete authored expression must report a parsing error")
			var expressionError *taint.ExpressionError
			require.ErrorAs(t, err, &expressionError, "authored expression errors must expose the documented error type")
			assert.NotContains(t, err.Error(), "readme-value-marker",
				"value-safe expression errors must not reproduce literal authored values")
		})
	}
}
