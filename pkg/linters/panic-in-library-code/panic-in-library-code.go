// Package panicinlibrarycode implements a Go analysis linter that flags
// panic() calls in library (pkg/) packages.
package panicinlibrarycode

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
	"github.com/github/gh-aw/pkg/logger"
)

var pkgLog = logger.New("linters:panicinlibrarycode")

// Analyzer is the panic-in-library-code analysis pass.
var Analyzer = analyzerutil.NewAtPath("panicinlibrarycode", "reports panic() calls in library code under pkg/ that should return errors instead", "panic-in-library-code", analyzePanicCalls)

func analyzePanicCalls(pass *analysis.Pass) (any, error) {
	insp, err := astutil.Inspector(pass)
	if err != nil {
		return nil, err
	}

	pkgPath := pass.Pkg.Path()
	// Skip packages under cmd/ entry-points — they are allowed to call panic.
	if pass.Pkg.Name() == "main" || strings.HasSuffix(pkgPath, "/main") || strings.Contains(pkgPath, "/cmd/") {
		pkgLog.Printf("skipping cmd/main package %s", pkgPath)
		return nil, nil
	}
	pkgLog.Printf("analyzing package %s", pkgPath)

	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	for cur := range insp.Root().Preorder((*ast.CallExpr)(nil)) {
		call, ok := cur.Node().(*ast.CallExpr)
		if !ok {
			continue
		}
		// Skip test files
		if strings.HasSuffix(pkgPath, ".test") || filecheck.ShouldSkipFilename(pass.Fset.Position(call.Pos()).Filename, generatedFiles) {
			continue
		}

		// Check if this is a call to the builtin panic function
		ident, ok := call.Fun.(*ast.Ident)
		if !ok || ident.Name != "panic" {
			continue
		}

		// Verify it's the builtin panic, not a user-defined function
		if obj := pass.TypesInfo.Uses[ident]; obj != nil {
			if _, ok := obj.(*types.Builtin); !ok {
				continue // Not the builtin panic
			}
		}

		if shouldSkipPanic(pass, call, cur) {
			continue
		}
		position := pass.Fset.PositionFor(call.Pos(), false)
		if nolint.HasDirectiveForLinter(position, noLintIndex, "panicinlibrarycode") {
			continue
		}

		pkgLog.Printf("flagging panic() call at %s", position)
		pass.ReportRangef(call, "avoid panic in library code; return an error instead")
	}

	return nil, nil
}

func shouldSkipPanic(pass *analysis.Pass, call *ast.CallExpr, cur inspector.Cursor) bool {
	return isInSyncOnceFuncLit(pass, cur) ||
		panicMessageStartsWithBUG(pass, call) ||
		astutil.IsInInitFunction(cur) ||
		hasDocumentedPanicContract(cur)
}

func isInSyncOnceFuncLit(pass *analysis.Pass, cur inspector.Cursor) bool {
	for encl := range cur.Enclosing((*ast.FuncLit)(nil)) {
		funcLit, ok := encl.Node().(*ast.FuncLit)
		if !ok {
			break
		}
		parent := encl.Parent()
		call, ok := parent.Node().(*ast.CallExpr)
		if !ok || !callArgsContainExpr(call.Args, funcLit) {
			continue
		}
		sel, ok := selectorExprFromCallFun(call.Fun)
		if !ok {
			continue
		}
		if isSyncOnceDoCall(pass, sel) || isSyncOnceConstructorCall(pass, sel) {
			return true
		}
	}
	return false
}

func selectorExprFromCallFun(fun ast.Expr) (*ast.SelectorExpr, bool) {
	switch f := fun.(type) {
	case *ast.SelectorExpr:
		return f, true
	case *ast.IndexExpr:
		sel, ok := f.X.(*ast.SelectorExpr)
		return sel, ok
	case *ast.IndexListExpr:
		sel, ok := f.X.(*ast.SelectorExpr)
		return sel, ok
	default:
		return nil, false
	}
}

func isSyncPackageFunc(pass *analysis.Pass, sel *ast.SelectorExpr, names ...string) bool {
	if !slices.Contains(names, sel.Sel.Name) {
		return false
	}
	obj := pass.TypesInfo.Uses[sel.Sel]
	if obj == nil || obj.Pkg() == nil {
		return false
	}
	return obj.Pkg().Path() == "sync" && slices.Contains(names, obj.Name())
}

func isSyncOnceDoCall(pass *analysis.Pass, sel *ast.SelectorExpr) bool {
	if sel.Sel.Name != "Do" {
		return false
	}
	return isSyncOnceType(pass.TypesInfo.TypeOf(sel.X))
}

func isSyncOnceConstructorCall(pass *analysis.Pass, sel *ast.SelectorExpr) bool {
	return isSyncPackageFunc(pass, sel, "OnceValue", "OnceFunc")
}

func callArgsContainExpr(args []ast.Expr, target ast.Expr) bool {
	return slices.Contains(args, target)
}

func isSyncOnceType(t types.Type) bool {
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}

	named, ok := t.(*types.Named)
	if !ok || named.Obj() == nil || named.Obj().Pkg() == nil {
		return false
	}

	return named.Obj().Pkg().Path() == "sync" && named.Obj().Name() == "Once"
}

func panicMessageStartsWithBUG(pass *analysis.Pass, call *ast.CallExpr) bool {
	if len(call.Args) == 0 {
		return false
	}

	prefix, ok := extractConstantStringPrefix(pass, call.Args[0])
	if !ok {
		return false
	}

	return strings.HasPrefix(strings.ToUpper(strings.TrimSpace(prefix)), "BUG:")
}

func extractConstantStringPrefix(pass *analysis.Pass, expr ast.Expr) (string, bool) {
	if tv, ok := pass.TypesInfo.Types[expr]; ok && tv.Value != nil && tv.Value.Kind() == constant.String {
		return constant.StringVal(tv.Value), true
	}

	switch e := expr.(type) {
	case *ast.BinaryExpr:
		if e.Op != token.ADD {
			return "", false
		}
		return extractConstantStringPrefix(pass, e.X)
	case *ast.CallExpr:
		if len(e.Args) == 0 {
			return "", false
		}
		// Only inspect the format argument of fmt.Sprintf to avoid false negatives
		// from arbitrary user functions that happen to receive a "BUG:" string.
		if !isFmtSprintf(pass, e) {
			return "", false
		}
		return extractConstantStringPrefix(pass, e.Args[0])
	default:
		return "", false
	}
}

// isFmtSprintf reports whether call is an invocation of the fmt.Sprintf function.
func isFmtSprintf(pass *analysis.Pass, call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Sprintf" {
		return false
	}
	if obj := pass.TypesInfo.Uses[sel.Sel]; obj != nil {
		return obj.Pkg() != nil && obj.Pkg().Path() == "fmt"
	}
	return false
}

func hasDocumentedPanicContract(cur inspector.Cursor) bool {
	for encl := range cur.Enclosing((*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)) {
		if _, isFuncLit := encl.Node().(*ast.FuncLit); isFuncLit {
			return false
		}
		decl, ok := encl.Node().(*ast.FuncDecl)
		if !ok {
			break
		}
		if decl.Doc != nil {
			doc := strings.ToLower(decl.Doc.Text())
			if strings.Contains(doc, "panics on") ||
				strings.Contains(doc, "panics if") ||
				strings.Contains(doc, "panic on") ||
				strings.Contains(doc, "panic if") {
				return true
			}
		}
		break // only check the immediate enclosing FuncDecl
	}
	return false
}
