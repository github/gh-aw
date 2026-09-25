// Package loopindexaddresstaken implements a Go analysis linter that flags
// taking the address of loop index variables in for-range loops when the
// address is then used in goroutines or deferred functions, where the loop
// variable may be reassigned before the goroutine runs or the deferred function
// executes.
//
// Loop variables in for-range loops are reused across iterations, so taking
// their address and using it in a goroutine or deferred function can lead to
// subtle bugs where all captures share the same address, causing the captured
// value to change unexpectedly.
//
// The linter flags patterns like:
//
//	for i, v := range items {
//	    go func() {
//	        fmt.Println(&i)  // BUG: address of loop var used in goroutine
//	    }()
//	}
//
// And:
//
//	for i := range items {
//	    defer func() {
//	        fmt.Println(&i)  // BUG: address of loop var used in defer
//	    }()
//	}
//
// It does NOT flag:
// - Capturing the loop variable by value: go func(i int) { ... }(i)
// - Just using the variable (not its address): go func() { ... fmt.Println(i) ... }()
// - Breaking out of the loop after starting the goroutine (single iteration)
package loopindexaddresstaken

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
	"github.com/github/gh-aw/pkg/logger"
)

var pkgLog = logger.New("linters:loopindexaddresstaken")

// Analyzer is the loop-index-address-taken analysis pass.
var Analyzer = analyzerutil.New("loopindexaddresstaken", "reports taking the address of loop index variables in for-range loops that are then used in goroutines or deferred functions", run)

func run(pass *analysis.Pass) (any, error) {
	insp, err := astutil.Inspector(pass)
	if err != nil {
		return nil, err
	}

	pkgLog.Printf("analyzing package %s", pass.Pkg.Path())

	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	// Find all range statements and analyze their bodies
	for cur := range insp.Root().Preorder((*ast.RangeStmt)(nil)) {
		rangeStmt, ok := cur.Node().(*ast.RangeStmt)
		if !ok {
			continue
		}

		// Get the loop variables
		indexVar := getLoopVar(rangeStmt.Key)
	
		if indexVar == nil {
			continue
		}

		pos := pass.Fset.PositionFor(rangeStmt.Pos(), false)
		if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
			continue
		}

		// Analyze the loop body for address-of operations on the loop index variable
		// used in goroutines or deferred functions
		checkLoopBody(pass, rangeStmt.Body, []*ast.Ident{indexVar}, noLintIndex)
	}

	return nil, nil
}

// getLoopVar extracts the identifier from a loop variable expression.
// For range loops, Key can be an Ident or nil.
func getLoopVar(key ast.Expr) *ast.Ident {
	if key == nil {
		return nil
	}
	ident, ok := key.(*ast.Ident)
	if !ok || ident.Name == "_" {
		return nil
	}
	return ident
}

// checkLoopBody traverses the loop body looking for goroutines and deferred
// functions that reference the address of the loop variables.
func checkLoopBody(pass *analysis.Pass, body *ast.BlockStmt, loopVars []*ast.Ident, noLintIndex nolint.DirectiveIndex) {
	if body == nil {
		return
	}

	// Walk through statements in the loop body
	ast.Inspect(body, func(n ast.Node) bool {
		// Check for goroutines (go statements)
		if goStmt, ok := n.(*ast.GoStmt); ok {
			checkGoroutine(pass, goStmt, loopVars, noLintIndex)
			return true // Still inspect nested structures
		}

		// Check for deferred functions
		if deferStmt, ok := n.(*ast.DeferStmt); ok {
			checkDeferStmt(pass, deferStmt, loopVars, noLintIndex)
			return true // Still inspect nested structures
		}

		return true
	})
}

