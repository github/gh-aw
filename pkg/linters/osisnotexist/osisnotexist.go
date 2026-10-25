// Package osisnotexist reports legacy os error predicates that do not inspect
// arbitrary wrapped errors.
package osisnotexist

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

// Analyzer is the osisnotexist analysis pass.
var Analyzer = analyzerutil.New("osisnotexist", "reports legacy os error predicates that should use wrapped-error-aware checks", run)

func run(pass *analysis.Pass) (any, error) {
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}
	root, err := astutil.Root(pass)
	if err != nil {
		return nil, err
	}
	for cur := range root.Preorder((*ast.CallExpr)(nil)) {
		call, ok := cur.Node().(*ast.CallExpr)
		if !ok {
			continue
		}
		pos := pass.Fset.PositionFor(call.Pos(), false)
		if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) ||
			nolint.HasDirectiveForLinter(pos, noLintIndex, "osisnotexist") {
			continue
		}
		pkgPath, name, ok := astutil.PackageCall(pass, call)
		if !ok || pkgPath != "os" {
			continue
		}
		var replacement string
		switch name {
		case "IsNotExist":
			replacement = "errors.Is(err, fs.ErrNotExist)"
		case "IsExist":
			replacement = "errors.Is(err, fs.ErrExist)"
		case "IsPermission":
			replacement = "errors.Is(err, fs.ErrPermission)"
		case "IsTimeout":
		default:
			continue
		}
		if checksDirectOSError(pass, cur, call) {
			continue
		}
		if name == "IsTimeout" {
			pass.ReportRangef(call, "os.IsTimeout does not inspect arbitrary wrapped errors; use errors.As with interface{ Timeout() bool } and check Timeout(); errors.Is(err, os.ErrDeadlineExceeded) only covers deadline errors")
			continue
		}
		pass.ReportRangef(call, "os.%s does not inspect arbitrary wrapped errors; use %s instead", name, replacement)
	}
	return nil, nil
}

// checksDirectOSError only follows adjacent statements and enclosing ifs, never
// crossing a closure, loop, or intervening statement.
func checksDirectOSError(pass *analysis.Pass, cur inspector.Cursor, call *ast.CallExpr) bool {
	if len(call.Args) != 1 {
		return false
	}
	argExpr, ok := astutil.RhsExprForIndex(call.Args, 0)
	if !ok {
		return false
	}
	arg, ok := astutil.UnwrapParenExpr(argExpr).(*ast.Ident)
	if !ok {
		return false
	}
	obj := pass.TypesInfo.ObjectOf(arg)
	if obj == nil {
		return false
	}
	for parent := cur.Parent(); parent.Node() != nil; parent = parent.Parent() {
		switch node := parent.Node().(type) {
		case *ast.BlockStmt:
			if prev, ok := cur.PrevSibling(); ok {
				source, ok := prev.Node().(ast.Stmt)
				if !ok {
					return false
				}
				return directOSErrorAssignment(pass, source, obj) &&
					!hasInterveningEffect(pass, cur.Node(), source.End(), call.Pos(), obj)
			}
		case *ast.IfStmt:
			if node.Init != nil {
				return directOSErrorAssignment(pass, node.Init, obj) &&
					!hasInterveningEffect(pass, node, node.Init.End(), call.Pos(), obj)
			}
		case *ast.FuncLit, *ast.FuncDecl:
			return false
		case *ast.ExprStmt, *ast.AssignStmt, *ast.ReturnStmt:
		default:
			if _, ok := node.(ast.Stmt); ok {
				return false
			}
		}
		cur = parent
	}
	return false
}

func directOSErrorAssignment(pass *analysis.Pass, stmt ast.Stmt, obj types.Object) bool {
	assign, ok := stmt.(*ast.AssignStmt)
	if !ok || assign.Tok != token.DEFINE || len(assign.Rhs) != 1 || len(assign.Lhs) == 0 {
		return false
	}
	// A fresh binding cannot have escaped or been captured before this assignment.
	var last ast.Expr
	for _, expr := range assign.Lhs {
		last = expr
	}
	lhs, ok := astutil.UnwrapParenExpr(last).(*ast.Ident)
	if !ok || pass.TypesInfo.Defs[lhs] != obj {
		return false
	}
	rhs, ok := astutil.RhsExprForIndex(assign.Rhs, 0)
	if !ok {
		return false
	}
	call, ok := astutil.UnwrapParenExpr(rhs).(*ast.CallExpr)
	if !ok {
		return false
	}
	if fun, ok := astutil.UnwrapParenExpr(call.Fun).(*ast.Ident); ok {
		if _, ok := pass.TypesInfo.Uses[fun].(*types.Func); !ok {
			return false // Do not follow function-value aliases for the error's origin.
		}
	}
	pkgPath, name, ok := astutil.PackageCall(pass, call)
	if !ok || pkgPath != "os" {
		return false
	}
	// These functions return an error last and cannot wrap caller-provided errors.
	switch name {
	case "Stat", "Lstat", "Open", "OpenFile", "Create", "ReadFile", "WriteFile", "ReadDir",
		"Mkdir", "MkdirAll", "MkdirTemp", "CreateTemp", "Remove", "RemoveAll",
		"Rename", "Link", "Symlink", "Readlink", "Chmod", "Chown", "Lchown",
		"Truncate", "Chtimes", "Chdir", "Getwd":
		return true
	}
	return false
}

func hasInterveningEffect(pass *analysis.Pass, node ast.Node, start, end token.Pos, obj types.Object) bool {
	effect := false
	var skippedBody *ast.BlockStmt
	ast.Inspect(node, func(node ast.Node) bool {
		if node == nil || node == skippedBody || effect || node.End() <= start || node.Pos() >= end {
			return false
		}
		switch node := node.(type) {
		case *ast.IfStmt:
			if node.Else != nil && node.Else.Pos() <= end && end < node.Else.End() {
				// Its condition executes, but its then-body cannot reach this check.
				skippedBody = node.Body
			}
		case *ast.CallExpr, *ast.FuncLit:
			// Any completed call, closure, or address-of before the predicate may mutate obj.
			effect = node.End() <= end
		case *ast.UnaryExpr:
			effect = node.Op == token.AND && node.End() <= end
		case *ast.AssignStmt:
			for _, lhs := range node.Lhs {
				if ident, ok := astutil.UnwrapParenExpr(lhs).(*ast.Ident); ok && pass.TypesInfo.ObjectOf(ident) == obj {
					effect = true
				}
			}
		}
		return !effect
	})
	return effect
}
