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

type decoderIndex struct {
	constructions       []decoderConstruction
	strictCalls         []decoderMethodCall
	decodeCalls         []decoderMethodCall
	writes              map[types.Object][]token.Pos
	constructionObjects map[*ast.CallExpr]types.Object
}

func run(pass *analysis.Pass) (any, error) {
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	index := collectDecoderCalls(pass)
	for _, construction := range index.constructions {
		if isStrictBeforeDecode(construction, index.strictCalls, index.decodeCalls, index.writes) {
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

func collectDecoderCalls(pass *analysis.Pass) decoderIndex {
	index := decoderIndex{
		writes:              make(map[types.Object][]token.Pos),
		constructionObjects: make(map[*ast.CallExpr]types.Object),
	}
	for _, file := range pass.Files {
		collectFileCalls(pass, file, &index)
	}
	for i := range index.constructions {
		index.constructions[i].object = index.constructionObjects[index.constructions[i].call]
	}
	return index
}

func collectFileCalls(pass *analysis.Pass, file *ast.File, index *decoderIndex) {
	ast.Inspect(file, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.CallExpr:
			collectDecoderCall(pass, node, index)
		case *ast.AssignStmt:
			for i, lhs := range node.Lhs {
				recordWrite(pass, lhs, node.Pos(), index)
				rhs, ok := astutil.RhsExprForIndex(node.Rhs, i)
				if ok {
					recordConstructionObject(pass, astutil.UnwrapParenExpr(rhs), lhs, index.constructionObjects)
				}
			}
		case *ast.ValueSpec:
			for i, name := range node.Names {
				object := pass.TypesInfo.ObjectOf(name)
				if object != nil {
					index.writes[object] = append(index.writes[object], node.Pos())
				}
				rhs, ok := astutil.RhsExprForIndex(node.Values, i)
				if ok {
					recordConstructionObject(pass, astutil.UnwrapParenExpr(rhs), name, index.constructionObjects)
				}
			}
		}
		return true
	})
}

func collectDecoderCall(pass *analysis.Pass, call *ast.CallExpr, index *decoderIndex) {
	if isJSONNewDecoder(pass, call) {
		index.constructions = append(index.constructions, decoderConstruction{call: call})
	}
	selector, ok := astutil.UnwrapParenExpr(call.Fun).(*ast.SelectorExpr)
	if !ok || !isJSONDecoder(pass, selector.X) {
		return
	}
	methodCall := decoderMethodCall{object: decoderObject(pass, selector.X), call: call}
	switch selector.Sel.Name {
	case "DisallowUnknownFields":
		index.strictCalls = append(index.strictCalls, methodCall)
	case "Decode":
		index.decodeCalls = append(index.decodeCalls, methodCall)
	}
}

func recordWrite(pass *analysis.Pass, lhs ast.Expr, pos token.Pos, index *decoderIndex) {
	object := decoderObject(pass, lhs)
	if object != nil {
		index.writes[object] = append(index.writes[object], pos)
	}
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
