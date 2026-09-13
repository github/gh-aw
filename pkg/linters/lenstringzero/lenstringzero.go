// Package lenstringzero implements a Go analysis linter that flags len(s) == 0,
// len(s) != 0, and equivalent relational comparisons (len(s) > 0, len(s) >= 1,
// len(s) < 1, len(s) <= 0) on string values that should use == "" or != "" instead.
package lenstringzero

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
	"golang.org/x/tools/go/analysis"
)

var Analyzer = analyzerutil.New("lenstringzero", "reports len(s) == 0, len(s) != 0, and equivalent relational comparisons "+
	"(len(s) > 0, len(s) >= 1, len(s) < 1, len(s) <= 0) on string values "+
	"that should use == \"\" or != \"\" instead", run)

func run(pass *analysis.Pass) (any, error) {
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}
	lenStringAliases := collectLenStringAliases(pass)
	nodeFilter := []ast.Node{(*ast.BinaryExpr)(nil)}
	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		analyzeLenStringExpr(pass, n, generatedFiles, noLintIndex, lenStringAliases)
	})
}

// analyzeLenStringExpr checks whether a binary expression is a len(s) comparison
// with 0 or 1 that should use == "" or != "" instead.
func analyzeLenStringExpr(pass *analysis.Pass, n ast.Node, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex, lenStringAliases map[types.Object]ast.Expr) {
	expr, ok := n.(*ast.BinaryExpr)
	if !ok {
		return
	}
	switch expr.Op {
	case token.EQL, token.NEQ, token.GTR, token.GEQ, token.LSS, token.LEQ:
	default:
		return
	}
	pos := pass.Fset.PositionFor(expr.Pos(), false)
	if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
		return
	}
	if nolint.HasDirectiveForLinter(pos, noLintIndex, "lenstringzero") {
		return
	}
	lenArg, lenNode, isDirect, normalOp, lit, matched := matchLenLiteralExpr(pass, expr, lenStringAliases)
	if !matched {
		return
	}
	fixOp, cmpVerb, valid := resolveFixOp(normalOp, lit)
	if !valid {
		return
	}
	t := pass.TypesInfo.TypeOf(lenArg)
	if t == nil {
		return
	}
	basic, ok := t.Underlying().(*types.Basic)
	if !ok || basic.Kind() != types.String {
		return
	}
	argText := exprTextOr(pass, lenArg, "s")
	lenText := exprTextOr(pass, lenNode, "len(s)")
	var fixes []analysis.SuggestedFix
	if isDirect {
		fixes = buildLenStringFix(pass, expr, lenArg, fixOp)
	}
	pass.Report(analysis.Diagnostic{
		Pos:            expr.Pos(),
		End:            expr.End(),
		Message:        fmt.Sprintf(`use %s %s "" to check for %s string instead of %s %s %d`, argText, fixOp, cmpVerb, lenText, normalOp, lit),
		SuggestedFixes: fixes,
	})
}

