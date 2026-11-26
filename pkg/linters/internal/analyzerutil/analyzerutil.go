// Package analyzerutil provides shared analyzer setup for custom linters.
package analyzerutil

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"

	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

const repositoryURL = "https://github.com/github/gh-aw/tree/main/pkg/linters/"

// New creates an analyzer with the standard linter dependencies and URL.
// Additional shared fact/result analyzers (for example parentmap.Analyzer)
// can be listed in requires.
func New(name, doc string, run func(*analysis.Pass) (any, error), requires ...*analysis.Analyzer) *analysis.Analyzer {
	return NewAtPath(name, doc, name, run, requires...)
}

// NewAtPath creates an analyzer with the standard linter dependencies and URL
// for packagePath.
func NewAtPath(name, doc, packagePath string, run func(*analysis.Pass) (any, error), requires ...*analysis.Analyzer) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name:     name,
		Doc:      doc,
		URL:      repositoryURL + packagePath,
		Requires: append([]*analysis.Analyzer{inspect.Analyzer, nolint.Analyzer, filecheck.Analyzer}, requires...),
		Run:      run,
	}
}

// Indexes returns the nolint directive index and the generated-file index for
// pass. Both indexes are guaranteed to be available for analyzers created with
// New or NewAtPath, which declare the producing analyzers in Requires.
func Indexes(pass *analysis.Pass) (nolint.DirectiveIndex, filecheck.GeneratedIndex, error) {
	noLintIndex, err := nolint.Index(pass)
	if err != nil {
		return nil, nil, err
	}
	generatedFiles, err := filecheck.Index(pass)
	if err != nil {
		return nil, nil, err
	}
	return noLintIndex, generatedFiles, nil
}

// Preorder runs fn for each node matching nodeFilter.
func Preorder(pass *analysis.Pass, nodeFilter []ast.Node, fn func(ast.Node)) (any, error) {
	insp, err := astutil.Inspector(pass)
	if err != nil {
		return nil, err
	}
	for cur := range insp.Root().Preorder(nodeFilter...) {
		fn(cur.Node())
	}
	return nil, nil
}
