// Package tolowerequalfold implements a Go analysis linter that flags
// case-insensitive string comparisons performed via strings.ToLower (or
// strings.ToUpper) combined with == that should instead use strings.EqualFold.
package tolowerequalfold

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/coverage"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
	"github.com/github/gh-aw/pkg/logger"
)

var pkgLog = logger.New("linters:tolowerequalfold")

// Analyzer is the tolower-equalfold analysis pass.
var Analyzer = analyzerutil.New("tolowerequalfold", "reports case-insensitive string comparisons using strings.ToLower/ToUpper that should use strings.EqualFold", run)

// hotThreshold gates findings on coverage data; see coverage package docs.
var hotThreshold *int

func init() {
	hotThreshold = coverage.RegisterHotThresholdFlag(Analyzer)
}

func run(pass *analysis.Pass) (any, error) {
	pkgLog.Printf("analyzing package %s", pass.Pkg.Path())
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}
	caseConvAliases := collectCaseConvAliases(pass)

	nodeFilter := []ast.Node{
		(*ast.BinaryExpr)(nil),
	}

	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		expr, ok := n.(*ast.BinaryExpr)
		if !ok {
			return
		}
		if expr.Op != token.EQL && expr.Op != token.NEQ {
			return
		}

		if filecheck.ShouldSkipFilename(pass.Fset.Position(expr.Pos()).Filename, generatedFiles) {
			return
		}

		if arg, ok := caseConvArg(pass, expr.X); ok && sameOperand(pass, arg, expr.Y) {
			return
		}
		if arg, ok := caseConvArg(pass, expr.Y); ok && sameOperand(pass, expr.X, arg) {
			return
		}
		if arg, ok := caseConvAliasArg(pass, expr.X, caseConvAliases); ok && sameOperand(pass, arg, expr.Y) {
			return
		}
		if arg, ok := caseConvAliasArg(pass, expr.Y, caseConvAliases); ok && sameOperand(pass, expr.X, arg) {
			return
		}

		if isEquivalentToEqualFold(pass, expr, caseConvAliases) {
			if nolint.HasDirectiveForLinter(pass.Fset.PositionFor(expr.Pos(), false), noLintIndex, "tolowerequalfold") {
				return
			}
			if !coverage.ShouldApply(pass, expr.Pos(), *hotThreshold) {
				return
			}
			pkgLog.Printf("flagging case-insensitive comparison at %s", pass.Fset.PositionFor(expr.Pos(), false))
			pass.Report(analysis.Diagnostic{
				Pos:            expr.Pos(),
				End:            expr.End(),
				Message:        "use strings.EqualFold for case-insensitive comparison instead of strings.ToLower/ToUpper with ==",
				SuggestedFixes: buildEqualFoldFix(pass, expr),
			})
		}
	})
}

// buildEqualFoldFix returns a SuggestedFix that rewrites a direct
// strings.ToLower/ToUpper comparison to strings.EqualFold.
// A fix is only emitted when at least one side is a direct caseConvCall (not
// an alias variable), since alias variables may be defined at a different
// source location.
func buildEqualFoldFix(pass *analysis.Pass, expr *ast.BinaryExpr) []analysis.SuggestedFix {
	if astutil.HasOverlappingComment(pass.Files, expr.Pos(), expr.End()) {
		return nil
	}
	leftArg, leftOK := caseConvArg(pass, expr.X)
	rightArg, rightOK := caseConvArg(pass, expr.Y)
	if !leftOK && !rightOK {
		return nil
	}
	arg1 := expr.X
	if leftOK {
		arg1 = leftArg
	}
	arg2 := expr.Y
	if rightOK {
		arg2 = rightArg
	}
	text1 := astutil.NodeText(pass.Fset, arg1)
	text2 := astutil.NodeText(pass.Fset, arg2)
	if text1 == "" || text2 == "" {
		return nil
	}
	equalFoldPkg := "strings"
	if leftOK {
		if pkgName, ok := caseConvPkgName(pass, expr.X); ok {
			equalFoldPkg = pkgName
		}
	} else if pkgName, ok := caseConvPkgName(pass, expr.Y); ok {
		equalFoldPkg = pkgName
	}
	call := fmt.Sprintf("%s.EqualFold(%s, %s)", equalFoldPkg, text1, text2)
	if expr.Op == token.NEQ {
		call = "!" + call
	}
	return []analysis.SuggestedFix{{
		Message: fmt.Sprintf("Replace with %s.EqualFold", equalFoldPkg),
		TextEdits: []analysis.TextEdit{{
			Pos:     expr.Pos(),
			End:     expr.End(),
			NewText: []byte(call),
		}},
	}}
}

