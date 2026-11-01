// Package boolliteralcompare implements a Go analysis linter that flags
// comparisons of bool-typed expressions against the true or false literals.
package boolliteralcompare

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

// Analyzer is the bool-literal-compare analysis pass.
var Analyzer = analyzerutil.New("boolliteralcompare", "reports == and != comparisons of bool-typed expressions against the true or false literals, which should use the expression directly or its negation", run)

func run(pass *analysis.Pass) (any, error) {
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	nodeFilter := []ast.Node{(*ast.BinaryExpr)(nil)}
	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		analyzeBinary(pass, n, generatedFiles, noLintIndex)
	})
}

func analyzeBinary(pass *analysis.Pass, n ast.Node, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) {
	bin, ok := n.(*ast.BinaryExpr)
	if !ok || (bin.Op != token.EQL && bin.Op != token.NEQ) {
		return
	}

	operand, literal, ok := splitOperands(pass, bin)
	if !ok {
		return
	}
	if !isBoolType(pass, operand) {
		return
	}

	pos := pass.Fset.PositionFor(bin.Pos(), false)
	if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
		return
	}
	if nolint.HasDirectiveForLinter(pos, noLintIndex, "boolliteralcompare") {
		return
	}

	// "x == true" and "x != false" keep x; the other two negate it.
	negate := (bin.Op == token.EQL) != (literal == "true")
	// Both sides being constants is not worth rewriting.
	if tv, ok := pass.TypesInfo.Types[operand]; ok && tv.Value != nil {
		return
	}

	text := astutil.NodeText(pass.Fset, operand)
	replacement := text
	message := "comparing a bool to " + literal + " is redundant; use the expression directly"
	if negate {
		message = "comparing a bool to " + literal + " is redundant; use the negated expression"
		replacement = "!" + text
	}

	diag := analysis.Diagnostic{Pos: bin.Pos(), End: bin.End(), Message: message}
	if canFix(operand) && !astutil.HasOverlappingComment(pass.Files, bin.Pos(), bin.End()) {
		diag.SuggestedFixes = []analysis.SuggestedFix{{
			Message:   "Remove the redundant bool literal comparison",
			TextEdits: []analysis.TextEdit{{Pos: bin.Pos(), End: bin.End(), NewText: []byte(replacement)}},
		}}
	}
	pass.Report(diag)
}

// splitOperands returns the non-literal operand and the literal name ("true"
// or "false") when exactly one side is the predeclared true or false.
func splitOperands(pass *analysis.Pass, bin *ast.BinaryExpr) (ast.Expr, string, bool) {
	if lit, ok := boolLiteral(pass, bin.Y); ok {
		if _, other := boolLiteral(pass, bin.X); !other {
			return bin.X, lit, true
		}
		return nil, "", false
	}
	if lit, ok := boolLiteral(pass, bin.X); ok {
		return bin.Y, lit, true
	}
	return nil, "", false
}

func boolLiteral(pass *analysis.Pass, expr ast.Expr) (string, bool) {
	ident, ok := astutil.UnwrapParenExpr(expr).(*ast.Ident)
	if !ok || (ident.Name != "true" && ident.Name != "false") {
		return "", false
	}
	if obj := pass.TypesInfo.Uses[ident]; obj == nil || obj != types.Universe.Lookup(ident.Name) {
		return "", false
	}
	return ident.Name, true
}

// isBoolType reports whether expr has a bool underlying type; comparisons of
// interface values against true or false are not redundant.
func isBoolType(pass *analysis.Pass, expr ast.Expr) bool {
	tv, ok := pass.TypesInfo.Types[expr]
	if !ok || tv.Type == nil {
		return false
	}
	basic, ok := tv.Type.Underlying().(*types.Basic)
	return ok && basic.Info()&types.IsBoolean != 0 && basic.Info()&types.IsUntyped == 0
}

// canFix reports whether the operand can be negated with a plain "!" prefix
// without changing precedence.
func canFix(expr ast.Expr) bool {
	switch expr.(type) {
	case *ast.Ident, *ast.SelectorExpr, *ast.CallExpr, *ast.IndexExpr, *ast.ParenExpr:
		return true
	}
	return false
}
