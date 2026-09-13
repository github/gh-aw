// Package writebytestring implements a Go analysis linter that flags
// w.Write([]byte(s)) calls where s is a string, which can be replaced with
// io.WriteString(w, s) to avoid an unnecessary []byte allocation.
package writebytestring

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/coverage"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

const (
	ioPkg            = "io"
	importSpecIndent = "\t"
)

// writerIface is a synthetic *types.Interface matching io.Writer:
//
//	Write(p []byte) (n int, err error)
//
// Built once at package init so it can be reused across analysis passes.
var writerIface = func() *types.Interface {
	byteSlice := types.NewSlice(types.Typ[types.Byte])
	errType := types.Universe.Lookup("error").Type()
	params := types.NewTuple(types.NewVar(token.NoPos, nil, "p", byteSlice))
	results := types.NewTuple(
		types.NewVar(token.NoPos, nil, "n", types.Typ[types.Int]),
		types.NewVar(token.NoPos, nil, "err", errType),
	)
	sig := types.NewSignatureType(nil, nil, nil, params, results, false)
	method := types.NewFunc(token.NoPos, nil, "Write", sig)
	iface := types.NewInterfaceType([]*types.Func{method}, nil)
	iface.Complete()
	return iface
}()

// Analyzer is the write-byte-string analysis pass.
var Analyzer = analyzerutil.New("writebytestring", "reports w.Write([]byte(s)) calls where s is a string that can be replaced with io.WriteString(w, s)", run)

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
	filesWithImportEdit := make(map[token.Pos]bool)

	nodeFilter := []ast.Node{(*ast.CallExpr)(nil)}
	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		analyzeWriteCall(pass, n, generatedFiles, noLintIndex, filesWithImportEdit)
	})
}

// analyzeWriteCall checks whether a call expression is a w.Write([]byte(s))
// pattern that can be replaced with io.WriteString(w, s).
func analyzeWriteCall(pass *analysis.Pass, n ast.Node, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex, filesWithImportEdit map[token.Pos]bool) {
	call, ok := n.(*ast.CallExpr)
	if !ok {
		return
	}

	// Match <expr>.Write(<arg>)
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Write" {
		return
	}
	if len(call.Args) != 1 {
		return
	}

	pos := pass.Fset.PositionFor(call.Pos(), false)
	if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
		return
	}
	if nolint.HasDirectiveForLinter(pos, noLintIndex, "writebytestring") {
		return
	}

	// The single argument must be a []byte(s) conversion where s is a string.
	conv, ok := call.Args[0].(*ast.CallExpr)
	if !ok {
		return
	}
	if !astutil.IsByteSliceConversion(pass, conv) {
		return
	}
	strArg := conv.Args[0]
	if !astutil.IsStringType(pass, strArg) {
		return
	}

	// The receiver must implement io.Writer.
	if !implementsWriter(pass, sel.X) {
		return
	}

	if !coverage.ShouldApply(pass, call.Pos(), *hotThreshold) {
		return
	}

	sText := astutil.NodeText(pass.Fset, strArg)
	wText := astutil.NodeText(pass.Fset, sel.X)
	if sText == "" || wText == "" {
		return
	}

	sExpr := buildStringExpr(pass, strArg, sText)
	writerArg := buildWriterArg(pass, sel.X, wText)

	pass.Report(analysis.Diagnostic{
		Pos:            call.Pos(),
		End:            call.End(),
		Message:        fmt.Sprintf("%s.Write([]byte(%s)) can be replaced with io.WriteString(%s, %s) to potentially avoid a []byte allocation if the writer implements io.StringWriter", wText, sText, writerArg, sExpr),
		SuggestedFixes: buildFix(pass, call, writerArg, sExpr, filesWithImportEdit),
	})
}

// buildStringExpr returns the expression text for the io.WriteString string argument.
// If the argument is a named string type, it wraps it in string(...) for type safety.
func buildStringExpr(pass *analysis.Pass, strArg ast.Expr, sText string) string {
	if st := pass.TypesInfo.TypeOf(strArg); st != nil && !isExactString(st) {
		return "string(" + sText + ")"
	}
	return sText
}

// buildWriterArg returns the expression text for the io.WriteString writer argument.
// When the receiver's Write method lives on the pointer type, it returns "&wText".
func buildWriterArg(pass *analysis.Pass, recv ast.Expr, wText string) string {
	if t := pass.TypesInfo.TypeOf(recv); t != nil &&
		!types.Implements(t, writerIface) &&
		types.Implements(types.NewPointer(t), writerIface) {
		return "&" + wText
	}
	return wText
}

// isExactString reports whether t is the predeclared string type, not a named
// type whose underlying type is string. io.WriteString(w Writer, s string)
// requires a predeclared string; named string types need an explicit string(...)
// conversion to satisfy the parameter type.
func isExactString(t types.Type) bool {
	b, ok := t.(*types.Basic)
	return ok && b.Kind() == types.String
}

