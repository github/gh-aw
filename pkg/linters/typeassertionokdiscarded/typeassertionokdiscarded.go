// Package typeassertionokdiscarded implements a Go analysis linter that flags
// type assertions in the two-value form where the ok return is explicitly
// discarded via blank identifier, which can hide runtime panics.
package typeassertionokdiscarded

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

// Analyzer is the type-assertion-ok-discarded analysis pass.
var Analyzer = analyzerutil.New("typeassertionokdiscarded", "reports type assertions using the two-value form where the ok return is explicitly discarded via blank identifier, which can hide runtime panics", run)

func run(pass *analysis.Pass) (any, error) {
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	// Build a parent map for each file so we can detect two-value assignments.
	fileParents := make(map[*ast.File]map[ast.Node]ast.Node)
	for _, f := range pass.Files {
		fileParents[f] = buildParentMap(f)
	}

	nodeFilter := []ast.Node{
		(*ast.TypeAssertExpr)(nil),
	}

	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		inspectTypeAssertExpr(pass, noLintIndex, generatedFiles, fileParents, n)
	})
}

func inspectTypeAssertExpr(pass *analysis.Pass, noLintIndex nolint.DirectiveIndex, generatedFiles filecheck.GeneratedIndex, fileParents map[*ast.File]map[ast.Node]ast.Node, n ast.Node) {
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

	// Find the parent map for the file containing this node.
	f := astutil.FileForPos(pass.Files, typeAssert.Pos())
	var parents map[ast.Node]ast.Node
	if f != nil {
		parents = fileParents[f]
	}

	// Check if this is a two-value assignment where the ok is blank.
	if parents != nil {
		if isTwoValueBlankOkAssertion(typeAssert, parents) {
			if nolint.HasDirectiveForLinter(pos, noLintIndex, "typeassertionokdiscarded") {
				return
			}

			t := pass.TypesInfo.TypeOf(typeAssert.Type)
			if t == nil {
				return
			}

			pass.ReportRangef(
				typeAssert,
				"type assertion ok value is explicitly discarded with blank identifier; use single-value form x.(%s) or check the ok value instead",
				t,
			)
		}
	}
}

func isTwoValueBlankOkAssertion(typeAssert *ast.TypeAssertExpr, parents map[ast.Node]ast.Node) bool {
	parent := parents[typeAssert]
	for parent != nil {
		paren, ok := parent.(*ast.ParenExpr)
		if !ok {
			break
		}
		parent = parents[paren]
	}

	switch p := parent.(type) {
	case *ast.AssignStmt:
		// Check for two-value assignment where second value is blank identifier.
		if len(p.Lhs) == 2 && len(p.Rhs) == 1 {
			// The second LHS must be a blank identifier.
			if ident, ok := p.Lhs[1].(*ast.Ident); ok && ident.Name == "_" {
				return true
			}
		}
	case *ast.ValueSpec:
		// Check for two-value var/const declaration where second value is blank identifier.
		if len(p.Names) == 2 && len(p.Values) == 1 {
			// The second name must be a blank identifier.
			if p.Names[1].Name == "_" {
				return true
			}
		}
	}
	return false
}

// buildParentMap constructs a map from each AST node to its direct parent node.
func buildParentMap(root ast.Node) map[ast.Node]ast.Node {
	parents := make(map[ast.Node]ast.Node)
	var stack []ast.Node

	ast.Inspect(root, func(n ast.Node) bool {
		if n == nil {
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			return false
		}
		if len(stack) > 0 {
			parents[n] = stack[len(stack)-1]
		}
		stack = append(stack, n)
		return true
	})

	return parents
}
