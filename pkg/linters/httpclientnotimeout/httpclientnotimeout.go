// Package httpclientnotimeout implements a Go analysis linter that reports
// net/http.Client composite literals without a Timeout field.
package httpclientnotimeout

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

// Analyzer reports net/http.Client composite literals without a Timeout field.
var Analyzer = analyzerutil.New("httpclientnotimeout", "reports net/http.Client composite literals without a Timeout field", run)

func run(pass *analysis.Pass) (any, error) {
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	return analyzerutil.Preorder(pass, []ast.Node{(*ast.CompositeLit)(nil)}, func(n ast.Node) {
		literal, ok := n.(*ast.CompositeLit)
		if !ok || !isHTTPClient(pass.TypesInfo.TypeOf(literal)) || hasTimeoutField(pass, literal) {
			return
		}

		pos := pass.Fset.PositionFor(literal.Pos(), false)
		if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) ||
			nolint.HasDirectiveForLinter(pos, noLintIndex, "httpclientnotimeout") {
			return
		}

		pass.ReportRangef(literal, "net/http.Client literal has no Timeout field; set Timeout to bound request duration")
	})
}

func isHTTPClient(t types.Type) bool {
	if pointer, ok := t.(*types.Pointer); ok {
		t = pointer.Elem()
	}

	named, ok := types.Unalias(t).(*types.Named)
	return ok && named.Obj().Name() == "Client" && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == "net/http"
}

func hasTimeoutField(pass *analysis.Pass, literal *ast.CompositeLit) bool {
	keyed := false
	for _, element := range literal.Elts {
		keyValue, ok := element.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		keyed = true
		if key, ok := keyValue.Key.(*ast.Ident); ok && key.Name == "Timeout" {
			return true
		}
	}
	if keyed {
		return false
	}

	t := pass.TypesInfo.TypeOf(literal)
	if pointer, ok := t.(*types.Pointer); ok {
		t = pointer.Elem()
	}
	named, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return false
	}
	fields, ok := named.Underlying().(*types.Struct)
	if !ok {
		return false
	}
	for i := range fields.NumFields() {
		if fields.Field(i).Name() == "Timeout" {
			return len(literal.Elts) > i
		}
	}
	return false
}
