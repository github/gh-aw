// Package osopenreadallpreferreadfile reports files opened only to read their contents.
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

// Analyzer is the os.Open/ReadAll simplification analysis pass.
var Analyzer = analyzerutil.New(linterName, "reports os.Open followed only by deferred Close and a single ReadAll; prefer os.ReadFile", run)

const linterName = "osopenreadallpreferreadfile"

func run(pass *analysis.Pass) (any, error) {
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	uses := make(map[types.Object][]*ast.Ident)
	for ident, obj := range pass.TypesInfo.Uses {
		uses[obj] = append(uses[obj], ident)
	}
	return analyzerutil.Preorder(pass, []ast.Node{(*ast.BlockStmt)(nil)}, func(n ast.Node) {
		block, ok := n.(*ast.BlockStmt)
		if !ok {
			return
		}
		for i, stmt := range block.List {
			assign, ok := stmt.(*ast.AssignStmt)
			if !ok || assign.Tok != token.DEFINE || len(assign.Lhs) != 2 || len(assign.Rhs) != 1 {
				continue
			}
			file, ok := assign.Lhs[0].(*ast.Ident) //nolint:uncheckedsliceindex // len(assign.Lhs) == 2 is checked above.
			if !ok || file.Name == "_" {
				continue
			}
			obj := pass.TypesInfo.Defs[file]
			if obj == nil || len(uses[obj]) != 2 {
				continue
			}
			open, ok := astutil.UnwrapParenExpr(assign.Rhs[0]).(*ast.CallExpr) //nolint:uncheckedsliceindex // len(assign.Rhs) == 1 is checked above.
			if !ok || !isCall(pass, open, "os", "Open") {
				continue
			}
			if !onlyCloseAndRead(pass, block.List[i+1:], obj, uses[obj]) {
				continue
			}
			pos := pass.Fset.PositionFor(open.Pos(), false)
			if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) ||
				nolint.HasDirectiveForLinter(pos, noLintIndex, linterName) {
				continue
			}
			pass.ReportRangef(open, "use os.ReadFile instead of os.Open followed by deferred Close and ReadAll when the file has no other use")
		}
	})
}

func isCall(pass *analysis.Pass, call *ast.CallExpr, pkg, name string) bool {
	pkgPath, funcName, ok := astutil.PackageCall(pass, call)
	return ok && pkgPath == pkg && funcName == name
}

func fileIdent(pass *analysis.Pass, expr ast.Expr, obj types.Object) *ast.Ident {
	ident, ok := astutil.UnwrapParenExpr(expr).(*ast.Ident)
	if !ok || pass.TypesInfo.ObjectOf(ident) != obj {
		return nil
	}
	return ident
}

func onlyCloseAndRead(pass *analysis.Pass, stmts []ast.Stmt, obj types.Object, uses []*ast.Ident) bool {
	var closeUse, readUse *ast.Ident
	for _, stmt := range stmts {
		if deferred, ok := stmt.(*ast.DeferStmt); ok {
			sel, ok := astutil.UnwrapParenExpr(deferred.Call.Fun).(*ast.SelectorExpr)
			if ok && sel.Sel.Name == "Close" && len(deferred.Call.Args) == 0 {
				if ident := fileIdent(pass, sel.X, obj); ident != nil {
					if closeUse != nil || readUse != nil {
						return false
					}
					closeUse = ident
				}
			}
			continue
		}

		var exprs []ast.Expr
		switch stmt := stmt.(type) {
		case *ast.AssignStmt:
			exprs = stmt.Rhs
		case *ast.ReturnStmt:
			exprs = stmt.Results
		case *ast.ExprStmt:
			exprs = []ast.Expr{stmt.X}
		}
		for _, expr := range exprs {
			call, ok := astutil.UnwrapParenExpr(expr).(*ast.CallExpr)
			if !ok || len(call.Args) != 1 ||
				(!isCall(pass, call, "io", "ReadAll") && !isCall(pass, call, "io/ioutil", "ReadAll")) {
				continue
			}
			if ident := fileIdent(pass, call.Args[0], obj); ident != nil { //nolint:uncheckedsliceindex // len(call.Args) == 1 is checked above.
				if readUse != nil || closeUse == nil {
					return false
				}
				readUse = ident
			}
		}
	}
	if closeUse == nil || readUse == nil {
		return false
	}
	for _, use := range uses {
		if use != closeUse && use != readUse {
			return false
		}
	}
	return true
}
