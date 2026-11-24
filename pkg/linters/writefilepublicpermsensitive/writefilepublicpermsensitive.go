// Package writefilepublicpermsensitive detects public permissions on potentially sensitive writes.
package writefilepublicpermsensitive

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

const analyzerName = "writefilepublicpermsensitive"

var camelCaseBoundary = regexp.MustCompile(`([\p{Ll}\p{Nd}])(\p{Lu})|(\p{Lu})(\p{Lu}\p{Ll})`)

var Analyzer = analyzerutil.New(analyzerName, "reports FilePermPublic on potentially sensitive paths or content", run)

func run(pass *analysis.Pass) (any, error) {
	nolintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	return analyzerutil.Preorder(pass, []ast.Node{(*ast.CallExpr)(nil)}, func(n ast.Node) {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return
		}
		fn, ok := astutil.CalledOSFunc(pass, call, "WriteFile", "OpenFile", "MkdirAll")
		if !ok {
			return
		}
		modeIndex := 2
		if fn.Name() == "MkdirAll" {
			modeIndex = 1
		}
		if len(call.Args) != modeIndex+1 || !isPublicPerm(pass, call.Args[modeIndex]) { //nolint:uncheckedsliceindex // modeIndex is 1 or 2 and the argument count is checked
			return
		}
		if fn.Name() == "OpenFile" && !openFileCreatesFile(pass, call.Args[1], fn) { //nolint:uncheckedsliceindex // OpenFile has at least three arguments
			return
		}
		sensitive := containsSensitive(pass, call.Args[0]) //nolint:uncheckedsliceindex // the argument count is checked above
		if fn.Name() == "WriteFile" {
			sensitive = sensitive || containsSensitive(pass, call.Args[1]) //nolint:uncheckedsliceindex // the argument count is checked above
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
	foundPublicPerm := false
	ast.Inspect(expr, func(n ast.Node) bool {
		ident, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		obj, ok := pass.TypesInfo.ObjectOf(ident).(*types.Const)
		if ok && obj.Name() == "FilePermPublic" && obj.Pkg() != nil && obj.Pkg().Path() == "github.com/github/gh-aw/pkg/constants" {
			foundPublicPerm = true
		}
		return true
	})
	if !foundPublicPerm {
		return false
	}

	value := pass.TypesInfo.Types[expr].Value
	if value == nil {
		return true
	}
	mode, ok := constant.Int64Val(constant.ToInt(value))
	return !ok || mode&0007 != 0
}

func openFileCreatesFile(pass *analysis.Pass, flags ast.Expr, fn *types.Func) bool {
	value := pass.TypesInfo.Types[flags].Value
	if value == nil || fn.Pkg() == nil {
		return true
	}
	create, ok := fn.Pkg().Scope().Lookup("O_CREATE").(*types.Const)
	if !ok {
		return true
	}
	flagsValue := constant.ToInt(value)
	createValue := constant.ToInt(create.Val())
	if flagsValue.Kind() != constant.Int || createValue.Kind() != constant.Int {
		return true
	}
	return constant.Sign(constant.BinaryOp(flagsValue, token.AND, createValue)) != 0
}

func containsSensitive(pass *analysis.Pass, expr ast.Expr) bool {
	ignored := make(map[*ast.Ident]struct{})
	ast.Inspect(expr, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.CallExpr:
			ast.Inspect(n.Fun, func(funNode ast.Node) bool {
				if ident, ok := funNode.(*ast.Ident); ok {
					ignored[ident] = struct{}{}
				}
				return true
			})
		case *ast.SelectorExpr:
			ignored[n.Sel] = struct{}{}
		}
		return true
	})

	terms := map[string]struct{}{
		"token": {}, "tokens": {},
		"secret": {}, "secrets": {},
		"key": {}, "keys": {},
		"credential": {}, "credentials": {},
		"password": {}, "passwords": {},
		"auth": {}, "authentication": {}, "authorization": {},
		"env": {},
	}
	found := false
	ast.Inspect(expr, func(n ast.Node) bool {
		if found || n == nil {
			return false
		}
		if ident, ok := n.(*ast.Ident); ok {
			if _, skip := ignored[ident]; !skip && containsSensitiveTerm(ident.Name, terms) {
				found = true
				return false
			}
		}
		if e, ok := n.(ast.Expr); ok {
			if value := pass.TypesInfo.Types[e].Value; value != nil && value.Kind() == constant.String && containsSensitiveTerm(constant.StringVal(value), terms) {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

func containsSensitiveTerm(value string, terms map[string]struct{}) bool {
	value = camelCaseBoundary.ReplaceAllString(value, "$1$3 $2$4")
	for _, segment := range strings.FieldsFunc(value, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if _, found := terms[strings.ToLower(segment)]; found {
			return true
		}
	}
	return false
}
