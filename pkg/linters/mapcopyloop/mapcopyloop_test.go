//go:build !integration

package mapcopyloop_test

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/github/gh-aw/pkg/linters/mapcopyloop"
)

func TestAnalyzer(t *testing.T) {
	t.Parallel()
	testdata := analysistest.TestData()
	analysistest.RunWithSuggestedFixes(t, testdata, mapcopyloop.Analyzer, "mapcopyloop")
	analysistest.Run(t, testdata, mapcopyloop.Analyzer, "mapcopyloopnoimport")
}

func TestSuggestedFixTypeChecks(t *testing.T) {
	filename := "testdata/src/mapcopyloop/mapcopyloop.go.golden"
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, nil, parser.AllErrors)
	if err != nil {
		t.Fatalf("parse fixed source: %v", err)
	}

	config := types.Config{Importer: importer.Default()}
	if _, err := config.Check("mapcopyloop", fset, []*ast.File{file}, nil); err != nil {
		t.Fatalf("type-check fixed source: %v", err)
	}
}
