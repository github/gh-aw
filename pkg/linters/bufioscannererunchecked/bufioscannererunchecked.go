// Package bufioscannererunchecked implements a Go analysis linter that flags
// bufio.Scanner usage where Err() is not called after Scan() loop completes,
// potentially silencing read errors.
package bufioscannererunchecked

import (
	"fmt"
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
	"github.com/github/gh-aw/pkg/logger"
)

var pkgLog = logger.New("linters:bufioscannererunchecked")

// Analyzer is the bufio-scanner-err-unchecked analysis pass.
var Analyzer = analyzerutil.New("bufioscannererunchecked", "reports bufio.Scanner usage where Err() is not checked after Scan() loop completes, potentially silencing read errors", run)

func run(pass *analysis.Pass) (any, error) {
	pkgLog.Printf("analyzing package %s", pass.Pkg.Path())
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	nodeFilter := []ast.Node{(*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)}
	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		analyzeFuncBody(pass, n, generatedFiles, noLintIndex)
	})
}

// analyzeFuncBody analyzes a function body for unchecked scanner errors.
func analyzeFuncBody(pass *analysis.Pass, n ast.Node, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) {
	var body *ast.BlockStmt
	switch fn := n.(type) {
	case *ast.FuncDecl:
		body = fn.Body
	case *ast.FuncLit:
		body = fn.Body
	default:
		return
	}

	if body == nil {
		return
	}

	// Find all scanner variables and their assignments
	scanners := findScannerVariables(pass, body)
	if len(scanners) == 0 {
		return
	}

	// For each statement in the function body, check for scanner loops
	analyzeBlockStatements(pass, body.List, scanners, generatedFiles, noLintIndex)
}

// ScannerInfo holds information about a scanner variable
type ScannerInfo struct {
	Name   string           // Variable name
	ObjSet map[types.Object]bool // All versions of this object (due to shadowing)
}

// findScannerVariables finds all bufio.Scanner variables in the given block
func findScannerVariables(pass *analysis.Pass, block *ast.BlockStmt) map[string]*ScannerInfo {
	scanners := make(map[string]*ScannerInfo)

	ast.Inspect(block, func(n ast.Node) bool {
		// Look for assignments: scanner := bufio.NewScanner(...)
		assign, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}

		for i, rhs := range assign.Rhs {
			if i >= len(assign.Lhs) {
				continue
			}

			// Check if this is a bufio.NewScanner call
			if !isNewScannerCall(pass, rhs) {
				continue
			}

			// Get the identifier on the LHS
			ident, ok := assign.Lhs[i].(*ast.Ident)
			if !ok || ident.Name == "_" {
				continue
			}

			obj := pass.TypesInfo.Defs[ident]
			if obj == nil {
				continue
			}

			if _, exists := scanners[ident.Name]; !exists {
				scanners[ident.Name] = &ScannerInfo{
					Name:   ident.Name,
					ObjSet: make(map[types.Object]bool),
				}
			}
			scanners[ident.Name].ObjSet[obj] = true
		}

		return true
	})

	return scanners
}

// isNewScannerCall checks if the expression is a bufio.NewScanner(...) call
func isNewScannerCall(pass *analysis.Pass, expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}

	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}

	if sel.Sel.Name != "NewScanner" {
		return false
	}

	return astutil.IsPkgSelector(pass, sel, "bufio")
}

// analyzeBlockStatements analyzes a list of statements for scanner loops
func analyzeBlockStatements(pass *analysis.Pass, stmts []ast.Stmt, scanners map[string]*ScannerInfo, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) {
	for i, stmt := range stmts {
		if stmt == nil {
			continue
		}

		forStmt, ok := stmt.(*ast.ForStmt)
		if !ok {
			continue
		}

		// Check if this for loop uses scanner.Scan()
		scannerVar := findScannerInForLoop(pass, forStmt, scanners)
		if scannerVar == "" {
			continue
		}

		pos := pass.Fset.PositionFor(forStmt.Pos(), false)
		if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
			continue
		}
		if nolint.HasDirectiveForLinter(pos, noLintIndex, "bufioscannererunchecked") {
			continue
		}

		// Check if scanner.Err() is called after the loop in the same block
		if hasScannerErrCheck(pass, stmts[i+1:], scannerVar, scanners[scannerVar]) {
			continue
		}

		pkgLog.Printf("flagging unchecked scanner error at %s", pos)
		pass.Report(analysis.Diagnostic{
			Pos:     forStmt.Pos(),
			End:     forStmt.End(),
			Message: fmt.Sprintf("Scanner loop does not check Err() after completion; read errors may be silently dropped"),
		})
	}
}

