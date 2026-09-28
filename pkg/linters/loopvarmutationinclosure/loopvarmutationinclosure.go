// Package loopvarmutationinclosure implements a Go analysis linter that flags
// loop variables captured in closures (goroutines, anonymous functions) without
// proper scoping, which can cause unintended mutations and data races.
package loopvarmutationinclosure

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
	"github.com/github/gh-aw/pkg/logger"
)

var pkgLog = logger.New("linters:loopvarmutationinclosure")

// Analyzer is the loop-var-mutation-in-closure analysis pass.
var Analyzer = analyzerutil.New("loopvarmutationinclosure",
	"reports loop variables captured in closures that may be mutated before closure execution",
	run)

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

	// Check both ForStmt and RangeStmt
	for cur := range insp.Root().Preorder((*ast.ForStmt)(nil), (*ast.RangeStmt)(nil)) {
		checkLoopNode(pass, cur, generatedFiles, noLintIndex)
	}

	return nil, nil
}

// checkLoopNode analyzes a single loop (for or range) for loop variable captures
func checkLoopNode(pass *analysis.Pass, cur inspector.Cursor, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) {
	// Extract loop variables and their definitions
	var loopBody *ast.BlockStmt
	var loopVarDefs map[string]types.Object // maps var name to its Object

	switch stmt := cur.Node().(type) {
	case *ast.ForStmt:
		loopBody = stmt.Body
		loopVarDefs = extractForLoopVarDefs(pass, stmt)
	case *ast.RangeStmt:
		loopBody = stmt.Body
		loopVarDefs = extractRangeLoopVarDefs(pass, stmt)
	default:
		return
	}

	if loopBody == nil || len(loopVarDefs) == 0 {
		return
	}

	// Walk the loop body to find function literals that capture loop variables
	walkForCaptures(pass, loopBody, loopVarDefs, generatedFiles, noLintIndex)
}

// extractForLoopVarDefs extracts variable definitions from a for loop
func extractForLoopVarDefs(pass *analysis.Pass, stmt *ast.ForStmt) map[string]types.Object {
	vars := make(map[string]types.Object)

	if stmt.Init == nil {
		return vars
	}

	// Handle: for i := 0; ...; ...
	if assign, ok := stmt.Init.(*ast.AssignStmt); ok && assign.Tok.String() == ":=" {
		for _, lhs := range assign.Lhs {
			if ident, ok := lhs.(*ast.Ident); ok && ident.Name != "_" {
				if obj := pass.TypesInfo.Defs[ident]; obj != nil {
					vars[ident.Name] = obj
				}
			}
		}
	}

	return vars
}

// extractRangeLoopVarDefs extracts variable definitions from a range loop
func extractRangeLoopVarDefs(pass *analysis.Pass, stmt *ast.RangeStmt) map[string]types.Object {
	vars := make(map[string]types.Object)

	// Handle: for i, item := range ...
	if stmt.Key != nil {
		if ident, ok := stmt.Key.(*ast.Ident); ok && ident.Name != "_" {
			if obj := pass.TypesInfo.Defs[ident]; obj != nil {
				vars[ident.Name] = obj
			}
		}
	}

	if stmt.Value != nil {
		if ident, ok := stmt.Value.(*ast.Ident); ok && ident.Name != "_" {
			if obj := pass.TypesInfo.Defs[ident]; obj != nil {
				vars[ident.Name] = obj
			}
		}
	}

	return vars
}

// walkForCaptures walks the loop body to find function literals and check for captures
func walkForCaptures(pass *analysis.Pass, loopBody *ast.BlockStmt, loopVarDefs map[string]types.Object,
	generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) {

	ast.Inspect(loopBody, func(node ast.Node) bool {
		funcLit, ok := node.(*ast.FuncLit)
		if !ok {
			return true
		}

		checkFuncLitForCaptures(pass, funcLit, loopVarDefs, generatedFiles, noLintIndex)

		// Don't recurse into nested function literals to avoid double-checking
		return false
	})
}

