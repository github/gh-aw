// Package osstatignorederrorderef implements a Go analysis linter that flags
// os.Stat and os.Lstat calls whose error is discarded and whose returned
// FileInfo is later dereferenced without a nil check.
package osstatignorederrorderef

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

// Analyzer is the os-stat-ignored-error-deref analysis pass.
var Analyzer = analyzerutil.NewAtPath(
	"osstatignorederrorderef",
	"reports when os.Stat or os.Lstat returns FileInfo with an ignored error and the FileInfo value is later dereferenced without a nil check, which can panic when the path is missing",
	"os-stat-ignored-error-deref",
	run,
)

func run(pass *analysis.Pass) (any, error) {
	nolintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}
	nodeFilter := []ast.Node{(*ast.BlockStmt)(nil), (*ast.CaseClause)(nil), (*ast.CommClause)(nil), (*ast.IfStmt)(nil)}
	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		switch s := n.(type) {
		case *ast.BlockStmt:
			checkList(pass, s.List, generatedFiles, nolintIndex)
		case *ast.CaseClause:
			checkList(pass, s.Body, generatedFiles, nolintIndex)
		case *ast.CommClause:
			checkList(pass, s.Body, generatedFiles, nolintIndex)
		case *ast.IfStmt:
			if s.Init == nil {
				return
			}
			rest := []ast.Node{s.Cond, s.Body}
			if s.Else != nil {
				rest = append(rest, s.Else)
			}
			checkAssign(pass, s.Init, rest, generatedFiles, nolintIndex)
		}
	})
}

func checkList(pass *analysis.Pass, list []ast.Stmt, generated filecheck.GeneratedIndex, idx nolint.DirectiveIndex) {
	for i, stmt := range list {
		rest := make([]ast.Node, 0, len(list)-i-1)
		for _, r := range list[i+1:] {
			rest = append(rest, r)
		}
		checkAssign(pass, stmt, rest, generated, idx)
	}
}

// checkAssign inspects stmt for "fi, _ := os.Stat(...)" and scans following
// nodes for an unguarded use of fi.
func checkAssign(pass *analysis.Pass, stmt ast.Stmt, rest []ast.Node, generated filecheck.GeneratedIndex, idx nolint.DirectiveIndex) {
	assign, ok := stmt.(*ast.AssignStmt)
	if !ok || len(assign.Lhs) != 2 || len(assign.Rhs) != 1 {
		return
	}
	if assign.Tok != token.DEFINE && assign.Tok != token.ASSIGN {
		return
	}
	fiIdent, ok := ast.Unparen(assign.Lhs[0]).(*ast.Ident)
	if !ok || fiIdent.Name == "_" {
		return
	}
	errIdent, ok := ast.Unparen(assign.Lhs[1]).(*ast.Ident)
	if !ok || errIdent.Name != "_" {
		return
	}
	call, ok := ast.Unparen(assign.Rhs[0]).(*ast.CallExpr)
	if !ok {
		return
	}
	fn, ok := typeutil.Callee(pass.TypesInfo, call).(*types.Func)
	if !ok || fn.Pkg() == nil || fn.Pkg().Path() != "os" || (fn.Name() != "Stat" && fn.Name() != "Lstat") {
		return
	}
	obj := pass.TypesInfo.ObjectOf(fiIdent)
	if obj == nil {
		return
	}
	use := findUnguardedUse(pass, obj, rest)
	if use == nil {
		return
	}
	position := pass.Fset.PositionFor(use.Pos(), false)
	if filecheck.ShouldSkipFilename(position.Filename, generated) {
		return
	}
	if nolint.HasDirectiveForLinter(position, idx, "osstatignorederrorderef") {
		return
	}
	pass.Reportf(use.Pos(), "%s from os.%s is used without a nil check but its error is ignored; a missing path makes it nil and this panics", fiIdent.Name, fn.Name())
}

// findUnguardedUse returns the first selector use of obj in nodes, stopping
// when a nil comparison or reassignment of obj is seen first.
func findUnguardedUse(pass *analysis.Pass, obj types.Object, nodes []ast.Node) ast.Node {
	isObj := func(e ast.Expr) bool {
		id, ok := ast.Unparen(e).(*ast.Ident)
		return ok && pass.TypesInfo.ObjectOf(id) == obj
	}
	for _, n := range nodes {
		var use ast.Node
		guarded := false
		ast.Inspect(n, func(x ast.Node) bool {
			if guarded || use != nil {
				return false
			}
			switch e := x.(type) {
			case *ast.BinaryExpr:
				if e.Op == token.EQL || e.Op == token.NEQ {
					if (isObj(e.X) && isNilIdent(pass, e.Y)) || (isObj(e.Y) && isNilIdent(pass, e.X)) {
						guarded = true
						return false
					}
				}
			case *ast.AssignStmt:
				for _, l := range e.Lhs {
					if isObj(l) {
						guarded = true
						return false
					}
				}
			case *ast.SelectorExpr:
				if isObj(e.X) {
					use = e
					return false
				}
			}
			return true
		})
		if use != nil {
			return use
		}
		if guarded {
			return nil
		}
	}
	return nil
}

func isNilIdent(pass *analysis.Pass, e ast.Expr) bool {
	id, ok := ast.Unparen(e).(*ast.Ident)
	if !ok {
		return false
	}
	_, isNil := pass.TypesInfo.ObjectOf(id).(*types.Nil)
	return isNil
}