// caseConvAliasInfo records the case-conversion function and its argument for
// a local variable that aliases a strings.ToLower/ToUpper call.
type caseConvAliasInfo struct {
	funcName string // "ToLower" or "ToUpper"
	arg      ast.Expr
}

func collectCaseConvAliases(pass *analysis.Pass) map[types.Object]caseConvAliasInfo {
	aliases := make(map[types.Object]caseConvAliasInfo)
	for _, file := range pass.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			switch n := node.(type) {
			case *ast.AssignStmt:
				collectAliasesFromAssignStmt(pass, n, aliases)
			case *ast.ValueSpec:
				collectAliasesFromValueSpec(pass, n, aliases)
			case *ast.IncDecStmt:
				if ident, ok := n.X.(*ast.Ident); ok {
					delete(aliases, pass.TypesInfo.ObjectOf(ident))
				}
			case *ast.RangeStmt:
				if n.Tok == token.ASSIGN {
					deleteAliasForExpr(pass, aliases, n.Key)
					deleteAliasForExpr(pass, aliases, n.Value)
				}
			}
			return true
		})
	}
	return aliases
}

func collectAliasesFromAssignStmt(pass *analysis.Pass, stmt *ast.AssignStmt, aliases map[types.Object]caseConvAliasInfo) {
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
			funcName, arg, ok := caseConvFuncAndArg(pass, rhs)
			if !ok {
				delete(aliases, obj)
				continue
			}
			aliases[obj] = caseConvAliasInfo{funcName: funcName, arg: arg}
		case token.ASSIGN:
			delete(aliases, obj)
		}
	}
}

func collectAliasesFromValueSpec(pass *analysis.Pass, spec *ast.ValueSpec, aliases map[types.Object]caseConvAliasInfo) {
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
		funcName, arg, ok := caseConvFuncAndArg(pass, rhs)
		if !ok {
			delete(aliases, obj)
			continue
		}
		aliases[obj] = caseConvAliasInfo{funcName: funcName, arg: arg}
	}
}

func deleteAliasForExpr(pass *analysis.Pass, aliases map[types.Object]caseConvAliasInfo, expr ast.Expr) {
	ident, ok := expr.(*ast.Ident)
	if !ok {
		return
	}
	delete(aliases, pass.TypesInfo.ObjectOf(ident))
}

// isCaseConvCall reports whether node is a call to strings.ToLower or strings.ToUpper.
func isCaseConvCall(pass *analysis.Pass, n ast.Node) bool {
	_, ok := caseConvArg(pass, n)
	return ok
}

func isCaseConvAlias(pass *analysis.Pass, expr ast.Expr, aliases map[types.Object]caseConvAliasInfo) bool {
	_, ok := caseConvAliasArg(pass, expr, aliases)
	return ok
}

func caseConvAliasArg(pass *analysis.Pass, expr ast.Expr, aliases map[types.Object]caseConvAliasInfo) (ast.Expr, bool) {
	ident, ok := expr.(*ast.Ident)
	if !ok {
		return nil, false
	}
	obj := pass.TypesInfo.ObjectOf(ident)
	if obj == nil {
		return nil, false
	}
	info, ok := aliases[obj]
	if !ok {
		return nil, false
	}
	return info.arg, true
}

// caseConvFuncAndArg returns the function name ("ToLower" or "ToUpper") and
// the argument when n is a direct strings.ToLower/ToUpper call.
func caseConvFuncAndArg(pass *analysis.Pass, n ast.Node) (funcName string, arg ast.Expr, ok bool) {
	call, callOK := n.(*ast.CallExpr)
	if !callOK {
		return "", nil, false
	}
	if len(call.Args) != 1 {
		return "", nil, false
	}
	sel, selOK := call.Fun.(*ast.SelectorExpr)
	if !selOK {
		return "", nil, false
	}
	if !astutil.IsPkgSelector(pass, sel, "strings") {
		return "", nil, false
	}
	if sel.Sel.Name != "ToLower" && sel.Sel.Name != "ToUpper" {
		return "", nil, false
	}
	return sel.Sel.Name, call.Args[0], true
}

// caseConvArg returns the argument when n is strings.ToLower/ToUpper(<arg>).
func caseConvArg(pass *analysis.Pass, n ast.Node) (ast.Expr, bool) {
	_, arg, ok := caseConvFuncAndArg(pass, n)
	return arg, ok
}

