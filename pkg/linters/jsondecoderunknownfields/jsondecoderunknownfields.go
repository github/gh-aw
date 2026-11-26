// Package jsondecoderunknownfields reports json.Decoder values that decode
// JSON without calling DisallowUnknownFields.
package jsondecoderunknownfields

import (
	"go/ast"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

const linterName = "jsondecoderunknownfields"

// Analyzer is the json-decoder-unknown-fields analysis pass.
var Analyzer = analyzerutil.New(linterName, "reports json.Decoder values used to Decode JSON without calling DisallowUnknownFields on that decoder", run)

func run(pass *analysis.Pass) (any, error) {
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	// defs maps variables to their initializer/assigned expressions.
	defs := make(map[types.Object][]ast.Expr)
	for _, file := range pass.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			switch n := node.(type) {
			case *ast.AssignStmt:
				for i, lhs := range n.Lhs {
					ident, ok := astutil.UnwrapParenExpr(lhs).(*ast.Ident)
					if !ok {
						continue
					}
					if rhs, ok := astutil.RhsExprForIndex(n.Rhs, i); ok {
						if obj := pass.TypesInfo.ObjectOf(ident); obj != nil {
							defs[obj] = append(defs[obj], rhs)
						}
					}
				}
			case *ast.ValueSpec:
				for i, name := range n.Names {
					if rhs, ok := astutil.RhsExprForIndex(n.Values, i); ok {
						if obj := pass.TypesInfo.ObjectOf(name); obj != nil {
							defs[obj] = append(defs[obj], rhs)
						}
					}
				}
			}
			return true
		})
	}

	// resolve returns the identity of the decoder created by json.NewDecoder
	// that expr refers to, or nil when it is not locally constructed.
	var resolve func(expr ast.Expr, depth int) any
	resolve = func(expr ast.Expr, depth int) any {
		if depth > 8 {
			return nil
		}
		expr = astutil.UnwrapParenExpr(expr)
		switch e := expr.(type) {
		case *ast.CallExpr:
			if isNewDecoder(pass, e) {
				return e
			}
		case *ast.Ident:
			obj := pass.TypesInfo.ObjectOf(e)
			rhsList := defs[obj]
			if len(rhsList) == 0 {
				return nil
			}
			var root any
			for _, rhs := range rhsList {
				r := resolve(rhs, depth+1)
				if r == nil {
					return nil
				}
				if _, direct := r.(*ast.CallExpr); direct {
					r = obj
				}
				if root != nil && root != r {
					return nil
				}
				root = r
			}
			return root
		}
		return nil
	}

	disallowed := make(map[any]bool)
	type decodeCall struct {
		call *ast.CallExpr
		key  any
	}
	var decodes []decodeCall
	for _, file := range pass.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := astutil.UnwrapParenExpr(call.Fun).(*ast.SelectorExpr)
			if !ok {
				return true
			}
			switch sel.Sel.Name {
			case "DisallowUnknownFields", "Decode":
			default:
				return true
			}
			if !isJSONDecoder(pass, sel.X) {
				return true
			}
			key := resolve(sel.X, 0)
			if key == nil {
				return true
			}
			if sel.Sel.Name == "Decode" {
				decodes = append(decodes, decodeCall{call, key})
			} else {
				disallowed[key] = true
			}
			return true
		})
	}

	slices.SortFunc(decodes, func(a, b decodeCall) int { return int(a.call.Pos() - b.call.Pos()) })
	reported := make(map[any]bool)
	for _, d := range decodes {
		if disallowed[d.key] || reported[d.key] {
			continue
		}
		reported[d.key] = true
		pos := pass.Fset.PositionFor(d.call.Pos(), false)
		if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) ||
			nolint.HasDirectiveForLinter(pos, noLintIndex, linterName) {
			continue
		}
		pass.ReportRangef(d.call, "json.Decoder decodes without DisallowUnknownFields; call DisallowUnknownFields on the decoder to reject unexpected object fields")
	}
	return nil, nil
}

func isNewDecoder(pass *analysis.Pass, call *ast.CallExpr) bool {
	pkgPath, name, ok := astutil.PackageCall(pass, call)
	return ok && pkgPath == "encoding/json" && name == "NewDecoder"
}

func isJSONDecoder(pass *analysis.Pass, expr ast.Expr) bool {
	t := pass.TypesInfo.TypeOf(expr)
	if t == nil {
		return false
	}
	if pointer, ok := t.(*types.Pointer); ok {
		t = pointer.Elem()
	}
	named, ok := types.Unalias(t).(*types.Named)
	return ok && named.Obj().Name() == "Decoder" && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == "encoding/json"
}
