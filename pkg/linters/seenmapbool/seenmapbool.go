// Package seenmapbool implements a Go analysis linter that flags "seen" maps
// declared as map[string]bool (using true as sentinel) that should use
// map[string]struct{} to avoid allocating a bool per entry.
package seenmapbool

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/coverage"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
	"github.com/github/gh-aw/pkg/logger"
)

var pkgLog = logger.New("linters:seenmapbool")

// Analyzer is the seen-map-bool analysis pass.
var Analyzer = analyzerutil.New("seenmapbool", "reports map[string]bool used as a set (values always true) where map[string]struct{} should be used instead", run)

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

	nodeFilter := []ast.Node{
		(*ast.FuncDecl)(nil),
		(*ast.FuncLit)(nil),
	}

	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		var body *ast.BlockStmt
		switch fn := n.(type) {
		case *ast.FuncDecl:
			if fn.Body == nil {
				return
			}
			body = fn.Body
		case *ast.FuncLit:
			if fn.Body == nil {
				return
			}
			body = fn.Body
		}
		pos := pass.Fset.PositionFor(n.Pos(), false)
		if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
			return
		}
		reportSeenMapBoolCandidates(pass, body, noLintIndex)
	})
}

// reportSeenMapBoolCandidates walks a function body and reports map[string]bool variables
// that are only ever assigned the literal true (i.e., used as a set).
func reportSeenMapBoolCandidates(pass *analysis.Pass, body *ast.BlockStmt, noLintIndex nolint.DirectiveIndex) {
	candidates := collectSeenMapCandidates(pass, body)
	if len(candidates) == 0 {
		return
	}

	nonSetMaps := findNonSetMaps(pass, body, candidates)

	for obj, declNode := range candidates {
		if nonSetMaps[obj] {
			continue
		}
		if nolint.HasDirectiveForLinter(pass.Fset.PositionFor(declNode.Pos(), false), noLintIndex, "seenmapbool") {
			continue
		}
		if !coverage.ShouldApply(pass, declNode.Pos(), *hotThreshold) {
			continue
		}
		pkgLog.Printf("flagging map[string]bool used as set: %s", obj.Name())
		pass.ReportRangef(
			declNode,
			"map[string]bool %q used as a set; use map[string]struct{} to avoid allocating a bool per entry",
			obj.Name(),
		)
	}
}

// collectSeenMapCandidates returns a map of local map[string]bool variables
// declared in body (via := or var), stopping at nested function literals.
func collectSeenMapCandidates(pass *analysis.Pass, body *ast.BlockStmt) map[types.Object]ast.Node {
	candidates := make(map[types.Object]ast.Node)
	ast.Inspect(body, func(n ast.Node) bool {
		if n == nil {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false // do not descend into nested closures
		}
		switch stmt := n.(type) {
		case *ast.AssignStmt:
			if stmt.Tok.String() != ":=" {
				return true
			}
			for _, lhs := range stmt.Lhs {
				ident, ok := lhs.(*ast.Ident)
				if !ok || ident.Name == "_" {
					continue
				}
				obj := pass.TypesInfo.ObjectOf(ident)
				if obj == nil {
					continue
				}
				if isMapStringBool(pass.TypesInfo.TypeOf(ident)) {
					candidates[obj] = ident
				}
			}
		case *ast.DeclStmt:
			genDecl, ok := stmt.Decl.(*ast.GenDecl)
			if !ok {
				return true
			}
			for _, spec := range genDecl.Specs {
				valSpec, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, name := range valSpec.Names {
					if name.Name == "_" {
						continue
					}
					obj := pass.TypesInfo.ObjectOf(name)
					if obj == nil {
						continue
					}
					if isMapStringBool(pass.TypesInfo.TypeOf(name)) {
						candidates[obj] = name
					}
				}
			}
		}
		return true
	})
	return candidates
}

// findNonSetMaps returns the subset of candidates that are assigned a value
// other than the literal true (and therefore cannot be treated as sets).
func findNonSetMaps(pass *analysis.Pass, body *ast.BlockStmt, candidates map[types.Object]ast.Node) map[types.Object]bool {
	nonSetMaps := make(map[types.Object]bool)
	ast.Inspect(body, func(n ast.Node) bool {
		if valSpec, ok := n.(*ast.ValueSpec); ok {
			for i, name := range valSpec.Names {
				if i < len(valSpec.Values) {
					markIfNonSetLiteral(pass, name, valSpec.Values[i], candidates, nonSetMaps)
				}
			}
			return true
		}
		assign, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, lhs := range assign.Lhs {
			if ident, ok := lhs.(*ast.Ident); ok {
				if i < len(assign.Rhs) {
					markIfNonSetLiteral(pass, ident, assign.Rhs[i], candidates, nonSetMaps)
				}
				continue
			}
			indexExpr, ok := lhs.(*ast.IndexExpr)
			if !ok {
				continue
			}
			ident, ok := indexExpr.X.(*ast.Ident)
			if !ok {
				continue
			}
			obj := pass.TypesInfo.ObjectOf(ident)
			if obj == nil {
				continue
			}
			if _, isCandidate := candidates[obj]; !isCandidate {
				continue
			}
			if i < len(assign.Rhs) && !isBoolTrue(assign.Rhs[i]) {
				nonSetMaps[obj] = true
			}
		}
		return true
	})
	return nonSetMaps
}

// markIfNonSetLiteral marks the candidate named by ident as a non-set map when
// the value it is initialized with is a composite literal containing an entry
// whose value is not the literal true.
func markIfNonSetLiteral(pass *analysis.Pass, ident *ast.Ident, value ast.Expr, candidates map[types.Object]ast.Node, nonSetMaps map[types.Object]bool) {
	if ident.Name == "_" {
		return
	}
	obj := pass.TypesInfo.ObjectOf(ident)
	if obj == nil {
		return
	}
	if _, isCandidate := candidates[obj]; !isCandidate {
		return
	}
	lit, ok := value.(*ast.CompositeLit)
	if !ok {
		return
	}
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if !isBoolTrue(kv.Value) {
			nonSetMaps[obj] = true
			return
		}
	}
}

// isMapStringBool returns true if t is map[string]bool.
func isMapStringBool(t types.Type) bool {
	if t == nil {
		return false
	}
	m, ok := t.Underlying().(*types.Map)
	if !ok {
		return false
	}
	key, ok := m.Key().(*types.Basic)
	if !ok || key.Kind() != types.String {
		return false
	}
	val, ok := m.Elem().(*types.Basic)
	return ok && val.Kind() == types.Bool
}

// isBoolTrue reports whether expr is the boolean literal true.
func isBoolTrue(expr ast.Expr) bool {
	ident, ok := expr.(*ast.Ident)
	return ok && ident.Name == "true"
}