// checkGoroutine analyzes a goroutine to see if it takes the address of any loopVar.
func checkGoroutine(pass *analysis.Pass, goStmt *ast.GoStmt, loopVars []*ast.Ident, noLintIndex nolint.DirectiveIndex) {
	call := goStmt.Call
	if call == nil {
		return
	}

	// Unwrap parentheses
	fun := unwrapParens(call.Fun)
	funcLit, ok := fun.(*ast.FuncLit)
	if !ok {
		return
	}

	// Check each loop variable
	for _, loopVar := range loopVars {
		if loopVar == nil {
			continue
		}

		// Check if this function has loopVar as a parameter (captured by value)
		if hasLoopVarAsParam(funcLit, loopVar) {
			continue
		}

		// Find and report the address-of expressions for this loopVar
		reportAddressesInBody(pass, funcLit.Body, loopVar, "goroutine", noLintIndex)
	}
}

// checkDeferStmt analyzes a deferred function to see if it takes the address of any loopVar.
func checkDeferStmt(pass *analysis.Pass, deferStmt *ast.DeferStmt, loopVars []*ast.Ident, noLintIndex nolint.DirectiveIndex) {
	call := deferStmt.Call
	if call == nil {
		return
	}

	// Unwrap parentheses
	fun := unwrapParens(call.Fun)
	funcLit, ok := fun.(*ast.FuncLit)
	if !ok {
		return
	}

	// Check each loop variable
	for _, loopVar := range loopVars {
		if loopVar == nil {
			continue
		}

		// Check if this function has loopVar as a parameter (captured by value)
		if hasLoopVarAsParam(funcLit, loopVar) {
			continue
		}

		// Find and report the address-of expressions for this loopVar
		reportAddressesInBody(pass, funcLit.Body, loopVar, "deferred function", noLintIndex)
	}
}

// hasLoopVarAsParam returns true if funcLit has loopVar as a parameter.
// This means it's captured by value, not by reference through address-of.
func hasLoopVarAsParam(funcLit *ast.FuncLit, loopVar *ast.Ident) bool {
	if funcLit.Type.Params == nil {
		return false
	}
	for _, field := range funcLit.Type.Params.List {
		for _, name := range field.Names {
			if name.Name == loopVar.Name {
				return true
			}
		}
	}
	return false
}

// reportAddressesInBody finds and reports each address-of expression for loopVar
// in the function body.
func reportAddressesInBody(pass *analysis.Pass, body *ast.BlockStmt, loopVar *ast.Ident, context string, noLintIndex nolint.DirectiveIndex) {
	if body == nil {
		return
	}

	ast.Inspect(body, func(n ast.Node) bool {
		// Skip nested function literals — their scope is separate
		if _, ok := n.(*ast.FuncLit); ok && n != body {
			return false
		}

		// Check for address-of operator
		unary, ok := n.(*ast.UnaryExpr)
		if !ok || unary.Op.String() != "&" {
			return true
		}

		// Check if the operand is the loop variable
		operandIdent, ok := unary.X.(*ast.Ident)
		if !ok || operandIdent.Name != loopVar.Name {
			return true
		}

		// Found it — report at the position of the address-of expression
		pos := pass.Fset.PositionFor(unary.Pos(), false)
		if nolint.HasDirectiveForLinter(pos, noLintIndex, "loopindexaddresstaken") {
			return true
		}

		pkgLog.Printf("flagging address-of loop var in %s at %s", context, pos)
		contextStr := context
		if contextStr == "deferred function" {
			contextStr = "a deferred function; the variable may be reassigned before the function executes"
		} else {
			contextStr = "a goroutine; the variable may be reassigned before the goroutine runs"
		}
		pass.ReportRangef(unary, "taking the address of loop variable %s in %s", loopVar.Name, contextStr)

		return true
	})
}

// unwrapParens removes any surrounding *ast.ParenExpr nodes, returning the
// innermost non-parenthesised expression.
func unwrapParens(expr ast.Expr) ast.Expr {
	for {
		p, ok := expr.(*ast.ParenExpr)
		if !ok {
			return expr
		}
		expr = p.X
	}
}
