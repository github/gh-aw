// Package parentmap provides a shared analyzer that maps every AST node in a
// package to its direct parent, so analyzers that need parent context reuse a
// single traversal per package instead of each rebuilding the map.
package parentmap

import (
	"fmt"
	"go/ast"
	"reflect"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/astutil"
)

// Index maps each file in a package to its node-to-parent map. Callers must
// treat the maps as read-only because the result is shared across analyzers.
type Index map[*ast.File]map[ast.Node]ast.Node

// Analyzer builds the shared parent index once per package.
var Analyzer = &analysis.Analyzer{
	Name:             "parentmapindex",
	Doc:              "indexes AST node parents for gh-aw custom linters",
	ResultType:       reflect.TypeFor[Index](),
	RunDespiteErrors: true,
	Run: func(pass *analysis.Pass) (any, error) {
		idx := make(Index, len(pass.Files))
		for _, file := range pass.Files {
			idx[file] = astutil.BuildParentMap(file)
		}
		return idx, nil
	},
}

// Of returns the shared parent index for pass.
func Of(pass *analysis.Pass) (Index, error) {
	idx, ok := pass.ResultOf[Analyzer].(Index)
	if !ok {
		return nil, fmt.Errorf("parent map analyzer result has unexpected type %T", pass.ResultOf[Analyzer])
	}
	return idx, nil
}
