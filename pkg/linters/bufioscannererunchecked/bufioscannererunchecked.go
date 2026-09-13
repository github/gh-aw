// Package bufioscannererunchecked implements a Go analysis linter that flags
// bufio.Scanner usage where Err() is not called after Scan() loop completes,
// potentially silencing read errors.
package bufioscannererunchecked

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
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

	ast.Inspect(body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.BlockStmt:
			analyzeBlockStatements(pass, node.List, generatedFiles, noLintIndex)
		case *ast.CaseClause:
			analyzeBlockStatements(pass, node.Body, generatedFiles, noLintIndex)
		case *ast.CommClause:
			analyzeBlockStatements(pass, node.Body, generatedFiles, noLintIndex)
		}
		return true
	})
}

// receiverRef is the comparable identity used to match Scan and Err receivers.
type receiverRef struct {
	obj  types.Object
	path string
}

// analyzeBlockStatements analyzes a list of statements for scanner loops
func analyzeBlockStatements(pass *analysis.Pass, stmts []ast.Stmt, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) {
	for i, stmt := range stmts {
		if stmt == nil {
			continue
		}

		forStmt, ok := stmt.(*ast.ForStmt)
		if !ok {
			continue
		}

		// Check if this for loop uses scanner.Scan()
		scanner := findScannerInForLoop(pass, forStmt)
		if scanner == nil {
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
		if hasScannerErrCheck(pass, stmts[i+1:], scanner) {
			continue
		}

		pkgLog.Printf("flagging unchecked scanner error at %s", pos)
		pass.Report(analysis.Diagnostic{
			Pos:     forStmt.Pos(),
			End:     forStmt.End(),
			Message: "Scanner loop does not check Err() after completion; read errors may be silently dropped",
		})
	}
}

// findScannerInForLoop checks if a for loop uses bufio.Scanner.Scan().
func findScannerInForLoop(pass *analysis.Pass, forStmt *ast.ForStmt) *receiverRef {
	if scanner := scannerMethodReceiver(pass, forStmt.Cond, "Scan"); scanner != nil {
		return scanner
	}
	if forStmt.Body == nil {
		return nil
	}
	return scannerMethodReceiverSkippingNestedLoops(pass, forStmt.Body, "Scan")
}

// scannerMethodReceiver returns the receiver of a bufio.Scanner method call.
func scannerMethodReceiver(pass *analysis.Pass, expr ast.Node, methodName string) *receiverRef {
	if expr == nil {
		return nil
	}
	var result *receiverRef
	ast.Inspect(expr, func(n ast.Node) bool {
		if result != nil {
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

		if sel.Sel.Name != methodName {
			return true
		}

		if !isBufioScanner(pass, sel.X) {
			return true
		}
		if receiver := receiverKey(pass, sel.X); receiver != nil {
			result = receiver
			return false
		}

		return true
	})

	return result
}

// scannerMethodReceiverSkippingNestedLoops finds scanner calls in loop bodies
// without attributing scanner loops nested inside another loop to the outer loop.
func scannerMethodReceiverSkippingNestedLoops(pass *analysis.Pass, expr ast.Node, methodName string) *receiverRef {
	if expr == nil {
		return nil
	}
	var result *receiverRef
	ast.Inspect(expr, func(n ast.Node) bool {
		if result != nil {
			return false
		}
		if n != expr {
			switch n.(type) {
			case *ast.ForStmt, *ast.RangeStmt:
				return false
			}
		}

		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		if sel.Sel.Name != methodName {
			return true
		}

		if !isBufioScanner(pass, sel.X) {
			return true
		}
		if receiver := receiverKey(pass, sel.X); receiver != nil {
			result = receiver
			return false
		}

		return true
	})

	return result
}

// hasScannerErrCheck checks if the following statements call scanner.Err()
func hasScannerErrCheck(pass *analysis.Pass, stmts []ast.Stmt, scanner *receiverRef) bool {
	for _, stmt := range stmts {
		if stmt == nil {
			continue
		}

		if scannerMethodReceiverMatches(pass, stmt, "Err", scanner) {
			return true
		}
	}

	return false
}

// scannerMethodReceiverMatches checks if a node contains a scanner method call on the same receiver.
func scannerMethodReceiverMatches(pass *analysis.Pass, stmt ast.Node, methodName string, scanner *receiverRef) bool {
	if scanner == nil {
		return false
	}
	found := false
	ast.Inspect(stmt, func(n ast.Node) bool {
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

		if sel.Sel.Name != methodName {
			return true
		}

		if !isBufioScanner(pass, sel.X) {
			return true
		}
		if sameReceiver(receiverKey(pass, sel.X), scanner) {
			found = true
			return false
		}

		return true
	})

	return found
}

// isBufioScanner reports whether expr has type bufio.Scanner or *bufio.Scanner.
func isBufioScanner(pass *analysis.Pass, expr ast.Expr) bool {
	t := pass.TypesInfo.TypeOf(expr)
	if t == nil {
		return false
	}
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}
	named, ok := t.(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj != nil && obj.Name() == "Scanner" && obj.Pkg() != nil && obj.Pkg().Path() == "bufio"
}

// receiverKey identifies a scanner receiver by its base object plus selector path.
// Identifiers use type-checker objects to distinguish shadowed variables; selectors
// append field names so calls like holder.scanner.Scan() match holder.scanner.Err().
// Receivers without object identity are skipped instead of matched textually.
func receiverKey(pass *analysis.Pass, expr ast.Expr) *receiverRef {
	switch e := expr.(type) {
	case *ast.Ident:
		obj := objectForIdent(pass, e)
		if obj == nil {
			return nil
		}
		return &receiverRef{obj: obj}
	case *ast.SelectorExpr:
		base := receiverKey(pass, e.X)
		if base == nil {
			return nil
		}
		key := *base
		if key.path == "" {
			key.path = e.Sel.Name
		} else {
			key.path += "." + e.Sel.Name
		}
		return &key
	default:
		return nil
	}
}

// objectForIdent returns the type-checker object for an identifier use or definition.
func objectForIdent(pass *analysis.Pass, ident *ast.Ident) types.Object {
	if obj := pass.TypesInfo.Uses[ident]; obj != nil {
		return obj
	}
	return pass.TypesInfo.Defs[ident]
}

// sameReceiver reports whether two scanner method calls refer to the same receiver key.
func sameReceiver(a, b *receiverRef) bool {
	if a == nil || b == nil {
		return false
	}
	return a.obj == b.obj && a.path == b.path
}
