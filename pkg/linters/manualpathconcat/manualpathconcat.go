// Package manualpathconcat implements a Go analysis linter that flags string
// concatenation using a literal "/" separator to build filesystem paths
// (e.g. dir + "/" + file), which should use filepath.Join (or path.Join for
// slash-separated paths) instead.
//
// Manual "/" concatenation is error-prone: it can produce double slashes when
// an operand already ends with a separator, it skips the Clean-style
// normalization that filepath.Join performs, and it hard-codes the forward
// slash separator instead of the OS-specific one.
package manualpathconcat

import (
	"fmt"
	"go/ast"
	"go/token"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

// linterName is the analyzer name, also used for nolint directive matching.
const linterName = "manualpathconcat"

// Analyzer is the manual-path-concat analysis pass.
var Analyzer = analyzerutil.New(linterName, `reports manual "/" separator string concatenation used to build paths (e.g. dir + "/" + file) that should use filepath.Join or path.Join`, run)

func run(pass *analysis.Pass) (any, error) {
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	// reported tracks the sub-expressions of an already reported concatenation
	// chain so that `a + "/" + b + "/" + c` produces a single diagnostic.
	reported := make(map[ast.Expr]bool)

	nodeFilter := []ast.Node{(*ast.BinaryExpr)(nil), (*ast.AssignStmt)(nil)}
	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		analyzeBinaryExpr(pass, n, generatedFiles, noLintIndex, reported)
		analyzeAssignStmt(pass, n, generatedFiles, noLintIndex)
	})
}

// analyzeBinaryExpr reports binary expressions of the shape X + "/" + Y.
func analyzeBinaryExpr(pass *analysis.Pass, n ast.Node, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex, reported map[ast.Expr]bool) {
	bin, ok := n.(*ast.BinaryExpr)
	if !ok || reported[bin] {
		return
	}
	left, rightOverride, ok := matchSlashSeparator(bin)
	if !ok {
		return
	}
	// A fully constant expression may appear in a const declaration, where a
	// filepath.Join call is not valid Go, so it is left alone.
	if tv, found := pass.TypesInfo.Types[bin]; found && tv.Value != nil {
		return
	}
	pos := pass.Fset.PositionFor(bin.Pos(), false)
	if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
		return
	}
	if nolint.HasDirectiveForLinter(pos, noLintIndex, linterName) {
		return
	}
	markChain(bin, reported)

	leftText := astutil.NodeText(pass.Fset, left)
	rightText := astutil.NodeText(pass.Fset, bin.Y)
	if rightOverride != "" {
		rightText = rightOverride
	}
	message := `manual "/" path concatenation; use filepath.Join (or path.Join) instead`
	if isShortOperandText(leftText) && isShortOperandText(rightText) && !containsSlashConcat(left) {
		message = fmt.Sprintf(`manual "/" path concatenation; use filepath.Join(%s, %s) (or path.Join) instead`, leftText, rightText)
	}
	pass.Report(analysis.Diagnostic{
		Pos:     bin.Pos(),
		End:     bin.End(),
		Message: message,
	})
}

// analyzeAssignStmt reports compound assignments of the shape X += "/" + Y,
// as well as the two-operand X += "/subpath" form.
func analyzeAssignStmt(pass *analysis.Pass, n ast.Node, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) {
	assign, ok := n.(*ast.AssignStmt)
	if !ok || assign.Tok != token.ADD_ASSIGN || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
		return
	}
	var rightExpr ast.Expr
	var rightOverride string
	switch rhs := assign.Rhs[0].(type) {
	case *ast.BinaryExpr:
		if rhs.Op != token.ADD || !isSlashLiteral(rhs.X) {
			return
		}
		rightExpr = rhs.Y
	default:
		trimmed, isEmbedded := embeddedSlashLiteral(assign.Rhs[0])
		if !isEmbedded {
			return
		}
		rightExpr = assign.Rhs[0]
		rightOverride = trimmed
	}
	pos := pass.Fset.PositionFor(assign.Pos(), false)
	if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
		return
	}
	if nolint.HasDirectiveForLinter(pos, noLintIndex, linterName) {
		return
	}

	leftText := astutil.NodeText(pass.Fset, assign.Lhs[0])
	rightText := astutil.NodeText(pass.Fset, rightExpr)
	if rightOverride != "" {
		rightText = rightOverride
	}
	message := `manual "/" path concatenation; use filepath.Join (or path.Join) instead`
	if isShortOperandText(leftText) && isShortOperandText(rightText) && !containsSlashConcat(assign.Lhs[0]) {
		message = fmt.Sprintf(`manual "/" path concatenation; use filepath.Join(%s, %s) (or path.Join) instead`, leftText, rightText)
	}
	pass.Report(analysis.Diagnostic{
		Pos:     assign.Pos(),
		End:     assign.End(),
		Message: message,
	})
}