// matchLenLiteralExpr tries to match len(s)/alias OP literal or literal OP len(s)/alias.
// Returns (lenArg, lenNode, isDirect, normalOp, lit, matched) where:
//   - lenArg is the string expression passed to len()
//   - lenNode is the expression being compared (the len() call or the alias identifier)
//   - isDirect indicates a direct len() call (true) vs a stored alias (false)
//   - normalOp is the operator normalized so that len is on the left side
//   - lit is the integer literal value (0 or 1)
//   - matched indicates whether a valid pattern was found
func matchLenLiteralExpr(pass *analysis.Pass, expr *ast.BinaryExpr, aliases map[types.Object]ast.Expr) (lenArg ast.Expr, lenNode ast.Expr, isDirect bool, normalOp token.Token, lit int, ok bool) {
	op := expr.Op

	// Normal order: len/alias on the left, literal on the right.
	if isLenCall(expr.X) {
		if isIntZero(expr.Y) {
			return lenCallArg(expr.X), expr.X, true, op, 0, true
		}
		if isIntOne(expr.Y) {
			return lenCallArg(expr.X), expr.X, true, op, 1, true
		}
	}
	if isIntZero(expr.Y) {
		if arg, ok2 := lenAliasArg(pass, expr.X, aliases); ok2 {
			return arg, expr.X, false, op, 0, true
		}
	}
	if isIntOne(expr.Y) {
		if arg, ok2 := lenAliasArg(pass, expr.X, aliases); ok2 {
			return arg, expr.X, false, op, 1, true
		}
	}

	// Yoda order: literal on the left, len/alias on the right.
	// Flip the operator so the normalized form has len on the left.
	if isLenCall(expr.Y) {
		if isIntZero(expr.X) {
			return lenCallArg(expr.Y), expr.Y, true, astutil.FlipComparisonOp(op), 0, true
		}
		if isIntOne(expr.X) {
			return lenCallArg(expr.Y), expr.Y, true, astutil.FlipComparisonOp(op), 1, true
		}
	}
	if isIntZero(expr.X) {
		if arg, ok2 := lenAliasArg(pass, expr.Y, aliases); ok2 {
			return arg, expr.Y, false, astutil.FlipComparisonOp(op), 0, true
		}
	}
	if isIntOne(expr.X) {
		if arg, ok2 := lenAliasArg(pass, expr.Y, aliases); ok2 {
			return arg, expr.Y, false, astutil.FlipComparisonOp(op), 1, true
		}
	}

	return nil, nil, false, 0, 0, false
}

// resolveFixOp returns the fix operator and comparison verb for a normalized
// comparison where len is on the left side.
// Only the semantically meaningful cases are flagged:
//
//	len(s) == 0  →  s == ""   (empty)
//	len(s) != 0  →  s != ""   (non-empty)
//	len(s) > 0   →  s != ""   (non-empty)
//	len(s) >= 1  →  s != ""   (non-empty)
//	len(s) < 1   →  s == ""   (empty)
//	len(s) <= 0  →  s == ""   (empty)
//
// Tautologies (len(s) >= 0) and contradictions (len(s) < 0) are not flagged.
func resolveFixOp(normalOp token.Token, lit int) (fixOp token.Token, cmpVerb string, ok bool) {
	switch normalOp {
	case token.EQL:
		if lit == 0 {
			return token.EQL, "empty", true
		}
	case token.NEQ:
		if lit == 0 {
			return token.NEQ, "non-empty", true
		}
	case token.GTR:
		if lit == 0 {
			return token.NEQ, "non-empty", true
		}
	case token.GEQ:
		if lit == 1 {
			return token.NEQ, "non-empty", true
		}
		// lit == 0 → len(s) >= 0 is always true; do not flag.
	case token.LSS:
		if lit == 1 {
			return token.EQL, "empty", true
		}
		// lit == 0 → len(s) < 0 is always false; do not flag.
	case token.LEQ:
		if lit == 0 {
			return token.EQL, "empty", true
		}
	}
	return 0, "", false
}

// buildLenStringFix returns a SuggestedFix that rewrites a direct len(s) comparison
// to a direct string comparison using fixOp (== or !=).
func buildLenStringFix(pass *analysis.Pass, expr *ast.BinaryExpr, lenArg ast.Expr, fixOp token.Token) []analysis.SuggestedFix {
	if astutil.HasOverlappingComment(pass.Files, expr.Pos(), expr.End()) {
		return nil
	}
	text := astutil.NodeText(pass.Fset, lenArg)
	if text == "" {
		return nil
	}
	replacement := fmt.Sprintf(`%s %s ""`, text, fixOp.String())
	return []analysis.SuggestedFix{{
		Message: "Replace with direct string comparison",
		TextEdits: []analysis.TextEdit{{
			Pos:     expr.Pos(),
			End:     expr.End(),
			NewText: []byte(replacement),
		}},
	}}
}

// exprTextOr renders node's source text, falling back to fallback when the
// node cannot be printed.
func exprTextOr(pass *analysis.Pass, node ast.Expr, fallback string) string {
	if node == nil {
		return fallback
	}
	if text := astutil.NodeText(pass.Fset, node); text != "" {
		return text
	}
	return fallback
}

func isLenCall(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return false
	}
	ident, ok := call.Fun.(*ast.Ident)
	return ok && ident.Name == "len"
}

func lenCallArg(expr ast.Expr) ast.Expr {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return nil
	}
	return call.Args[0]
}

