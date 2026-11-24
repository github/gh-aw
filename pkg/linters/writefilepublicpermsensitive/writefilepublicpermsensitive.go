// Package writefilepublicpermsensitive detects public permissions on potentially sensitive writes.
package writefilepublicpermsensitive

import (
	"go/ast"
	"go/constant"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

const analyzerName = "writefilepublicpermsensitive"

var Analyzer = analyzerutil.New(analyzerName, "reports FilePermPublic on potentially sensitive paths or content", run)

func run(pass *analysis.Pass) (any, error) {
	nolintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	return analyzerutil.Preorder(pass, []ast.Node{(*ast.CallExpr)(nil)}, func(n ast.Node) {
		call := n.(*ast.CallExpr)
		fn, ok := astutil.CalledOSFunc(pass, call, "WriteFile", "OpenFile", "MkdirAll")
		if !ok {
			return
		}
		modeIndex := 2
		if fn.Name() == "MkdirAll" {
			modeIndex = 1
		}
		if len(call.Args) != modeIndex+1 || !isPublicPerm(pass, call.Args[modeIndex]) {
			return
		}
		sensitive := containsSensitive(pass, call.Args[0])
		if fn.Name() == "WriteFile" {
			sensitive = sensitive || containsSensitive(pass, call.Args[1])
		}
		if !sensitive {
			return
		}
		position := pass.Fset.PositionFor(call.Pos(), false)
		if filecheck.ShouldSkipFilename(position.Filename, generatedFiles) || nolint.HasDirectiveForLinter(position, nolintIndex, analyzerName) {
			return
		}
		if fn.Name() == "MkdirAll" {
			pass.ReportRangef(call, "os.MkdirAll uses FilePermPublic for a potentially sensitive path; use an appropriate restricted directory mode")
		} else {
			pass.ReportRangef(call, "os.%s uses FilePermPublic for a potentially sensitive path or content; consider FilePermSensitive", fn.Name())
		}
	})
}

func isPublicPerm(pass *analysis.Pass, expr ast.Expr) bool {
	var ident *ast.Ident
	switch expr := astutil.UnwrapParenExpr(expr).(type) {
	case *ast.Ident:
		ident = expr
	case *ast.SelectorExpr:
		ident = expr.Sel
	default:
		return false
	}
	obj, ok := pass.TypesInfo.ObjectOf(ident).(*types.Const)
	return ok && obj.Name() == "FilePermPublic" && obj.Pkg() != nil && obj.Pkg().Path() == "github.com/github/gh-aw/pkg/constants"
}

func containsSensitive(pass *analysis.Pass, expr ast.Expr) bool {
	terms := []string{"token", "secret", "key", "credential", "password", "auth", "env", "lock"}
	match := func(s string) bool {
		s = strings.ToLower(s)
		for _, term := range terms {
			if strings.Contains(s, term) {
				return true
			}
		}
		return false
	}
	found := false
	ast.Inspect(expr, func(n ast.Node) bool {
		if found || n == nil {
			return false
		}
		if ident, ok := n.(*ast.Ident); ok && match(ident.Name) {
			found = true
			return false
		}
		if e, ok := n.(ast.Expr); ok {
			if value := pass.TypesInfo.Types[e].Value; value != nil && value.Kind() == constant.String && match(constant.StringVal(value)) {
				found = true
				return false
			}
		}
		return true
	})
	return found
}
