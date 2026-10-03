package nolint

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestBuildDirectiveIndex_ParsesDirectiveTokens(t *testing.T) {
	t.Parallel()
	const filename = "nolint_tokens.go"
	const src = `package p

//nolint:gosec,tolowerequalfold // second token should match
var _ = 1

//nolint:tolowerequalfold,gosec
var _ = 2

//nolint:tolowerequalfoldX
var _ = 3

//nolint:all
var _ = 4

//nolint:tolowerequalfold
var _ = 5

// nolint:tolowerequalfold
var _ = 6
`

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, parser.ParseComments)
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}

	shared := BuildDirectiveIndex(&analysis.Pass{
		Fset:  fset,
		Files: []*ast.File{file},
		Pkg:   types.NewPackage("github.com/github/gh-aw/pkg/linters/internal/nolint/testdata", "p"),
	})
	if !HasDirectiveForLinter(token.Position{Filename: filename, Line: 4}, shared, "tolowerequalfold") {
		t.Fatalf("expected previous-line shared directive match")
	}
	if !HasDirectiveForLinter(token.Position{Filename: filename, Line: 13}, shared, "differentlinter") {
		t.Fatalf("expected nolint:all shared directive match")
	}
	if HasDirectiveForLinter(token.Position{Filename: filename, Line: 10}, shared, "tolowerequalfold") {
		t.Fatalf("unexpected shared directive match for prefix-only directive")
	}
	if !HasDirectiveForLinter(token.Position{Filename: filename, Line: 16}, shared, "tolowerequalfold") {
		t.Fatalf("expected spaced previous-line directive match")
	}
}
