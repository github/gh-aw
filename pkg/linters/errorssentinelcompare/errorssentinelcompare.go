// Package errorssentinelcompare implements a Go analysis linter that flags
// direct ==/!= comparisons of an error value against a package-level sentinel
// error variable (such as io.ErrUnexpectedEOF or sql.ErrNoRows). Such
// comparisons fail once the error is wrapped; errors.Is should be used instead.
package errorssentinelcompare

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
	"github.com/github/gh-aw/pkg/logger"
)

var pkgLog = logger.New("linters:errorssentinelcompare")

// Analyzer is the errors-sentinel-compare analysis pass.
var Analyzer = analyzerutil.New("errorssentinelcompare", "reports ==/!= comparisons of an error value against a package-level sentinel error variable that should use errors.Is so wrapped errors still match", run)

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
	pkgLog.Printf("analyzing package %s", pass.Pkg.Path())

	for cur := range root.Preorder((*ast.BinaryExpr)(nil)) {
		expr, ok := cur.Node().(*ast.BinaryExpr)
		if !ok || (expr.Op != token.EQL && expr.Op != token.NEQ) {
			continue
		}
		pos := pass.Fset.PositionFor(expr.Pos(), false)
		if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
			continue
		}
		if nolint.HasDirectiveForLinter(pos, noLintIndex, "errorssentinelcompare") {
			continue
		}
		sentinel, ok := sentinelOperand(pass, expr)
		if !ok {
			continue
		}
		if insideIsMethod(cur) {
			continue
		}
		pass.ReportRangef(expr, "use errors.Is(err, %s) instead of comparing with %s so wrapped errors still match", sentinel, expr.Op)
	}
	return nil, nil
}

// sentinelOperand returns the printed name of a package-level error variable
// operand when the other operand is a non-nil error value.
func sentinelOperand(pass *analysis.Pass, expr *ast.BinaryExpr) (string, bool) {
	for _, pair := range [2][2]ast.Expr{{expr.X, expr.Y}, {expr.Y, expr.X}} {
		name, ok := sentinelName(pass, pair[0])
		if !ok || !isErrorValue(pass, pair[1]) {
			continue
		}
		return name, true
	}
	return "", false
}

// sentinelName reports whether e names a package-level variable of type error.
func sentinelName(pass *analysis.Pass, e ast.Expr) (string, bool) {
	var ident *ast.Ident
	var name string
	switch v := astutil.UnwrapParenExpr(e).(type) {
	case *ast.Ident:
		ident, name = v, v.Name
	case *ast.SelectorExpr:
		pkg, ok := v.X.(*ast.Ident)
		if !ok {
			return "", false
		}
		if _, isPkg := pass.TypesInfo.ObjectOf(pkg).(*types.PkgName); !isPkg {
			return "", false
		}
		ident, name = v.Sel, pkg.Name+"."+v.Sel.Name
	default:
		return "", false
	}
	obj, ok := pass.TypesInfo.ObjectOf(ident).(*types.Var)
	if !ok || obj.IsField() || obj.Pkg() == nil || obj.Parent() != obj.Pkg().Scope() {
		return "", false
	}
	if !types.Identical(obj.Type(), types.Universe.Lookup("error").Type()) {
		return "", false
	}
	// io.EOF is documented to be returned unwrapped and compared directly.
	if obj.Pkg().Path() == "io" && obj.Name() == "EOF" {
		return "", false
	}
	return name, true
}

// isErrorValue reports whether e is a non-nil expression of an interface type
// implementing error.
func isErrorValue(pass *analysis.Pass, e ast.Expr) bool {
	tv, ok := pass.TypesInfo.Types[astutil.UnwrapParenExpr(e)]
	if !ok || tv.IsNil() || tv.Type == nil {
		return false
	}
	if !types.IsInterface(tv.Type) {
		return false
	}
	return types.Implements(tv.Type, types.Universe.Lookup("error").Type().Underlying().(*types.Interface))
}

// insideIsMethod reports whether cur is within a method named Is, where direct
// comparison is the documented way to implement errors.Is matching.
func insideIsMethod(cur inspector.Cursor) bool {
	for enc := range cur.Enclosing((*ast.FuncDecl)(nil)) {
		fn, ok := enc.Node().(*ast.FuncDecl)
		return ok && fn.Recv != nil && fn.Name.Name == "Is"
	}
	return false
}
