// Package readallunboundedhttpbody implements a Go analysis linter that flags
// io.ReadAll calls reading directly from an HTTP response Body without an
// io.LimitReader bound, which can exhaust memory on large or hostile responses.
package readallunboundedhttpbody

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

// Analyzer is the readall-unbounded-http-body analysis pass.
var Analyzer = analyzerutil.New("readallunboundedhttpbody", "reports io.ReadAll calls on an HTTP response Body that are not wrapped in io.LimitReader, since an unbounded read can exhaust memory", run)

func run(pass *analysis.Pass) (any, error) {
	root, err := astutil.Root(pass)
	if err != nil {
		return nil, err
	}
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}
	if pass.TypesInfo == nil {
		return nil, nil
	}

	for cur := range root.Preorder((*ast.CallExpr)(nil)) {
		call, ok := cur.Node().(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			continue
		}
		pos := pass.Fset.PositionFor(call.Pos(), false)
		if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
			continue
		}
		if nolint.HasDirectiveForLinter(pos, noLintIndex, "readallunboundedhttpbody") {
			continue
		}
		if !isIOReadAll(pass, call) {
			continue
		}
		sel, ok := astutil.UnwrapParenExpr(firstArg(call)).(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Body" || !isHTTPResponse(pass.TypesInfo.TypeOf(sel.X)) {
			continue
		}
		pass.Reportf(call.Pos(), "io.ReadAll on an HTTP response Body is unbounded; wrap it in io.LimitReader")
	}
	return nil, nil
}

func isIOReadAll(pass *analysis.Pass, call *ast.CallExpr) bool {
	sel, ok := astutil.UnwrapParenExpr(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return false
	}
	fn, ok := pass.TypesInfo.Uses[sel.Sel].(*types.Func)
	return ok && fn.Name() == "ReadAll" && fn.Pkg() != nil && fn.Pkg().Path() == "io"
}

func isHTTPResponse(t types.Type) bool {
	if t == nil {
		return false
	}
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	named, ok := t.(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj.Pkg() != nil && obj.Pkg().Path() == "net/http" && obj.Name() == "Response"
}

// firstArg returns the first call argument; callers guarantee one exists.
func firstArg(call *ast.CallExpr) ast.Expr {
	for _, a := range call.Args {
		return a
	}
	return nil
}