// caseConvFuncName returns the function name ("ToLower" or "ToUpper") when n
// is a direct strings.ToLower/ToUpper call.
func caseConvFuncName(pass *analysis.Pass, n ast.Node) (string, bool) {
	name, _, ok := caseConvFuncAndArg(pass, n)
	return name, ok
}

// literalCaseMatchesConv reports whether lit is already in the correct case for
// funcName and uses ASCII-only characters. This conservative guard avoids Unicode
// simple-fold mismatches where ToLower/ToUpper equality and EqualFold differ.
func literalCaseMatchesConv(funcName, lit string) bool {
	if !isASCIIString(lit) {
		return false
	}
	switch funcName {
	case "ToLower":
		return strings.ToLower(lit) == lit
	case "ToUpper":
		return strings.ToUpper(lit) == lit
	}
	return false
}

func isASCIIString(s string) bool {
	for _, b := range []byte(s) {
		if b > 0x7f {
			return false
		}
	}
	return true
}

// isEquivalentToEqualFold reports whether the == or != comparison expr is
// semantically equivalent to a strings.EqualFold rewrite. It returns true only
// when at least one side is a case-conversion call (or alias) and the other
// operand is case-compatible with that conversion.
func isEquivalentToEqualFold(pass *analysis.Pass, expr *ast.BinaryExpr, caseConvAliases map[types.Object]caseConvAliasInfo) bool {
	return (isCaseConvCall(pass, expr.X) && caseConvIsCompatible(pass, expr.X, expr.Y)) ||
		(isCaseConvCall(pass, expr.Y) && caseConvIsCompatible(pass, expr.Y, expr.X)) ||
		(isCaseConvAlias(pass, expr.X, caseConvAliases) && astutil.IsStringLiteral(expr.Y) && caseConvAliasIsCompatible(pass, expr.X, expr.Y, caseConvAliases)) ||
		(isCaseConvAlias(pass, expr.Y, caseConvAliases) && astutil.IsStringLiteral(expr.X) && caseConvAliasIsCompatible(pass, expr.Y, expr.X, caseConvAliases))
}

// caseConvIsCompatible reports whether it is safe to rewrite a comparison
// where convSide is a case-conversion call and otherSide is the other operand.
// Returns true only for ASCII string literals whose case already matches the
// conversion function. All other forms fail closed.
func caseConvIsCompatible(pass *analysis.Pass, convSide ast.Node, otherSide ast.Expr) bool {
	funcName, ok := caseConvFuncName(pass, convSide)
	if !ok {
		return false
	}
	// String-literal operand: the literal must already be in the correct case.
	if lit, ok := astutil.StringLitValue(otherSide); ok {
		return literalCaseMatchesConv(funcName, lit)
	}
	return false
}

// caseConvAliasIsCompatible reports whether it is safe to rewrite a comparison
// where aliasExpr is a case-conversion alias and litExpr is a string literal.
func caseConvAliasIsCompatible(pass *analysis.Pass, aliasExpr ast.Expr, litExpr ast.Expr, aliases map[types.Object]caseConvAliasInfo) bool {
	ident, ok := aliasExpr.(*ast.Ident)
	if !ok {
		return false
	}
	obj := pass.TypesInfo.ObjectOf(ident)
	if obj == nil {
		return false
	}
	info, ok := aliases[obj]
	if !ok {
		return false
	}
	lit, ok := astutil.StringLitValue(litExpr)
	if !ok {
		return false
	}
	return literalCaseMatchesConv(info.funcName, lit)
}

func caseConvPkgName(pass *analysis.Pass, n ast.Node) (string, bool) {
	call, ok := n.(*ast.CallExpr)
	if !ok {
		return "", false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || !astutil.IsPkgSelector(pass, sel, "strings") {
		return "", false
	}
	if sel.Sel.Name != "ToLower" && sel.Sel.Name != "ToUpper" {
		return "", false
	}
	pkgName := astutil.NodeText(pass.Fset, sel.X)
	return pkgName, pkgName != ""
}

func sameOperand(pass *analysis.Pass, left ast.Expr, right ast.Expr) bool {
	leftIdent, leftOK := left.(*ast.Ident)
	rightIdent, rightOK := right.(*ast.Ident)
	if !leftOK || !rightOK {
		return false
	}

	leftObj := pass.TypesInfo.ObjectOf(leftIdent)
	rightObj := pass.TypesInfo.ObjectOf(rightIdent)
	return leftObj != nil && rightObj != nil && leftObj == rightObj
}
