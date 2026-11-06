// Package ioutildeprecated implements a Go analysis linter that flags calls to
// functions from the deprecated io/ioutil package and suggests their replacements
// in the io and os packages (deprecated since Go 1.16).
package ioutildeprecated

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

// replacements maps deprecated ioutil function names to their modern equivalents.
var replacements = map[string]string{
	"ReadAll":   "io.ReadAll",
	"ReadFile":  "os.ReadFile",
	"WriteFile": "os.WriteFile",
	"TempFile":  "os.CreateTemp",
	"TempDir":   "os.MkdirTemp",
	"ReadDir":   "os.ReadDir",
	"NopCloser": "io.NopCloser",
	"Discard":   "io.Discard",
}

// Analyzer is the ioutil-deprecated analysis pass.
var Analyzer = analyzerutil.New("ioutildeprecated", "reports uses of deprecated io/ioutil functions that should be replaced with io or os package equivalents", run)

func run(pass *analysis.Pass) (any, error) {
	root, err := astutil.Root(pass)
	if err != nil {
		return nil, err
	}
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	if pass.TypesInfo == nil {
		return nil, nil
	}

	checkQualifiedIoutilUsage(pass, root, generatedFiles, noLintIndex)
	checkDotImportIoutilUsage(pass, root, generatedFiles, noLintIndex)
	return nil, nil
}

// checkQualifiedIoutilUsage reports ioutil.X usages via qualified selector expressions.
func checkQualifiedIoutilUsage(pass *analysis.Pass, root inspector.Cursor, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) {
	for cur := range root.Preorder((*ast.SelectorExpr)(nil)) {
		sel, ok := cur.Node().(*ast.SelectorExpr)
		if !ok {
			continue
		}
		pos := pass.Fset.PositionFor(sel.Pos(), false)
		if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
			continue
		}
		if nolint.HasDirectiveForLinter(pos, noLintIndex, "ioutildeprecated") {
			continue
		}
		pkgIdent, ok := sel.X.(*ast.Ident)
		if !ok {
			continue
		}
		obj := pass.TypesInfo.ObjectOf(pkgIdent)
		if obj == nil {
			continue
		}
		pkgName, ok := obj.(*types.PkgName)
		if !ok || pkgName.Imported().Path() != "io/ioutil" {
			continue
		}
		funcName := sel.Sel.Name
		if replacement, found := replacements[funcName]; found {
			pass.ReportRangef(sel, "ioutil.%s is deprecated; use %s instead", funcName, replacement)
		}
	}
}

// checkDotImportIoutilUsage reports bare ioutil identifiers (functions and variables)
// used via dot-imports (import . "io/ioutil").
func checkDotImportIoutilUsage(pass *analysis.Pass, root inspector.Cursor, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) {
	for cur := range root.Preorder((*ast.Ident)(nil)) {
		ident, ok := cur.Node().(*ast.Ident)
		if !ok {
			continue
		}
		// Skip the Sel of a selector (ioutil.X is handled by
		// checkQualifiedIoutilUsage; x.Discard is a field or method), but keep
		// the X side so a dot-imported receiver such as Discard.Write(p) is
		// still reported.
		if sel, ok := cur.Parent().Node().(*ast.SelectorExpr); ok && sel.Sel == ident {
			continue
		}
		pos := pass.Fset.PositionFor(ident.Pos(), false)
		if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
			continue
		}
		if nolint.HasDirectiveForLinter(pos, noLintIndex, "ioutildeprecated") {
			continue
		}
		obj := pass.TypesInfo.Uses[ident]
		if obj == nil {
			continue
		}
		pkg := obj.Pkg()
		if pkg == nil || pkg.Path() != "io/ioutil" {
			continue
		}
		name := obj.Name()
		if replacement, found := replacements[name]; found {
			pass.ReportRangef(ident, "ioutil.%s is deprecated; use %s instead", name, replacement)
		}
	}
}
