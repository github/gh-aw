package lint

import (
	"encoding/json"
	"testing"

	"github.com/github/gh-aw/pkg/workflow/taint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGraderJavaScriptReferenceName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		node *taint.JavaScriptNode
		want string
	}{
		{name: "nil node has no reference"},
		{name: "zero node has no reference", node: &taint.JavaScriptNode{}},
		{name: "unsupported expression has no reference", node: &taint.JavaScriptNode{Kind: "CallExpression", Text: "fs.readFile"}},
		{name: "identifier preserves text", node: &taint.JavaScriptNode{Kind: "Identifier", Text: "fs"}, want: "fs"},
		{name: "empty identifier stays empty", node: &taint.JavaScriptNode{Kind: "Identifier"}},
		{name: "identifier preserves Unicode", node: &taint.JavaScriptNode{Kind: "Identifier", Text: "文件"}, want: "文件"},
		{
			name: "property appends its name",
			node: &taint.JavaScriptNode{
				Kind:       "PropertyAccessExpression",
				Expression: &taint.JavaScriptNode{Kind: "Identifier", Text: "fs"},
				Name:       &taint.JavaScriptNode{Kind: "Identifier", Text: "readFile"},
			},
			want: "fs.readFile",
		},
		{
			name: "missing property name has no reference",
			node: &taint.JavaScriptNode{Kind: "PropertyAccessExpression", Expression: &taint.JavaScriptNode{Kind: "Identifier", Text: "fs"}},
		},
		{
			name: "missing property base has no reference",
			node: &taint.JavaScriptNode{Kind: "PropertyAccessExpression", Name: &taint.JavaScriptNode{Text: "readFile"}},
		},
		{
			name: "empty property name has no reference",
			node: &taint.JavaScriptNode{
				Kind:       "PropertyAccessExpression",
				Expression: &taint.JavaScriptNode{Kind: "Identifier", Text: "fs"},
				Name:       &taint.JavaScriptNode{},
			},
		},
		{
			name: "string element appends its key",
			node: &taint.JavaScriptNode{
				Kind:               "ElementAccessExpression",
				Expression:         &taint.JavaScriptNode{Kind: "Identifier", Text: "Deno"},
				ArgumentExpression: &taint.JavaScriptNode{Kind: "StringLiteral", Text: "readTextFile"},
			},
			want: "Deno.readTextFile",
		},
		{
			name: "template element appends its key",
			node: &taint.JavaScriptNode{
				Kind:               "ElementAccessExpression",
				Expression:         &taint.JavaScriptNode{Kind: "Identifier", Text: "Bun"},
				ArgumentExpression: &taint.JavaScriptNode{Kind: "NoSubstitutionTemplateLiteral", Text: "write"},
			},
			want: "Bun.write",
		},
		{
			name: "missing element key has no reference",
			node: &taint.JavaScriptNode{Kind: "ElementAccessExpression", Expression: &taint.JavaScriptNode{Kind: "Identifier", Text: "fs"}},
		},
		{
			name: "dynamic element key has no reference",
			node: &taint.JavaScriptNode{
				Kind:               "ElementAccessExpression",
				Expression:         &taint.JavaScriptNode{Kind: "Identifier", Text: "fs"},
				ArgumentExpression: &taint.JavaScriptNode{Kind: "Identifier", Text: "readFile"},
			},
		},
		{
			name: "numeric element key has no reference",
			node: &taint.JavaScriptNode{
				Kind:               "ElementAccessExpression",
				Expression:         &taint.JavaScriptNode{Kind: "Identifier", Text: "fs"},
				ArgumentExpression: &taint.JavaScriptNode{Kind: "NumericLiteral", Text: "0"},
			},
		},
		{
			name: "empty element key has no reference",
			node: &taint.JavaScriptNode{
				Kind:               "ElementAccessExpression",
				Expression:         &taint.JavaScriptNode{Kind: "Identifier", Text: "fs"},
				ArgumentExpression: &taint.JavaScriptNode{Kind: "StringLiteral"},
			},
		},
		{
			name: "missing element base has no reference",
			node: &taint.JavaScriptNode{
				Kind:               "ElementAccessExpression",
				ArgumentExpression: &taint.JavaScriptNode{Kind: "StringLiteral", Text: "读取"},
			},
		},
		{
			name: "parentheses unwrap their expression",
			node: &taint.JavaScriptNode{Kind: "ParenthesizedExpression", Expression: &taint.JavaScriptNode{Kind: "Identifier", Text: "process"}},
			want: "process",
		},
		{
			name: "non-null assertion unwraps its expression",
			node: &taint.JavaScriptNode{Kind: "NonNullExpression", Expression: &taint.JavaScriptNode{Kind: "Identifier", Text: "module"}},
			want: "module",
		},
		{name: "empty parentheses have no reference", node: &taint.JavaScriptNode{Kind: "ParenthesizedExpression"}},
		{name: "empty non-null assertion has no reference", node: &taint.JavaScriptNode{Kind: "NonNullExpression"}},
		{
			name: "mixed nested access resolves the complete reference",
			node: &taint.JavaScriptNode{
				Kind: "PropertyAccessExpression",
				Expression: &taint.JavaScriptNode{
					Kind: "NonNullExpression",
					Expression: &taint.JavaScriptNode{
						Kind: "ElementAccessExpression",
						Expression: &taint.JavaScriptNode{
							Kind:       "ParenthesizedExpression",
							Expression: &taint.JavaScriptNode{Kind: "Identifier", Text: "process"},
						},
						ArgumentExpression: &taint.JavaScriptNode{Kind: "StringLiteral", Text: "mainModule"},
					},
				},
				Name: &taint.JavaScriptNode{Kind: "Identifier", Text: "require"},
			},
			want: "process.mainModule.require",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			before, err := json.Marshal(tt.node)
			require.NoError(t, err)

			assert.Equal(t, tt.want, graderJavaScriptReferenceName(tt.node))

			after, err := json.Marshal(tt.node)
			require.NoError(t, err)
			assert.Equal(t, before, after, "reference extraction must not mutate the input AST")
		})
	}
}