// findScannerInForLoop checks if a for loop uses scanner.Scan() from the scanners map
func findScannerInForLoop(pass *analysis.Pass, forStmt *ast.ForStmt, scanners map[string]*ScannerInfo) string {
	// Check the condition for scanner.Scan()
	if usesScanner(pass, forStmt.Cond, scanners) != "" {
		return usesScanner(pass, forStmt.Cond, scanners)
	}

	// Check the body for scanner.Scan()
	if forStmt.Body != nil {
		for _, stmt := range forStmt.Body.List {
			if name := usesScanner(pass, stmt, scanners); name != "" {
				return name
			}
		}
	}

	return ""
}

// usesScanner checks if an expression uses any of the scanner variables
func usesScanner(pass *analysis.Pass, expr ast.Node, scanners map[string]*ScannerInfo) string {
	var result string
	ast.Inspect(expr, func(n ast.Node) bool {
		if result != "" {
			return false
		}

		// Look for scanner.Scan() calls
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		if sel.Sel.Name != "Scan" {
			return true
		}

		ident, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}

		scannerInfo, exists := scanners[ident.Name]
		if !exists {
			return true
		}

		obj := pass.TypesInfo.Uses[ident]
		if obj == nil {
			return true
		}

		if scannerInfo.ObjSet[obj] {
			result = ident.Name
			return false
		}

		return true
	})

	return result
}

// hasScannerErrCheck checks if the following statements call scanner.Err()
func hasScannerErrCheck(pass *analysis.Pass, stmts []ast.Stmt, scannerName string, scannerInfo *ScannerInfo) bool {
	for _, stmt := range stmts {
		if stmt == nil {
			continue
		}

		if hasErrCall(pass, stmt, scannerName, scannerInfo) {
			return true
		}
	}

	return false
}

// hasErrCall checks if a statement contains a scanner.Err() call
func hasErrCall(pass *analysis.Pass, stmt ast.Node, scannerName string, scannerInfo *ScannerInfo) bool {
	found := false
	ast.Inspect(stmt, func(n ast.Node) bool {
		if found {
			return false
		}

		// Check if this is a return statement that uses scanner.Err()
		// This handles cases like: return scanner.Err()
		retStmt, ok := n.(*ast.ReturnStmt)
		if ok {
			for _, val := range retStmt.Results {
				if hasErrCallInExpr(pass, val, scannerName, scannerInfo) {
					found = true
					return false
				}
			}
		}

		// Look for scanner.Err() calls
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		if sel.Sel.Name != "Err" {
			return true
		}

		ident, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}

		if ident.Name != scannerName {
			return true
		}

		obj := pass.TypesInfo.Uses[ident]
		if obj == nil {
			return true
		}

		if scannerInfo.ObjSet[obj] {
			found = true
			return false
		}

		return true
	})

	return found
}

// hasErrCallInExpr checks if an expression contains a scanner.Err() call
func hasErrCallInExpr(pass *analysis.Pass, expr ast.Expr, scannerName string, scannerInfo *ScannerInfo) bool {
	found := false
	ast.Inspect(expr, func(n ast.Node) bool {
		if found {
			return false
		}

		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		if sel.Sel.Name != "Err" {
			return true
		}

		ident, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}

		if ident.Name != scannerName {
			return true
		}

		obj := pass.TypesInfo.Uses[ident]
		if obj == nil {
			return true
		}

		if scannerInfo.ObjSet[obj] {
			found = true
			return false
		}

		return true
	})

	return found
}
