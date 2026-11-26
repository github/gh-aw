//go:build !integration

package parentmap

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/analysis"
)

func TestAnalyzerIndexesParentsPerFile(t *testing.T) {
	t.Parallel()
	fset := token.NewFileSet()
	first, err := parser.ParseFile(fset, "a.go", "package p\nfunc f() { _ = 1 }\n", 0)
	require.NoError(t, err, "first fixture must parse")
	second, err := parser.ParseFile(fset, "b.go", "package p\nfunc g() {}\n", 0)
	require.NoError(t, err, "second fixture must parse")

	pass := &analysis.Pass{Files: []*ast.File{first, second}, ResultOf: map[*analysis.Analyzer]any{}}
	result, err := Analyzer.Run(pass)
	require.NoError(t, err, "parent map analyzer must not fail")
	pass.ResultOf[Analyzer] = result

	idx, err := Of(pass)
	require.NoError(t, err, "Of must return the analyzer result")
	require.Len(t, idx, 2, "index must contain one parent map per file")

	fn, ok := first.Decls[0].(*ast.FuncDecl)
	require.True(t, ok, "first declaration must be a function")
	assert.Same(t, first, idx[first][fn], "top-level declaration parent must be its file")
	assert.Same(t, fn, idx[first][fn.Body], "function body parent must be its declaration")
	assert.NotContains(t, idx[second], ast.Node(fn), "parent maps must be scoped to their file")
}

func TestOfRejectsMissingResult(t *testing.T) {
	t.Parallel()
	_, err := Of(&analysis.Pass{ResultOf: map[*analysis.Analyzer]any{}})
	assert.Error(t, err, "Of must fail when the parent map result is missing")
}
