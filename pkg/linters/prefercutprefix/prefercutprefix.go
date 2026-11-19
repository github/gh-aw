// Package prefercutprefix implements a Go analysis linter that flags
// strings.HasPrefix/HasSuffix guards whose body calls strings.TrimPrefix/
// TrimSuffix on the same operands; strings.CutPrefix/CutSuffix should be used.
package prefercutprefix

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
	"github.com/github/gh-aw/pkg/logger"
)

var pkgLog = logger.New("linters:prefercutprefix")

// Analyzer is the prefer-cutprefix analysis pass.
var Analyzer = analyzerutil.New("prefercutprefix", "reports strings.HasPrefix/HasSuffix guards that are followed by strings.TrimPrefix/TrimSuffix on the same operands, which should use strings.CutPrefix/CutSuffix instead", run)

// pairs maps guard function names to the matching trim function and the
// suggested replacement.
var pairs = map[string][2]string{
	"HasPrefix": {"TrimPrefix", "CutPrefix"},
	"HasSuffix": {"TrimSuffix", "CutSuffix"},
}

func run(pass *analysis.Pass) (any, error) {
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	nodeFilter := []ast.Node{(*ast.IfStmt)(nil), (*ast.SwitchStmt)(nil)}
	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		switch stmt := n.(type) {
		case *ast.IfStmt:
			check(pass, stmt.Cond, stmt.Body.List, generatedFiles, noLintIndex)
		case *ast.SwitchStmt:
			if stmt.Tag != nil {
				return
			}
			for _, s := range stmt.Body.List {
				cc, ok := s.(*ast.CaseClause)
				if !ok {
					continue
				}
				for _, cond := range cc.List {
					check(pass, cond, cc.Body, generatedFiles, noLintIndex)
				}
			}
		}
	})
}

func check(pass *analysis.Pass, cond ast.Expr, body []ast.Stmt, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) {
	cond = astutil.UnwrapParenExpr(cond)
	for guard, pair := range pairs {
		call, ok := astutil.AsStringsMethodCall(pass, cond, guard)
		if !ok {
			continue
		}
		s, p, ok := operands(call)
		if !ok {
			continue
		}
		if !pure(s) || !pure(p) {
			return
		}
		pos := pass.Fset.PositionFor(call.Pos(), false)
		if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
			return
		}
		if nolint.HasDirectiveForLinter(pos, noLintIndex, "prefercutprefix") {
			return
		}
		sText, pText := astutil.NodeText(pass.Fset, s), astutil.NodeText(pass.Fset, p)
		roots := map[string]struct{}{rootName(s): {}, rootName(p): {}}
		delete(roots, "")

		found, mutated := scanBody(pass, body, pair[0], sText, pText, roots)
		if found && !mutated {
			pkgLog.Printf("flagging %s guard at %s", guard, pos)
			pass.Reportf(call.Pos(), "strings.%s guard followed by strings.%s on the same operands; use `if rest, ok := strings.%s(%s, %s); ok { ... }` instead", guard, pair[0], pair[1], sText, pText)
		}
		return
	}
}

// pure reports whether e is an identifier, a selector chain on an identifier,
// or a basic literal.
func pure(e ast.Expr) bool {
	switch v := astutil.UnwrapParenExpr(e).(type) {
	case *ast.Ident, *ast.BasicLit:
		return true
	case *ast.SelectorExpr:
		return pure(v.X)
	}
	return false
}

// rootName returns the root identifier name of an assignable expression, or "".
func rootName(e ast.Expr) string {
	for {
		switch v := e.(type) {
		case *ast.Ident:
			return v.Name
		case *ast.SelectorExpr:
			e = v.X
		case *ast.IndexExpr:
			e = v.X
		case *ast.StarExpr:
			e = v.X
		case *ast.ParenExpr:
			e = v.X
		default:
			return ""
		}
	}
}

func hasKey(m map[string]struct{}, k string) bool {
	_, ok := m[k]
	return ok
}

// operands returns the unparenthesized two arguments of call.
func operands(call *ast.CallExpr) (s, p ast.Expr, ok bool) {
	if len(call.Args) != 2 {
		return nil, nil, false
	}
	for i, a := range call.Args {
		if i == 0 {
			s = astutil.UnwrapParenExpr(a)
		} else {
			p = astutil.UnwrapParenExpr(a)
		}
	}
	return s, p, true
}

// scanBody reports whether body calls strings.<trim>(s, p) and whether it
// may modify any of the root identifiers in roots.
func scanBody(pass *analysis.Pass, body []ast.Stmt, trim, sText, pText string, roots map[string]struct{}) (found, mutated bool) {
	for _, st := range body {
		ast.Inspect(st, func(x ast.Node) bool {
			switch v := x.(type) {
			case *ast.FuncLit:
				return false
			case *ast.AssignStmt:
				for _, l := range v.Lhs {
					if hasKey(roots, rootName(l)) {
						mutated = true
					}
				}
			case *ast.IncDecStmt:
				if hasKey(roots, rootName(v.X)) {
					mutated = true
				}
			case *ast.UnaryExpr:
				if v.Op.String() == "&" && hasKey(roots, rootName(v.X)) {
					mutated = true
				}
			case *ast.CallExpr:
				if c, ok := astutil.AsStringsMethodCall(pass, v, trim); ok {
					if cs, cp, ok := operands(c); ok &&
						astutil.NodeText(pass.Fset, cs) == sText && astutil.NodeText(pass.Fset, cp) == pText {
						found = true
					}
				}
			}
			return true
		})
	}
	return found, mutated
}
