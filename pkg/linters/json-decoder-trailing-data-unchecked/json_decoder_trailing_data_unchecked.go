// Package jsondecodertrailingdataunchecked reports json.Decoder.Decode calls on
// decoders over in-memory readers that never verify io.EOF afterwards.
package jsondecodertrailingdataunchecked

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

const linterName = "jsondecodertrailingdataunchecked"

const message = "json.Decoder over an in-memory reader accepts trailing data; call Decode again and verify it returns io.EOF"

// Analyzer is the json-decoder-trailing-data-unchecked analysis pass.
var Analyzer = analyzerutil.NewAtPath(linterName, "reports json.Decoder.Decode calls on decoders created from strings.NewReader or bytes.NewReader that do not verify io.EOF with a follow-up Decode", "json-decoder-trailing-data-unchecked", run)

func run(pass *analysis.Pass) (any, error) {
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	report := func(call *ast.CallExpr) {
		position := pass.Fset.PositionFor(call.Pos(), false)
		if filecheck.ShouldSkipFilename(position.Filename, generatedFiles) ||
			nolint.HasDirectiveForLinter(position, noLintIndex, linterName) {
			return
		}
		pass.ReportRangef(call, message)
	}

	nodeFilter := []ast.Node{(*ast.FuncDecl)(nil)}
	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			return
		}
		analyzeFunc(pass, fn.Body, report)
	})
}

type functionState struct {
	decoders     map[types.Object]bool // object -> still eligible (single assignment)
	decodes      map[types.Object][]*ast.CallExpr
	eofPositions []token.Pos
	chained      []*ast.CallExpr
}

func analyzeFunc(pass *analysis.Pass, body *ast.BlockStmt, report func(*ast.CallExpr)) {
	state := functionState{
		decoders: make(map[types.Object]bool),
		decodes:  make(map[types.Object][]*ast.CallExpr),
	}

	ast.Inspect(body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.AssignStmt:
			state.recordAssign(pass, node)
		case *ast.ValueSpec:
			state.recordValueSpec(pass, node)
		case *ast.SelectorExpr:
			state.recordEOF(pass, node)
		case *ast.CallExpr:
			state.recordDecode(pass, node)
		}
		return true
	})

	state.report(report)
}

func (s *functionState) recordAssign(pass *analysis.Pass, assign *ast.AssignStmt) {
	if len(assign.Lhs) != len(assign.Rhs) {
		return
	}
	for i, lhs := range assign.Lhs {
		rhs, ok := astutil.RhsExprForIndex(assign.Rhs, i)
		if ok {
			s.noteInit(pass, lhs, rhs, assign.Tok == token.DEFINE)
		}
	}
}

func (s *functionState) recordValueSpec(pass *analysis.Pass, spec *ast.ValueSpec) {
	if len(spec.Names) != len(spec.Values) {
		return
	}
	for i, name := range spec.Names {
		rhs, ok := astutil.RhsExprForIndex(spec.Values, i)
		if ok {
			s.noteInit(pass, name, rhs, true)
		}
	}
}

func (s *functionState) noteInit(pass *analysis.Pass, lhs, rhs ast.Expr, define bool) {
	ident, ok := astutil.UnwrapParenExpr(lhs).(*ast.Ident)
	if !ok || ident.Name == "_" {
		return
	}
	obj := pass.TypesInfo.ObjectOf(ident)
	if obj == nil {
		return
	}
	if isInMemoryDecoder(pass, rhs) {
		if _, seen := s.decoders[obj]; seen {
			s.decoders[obj] = false
		} else {
			s.decoders[obj] = true
		}
		return
	}
	if !define {
		if _, seen := s.decoders[obj]; seen {
			s.decoders[obj] = false // reassigned to something else
		}
	}
}

func (s *functionState) recordEOF(pass *analysis.Pass, selector *ast.SelectorExpr) {
	if selector.Sel.Name != "EOF" {
		return
	}
	v, ok := pass.TypesInfo.Uses[selector.Sel].(*types.Var)
	if ok && v.Pkg() != nil && v.Pkg().Path() == "io" {
		s.eofPositions = append(s.eofPositions, selector.Pos())
	}
}

func (s *functionState) recordDecode(pass *analysis.Pass, call *ast.CallExpr) {
	sel, ok := astutil.UnwrapParenExpr(call.Fun).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Decode" {
		return
	}
	recv := astutil.UnwrapParenExpr(sel.X)
	if ident, ok := recv.(*ast.Ident); ok {
		if obj := pass.TypesInfo.ObjectOf(ident); obj != nil {
			s.decodes[obj] = append(s.decodes[obj], call)
		}
	} else if isInMemoryDecoder(pass, recv) {
		s.chained = append(s.chained, call)
	}
}

func (s *functionState) report(report func(*ast.CallExpr)) {
	for _, call := range s.chained {
		report(call)
	}
	for obj, eligible := range s.decoders {
		calls := s.decodes[obj]
		if !eligible || len(calls) == 0 {
			continue
		}
		var first *ast.CallExpr
		for _, call := range calls {
			if first == nil || call.Pos() < first.Pos() {
				first = call
			}
		}
		if verifiesEOF(calls, first, s.eofPositions) {
			continue
		}
		report(first)
	}
}

// verifiesEOF reports whether a later Decode on the same decoder is followed by
// an io.EOF reference.
func verifiesEOF(calls []*ast.CallExpr, first *ast.CallExpr, eofPositions []token.Pos) bool {
	for _, c := range calls {
		if c == first || c.Pos() <= first.Pos() {
			continue
		}
		for _, p := range eofPositions {
			if p >= c.Pos() {
				return true
			}
		}
	}
	return false
}

// isInMemoryDecoder matches json.NewDecoder(strings.NewReader(...)) and
// json.NewDecoder(bytes.NewReader(...)).
func isInMemoryDecoder(pass *analysis.Pass, expr ast.Expr) bool {
	call, ok := astutil.UnwrapParenExpr(expr).(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return false
	}
	if pkg, name, ok := astutil.PackageCall(pass, call); !ok || pkg != "encoding/json" || name != "NewDecoder" {
		return false
	}
	reader, ok := astutil.UnwrapParenExpr(call.Args[0]).(*ast.CallExpr) //nolint:uncheckedsliceindex // len(call.Args) == 1 is checked above.
	if !ok {
		return false
	}
	pkg, name, ok := astutil.PackageCall(pass, reader)
	return ok && name == "NewReader" && (pkg == "strings" || pkg == "bytes")
}
