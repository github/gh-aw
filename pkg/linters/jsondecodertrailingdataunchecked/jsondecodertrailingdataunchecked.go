// Package jsondecodertrailingdataunchecked reports in-memory JSON decoders
// whose first Decode is not followed by a Decode that checks for io.EOF.
package jsondecodertrailingdataunchecked

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

const linterName = "jsondecodertrailingdataunchecked"

// Analyzer is the json-decoder-trailing-data-unchecked analysis pass.
var Analyzer = analyzerutil.New(linterName, "reports in-memory JSON decoders whose first Decode is not followed by a Decode that checks for io.EOF", run)

func run(pass *analysis.Pass) (any, error) {
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	for _, file := range pass.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			switch fn := node.(type) {
			case *ast.FuncDecl:
				analyzeFunction(pass, fn.Body, generatedFiles, noLintIndex)
			case *ast.FuncLit:
				analyzeFunction(pass, fn.Body, generatedFiles, noLintIndex)
			}
			return true
		})
	}
	return nil, nil
}

func analyzeFunction(pass *analysis.Pass, body *ast.BlockStmt, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) {
	if body == nil {
		return
	}

	decoders := make(map[types.Object]struct{})
	var decodeCallsByDecoder = make(map[types.Object][]*ast.CallExpr)
	ast.Inspect(body, func(node ast.Node) bool {
		if _, ok := node.(*ast.FuncLit); ok {
			return false
		}

		switch node := node.(type) {
		case *ast.AssignStmt:
			for i, lhs := range node.Lhs {
				rhs, ok := astutil.RhsExprForIndex(node.Rhs, i)
				if !ok {
					continue
				}
				if obj := inMemoryDecoderObject(pass, lhs, rhs); obj != nil {
					decoders[obj] = struct{}{}
				}
			}
		case *ast.ValueSpec:
			for i, name := range node.Names {
				rhs, ok := astutil.RhsExprForIndex(node.Values, i)
				if !ok {
					continue
				}
				if obj := inMemoryDecoderObject(pass, name, rhs); obj != nil {
					decoders[obj] = struct{}{}
				}
			}
		case *ast.CallExpr:
			selector, ok := astutil.UnwrapParenExpr(node.Fun).(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "Decode" {
				break
			}
			receiver, ok := astutil.UnwrapParenExpr(selector.X).(*ast.Ident)
			if !ok || !isJSONDecoder(pass, selector.X) {
				break
			}
			obj := pass.TypesInfo.ObjectOf(receiver)
			if _, ok := decoders[obj]; ok {
				decodeCallsByDecoder[obj] = append(decodeCallsByDecoder[obj], node)
			}
		}
		return true
	})

	var firstDecodes []*ast.CallExpr
	for _, calls := range decodeCallsByDecoder {
		slices.SortFunc(calls, func(a, b *ast.CallExpr) int {
			return int(a.Pos() - b.Pos())
		})
		if len(calls) > 1 && decodeChecksEOF(pass, body, calls[1]) {
			continue
		}
		firstDecodes = append(firstDecodes, calls[0])
	}

	slices.SortFunc(firstDecodes, func(a, b *ast.CallExpr) int {
		return int(a.Pos() - b.Pos())
	})
	for _, firstDecode := range firstDecodes {
		pos := pass.Fset.PositionFor(firstDecode.Pos(), false)
		if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) ||
			nolint.HasDirectiveForLinter(pos, noLintIndex, linterName) {
			continue
		}
		pass.ReportRangef(firstDecode, "first Decode on an in-memory JSON reader must be followed by a Decode that checks for io.EOF to reject trailing JSON data")
	}
}

