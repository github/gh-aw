// Package prefercutprefix implements a Go analysis linter that flags
// strings.HasPrefix/HasSuffix guards followed by matching TrimPrefix/TrimSuffix
// calls that can use CutPrefix/CutSuffix instead.
package prefercutprefix

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

const linterName = "prefercutprefix"

// Analyzer is the prefer-cut-prefix analysis pass.
var Analyzer = analyzerutil.New(linterName, "reports strings.HasPrefix/HasSuffix guards followed by matching TrimPrefix/TrimSuffix calls; use strings.CutPrefix/CutSuffix", run)

type guard struct {
	trimMethod string
	value      ast.Expr
	affix      ast.Expr
}

func run(pass *analysis.Pass) (any, error) {
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	return analyzerutil.Preorder(pass, []ast.Node{(*ast.IfStmt)(nil)}, func(n ast.Node) {
		stmt, ok := n.(*ast.IfStmt)
		if !ok {
			return
		}
		analyzeIf(pass, stmt, generatedFiles, noLintIndex)
	})
}

func analyzeIf(pass *analysis.Pass, stmt *ast.IfStmt, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) {
	matchedGuard, ok := matchGuard(pass, stmt.Cond)
	if !ok {
		return
	}

	rootObjects := make(map[types.Object]struct{})
	for _, expr := range []ast.Expr{matchedGuard.value, matchedGuard.affix} {
		if root := supportedRoot(expr); root != nil {
			if obj := pass.TypesInfo.ObjectOf(root); obj != nil {
				rootObjects[obj] = struct{}{}
			}
		}
	}
	if reassignsRoot(pass, stmt.Body, rootObjects) {
		return
	}

	ast.Inspect(stmt.Body, func(n ast.Node) bool {
		if n == nil {
			return false
		}
		if _, isFuncLit := n.(*ast.FuncLit); isFuncLit {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		call, ok = astutil.AsStringsMethodCall(pass, call, matchedGuard.trimMethod)
		if !ok || len(call.Args) != 2 ||
			!sameOperand(pass, matchedGuard.value, call.Args[0]) ||
			!sameOperand(pass, matchedGuard.affix, call.Args[1]) {
			return true
		}

		pos := pass.Fset.PositionFor(call.Pos(), false)
		if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) ||
			nolint.HasDirectiveForLinter(pos, noLintIndex, linterName) {
			return true
		}
		cutMethod := "CutPrefix"
		if matchedGuard.trimMethod == "TrimSuffix" {
			cutMethod = "CutSuffix"
		}
		pass.Report(analysis.Diagnostic{
			Pos:     call.Pos(),
			End:     call.End(),
			Message: "strings." + matchedGuard.trimMethod + " inside a strings.HasPrefix/HasSuffix guard can be replaced with strings." + cutMethod,
		})
		return true
	})
}

func matchGuard(pass *analysis.Pass, expr ast.Expr) (guard, bool) {
	expr = astutil.UnwrapParenExpr(expr)
	if call, ok := astutil.AsStringsMethodCall(pass, expr, "HasPrefix"); ok &&
		len(call.Args) == 2 && supportedOperand(pass, call.Args[0]) && supportedOperand(pass, call.Args[1]) {
		return guard{trimMethod: "TrimPrefix", value: call.Args[0], affix: call.Args[1]}, true
	}
	if call, ok := astutil.AsStringsMethodCall(pass, expr, "HasSuffix"); ok &&
		len(call.Args) == 2 && supportedOperand(pass, call.Args[0]) && supportedOperand(pass, call.Args[1]) {
		return guard{trimMethod: "TrimSuffix", value: call.Args[0], affix: call.Args[1]}, true
	}
	return guard{}, false
}

func supportedOperand(pass *analysis.Pass, expr ast.Expr) bool {
	switch expr := astutil.UnwrapParenExpr(expr).(type) {
	case *ast.Ident:
		return pass.TypesInfo.ObjectOf(expr) != nil
	case *ast.SelectorExpr:
		return supportedSelector(pass, expr)
	case *ast.BasicLit:
		return true
	default:
		return false
	}
}

func supportedSelector(pass *analysis.Pass, expr *ast.SelectorExpr) bool {
	if expr == nil || pass.TypesInfo.Uses[expr.Sel] == nil {
		return false
	}
	switch base := astutil.UnwrapParenExpr(expr.X).(type) {
	case *ast.Ident:
		return pass.TypesInfo.ObjectOf(base) != nil
	case *ast.SelectorExpr:
		return supportedSelector(pass, base)
	default:
		return false
	}
}

func supportedRoot(expr ast.Expr) *ast.Ident {
	switch expr := astutil.UnwrapParenExpr(expr).(type) {
	case *ast.Ident:
		return expr
	case *ast.SelectorExpr:
		return supportedRoot(expr.X)
	default:
		return nil
	}
}

func sameOperand(pass *analysis.Pass, left, right ast.Expr) bool {
	left = astutil.UnwrapParenExpr(left)
	right = astutil.UnwrapParenExpr(right)
	switch left := left.(type) {
	case *ast.Ident:
		other, ok := right.(*ast.Ident)
		return ok && pass.TypesInfo.ObjectOf(left) != nil &&
			pass.TypesInfo.ObjectOf(left) == pass.TypesInfo.ObjectOf(other)
	case *ast.SelectorExpr:
		other, ok := right.(*ast.SelectorExpr)
		return ok && left.Sel.Name == other.Sel.Name && sameOperand(pass, left.X, other.X)
	case *ast.BasicLit:
		other, ok := right.(*ast.BasicLit)
		return ok && left.Kind == other.Kind && left.Value == other.Value
	default:
		return false
	}
}

func reassignsRoot(pass *analysis.Pass, body *ast.BlockStmt, roots map[types.Object]struct{}) bool {
	if len(roots) == 0 {
		return false
	}
	reassigned := false
	ast.Inspect(body, func(n ast.Node) bool {
		if n == nil {
			return false
		}
		switch n := n.(type) {
		case *ast.AssignStmt:
			for _, lhs := range n.Lhs {
				reassigned = reassigned || isRootObject(pass, lhs, roots)
			}
		case *ast.IncDecStmt:
			reassigned = isRootObject(pass, n.X, roots)
		case *ast.RangeStmt:
			if n.Tok == token.ASSIGN {
				reassigned = isRootObject(pass, n.Key, roots) || isRootObject(pass, n.Value, roots)
			}
		}
		return !reassigned
	})
	return reassigned
}

func isRootObject(pass *analysis.Pass, expr ast.Expr, roots map[types.Object]struct{}) bool {
	root := assignmentRoot(expr)
	if root == nil {
		return false
	}
	_, ok := roots[pass.TypesInfo.ObjectOf(root)]
	return ok
}

func assignmentRoot(expr ast.Expr) *ast.Ident {
	switch expr := astutil.UnwrapParenExpr(expr).(type) {
	case *ast.Ident:
		return expr
	case *ast.SelectorExpr:
		return assignmentRoot(expr.X)
	case *ast.IndexExpr:
		return assignmentRoot(expr.X)
	case *ast.IndexListExpr:
		return assignmentRoot(expr.X)
	case *ast.StarExpr:
		return assignmentRoot(expr.X)
	default:
		return nil
	}
}