func isIntZero(expr ast.Expr) bool {
	lit, ok := expr.(*ast.BasicLit)
	return ok && lit.Kind == token.INT && lit.Value == "0"
}

func isIntOne(expr ast.Expr) bool {
	lit, ok := expr.(*ast.BasicLit)
	return ok && lit.Kind == token.INT && lit.Value == "1"
}

func collectLenStringAliases(pass *analysis.Pass) map[types.Object]ast.Expr {
	aliases := make(map[types.Object]ast.Expr)
	for _, file := range pass.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			switch n := node.(type) {
			case *ast.AssignStmt:
				collectLenStringAliasesFromAssignStmt(pass, n, aliases)
			case *ast.ValueSpec:
				collectLenStringAliasesFromValueSpec(pass, n, aliases)
			case *ast.IncDecStmt:
				if ident, ok := n.X.(*ast.Ident); ok {
					delete(aliases, pass.TypesInfo.ObjectOf(ident))
				}
			case *ast.RangeStmt:
				if n.Tok == token.ASSIGN {
					deleteLenStringAliasForExpr(pass, aliases, n.Key)
					deleteLenStringAliasForExpr(pass, aliases, n.Value)
				}
			}
			return true
		})
	}
	return aliases
}

func collectLenStringAliasesFromAssignStmt(pass *analysis.Pass, stmt *ast.AssignStmt, aliases map[types.Object]ast.Expr) {
	for i, lhs := range stmt.Lhs {
		ident, ok := lhs.(*ast.Ident)
		if !ok || ident.Name == "_" {
			continue
		}
		obj := pass.TypesInfo.ObjectOf(ident)
		if obj == nil || !astutil.IsLocalObject(obj) {
			continue
		}

		switch stmt.Tok {
		case token.DEFINE:
			if obj.Pos() != ident.Pos() {
				delete(aliases, obj)
				continue
			}
			rhs, ok := astutil.RhsExprForIndex(stmt.Rhs, i)
			if !ok {
				delete(aliases, obj)
				continue
			}
			if arg, ok := lenStringArg(pass, rhs); ok {
				aliases[obj] = arg
			} else {
				delete(aliases, obj)
			}
		case token.ASSIGN:
			delete(aliases, obj)
		}
	}
}

func collectLenStringAliasesFromValueSpec(pass *analysis.Pass, spec *ast.ValueSpec, aliases map[types.Object]ast.Expr) {
	for i, name := range spec.Names {
		if name.Name == "_" {
			continue
		}
		obj := pass.TypesInfo.ObjectOf(name)
		if obj == nil || !astutil.IsLocalObject(obj) {
			continue
		}
		rhs, ok := astutil.RhsExprForIndex(spec.Values, i)
		if !ok {
			delete(aliases, obj)
			continue
		}
		if arg, ok := lenStringArg(pass, rhs); ok {
			aliases[obj] = arg
		} else {
			delete(aliases, obj)
		}
	}
}

func lenAliasArg(pass *analysis.Pass, expr ast.Expr, aliases map[types.Object]ast.Expr) (ast.Expr, bool) {
	ident, ok := expr.(*ast.Ident)
	if !ok {
		return nil, false
	}
	obj := pass.TypesInfo.ObjectOf(ident)
	if obj == nil {
		return nil, false
	}
	arg, ok := aliases[obj]
	if !ok {
		return nil, false
	}
	return arg, true
}

func lenStringArg(pass *analysis.Pass, expr ast.Expr) (ast.Expr, bool) {
	if !isLenCall(expr) {
		return nil, false
	}
	arg := lenCallArg(expr)
	t := pass.TypesInfo.TypeOf(arg)
	if t == nil {
		return nil, false
	}
	basic, ok := t.Underlying().(*types.Basic)
	if !ok || basic.Kind() != types.String {
		return nil, false
	}
	return arg, true
}

func deleteLenStringAliasForExpr(pass *analysis.Pass, aliases map[types.Object]ast.Expr, expr ast.Expr) {
	ident, ok := expr.(*ast.Ident)
	if !ok {
		return
	}
	delete(aliases, pass.TypesInfo.ObjectOf(ident))
}
