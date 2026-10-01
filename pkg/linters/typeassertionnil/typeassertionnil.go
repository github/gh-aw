// Package typeassertionnil implements a Go analysis linter that flags
// type assertions to pointer types without an ok-check before dereferencing,
// which can cause nil pointer panics.
package typeassertionnil

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

// Analyzer is the type-assertion-nil analysis pass.
var Analyzer = analyzerutil.New("typeassertionnil", "reports type assertions to pointer types without ok-check before dereferencing", run)

func run(pass *analysis.Pass) (any, error) {
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	// Build a parent map for each file to detect context (assignments, returns, etc.)
	fileParents := make(map[*ast.File]map[ast.Node]ast.Node)
	for _, f := range pass.Files {
		fileParents[f] = astutil.BuildParentMap(f)
	}

	nodeFilter := []ast.Node{
		(*ast.TypeAssertExpr)(nil),
	}

	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		analyzeTypeAssert(pass, n, noLintIndex, generatedFiles, fileParents)
	})
}

// analyzeTypeAssert checks if a type assertion is to a pointer type without ok-check
func analyzeTypeAssert(pass *analysis.Pass, n ast.Node, noLintIndex nolint.DirectiveIndex, generatedFiles filecheck.GeneratedIndex, fileParents map[*ast.File]map[ast.Node]ast.Node) {
	typeAssert, ok := n.(*ast.TypeAssertExpr)
	if !ok {
		return
	}

	// Type-switch guards have nil Type; skip them.
	if typeAssert.Type == nil {
		return
	}

	pos := pass.Fset.PositionFor(typeAssert.Pos(), false)
	if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
		return
	}
	if nolint.HasDirectiveForLinter(pos, noLintIndex, "typeassertionnil") {
		return
	}

	// Check if the asserted type is a pointer type (e.g., *SomeType)
	_, isPointerType := typeAssert.Type.(*ast.StarExpr)
	if !isPointerType {
		return
	}

	// Get the type information to determine if it's truly a pointer
	t := pass.TypesInfo.TypeOf(typeAssert.Type)
	if t == nil {
		return
	}
	if _, isPtr := t.(*types.Pointer); !isPtr {
		return
	}

	// Find the parent map for the file containing this node
	f := astutil.FileForPos(pass.Files, typeAssert.Pos())
	var parents map[ast.Node]ast.Node
	if f != nil {
		parents = fileParents[f]
	}

	// Skip the safe two-value form: v, ok := x.(*T) or v, ok = x.(*T)
	if parents != nil {
		if isSafeTwoValueAssertion(typeAssert, parents) {
			return
		}
	}

	// Flag the unchecked type assertion to a pointer type
	typeName := getTypeName(typeAssert.Type)
	pass.ReportRangef(
		typeAssert,
		"type assertion to %s without ok-check; use the two-value form v, ok := ... to safely check if the assertion succeeds",
		typeName,
	)
}

// isSafeTwoValueAssertion checks if the type assertion uses the safe two-value form
func isSafeTwoValueAssertion(typeAssert *ast.TypeAssertExpr, parents map[ast.Node]ast.Node) bool {
	_, isTwoValue := astutil.TwoValueTypeAssertionOKIdent(typeAssert, parents)
	return isTwoValue
}

// getTypeName returns a string representation of a type node
func getTypeName(typeNode ast.Expr) string {
	if star, ok := typeNode.(*ast.StarExpr); ok {
		if ident, ok := star.X.(*ast.Ident); ok {
			return "*" + ident.Name
		}
		if sel, ok := star.X.(*ast.SelectorExpr); ok {
			if pkg, ok := sel.X.(*ast.Ident); ok {
				return "*" + pkg.Name + "." + sel.Sel.Name
			}
		}
	}
	return "pointer type"
}
