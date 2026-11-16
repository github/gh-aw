// Package httpclientnotimeout implements a Go analysis linter that flags
// net/http.Client composite literals that do not set a Timeout field.
package httpclientnotimeout

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

// Analyzer is the httpclientnotimeout analysis pass.
var Analyzer = analyzerutil.New("httpclientnotimeout", "reports net/http client composite literals (&http.Client{} or http.Client{}) that do not set a Timeout field, because a client without a timeout can block indefinitely on a stalled connection", run)

func run(pass *analysis.Pass) (any, error) {
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}
	return analyzerutil.Preorder(pass, []ast.Node{(*ast.CompositeLit)(nil)}, func(n ast.Node) {
		lit := n.(*ast.CompositeLit)
		if !isHTTPClient(pass.TypesInfo.TypeOf(lit)) || hasTimeout(lit) {
			return
		}
		pos := pass.Fset.PositionFor(lit.Pos(), false)
		if filecheck.IsTestFile(pos.Filename) || filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
			return
		}
		if nolint.HasDirectiveForLinter(pos, noLintIndex, "httpclientnotimeout") {
			return
		}
		pass.Reportf(lit.Pos(), "http.Client literal has no Timeout; set Timeout so requests cannot hang indefinitely")
	})
}

func isHTTPClient(t types.Type) bool {
	if t == nil {
		return false
	}
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}
	named, ok := t.(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj.Pkg() != nil && obj.Pkg().Path() == "net/http" && obj.Name() == "Client"
}

func hasTimeout(lit *ast.CompositeLit) bool {
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "Timeout" {
			return true
		}
	}
	return false
}
