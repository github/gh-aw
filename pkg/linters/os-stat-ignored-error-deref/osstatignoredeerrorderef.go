// Package osstatignoredeerrorderef implements a Go analysis linter that flags
// dereferences of os.Stat and os.Lstat results when their errors are discarded.
package osstatignoredeerrorderef

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

// Analyzer is the os-stat-ignored-error-deref analysis pass.
var Analyzer = analyzerutil.New("osstatignoredeerrorderef", "reports dereferences of os.Stat and os.Lstat results whose errors are discarded without a nil check", run)

func run(pass *analysis.Pass) (any, error) {
	nolintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	reported := make(map[token.Pos]struct{})
	for _, file := range pass.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			assign, ok := node.(*ast.AssignStmt)
			if !ok {
				return true
			}
			_, pkgPath, funcName, ok := astutil.MatchDiscardedErrorCall(pass, assign)
			if !ok || pkgPath != "os" || (funcName != "Stat" && funcName != "Lstat") {
				return true
			}
			if len(assign.Lhs) == 0 {
				return true
			}
			result, ok := astutil.UnwrapParenExpr(assign.Lhs[0]).(*ast.Ident)
			if !ok {
				return true
			}
			object := pass.TypesInfo.Defs[result]
			if object == nil {
				object = pass.TypesInfo.Uses[result]
			}
			if object == nil {
				return true
			}

			ast.Inspect(file, func(use ast.Node) bool {
				if use == nil || use.Pos() <= assign.End() || !dereferencesObject(pass, use, object) {
					return true
				}
				if guardedByNilCheck(pass, file, object, use) {
					return true
				}
				position := pass.Fset.PositionFor(use.Pos(), false)
				if filecheck.ShouldSkipFilename(position.Filename, generatedFiles) ||
					nolint.HasDirectiveForLinter(position, nolintIndex, "osstatignoredeerrorderef") {
					return true
				}
				if _, exists := reported[use.Pos()]; exists {
					return true
				}
				reported[use.Pos()] = struct{}{}
				pass.ReportRangef(use, "result of os.%s is dereferenced after its error is discarded without a nil check", funcName)
				return true
			})
			return true
		})
	}
	return nil, nil
}

func dereferencesObject(pass *analysis.Pass, node ast.Node, object types.Object) bool {
	switch node := node.(type) {
	case *ast.SelectorExpr:
		return refersToObject(pass, node.X, object)
	case *ast.UnaryExpr:
		return node.Op == token.MUL && refersToObject(pass, node.X, object)
	default:
		return false
	}
}

func refersToObject(pass *analysis.Pass, expr ast.Expr, object types.Object) bool {
	ident, ok := astutil.UnwrapParenExpr(expr).(*ast.Ident)
	if !ok {
		return false
	}
	return pass.TypesInfo.Uses[ident] == object || pass.TypesInfo.Defs[ident] == object
}

func guardedByNilCheck(pass *analysis.Pass, file *ast.File, object types.Object, use ast.Node) bool {
	guarded := false
	ast.Inspect(file, func(node ast.Node) bool {
		if guarded {
			return false
		}
		switch node := node.(type) {
		case *ast.IfStmt:
			op, ok := nilComparison(pass, node.Cond, object)
			if !ok {
				return true
			}
			if op == token.NEQ && nodeContains(node.Body, use) {
				guarded = true
			} else if op == token.EQL && node.Else != nil && nodeContains(node.Else, use) {
				guarded = true
			}
		case *ast.BlockStmt:
			for _, stmt := range node.List {
				if nodeContains(stmt, use) {
					break
				}
				guard, ok := stmt.(*ast.IfStmt)
				if !ok {
					continue
				}
				op, ok := nilComparison(pass, guard.Cond, object)
				if ok && op == token.EQL && terminatesBlock(guard.Body) {
					guarded = true
					break
				}
			}
		}
		return true
	})
	return guarded
}

func nilComparison(pass *analysis.Pass, expr ast.Expr, object types.Object) (token.Token, bool) {
	comparison, ok := astutil.UnwrapParenExpr(expr).(*ast.BinaryExpr)
	if !ok || (comparison.Op != token.EQL && comparison.Op != token.NEQ) {
		return token.ILLEGAL, false
	}
	left, leftIsIdent := astutil.UnwrapParenExpr(comparison.X).(*ast.Ident)
	right, rightIsIdent := astutil.UnwrapParenExpr(comparison.Y).(*ast.Ident)
	if leftIsIdent && refersToObject(pass, left, object) && rightIsIdent && right.Name == "nil" {
		return comparison.Op, true
	}
	if rightIsIdent && refersToObject(pass, right, object) && leftIsIdent && left.Name == "nil" {
		return comparison.Op, true
	}
	return token.ILLEGAL, false
}

func nodeContains(parent, child ast.Node) bool {
	return parent != nil && child.Pos() >= parent.Pos() && child.End() <= parent.End()
}

func terminatesBlock(block *ast.BlockStmt) bool {
	if block == nil || len(block.List) == 0 {
		return false
	}
	var last ast.Stmt
	for _, stmt := range block.List {
		last = stmt
	}
	return terminatesStatement(last)
}

func terminatesStatement(stmt ast.Stmt) bool {
	switch stmt := stmt.(type) {
	case *ast.ReturnStmt:
		return true
	case *ast.ExprStmt:
		call, ok := astutil.UnwrapParenExpr(stmt.X).(*ast.CallExpr)
		if !ok {
			return false
		}
		ident, ok := astutil.UnwrapParenExpr(call.Fun).(*ast.Ident)
		return ok && ident.Name == "panic"
	case *ast.IfStmt:
		return stmt.Else != nil && terminatesBlock(stmt.Body) && terminatesStatement(stmt.Else)
	case *ast.BlockStmt:
		return terminatesBlock(stmt)
	}
	return false
}
