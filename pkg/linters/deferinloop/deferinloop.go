// Package deferinloop implements a Go analysis linter that flags defer
// statements inside loops where they run when the enclosing function returns,
// rather than at the end of each iteration.
package deferinloop

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
	"github.com/github/gh-aw/pkg/logger"
)

var pkgLog = logger.New("linters:deferinloop")

// Analyzer is the defer-in-loop analysis pass.
var Analyzer = analyzerutil.New("deferinloop", "reports defer statements inside loops where they run when the enclosing function returns rather than at the end of each iteration; range-over-function iterators and function literals form scope boundaries; test files are not checked", run)

func run(pass *analysis.Pass) (any, error) {
	insp, err := astutil.Inspector(pass)
	if err != nil {
		return nil, err
	}

	pkgLog.Printf("analyzing package %s", pass.Pkg.Path())

	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	for cur := range insp.Root().Preorder((*ast.DeferStmt)(nil)) {
		deferStmt, ok := cur.Node().(*ast.DeferStmt)
		if !ok {
			continue
		}

		pos := pass.Fset.PositionFor(deferStmt.Pos(), false)
		if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
			continue
		}
		if nolint.HasDirectiveForLinter(pos, noLintIndex, "deferinloop") {
			continue
		}

		if !isInsideLoop(pass, cur) {
			continue
		}

		pkgLog.Printf("flagging defer inside loop at %s", pos)
		pass.ReportRangef(deferStmt,
			"defer inside a loop does not execute at the end of each iteration; it runs when the enclosing function returns, which can cause resource leaks")
	}

	return nil, nil
}

// isInsideLoop reports whether cur (a DeferStmt) is enclosed anywhere within a
// for or range loop body, without crossing a function literal or range-over-func
// boundary.
func isInsideLoop(pass *analysis.Pass, cur inspector.Cursor) bool {
	for encl := range cur.Enclosing(
		(*ast.ForStmt)(nil),
		(*ast.RangeStmt)(nil),
		(*ast.FuncLit)(nil),
	) {
		switch node := encl.Node().(type) {
		case *ast.ForStmt:
			return true
		case *ast.RangeStmt:
			if pass.TypesInfo != nil {
				if typ := pass.TypesInfo.TypeOf(node.X); typ != nil {
					if _, ok := typ.Underlying().(*types.Signature); ok {
						return false
					}
				}
			}
			return true
		case *ast.FuncLit:
			return false
		}
	}
	return false
}
