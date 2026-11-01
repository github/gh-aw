package aw

import (
	"testing"

	"github.com/rhysd/actionlint"
	"github.com/stretchr/testify/assert"
)

func TestStatusOnlyExpressionPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		node actionlint.ExprNode
		want []string
		ok   bool
	}{
		{
			name: "nil expression has no path",
		},
		{
			name: "standalone string has no path",
			node: &actionlint.StringNode{Value: "needs.detection.result"},
		},
		{
			name: "variable starts a path",
			node: &actionlint.VariableNode{Name: "needs"},
			want: []string{"needs"},
			ok:   true,
		},
		{
			name: "empty variable name is preserved",
			node: &actionlint.VariableNode{},
			want: []string{""},
			ok:   true,
		},
		{
			name: "unicode variable name is preserved",
			node: &actionlint.VariableNode{Name: "結果"},
			want: []string{"結果"},
			ok:   true,
		},
		{
			name: "object dereference appends a property",
			node: &actionlint.ObjectDerefNode{
				Receiver: &actionlint.VariableNode{Name: "needs"},
				Property: "detection",
			},
			want: []string{"needs", "detection"},
			ok:   true,
		},
		{
			name: "empty object property is preserved",
			node: &actionlint.ObjectDerefNode{
				Receiver: &actionlint.VariableNode{Name: "needs"},
			},
			want: []string{"needs", ""},
			ok:   true,
		},
		{
			name: "unsupported object receiver invalidates the path",
			node: &actionlint.ObjectDerefNode{
				Receiver: &actionlint.StringNode{Value: "needs"},
				Property: "outputs",
			},
		},
		{
			name: "unsupported index operand invalidates the path",
			node: &actionlint.IndexAccessNode{
				Operand: &actionlint.StringNode{Value: "needs"},
				Index:   &actionlint.StringNode{Value: "detection"},
			},
		},
		{
			name: "string index appends its literal value",
			node: &actionlint.IndexAccessNode{
				Operand: &actionlint.VariableNode{Name: "needs"},
				Index:   &actionlint.StringNode{Value: "detection"},
			},
			want: []string{"needs", "detection"},
			ok:   true,
		},
		{
			name: "empty string index is preserved",
			node: &actionlint.IndexAccessNode{
				Operand: &actionlint.VariableNode{Name: "needs"},
				Index:   &actionlint.StringNode{},
			},
			want: []string{"needs", ""},
			ok:   true,
		},
		{
			name: "dynamic index becomes a wildcard",
			node: &actionlint.IndexAccessNode{
				Operand: &actionlint.VariableNode{Name: "needs"},
				Index:   &actionlint.VariableNode{Name: "job"},
			},
			want: []string{"needs", "*"},
			ok:   true,
		},
		{
			name: "nil index becomes a wildcard",
			node: &actionlint.IndexAccessNode{
				Operand: &actionlint.VariableNode{Name: "needs"},
			},
			want: []string{"needs", "*"},
			ok:   true,
		},
		{
			name: "mixed accesses preserve order and unicode output names",
			node: &actionlint.IndexAccessNode{
				Operand: &actionlint.ObjectDerefNode{
					Receiver: &actionlint.IndexAccessNode{
						Operand: &actionlint.VariableNode{Name: "needs"},
						Index:   &actionlint.StringNode{Value: "detection"},
					},
					Property: "outputs",
				},
				Index: &actionlint.StringNode{Value: "結果"},
			},
			want: []string{"needs", "detection", "outputs", "結果"},
			ok:   true,
		},
		{
			name: "nested invalid operand propagates through object access",
			node: &actionlint.ObjectDerefNode{
				Receiver: &actionlint.IndexAccessNode{
					Index: &actionlint.StringNode{Value: "detection"},
				},
				Property: "outputs",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := statusOnlyExpressionPath(tt.node)

			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.ok, ok)
		})
	}
}
