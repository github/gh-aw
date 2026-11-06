// Package sprintfbool implements a Go analysis linter that flags
// fmt.Sprintf("%t", b) calls where b is a single bool value and suggests
// using strconv.FormatBool(b) instead.
package sprintfbool

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

const (
	strconvPkg = "strconv"
	fmtPkg     = "fmt"
)

type replacement struct {
	argText   string
	qualifier string
	canFix    bool
}

type candidate struct {
	call        *ast.CallExpr
	arg         ast.Expr
	file        *ast.File
	replacement replacement
}

// Analyzer is the sprintfbool analysis pass.
var Analyzer = analyzerutil.New("sprintfbool", `reports fmt.Sprintf("%t", b) calls where b is a single bool value; use strconv.FormatBool(b) instead`, analyzeSprintfBool)

func analyzeSprintfBool(pass *analysis.Pass) (any, error) {
	insp, err := astutil.Inspector(pass)
	if err != nil {
		return nil, err
	}
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	seenImportFiles := make(map[token.Pos]bool)
	candidates, targetCallsByFile, filesByPos := collectSprintfBoolCandidates(pass, insp, generatedFiles, noLintIndex)

	resolvedCandidates := make([]candidate, 0, len(candidates))
	fixableCallsByFile := make(map[token.Pos]int)
	for _, c := range candidates {
		repl := replacementForCall(pass, c.call, c.arg, c.file)
		c.replacement = repl
		if repl.canFix && c.file != nil {
			fixableCallsByFile[c.file.Pos()]++
		}
		resolvedCandidates = append(resolvedCandidates, c)
	}

	orphanFmtByFile := computeOrphanedFmtImportStatus(pass, filesByPos, targetCallsByFile, fixableCallsByFile)
	reportSprintfBoolDiagnostics(pass, resolvedCandidates, seenImportFiles, orphanFmtByFile)

	return nil, nil
}

// collectSprintfBoolCandidates traverses the AST and returns all fmt.Sprintf("%t", b) calls.
func collectSprintfBoolCandidates(pass *analysis.Pass, insp *inspector.Inspector, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) ([]candidate, map[token.Pos]int, map[token.Pos]*ast.File) {
	candidates := make([]candidate, 0)
	targetCallsByFile, filesByPos := make(map[token.Pos]int), make(map[token.Pos]*ast.File)
	insp.Preorder([]ast.Node{(*ast.CallExpr)(nil)}, func(n ast.Node) {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return
		}
		pos := pass.Fset.PositionFor(call.Pos(), false)
		if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
			return
		}
		if nolint.HasDirectiveForLinter(pos, noLintIndex, "sprintfbool") {
			return
		}
		sel, ok := astutil.UnwrapParenExpr(call.Fun).(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Sprintf" {
			return
		}
		if !astutil.IsPkgSelector(pass, sel, "fmt") {
			return
		}
		if len(call.Args) != 2 {
			return
		}
		var formatArg ast.Expr
		var arg ast.Expr
		for i, callArg := range call.Args {
			switch i {
			case 0:
				formatArg = callArg
			case 1:
				arg = callArg
			}
		}
		if formatArg == nil || arg == nil {
			return
		}
		formatLit, ok := astutil.UnwrapParenExpr(formatArg).(*ast.BasicLit)
		if !ok || formatLit.Kind != token.STRING || formatLit.Value != `"%t"` {
			return
		}
		argType := pass.TypesInfo.TypeOf(arg)
		if argType == nil || argType != types.Universe.Lookup("bool").Type() {
			return
		}
		file := astutil.FileForPos(pass.Files, call.Pos())
		if file != nil {
			targetCallsByFile[file.Pos()]++
			filesByPos[file.Pos()] = file
		}
		candidates = append(candidates, candidate{call: call, arg: arg, file: file})
	})
	return candidates, targetCallsByFile, filesByPos
}