// checkFuncLitForCaptures checks if a function literal captures any loop variables improperly
func checkFuncLitForCaptures(pass *analysis.Pass, funcLit *ast.FuncLit, loopVarDefs map[string]types.Object,
	generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) {

	pos := pass.Fset.PositionFor(funcLit.Pos(), false)
	if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
		return
	}
	if nolint.HasDirectiveForLinter(pos, noLintIndex, "loopvarmutationinclosure") {
		return
	}

	// Get parameter names (safe captures via parameters)
	paramNames := extractFuncParamNames(funcLit)

	// Check if loop variables are shadowed in this function
	shadowedVars := extractFuncShadowedVars(pass, funcLit, loopVarDefs)

	// Walk the function body to find uses of loop variables
	if funcLit.Body != nil {
		walkFuncBodyForLoopVarUses(pass, funcLit.Body, loopVarDefs, paramNames, shadowedVars)
	}
}

// extractFuncParamNames returns the set of parameter names in a function
func extractFuncParamNames(funcLit *ast.FuncLit) map[string]struct{} {
	names := make(map[string]struct{})
	if funcLit.Type == nil || funcLit.Type.Params == nil {
		return names
	}

	for _, field := range funcLit.Type.Params.List {
		for _, ident := range field.Names {
			if ident.Name != "_" {
				names[ident.Name] = struct{}{}
			}
		}
	}
	return names
}

// extractFuncShadowedVars returns loop variables that are shadowed (redefined) in the function
func extractFuncShadowedVars(pass *analysis.Pass, funcLit *ast.FuncLit, loopVarDefs map[string]types.Object) map[string]struct{} {
	shadowed := make(map[string]struct{})

	if pass.TypesInfo == nil || funcLit.Body == nil {
		return shadowed
	}

	// Look for assignments that shadow loop variables: x := x or x := ...
	ast.Inspect(funcLit.Body, func(node ast.Node) bool {
		switch stmt := node.(type) {
		case *ast.AssignStmt:
			if stmt.Tok.String() != ":=" {
				return true
			}

			for _, lhs := range stmt.Lhs {
				if ident, ok := lhs.(*ast.Ident); ok {
					if loopObj, isLoopVar := loopVarDefs[ident.Name]; isLoopVar {
						// This statement defines a new binding for the loop variable name
						// Check if it's a shadowing assignment (x := x)
						if len(stmt.Rhs) == 1 {
							if rhsIdent, ok := stmt.Rhs[0].(*ast.Ident); ok {
								// This is x := x, which is a proper shadowing
								if rhsIdent.Name == ident.Name {
									// Verify that the RHS refers to the loop variable
									if rhsObj := pass.TypesInfo.Uses[rhsIdent]; rhsObj == loopObj {
										shadowed[ident.Name] = struct{}{}
									}
								}
							}
						}
					}
				}
			}

		case *ast.DeclStmt:
			if genDecl, ok := stmt.Decl.(*ast.GenDecl); ok {
				for _, spec := range genDecl.Specs {
					if valSpec, ok := spec.(*ast.ValueSpec); ok {
						for _, name := range valSpec.Names {
							if _, isLoopVar := loopVarDefs[name.Name]; isLoopVar {
								shadowed[name.Name] = struct{}{}
							}
						}
					}
				}
			}
		}
		return true
	})

	return shadowed
}

// walkFuncBodyForLoopVarUses walks the function body and reports uses of loop variables
func walkFuncBodyForLoopVarUses(pass *analysis.Pass, funcBody *ast.BlockStmt, loopVarDefs map[string]types.Object,
	paramNames, shadowedVars map[string]struct{}) {

	ast.Inspect(funcBody, func(node ast.Node) bool {
		ident, ok := node.(*ast.Ident)
		if !ok {
			return true
		}

		// Get the object this identifier refers to
		if pass.TypesInfo == nil {
			return true
		}

		usedObj := pass.TypesInfo.Uses[ident]
		if usedObj == nil {
			return true
		}

		// Check if this identifier uses a loop variable
		for varName, loopVarObj := range loopVarDefs {
			if usedObj == loopVarObj {
				// Found a use of loop variable

				// Check if it's safe (passed as parameter or shadowed)
				if _, isParam := paramNames[varName]; isParam {
					// Safe: passed as parameter
					return true
				}
				if _, isShadowed := shadowedVars[varName]; isShadowed {
					// Safe: shadowed in function
					return true
				}

				// Unsafe: loop variable captured without proper scoping
				pass.ReportRangef(ident,
					"loop variable %s captured in closure; a shadowed copy (e.g., %[1]s := %[1]s) or parameter passing (e.g., func(%[1]s)) is required to prevent unintended mutations",
					varName)
				return true
			}
		}

		return true
	})
}
