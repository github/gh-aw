// Package jsondecoderunknownfields reports JSON decoders that do not reject
// unknown fields.
package jsondecoderunknownfields

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

const linterName = "jsondecoderunknownfields"

// Analyzer reports JSON decoders that do not reject unknown fields.
var Analyzer = analyzerutil.New(linterName, "reports JSON decoders that do not reject unknown fields", run)

type decoderConstruction struct {
	call   *ast.CallExpr
	object types.Object
}

type decoderMethodCall struct {
	object types.Object
	call   *ast.CallExpr
}

func run(pass *analysis.Pass) (any, error) {
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	var constructions []decoderConstruction
	var strictCalls, decodeCalls []decoderMethodCall
	writes := make(map[types.Object][]token.Pos)
	constructionObjects := make(map[*ast.CallExpr]types.Object)

	for _, file := range pass.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			switch node := node.(type) {
			case *ast.CallExpr:
				if isJSONNewDecoder(pass, node) {
					constructions = append(constructions, decoderConstruction{call: node})
				}
				selector, ok := astutil.UnwrapParenExpr(node.Fun).(*ast.SelectorExpr)
				if !ok || !isJSONDecoder(pass, selector.X) {
					return true
				}
				switch selector.Sel.Name {
				case "DisallowUnknownFields":
					strictCalls = append(strictCalls, decoderMethodCall{
						object: decoderObject(pass, selector.X),
						call:   node,
					})
				case "Decode":
					decodeCalls = append(decodeCalls, decoderMethodCall{
						object: decoderObject(pass, selector.X),
						call:   node,
					})
				}
			case *ast.AssignStmt:
				for i, lhs := range node.Lhs {
					object := decoderObject(pass, lhs)
					if object != nil {
						writes[object] = append(writes[object], node.Pos())
					}
					rhs, ok := astutil.RhsExprForIndex(node.Rhs, i)
					if ok {
						recordConstructionObject(pass, astutil.UnwrapParenExpr(rhs), lhs, constructionObjects)
					}
				}
			case *ast.ValueSpec:
				for i, name := range node.Names {
					object := pass.TypesInfo.ObjectOf(name)
					if object != nil {
						writes[object] = append(writes[object], node.Pos())
					}
					rhs, ok := astutil.RhsExprForIndex(node.Values, i)
					if ok {
						recordConstructionObject(pass, astutil.UnwrapParenExpr(rhs), name, constructionObjects)
					}
				}
			}
			return true
		})
	}

	for _, construction := range constructions {
		construction.object = constructionObjects[construction.call]
		if isStrictBeforeDecode(construction, strictCalls, decodeCalls, writes) {
			continue
		}
		position := pass.Fset.PositionFor(construction.call.Pos(), false)
		if filecheck.ShouldSkipFilename(position.Filename, generatedFiles) ||
			nolint.HasDirectiveForLinter(position, noLintIndex, linterName) {
			continue
		}
		pass.ReportRangef(construction.call, "json.NewDecoder accepts unknown fields by default; call DisallowUnknownFields before Decode")
	}

	return nil, nil
}

func recordConstructionObject(pass *analysis.Pass, rhs ast.Expr, lhs ast.Expr, constructionObjects map[*ast.CallExpr]types.Object) {
	call, ok := rhs.(*ast.CallExpr)
	if !ok || !isJSONNewDecoder(pass, call) {
		return
	}
	object := decoderObject(pass, lhs)
	if object == nil {
		return
	}
	constructionObjects[call] = object
}

func isStrictBeforeDecode(construction decoderConstruction, strictCalls, decodeCalls []decoderMethodCall, writes map[types.Object][]token.Pos) bool {
	if construction.object == nil {
		return false
	}

	nextWrite := token.NoPos
	for _, pos := range writes[construction.object] {
		if pos > construction.call.Pos() && (nextWrite == token.NoPos || pos < nextWrite) {
			nextWrite = pos
		}
	}

	firstDecode := token.NoPos
	for _, methodCall := range decodeCalls {
		if methodCall.object == construction.object && methodCall.call.Pos() > construction.call.Pos() &&
			(nextWrite == token.NoPos || methodCall.call.Pos() < nextWrite) &&
			(firstDecode == token.NoPos || methodCall.call.Pos() < firstDecode) {
			firstDecode = methodCall.call.Pos()
		}
	}

	for _, methodCall := range strictCalls {
		if methodCall.object != construction.object || methodCall.call.Pos() <= construction.call.Pos() ||
			(nextWrite != token.NoPos && methodCall.call.Pos() >= nextWrite) {
			continue
		}
		if firstDecode == token.NoPos || methodCall.call.Pos() < firstDecode {
			return true
		}
	}
	return false
}

func isJSONNewDecoder(pass *analysis.Pass, call *ast.CallExpr) bool {
	pkgPath, funcName, ok := astutil.PackageCall(pass, call)
	return ok && pkgPath == "encoding/json" && funcName == "NewDecoder"
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

func decoderObject(pass *analysis.Pass, expr ast.Expr) types.Object {
	ident, ok := astutil.UnwrapParenExpr(expr).(*ast.Ident)
	if !ok {
		return nil
	}
	return pass.TypesInfo.ObjectOf(ident)
}