// implementsWriter reports whether expr's type implements io.Writer.
// It uses types.Implements against a synthetic io.Writer interface so the check
// is idiomatic and avoids manually re-implementing the signature comparison.
// Only T and *T are tried; **T is never constructed so pointer types are not
// double-wrapped.
func implementsWriter(pass *analysis.Pass, expr ast.Expr) bool {
	t := pass.TypesInfo.TypeOf(expr)
	if t == nil {
		return false
	}
	if types.Implements(t, writerIface) {
		return true
	}
	// Only add a pointer wrapper when t is not already a pointer, to avoid
	// constructing a semantically meaningless **T type.
	if _, alreadyPtr := t.Underlying().(*types.Pointer); alreadyPtr {
		return false
	}
	return types.Implements(types.NewPointer(t), writerIface)
}

// buildFix returns a SuggestedFix rewriting w.Write([]byte(s)) to io.WriteString(w, s).
//
// When "io" is already imported under an alias the alias is used as the
// qualifier so the fix compiles. When the qualifier name is shadowed by a local
// variable at the call site, no SuggestedFix is emitted (the diagnostic is
// still reported).
func buildFix(pass *analysis.Pass, call *ast.CallExpr, writerArg, sText string, filesWithImportEdit map[token.Pos]bool) []analysis.SuggestedFix {
	if astutil.HasOverlappingComment(pass.Files, call.Pos(), call.End()) {
		return nil
	}
	// Find the file containing this call.
	file := astutil.FileForPos(pass.Files, call.Pos())

	// Determine the local qualifier for "io": use the alias when the package
	// is already imported under a different name, or the default name when it
	// needs to be added.
	qualifier := ioPkg
	if file != nil {
		if localName, imported := astutil.ImportedAs(file, pass.TypesInfo, ioPkg); imported {
			// Dot-import or blank-import: can't safely qualify; skip fix.
			if localName == "." || localName == "_" {
				return nil
			}
			qualifier = localName
		}
		// Not imported yet: qualifier stays as ioPkg; the import will be added.
	}

	// Skip the fix if the qualifier is shadowed by a local at the call site.
	if astutil.QualifierShadowed(pass.Pkg, call.Pos(), qualifier, ioPkg) {
		return nil
	}

	replacement := fmt.Sprintf("%s.WriteString(%s, %s)", qualifier, writerArg, sText)
	edits := []analysis.TextEdit{{
		Pos:     call.Pos(),
		End:     call.End(),
		NewText: []byte(replacement),
	}}
	if importEdit, ok := addIOImportEdit(pass, call.Pos(), filesWithImportEdit); ok {
		edits = append(edits, importEdit)
	}

	return []analysis.SuggestedFix{{
		Message:   "Replace with " + replacement,
		TextEdits: edits,
	}}
}

// addIOImportEdit returns a TextEdit that inserts an import for "io" into the
// file containing pos, unless "io" is already imported in that file or an
// import edit for this file has already been emitted in this pass.
func addIOImportEdit(pass *analysis.Pass, pos token.Pos, filesWithImportEdit map[token.Pos]bool) (analysis.TextEdit, bool) {
	file := astutil.FileForPos(pass.Files, pos)
	if file == nil {
		return analysis.TextEdit{}, false
	}
	if filesWithImportEdit[file.Pos()] {
		return analysis.TextEdit{}, false
	}

	for _, imp := range file.Imports {
		if imp.Path.Value == `"`+ioPkg+`"` {
			return analysis.TextEdit{}, false
		}
	}

	edit, ok := buildIOImportTextEdit(pass, file)
	if ok {
		filesWithImportEdit[file.Pos()] = true
	}
	return edit, ok
}

// buildIOImportTextEdit constructs a TextEdit that adds an "io" import to file.
// It handles three cases: appending to an existing grouped import block,
// converting a single ungrouped import to a grouped block, or inserting a new
// standalone import after the package declaration.
func buildIOImportTextEdit(pass *analysis.Pass, file *ast.File) (analysis.TextEdit, bool) {
	// Case 1: existing grouped import block — append to its closing paren.
	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.IMPORT || !genDecl.Lparen.IsValid() {
			continue
		}
		return analysis.TextEdit{
			Pos:     genDecl.Rparen,
			End:     genDecl.Rparen,
			NewText: []byte(importSpecIndent + `"` + ioPkg + `"` + "\n"),
		}, true
	}

	// Case 2: exactly one ungrouped import — rebuild as a grouped block.
	var singleDecl *ast.GenDecl
	importDeclCount := 0
	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.IMPORT {
			continue
		}
		importDeclCount++
		if !genDecl.Lparen.IsValid() && len(genDecl.Specs) == 1 {
			singleDecl = genDecl
		}
	}
	if importDeclCount == 1 && singleDecl != nil {
		specText := astutil.NodeText(pass.Fset, singleDecl.Specs[0])
		if specText != "" {
			return analysis.TextEdit{
				Pos:     singleDecl.Pos(),
				End:     singleDecl.End(),
				NewText: []byte("import (\n" + importSpecIndent + specText + "\n" + importSpecIndent + `"` + ioPkg + `"` + "\n)"),
			}, true
		}
		// Fall through to the standalone insertion below.
	}

	// Case 3: no existing import block — insert a standalone import after the package name.
	return analysis.TextEdit{
		Pos:     file.Name.End(),
		End:     file.Name.End(),
		NewText: []byte("\n\nimport \"" + ioPkg + "\""),
	}, true
}
