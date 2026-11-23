// Package hasprefixdotdotpathescape implements a Go analysis linter that flags
// strings.HasPrefix(x, "..") calls used as path-containment checks.
package hasprefixdotdotpathescape

import (
	"go/ast"
	"go/token"
	"go/types"
	"regexp"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

const linterName = "hasprefixdotdotpathescape"

// Analyzer is the hasprefix-dotdot-path-escape analysis pass.
var Analyzer = analyzerutil.NewAtPath(
	linterName,
	"reports strings.HasPrefix(x, \"..\") path-containment checks that also match names such as \"..foo\" instead of the exact \"..\" component plus a separator",
	"hasprefix-dotdot-path-escape",
	run,
)

// relNameRe matches identifiers that conventionally hold a relative path.
var relNameRe = regexp.MustCompile(`^(rel|relative|relPath|relativePath|[a-z][A-Za-z0-9]*Rel(Path)?)$`)

func run(pass *analysis.Pass) (any, error) {
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}
	if pass.TypesInfo == nil {
		return nil, nil
	}

	var defs map[types.Object][]ast.Expr // built lazily
	nodeFilter := []ast.Node{(*ast.CallExpr)(nil)}
	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		call := n.(*ast.CallExpr)
		if len(call.Args) != 2 || !isStringsHasPrefix(pass, call) {
			return
		}
		if v, ok := pass.TypesInfo.Types[call.Args[1]]; !ok || v.Value == nil || v.Value.String() != `".."` {
			return
		}
		position := pass.Fset.PositionFor(call.Pos(), false)
		if filecheck.ShouldSkipFilename(position.Filename, generatedFiles) {
			return
		}
		if nolint.HasDirectiveForLinter(position, noLintIndex, linterName) {
			return
		}
		if defs == nil {
			defs = collectDefs(pass)
		}
		if !isPathValue(pass, call.Args[0], defs, 0) {
			return
		}
		pass.Reportf(call.Pos(),
			"strings.HasPrefix(%s, \"..\") also matches names like \"..foo\"; compare against \"..\" or use \"..\"+string(filepath.Separator) as the prefix, or use filepath.IsLocal",
			astutil.NodeText(pass.Fset, call.Args[0]))
	})
}

func isStringsHasPrefix(pass *analysis.Pass, call *ast.CallExpr) bool {
	pkg, name, ok := astutil.PackageCall(pass, call)
	return ok && pkg == "strings" && name == "HasPrefix"
}

// collectDefs maps each local variable to the expressions assigned to it. For
// tuple assignments from a single call, the call is recorded for the first
// result only (as in `rel, err := filepath.Rel(a, b)`).
func collectDefs(pass *analysis.Pass) map[types.Object][]ast.Expr {
	defs := make(map[types.Object][]ast.Expr)
	record := func(lhs ast.Expr, rhs ast.Expr) {
		id, ok := lhs.(*ast.Ident)
		if !ok || id.Name == "_" {
			return
		}
		obj := pass.TypesInfo.ObjectOf(id)
		if obj != nil {
			defs[obj] = append(defs[obj], rhs)
		}
	}
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			switch s := n.(type) {
			case *ast.AssignStmt:
				if s.Tok != token.ASSIGN && s.Tok != token.DEFINE {
					return true
				}
				if len(s.Lhs) == len(s.Rhs) {
					for i := range s.Lhs {
						record(s.Lhs[i], s.Rhs[i])
					}
				} else if len(s.Rhs) == 1 && len(s.Lhs) > 0 {
					record(s.Lhs[0], s.Rhs[0])
				}
			case *ast.ValueSpec:
				if len(s.Names) == len(s.Values) {
					for i := range s.Names {
						record(s.Names[i], s.Values[i])
					}
				} else if len(s.Values) == 1 && len(s.Names) > 0 {
					record(s.Names[0], s.Values[0])
				}
			}
			return true
		})
	}
	return defs
}

// isPathValue reports whether expr is evidently a relative path result: a
// direct filepath.Rel/Clean or path.Clean call, a variable assigned from one,
// or a variable with a conventional relative-path name.
func isPathValue(pass *analysis.Pass, expr ast.Expr, defs map[types.Object][]ast.Expr, depth int) bool {
	if depth > 4 {
		return false
	}
	expr = astutil.UnwrapParenExpr(expr)
	switch e := expr.(type) {
	case *ast.CallExpr:
		pkg, name, ok := astutil.PackageCall(pass, e)
		if !ok {
			return false
		}
		switch {
		case pkg == "path/filepath" && (name == "Rel" || name == "Clean" || name == "ToSlash" || name == "FromSlash"),
			pkg == "path" && name == "Clean":
			if name == "ToSlash" || name == "FromSlash" {
				return len(e.Args) == 1 && isPathValue(pass, e.Args[0], defs, depth+1)
			}
			return true
		}
	case *ast.Ident:
		obj := pass.TypesInfo.ObjectOf(e)
		if obj == nil {
			return false
		}
		if b, ok := obj.Type().Underlying().(*types.Basic); !ok || b.Kind() != types.String {
			return false
		}
		for _, rhs := range defs[obj] {
			if isPathValue(pass, rhs, defs, depth+1) {
				return true
			}
		}
		return relNameRe.MatchString(e.Name)
	}
	return false
}