// computeOrphanedFmtImportStatus returns a map of file positions where fmt is imported
// solely for the flagged Sprintf calls (and thus can be removed after the fix).
func computeOrphanedFmtImportStatus(pass *analysis.Pass, filesByPos map[token.Pos]*ast.File, targetCallsByFile, fixableCallsByFile map[token.Pos]int) map[token.Pos]bool {
	orphanFmtByFile := make(map[token.Pos]bool)
	for filePos, targetCalls := range targetCallsByFile {
		file := filesByPos[filePos]
		if file == nil {
			continue
		}
		_, fmtImported := astutil.ImportedAs(file, pass.TypesInfo, fmtPkg)
		orphanFmtByFile[filePos] = fmtImported &&
			astutil.CountPkgUsesInFile(pass, file, fmtPkg) == targetCalls &&
			fixableCallsByFile[filePos] == targetCalls
	}
	return orphanFmtByFile
}

// reportSprintfBoolDiagnostics emits one diagnostic per candidate.
func reportSprintfBoolDiagnostics(pass *analysis.Pass, candidates []candidate, seenImportFiles map[token.Pos]bool, orphanFmtByFile map[token.Pos]bool) {
	for _, c := range candidates {
		repl := c.replacement
		argText := repl.argText
		if argText == "" {
			argText = astutil.NodeText(pass.Fset, c.arg)
		}
		if argText == "" {
			argText = "b"
		}
		var fixes []analysis.SuggestedFix
		if repl.canFix {
			fixes = buildFormatBoolFix(pass, c.call, repl.argText, repl.qualifier, c.file, seenImportFiles, orphanFmtByFile)
		}
		pass.Report(analysis.Diagnostic{
			Pos:            c.call.Pos(),
			End:            c.call.End(),
			Message:        `use strconv.FormatBool(` + argText + `) instead of fmt.Sprintf("%t", ` + argText + `)`,
			SuggestedFixes: fixes,
		})
	}
}

func buildFormatBoolFix(
	pass *analysis.Pass,
	call *ast.CallExpr,
	argText string,
	qualifier string,
	file *ast.File,
	seenImportFiles map[token.Pos]bool,
	orphanFmtByFile map[token.Pos]bool,
) []analysis.SuggestedFix {
	edits := []analysis.TextEdit{{
		Pos:     call.Pos(),
		End:     call.End(),
		NewText: []byte(qualifier + ".FormatBool(" + argText + ")"),
	}}

	if file != nil {
		edits = append(edits, buildStrconvFmtImportEdits(pass, file, seenImportFiles, orphanFmtByFile)...)
	}

	return []analysis.SuggestedFix{{
		Message:   "Replace fmt.Sprintf with " + qualifier + ".FormatBool",
		TextEdits: edits,
	}}
}

// buildStrconvFmtImportEdits returns TextEdits that add "strconv" to file and,
// when the file's "fmt" import becomes unused after the fix, also remove it.
// seenImportFiles prevents duplicate overlapping edits in files with multiple
// violations.
func buildStrconvFmtImportEdits(
	pass *analysis.Pass,
	file *ast.File,
	seenImportFiles map[token.Pos]bool,
	orphanFmtByFile map[token.Pos]bool,
) []analysis.TextEdit {
	if seenImportFiles[file.Pos()] {
		return nil
	}

	_, fmtImported := astutil.ImportedAs(file, pass.TypesInfo, fmtPkg)
	orphanFmt := fmtImported && orphanFmtByFile[file.Pos()]

	edits, needed := astutil.SwapPkgImportEdits(pass, file, strconvPkg, fmtPkg, orphanFmt)
	if !needed {
		return nil
	}
	seenImportFiles[file.Pos()] = true
	return edits
}

func replacementForCall(pass *analysis.Pass, call *ast.CallExpr, arg ast.Expr, file *ast.File) replacement {
	argText := astutil.NodeText(pass.Fset, arg)
	if argText == "" {
		return replacement{}
	}

	qualifier := strconvPkg
	if file != nil {
		if localName, imported := astutil.ImportedAs(file, pass.TypesInfo, strconvPkg); imported {
			if localName == "." || localName == "_" {
				return replacement{argText: argText}
			}
			qualifier = localName
		}
	}

	if astutil.QualifierShadowed(pass.Pkg, call.Pos(), qualifier, strconvPkg) {
		return replacement{argText: argText}
	}
	if astutil.HasOverlappingComment(pass.Files, call.Pos(), call.End()) {
		return replacement{argText: argText}
	}

	return replacement{
		argText:   argText,
		qualifier: qualifier,
		canFix:    true,
	}
}