func inMemoryDecoderObject(pass *analysis.Pass, lhs ast.Expr, rhs ast.Expr) types.Object {
	ident, ok := astutil.UnwrapParenExpr(lhs).(*ast.Ident)
	if !ok {
		return nil
	}
	decoder, ok := astutil.UnwrapParenExpr(rhs).(*ast.CallExpr)
	if !ok || !isPackageCall(pass, decoder, "encoding/json", "NewDecoder") || len(decoder.Args) != 1 {
		return nil
	}
	reader, ok := astutil.UnwrapParenExpr(decoder.Args[0]).(*ast.CallExpr)
	if !ok || len(reader.Args) != 1 {
		return nil
	}
	pkgPath, funcName, ok := astutil.PackageCall(pass, reader)
	if !ok || funcName != "NewReader" || (pkgPath != "strings" && pkgPath != "bytes") {
		return nil
	}
	return pass.TypesInfo.ObjectOf(ident)
}

func isPackageCall(pass *analysis.Pass, call *ast.CallExpr, pkgPath, funcName string) bool {
	actualPkgPath, actualFuncName, ok := astutil.PackageCall(pass, call)
	return ok && actualPkgPath == pkgPath && actualFuncName == funcName
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

func decodeChecksEOF(pass *analysis.Pass, body *ast.BlockStmt, call *ast.CallExpr) bool {
	var checked bool
	ast.Inspect(body, func(node ast.Node) bool {
		if checked {
			return false
		}
		if _, ok := node.(*ast.FuncLit); ok {
			return false
		}
		if ifStmt, ok := node.(*ast.IfStmt); ok {
			if obj := decodeErrorObject(pass, ifStmt.Init, call); obj != nil && conditionChecksEOF(pass, ifStmt.Cond, obj) {
				checked = true
				return false
			}
		}
		if block, ok := node.(*ast.BlockStmt); ok {
			for i, stmt := range block.List {
				if i+1 >= len(block.List) {
					continue
				}
				if obj := decodeErrorObject(pass, stmt, call); obj != nil {
					if next, ok := block.List[i+1].(*ast.IfStmt); ok && conditionChecksEOF(pass, next.Cond, obj) {
						checked = true
						return false
					}
				}
			}
		}
		return true
	})
	return checked
}

func decodeErrorObject(pass *analysis.Pass, stmt ast.Stmt, call *ast.CallExpr) types.Object {
	assign, ok := stmt.(*ast.AssignStmt)
	if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 || astutil.UnwrapParenExpr(assign.Rhs[0]) != call {
		return nil
	}
	ident, ok := astutil.UnwrapParenExpr(assign.Lhs[0]).(*ast.Ident)
	if !ok || ident.Name == "_" {
		return nil
	}
	return pass.TypesInfo.ObjectOf(ident)
}

func conditionChecksEOF(pass *analysis.Pass, condition ast.Expr, errorObject types.Object) bool {
	found := false
	ast.Inspect(condition, func(node ast.Node) bool {
		if found {
			return false
		}
		switch node := node.(type) {
		case *ast.BinaryExpr:
			if node.Op == token.EQL || node.Op == token.NEQ {
				found = (sameObject(pass, node.X, errorObject) && isIOEOF(pass, node.Y)) ||
					(sameObject(pass, node.Y, errorObject) && isIOEOF(pass, node.X))
			}
		case *ast.CallExpr:
			if isPackageCall(pass, node, "errors", "Is") && len(node.Args) == 2 {
				found = sameObject(pass, node.Args[0], errorObject) && isIOEOF(pass, node.Args[1])
			}
		}
		return !found
	})
	return found
}

func sameObject(pass *analysis.Pass, expr ast.Expr, obj types.Object) bool {
	ident, ok := astutil.UnwrapParenExpr(expr).(*ast.Ident)
	return ok && pass.TypesInfo.ObjectOf(ident) == obj
}

func isIOEOF(pass *analysis.Pass, expr ast.Expr) bool {
	selector, ok := astutil.UnwrapParenExpr(expr).(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "EOF" {
		return false
	}
	object, ok := pass.TypesInfo.Uses[selector.Sel].(*types.Var)
	return ok && object.Pkg() != nil && object.Pkg().Path() == "io"
}
