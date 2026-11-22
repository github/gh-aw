// Package execcommandoutputerrdiscard implements a Go analysis linter that flags
// (*exec.Cmd).Output and CombinedOutput calls whose errors are ignored or
// handled without preserving the stderr available from *exec.ExitError.
package execcommandoutputerrdiscard

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

// Analyzer is the exec-command-output-err-discard analysis pass.
var Analyzer = analyzerutil.NewAtPath(
	"execcommandoutputerrdiscard",
	"reports exec.Command(...).Output() or CombinedOutput() errors that are ignored or logged without preserving stderr from *exec.ExitError, so command failures lose useful diagnostics",
	"exec-command-output-err-discard",
	run,
)

func run(pass *analysis.Pass) (any, error) {
	insp, err := astutil.Inspector(pass)
	if err != nil {
		return nil, err
	}
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	filter := []ast.Node{(*ast.AssignStmt)(nil), (*ast.ExprStmt)(nil)}
	for cur := range insp.Root().Preorder(filter...) {
		switch stmt := cur.Node().(type) {
		case *ast.ExprStmt:
			call, method, ok := outputCall(pass, stmt.X)
			if ok {
				report(pass, call, method, "result and error are ignored", generatedFiles, noLintIndex)
			}
		case *ast.AssignStmt:
			checkAssign(pass, cur, stmt, generatedFiles, noLintIndex)
		}
	}
	return nil, nil
}

func checkAssign(pass *analysis.Pass, cur inspector.Cursor, stmt *ast.AssignStmt, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) {
	if len(stmt.Rhs) != 1 || len(stmt.Lhs) != 2 {
		return
	}
	call, method, ok := outputCall(pass, stmt.Rhs[0])
	if !ok {
		return
	}
	outExpr := astutil.UnwrapParenExpr(stmt.Lhs[0])
	errIdent, ok := astutil.UnwrapParenExpr(stmt.Lhs[1]).(*ast.Ident)
	if !ok {
		return
	}
	if errIdent.Name == "_" {
		report(pass, call, method, "error is discarded", generatedFiles, noLintIndex)
		return
	}
	errObj := pass.TypesInfo.ObjectOf(errIdent)
	if errObj == nil {
		return
	}

	var body ast.Node
	for encl := range cur.Enclosing((*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)) {
		switch fn := encl.Node().(type) {
		case *ast.FuncDecl:
			body = fn.Body
		case *ast.FuncLit:
			body = fn.Body
		}
		break
	}
	if body == nil {
		return
	}

	if preservesStderr(pass, body, stmt, errObj) {
		return
	}
	if method == "CombinedOutput" && combinedOutputRetained(pass, body, outExpr) {
		return
	}
	report(pass, call, method, "error is handled without preserving stderr from *exec.ExitError", generatedFiles, noLintIndex)
}

func report(pass *analysis.Pass, call *ast.CallExpr, method, why string, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) {
	pos := pass.Fset.PositionFor(call.Pos(), false)
	if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
		return
	}
	if nolint.HasDirectiveForLinter(pos, noLintIndex, "execcommandoutputerrdiscard") {
		return
	}
	pass.Reportf(call.Pos(), "%s() %s; inspect *exec.ExitError.Stderr to keep command diagnostics", method, why)
}

// outputCall matches X.Output() / X.CombinedOutput() where X is *exec.Cmd.
func outputCall(pass *analysis.Pass, expr ast.Expr) (*ast.CallExpr, string, bool) {
	call, ok := astutil.UnwrapParenExpr(expr).(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return nil, "", false
	}
	sel, ok := astutil.UnwrapParenExpr(call.Fun).(*ast.SelectorExpr)
	if !ok || (sel.Sel.Name != "Output" && sel.Sel.Name != "CombinedOutput") {
		return nil, "", false
	}
	if !isNamed(pass.TypesInfo.TypeOf(sel.X), "Cmd") {
		return nil, "", false
	}
	return call, sel.Sel.Name, true
}

// isNamed reports whether t is (a pointer to) os/exec.<name>.
func isNamed(t types.Type, name string) bool {
	for t != nil {
		p, ok := t.(*types.Pointer)
		if !ok {
			break
		}
		t = p.Elem()
	}
	named, ok := t.(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj.Pkg() != nil && obj.Pkg().Path() == "os/exec" && obj.Name() == name
}

// preservesStderr reports whether body shows stderr retention for the call at
// stmt: the error is returned unchanged, or *exec.ExitError / Stderr is used.
func preservesStderr(pass *analysis.Pass, body ast.Node, stmt *ast.AssignStmt, errObj types.Object) bool {
	found := false
	isExit := func(e ast.Expr) bool { return e != nil && isNamed(pass.TypesInfo.TypeOf(e), "ExitError") }
	isErr := func(e ast.Expr) bool {
		id, ok := astutil.UnwrapParenExpr(e).(*ast.Ident)
		return ok && pass.TypesInfo.ObjectOf(id) == errObj
	}
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		switch x := n.(type) {
		case *ast.ReturnStmt:
			if x.Pos() <= stmt.End() {
				return true
			}
			for _, r := range x.Results {
				if isErr(r) {
					found = true
				}
			}
		case *ast.SelectorExpr:
			if x.Sel.Name == "Stderr" && (isNamed(pass.TypesInfo.TypeOf(x.X), "ExitError") || isNamed(pass.TypesInfo.TypeOf(x.X), "Cmd")) {
				found = true
			}
		case *ast.TypeAssertExpr:
			if isErr(x.X) && isExit(x.Type) {
				found = true
			}
		case *ast.TypeSwitchStmt:
			assign, ok := x.Assign.(*ast.AssignStmt)
			if !ok || len(assign.Rhs) != 1 {
				break
			}
			typeAssert, ok := astutil.UnwrapParenExpr(assign.Rhs[0]).(*ast.TypeAssertExpr)
			if !ok || !isErr(typeAssert.X) {
				break
			}
			for _, clauseNode := range x.Body.List {
				clause, ok := clauseNode.(*ast.CaseClause)
				if !ok {
					continue
				}
				for _, e := range clause.List {
					if isExit(e) {
						found = true
						break
					}
				}
			}
		case *ast.CallExpr:
			if sel, ok := astutil.UnwrapParenExpr(x.Fun).(*ast.SelectorExpr); ok && sel.Sel.Name == "As" && len(x.Args) == 2 && isErr(x.Args[0]) {
				if u, ok := astutil.UnwrapParenExpr(x.Args[1]).(*ast.UnaryExpr); ok && u.Op == token.AND && isExit(u.X) {
					found = true
				}
			}
		}
		return !found
	})
	return found
}

func combinedOutputRetained(pass *analysis.Pass, body ast.Node, outExpr ast.Expr) bool {
	id, ok := astutil.UnwrapParenExpr(outExpr).(*ast.Ident)
	if !ok || id.Name == "_" {
		return false
	}
	outObj := pass.TypesInfo.ObjectOf(id)
	if outObj == nil {
		return false
	}
	retained := false
	ast.Inspect(body, func(n ast.Node) bool {
		if retained {
			return false
		}
		use, ok := n.(*ast.Ident)
		if ok && use.Pos() != id.Pos() && pass.TypesInfo.ObjectOf(use) == outObj {
			retained = true
		}
		return !retained
	})
	return retained
}
