// Package osopenreadallpreferreadfile implements a Go analysis linter that
// flags os.Open + deferred Close + a single io.ReadAll that should be os.ReadFile.
//
// Only the read half is implemented. The write half (os.Create followed by a
// single Write/WriteString) is intentionally not reported: os.WriteFile uses a
// caller-supplied permission and the explicit Close error is part of the write
// result, so a high-confidence rewrite is not possible.
package osopenreadallpreferreadfile

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

// Analyzer is the osopen-readall-prefer-readfile analysis pass.
var Analyzer = analyzerutil.NewAtPath(
	"osopenreadallpreferreadfile",
	"reports os.Open followed by a deferred Close and a single io.ReadAll of the file that should use os.ReadFile",
	"osopen-readall-prefer-readfile",
	run,
)

func run(pass *analysis.Pass) (any, error) {
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	// useCounts maps each variable to its number of identifier uses (excluding
	// the defining identifier), computed lazily over the whole package.
	useCounts := make(map[types.Object]int)
	for _, obj := range pass.TypesInfo.Uses {
		useCounts[obj]++
	}

	nodeFilter := []ast.Node{(*ast.BlockStmt)(nil), (*ast.CaseClause)(nil), (*ast.CommClause)(nil)}
	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		var stmts []ast.Stmt
		switch b := n.(type) {
		case *ast.BlockStmt:
			stmts = b.List
		case *ast.CaseClause:
			stmts = b.Body
		case *ast.CommClause:
			stmts = b.Body
		}
		checkStmts(pass, stmts, useCounts, generatedFiles, noLintIndex)
	})
}

func checkStmts(pass *analysis.Pass, stmts []ast.Stmt, useCounts map[types.Object]int, generated filecheck.GeneratedIndex, noLint nolint.DirectiveIndex) {
	for i, stmt := range stmts {
		openCall, fileObj, errObj := matchOpen(pass, stmt)
		if openCall == nil {
			continue
		}
		pos := pass.Fset.PositionFor(openCall.Pos(), false)
		if filecheck.ShouldSkipFilename(pos.Filename, generated) {
			continue
		}

		next := i + 1
		// Optional immediate `if err != nil { ... }` check with no else/init.
		if next < len(stmts) && isErrCheck(pass, stmts[next], errObj) { //nolint:uncheckedsliceindex // bounds checked
			next++
		}
		if next+1 >= len(stmts) {
			continue
		}
		if !isDeferClose(pass, stmts[next], fileObj) { //nolint:uncheckedsliceindex // bounds checked
			continue
		}
		if !isReadAllStmt(pass, stmts[next+1], fileObj) { //nolint:uncheckedsliceindex // bounds checked
			continue
		}
		// The only uses of the file must be Close and ReadAll.
		if useCounts[fileObj] != 2 {
			continue
		}
		if nolint.HasDirectiveForLinter(pos, noLint, "osopenreadallpreferreadfile") {
			continue
		}
		pass.Reportf(openCall.Pos(), "os.Open followed by deferred Close and a single io.ReadAll of the file; use os.ReadFile instead")
	}
}

// matchOpen matches `f, err := os.Open(...)` (or `=`/`var`-free assignment) and
// returns the call, file object and error object (may be nil for `_`).
func matchOpen(pass *analysis.Pass, stmt ast.Stmt) (*ast.CallExpr, types.Object, types.Object) {
	assign, ok := stmt.(*ast.AssignStmt)
	if !ok || len(assign.Lhs) != 2 || len(assign.Rhs) != 1 {
		return nil, nil, nil
	}
	call, ok := astutil.UnwrapParenExpr(assign.Rhs[0]).(*ast.CallExpr)
	if !ok {
		return nil, nil, nil
	}
	if pkg, name, ok := astutil.PackageCall(pass, call); !ok || pkg != "os" || name != "Open" {
		return nil, nil, nil
	}
	fileIdent, ok := astutil.UnwrapParenExpr(assign.Lhs[0]).(*ast.Ident)
	if !ok || fileIdent.Name == "_" {
		return nil, nil, nil
	}
	fileObj := pass.TypesInfo.ObjectOf(fileIdent)
	if fileObj == nil {
		return nil, nil, nil
	}
	var errObj types.Object
	if errIdent, ok := astutil.UnwrapParenExpr(assign.Lhs[1]).(*ast.Ident); ok && errIdent.Name != "_" {
		errObj = pass.TypesInfo.ObjectOf(errIdent)
	}
	return call, fileObj, errObj
}

// isErrCheck matches `if err != nil { ... }` without init or else.
func isErrCheck(pass *analysis.Pass, stmt ast.Stmt, errObj types.Object) bool {
	ifStmt, ok := stmt.(*ast.IfStmt)
	if !ok || ifStmt.Init != nil || ifStmt.Else != nil || errObj == nil {
		return false
	}
	bin, ok := astutil.UnwrapParenExpr(ifStmt.Cond).(*ast.BinaryExpr)
	if !ok || bin.Op != token.NEQ {
		return false
	}
	id, ok := astutil.UnwrapParenExpr(bin.X).(*ast.Ident)
	if !ok || pass.TypesInfo.ObjectOf(id) != errObj {
		return false
	}
	nilIdent, ok := astutil.UnwrapParenExpr(bin.Y).(*ast.Ident)
	return ok && nilIdent.Name == "nil"
}

// isDeferClose matches `defer f.Close()`.
func isDeferClose(pass *analysis.Pass, stmt ast.Stmt, fileObj types.Object) bool {
	def, ok := stmt.(*ast.DeferStmt)
	if !ok || len(def.Call.Args) != 0 {
		return false
	}
	sel, ok := astutil.UnwrapParenExpr(def.Call.Fun).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Close" {
		return false
	}
	return isIdentFor(pass, sel.X, fileObj)
}

// isReadAllStmt matches a statement that directly assigns or returns the
// result of io.ReadAll(f) / ioutil.ReadAll(f).
func isReadAllStmt(pass *analysis.Pass, stmt ast.Stmt, fileObj types.Object) bool {
	var exprs []ast.Expr
	switch s := stmt.(type) {
	case *ast.AssignStmt:
		exprs = s.Rhs
	case *ast.ReturnStmt:
		exprs = s.Results
	default:
		return false
	}
	if len(exprs) != 1 {
		return false
	}
	call, ok := astutil.UnwrapParenExpr(exprs[0]).(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return false
	}
	pkg, name, ok := astutil.PackageCall(pass, call)
	if !ok || name != "ReadAll" || (pkg != "io" && pkg != "io/ioutil") {
		return false
	}
	return isIdentFor(pass, call.Args[0], fileObj)
}

func isIdentFor(pass *analysis.Pass, expr ast.Expr, obj types.Object) bool {
	id, ok := astutil.UnwrapParenExpr(expr).(*ast.Ident)
	return ok && pass.TypesInfo.ObjectOf(id) == obj
}
