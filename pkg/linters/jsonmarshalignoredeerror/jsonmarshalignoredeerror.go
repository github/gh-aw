// Package jsonmarshalignoredeerror implements a Go analysis linter that flags
// json.Marshal and json.Unmarshal calls where the error return is discarded.
package jsonmarshalignoredeerror

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
	"github.com/github/gh-aw/pkg/logger"
)

var pkgLog = logger.New("linters:jsonmarshalignoredeerror")

// Analyzer is the json-marshal-ignored-error analysis pass.
var Analyzer = analyzerutil.New("jsonmarshalignoredeerror", "reports json.Marshal and json.Unmarshal calls where the error return is discarded", run)

func run(pass *analysis.Pass) (any, error) {
	pkgLog.Printf("analyzing package %s", pass.Pkg.Path())
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}
	nodeFilter := []ast.Node{(*ast.AssignStmt)(nil), (*ast.ExprStmt)(nil)}
	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		switch stmt := n.(type) {
		case *ast.AssignStmt:
			position := pass.Fset.PositionFor(stmt.Pos(), false)
			if filecheck.ShouldSkipFilename(position.Filename, generatedFiles) {
				return
			}
			checkDiscardedJSONAssign(pass, stmt, noLintIndex)
		case *ast.ExprStmt:
			position := pass.Fset.PositionFor(stmt.Pos(), false)
			if filecheck.ShouldSkipFilename(position.Filename, generatedFiles) {
				return
			}
			checkDiscardedJSONExpr(pass, stmt, noLintIndex)
		}
	})
}

func checkDiscardedJSONAssign(pass *analysis.Pass, assign *ast.AssignStmt, noLintIndex nolint.DirectiveIndex) {
	// Pattern: val, _ := json.Marshal(x)  — 2 lhs, 1 rhs, Lhs[1] is blank
	if call, pkgPath, funcName, ok := astutil.MatchDiscardedErrorCall(pass, assign); ok &&
		pkgPath == "encoding/json" && funcName == "Marshal" {
		reportDiscardedJSONCall(pass, call, noLintIndex, "error return from json.Marshal is discarded; marshal failures produce nil bytes silently")
	}

	// Pattern: _ = json.Unmarshal(data, &v)  — 1 lhs, 1 rhs, Lhs[0] is blank
	if len(assign.Lhs) == 1 && len(assign.Rhs) == 1 {
		var lhs ast.Expr
		for _, expr := range assign.Lhs {
			lhs = expr
			break
		}
		blank, ok := lhs.(*ast.Ident)
		if ok && blank.Name == "_" {
			var rhs ast.Expr
			for _, expr := range assign.Rhs {
				rhs = expr
				break
			}
			call, ok := rhs.(*ast.CallExpr)
			if ok && isJSONFunc(pass, call, "Unmarshal") {
				reportDiscardedJSONCall(pass, call, noLintIndex, "error return from json.Unmarshal is discarded; unmarshal failures leave the target value in a partial state")
			}
		}
	}
}

func checkDiscardedJSONExpr(pass *analysis.Pass, stmt *ast.ExprStmt, noLintIndex nolint.DirectiveIndex) {
	call, ok := stmt.X.(*ast.CallExpr)
	if !ok {
		return
	}
	if isJSONFunc(pass, call, "Marshal") {
		reportDiscardedJSONCall(pass, call, noLintIndex, "error return from json.Marshal is discarded; marshal failures produce nil bytes silently")
		return
	}
	if isJSONFunc(pass, call, "Unmarshal") {
		reportDiscardedJSONCall(pass, call, noLintIndex, "error return from json.Unmarshal is discarded; unmarshal failures leave the target value in a partial state")
	}
}

func reportDiscardedJSONCall(pass *analysis.Pass, call *ast.CallExpr, noLintIndex nolint.DirectiveIndex, message string) {
	position := pass.Fset.PositionFor(call.Pos(), false)
	if nolint.HasDirectiveForLinter(position, noLintIndex, "jsonmarshalignoredeerror") {
		return
	}
	pkgLog.Printf("flagging discarded json error at %s:%d", position.Filename, position.Line)
	pass.ReportRangef(call, "%s", message)
}

func isJSONFunc(pass *analysis.Pass, call *ast.CallExpr, name string) bool {
	pkgPath, funcName, ok := astutil.PackageCall(pass, call)
	return ok && pkgPath == "encoding/json" && funcName == name
}
