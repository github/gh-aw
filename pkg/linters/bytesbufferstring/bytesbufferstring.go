// Package bytesbufferstring implements a Go analysis linter that flags
// string(buf.Bytes()) calls where buf is a bytes.Buffer value receiver,
// suggesting buf.String() instead.
package bytesbufferstring

import (
	"fmt"
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/coverage"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

// Analyzer is the bytes-buffer-string analysis pass.
var Analyzer = analyzerutil.New("bytesbufferstring", "reports string(buf.Bytes()) calls where buf is a bytes.Buffer value and suggests buf.String() instead", run)

// hotThreshold gates findings on coverage data; see coverage package docs.
var hotThreshold *int

func init() {
	hotThreshold = coverage.RegisterHotThresholdFlag(Analyzer)
}

func run(pass *analysis.Pass) (any, error) {
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	nodeFilter := []ast.Node{(*ast.CallExpr)(nil)}
	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		analyzeStringBytesCall(pass, n, generatedFiles, noLintIndex)
	})
}

// analyzeStringBytesCall checks whether a call is a string(buf.Bytes()) that
// can be simplified to buf.String() and reports a diagnostic if so.
func analyzeStringBytesCall(pass *analysis.Pass, n ast.Node, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) {
	call, ok := n.(*ast.CallExpr)
	if !ok {
		return
	}

	// Match string(...) type conversion.
	typeInfo, ok := pass.TypesInfo.Types[call.Fun]
	if !ok || !typeInfo.IsType() {
		return
	}
	basic, ok := typeInfo.Type.(*types.Basic)
	if !ok || basic.Kind() != types.String {
		return
	}
	if len(call.Args) != 1 {
		return
	}

	pos := pass.Fset.PositionFor(call.Pos(), false)
	if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
		return
	}
	if nolint.HasDirectiveForLinter(pos, noLintIndex, "bytesbufferstring") {
		return
	}

	_, sel, ok := matchBufBytesArg(pass, call)
	if !ok {
		return
	}

	receiverText := astutil.NodeText(pass.Fset, sel.X)
	if receiverText == "" {
		return
	}
	if !coverage.ShouldApply(pass, call.Pos(), *hotThreshold) {
		return
	}

	var fixes []analysis.SuggestedFix
	if !astutil.HasOverlappingComment(pass.Files, call.Pos(), call.End()) {
		fixes = []analysis.SuggestedFix{{
			Message: fmt.Sprintf("Replace string(%s.Bytes()) with %s.String()", receiverText, receiverText),
			TextEdits: []analysis.TextEdit{{
				Pos:     call.Pos(),
				End:     call.End(),
				NewText: []byte(receiverText + ".String()"),
			}},
		}}
	}
	pass.Report(analysis.Diagnostic{
		Pos:            call.Pos(),
		End:            call.End(),
		Message:        fmt.Sprintf("string(%s.Bytes()) can be simplified to %s.String()", receiverText, receiverText),
		SuggestedFixes: fixes,
	})
}

// matchBufBytesArg checks whether call.Args[0] is a buf.Bytes() call where
// buf is a bytes.Buffer value (not pointer). Returns the inner call, selector,
// and ok=true when matched.
func matchBufBytesArg(pass *analysis.Pass, call *ast.CallExpr) (*ast.CallExpr, *ast.SelectorExpr, bool) {
	inner, ok := call.Args[0].(*ast.CallExpr)
	if !ok {
		return nil, nil, false
	}
	sel, ok := inner.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Bytes" {
		return nil, nil, false
	}
	if len(inner.Args) != 0 {
		return nil, nil, false
	}
	receiverType := pass.TypesInfo.TypeOf(sel.X)
	if receiverType == nil || !isBytesBufferValue(receiverType) {
		return nil, nil, false
	}
	return inner, sel, true
}

// isBytesBufferValue reports whether t is exactly bytes.Buffer (value receiver, not pointer).
// We intentionally exclude *bytes.Buffer: the rewrite string(buf.Bytes()) → buf.String() is not
// semantics-preserving when buf is nil — the former panics while the latter returns "<nil>".
func isBytesBufferValue(t types.Type) bool {
	named, ok := t.(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj.Pkg() != nil && obj.Pkg().Path() == "bytes" && obj.Name() == "Buffer"
}