// matchSlashSeparator reports whether bin has the shape X + "/" + Y, which Go
// parses as ((X + "/") + Y), or the two-operand shape X + "/subpath", where
// the leading slash is embedded in a longer literal. It returns the X operand
// (left) and, for the two-operand shape, a quoted rightOverride text (the
// literal with its leading slash stripped) suitable for the diagnostic
// message; rightOverride is empty for the three-operand shape, where the
// caller uses bin.Y's own source text instead.
func matchSlashSeparator(bin *ast.BinaryExpr) (left ast.Expr, rightOverride string, ok bool) {
	if bin.Op != token.ADD {
		return nil, "", false
	}
	if inner, isBinary := bin.X.(*ast.BinaryExpr); isBinary && inner.Op == token.ADD && isSlashLiteral(inner.Y) {
		// A left operand that is itself the separator (e.g. `"/" + "/" + name`)
		// carries no path segment to join, so it is not a manual join.
		if isSlashLiteral(inner.X) {
			return nil, "", false
		}
		return inner.X, "", true
	}
	if trimmed, isEmbedded := embeddedSlashLiteral(bin.Y); isEmbedded {
		// A left operand that is itself a string literal makes the whole
		// expression a compile-time constant, where filepath.Join is not a
		// valid substitute (and the caller's constant check would exclude it
		// anyway); skip it here so matchSlashSeparator's result is correct on
		// its own.
		if _, leftIsLit := bin.X.(*ast.BasicLit); leftIsLit {
			return nil, "", false
		}
		return bin.X, trimmed, true
	}
	return nil, "", false
}

// isSlashLiteral reports whether expr is the string literal "/".
func isSlashLiteral(expr ast.Expr) bool {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return false
	}
	val, err := strconv.Unquote(lit.Value)
	return err == nil && val == "/"
}

// embeddedSlashLiteral reports whether expr is a string literal that begins
// with "/" and carries additional path text after it (e.g. "/config.yml"),
// as opposed to the bare separator "/" alone. On success it returns the
// literal's text quoted without the leading slash (e.g. `"config.yml"`),
// suitable for a filepath.Join diagnostic argument.
func embeddedSlashLiteral(expr ast.Expr) (trimmedQuoted string, ok bool) {
	lit, isLit := expr.(*ast.BasicLit)
	if !isLit || lit.Kind != token.STRING {
		return "", false
	}
	val, err := strconv.Unquote(lit.Value)
	if err != nil || !strings.HasPrefix(val, "/") || val == "/" {
		return "", false
	}
	return strconv.Quote(strings.TrimPrefix(val, "/")), true
}

// maxOperandTextLen bounds the operand source text embedded in a diagnostic
// message so that long or multi-line operands do not produce unreadable output.
const maxOperandTextLen = 48

// isShortOperandText reports whether text is a non-empty, single-line operand
// short enough to quote in a diagnostic message.
func isShortOperandText(text string) bool {
	return text != "" && len(text) <= maxOperandTextLen && !strings.ContainsAny(text, "\n\r")
}

// containsSlashConcat reports whether expr contains a string concatenation that
// includes a "/" literal operand (e.g. `a + "/" + b`).
func containsSlashConcat(expr ast.Expr) bool {
	bin, ok := expr.(*ast.BinaryExpr)
	if !ok || bin.Op != token.ADD {
		return false
	}
	if isSlashLiteral(bin.X) || isSlashLiteral(bin.Y) {
		return true
	}
	return containsSlashConcat(bin.X) || containsSlashConcat(bin.Y)
}

// markChain records bin and every nested left-hand concatenation operand so
// that the sub-expressions of a reported chain are not reported again.
func markChain(bin *ast.BinaryExpr, reported map[ast.Expr]bool) {
	for expr := ast.Expr(bin); ; {
		inner, ok := expr.(*ast.BinaryExpr)
		if !ok {
			return
		}
		reported[inner] = true
		expr = inner.X
	}
}
